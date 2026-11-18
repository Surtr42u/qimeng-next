package media.qimeng.next.data

import kotlinx.coroutines.currentCoroutineContext
import kotlinx.coroutines.ensureActive
import media.qimeng.next.sdk.api.DefaultApi
import media.qimeng.next.sdk.model.*
import okhttp3.MediaType.Companion.toMediaType
import okhttp3.RequestBody.Companion.asRequestBody
import java.io.File
import java.io.RandomAccessFile
import java.security.MessageDigest

/** Byte-range transport is device-local. All interpretation, revisions, conflicts and mutations belong to the selected Go. */
internal object DeviceSegments {
    suspend fun fetch(root: File, publisher: String, task: DeviceApplyTask, segment: DeviceApplySegment, api: DefaultApi): File {
        require(task.state.value == "claimed" && segment.bytes in 1..64L * 1024 * 1024 && segment.sha256.matches(Regex("[0-9a-f]{64}"))) { "设备包段清单无效" }
        check(root.mkdirs() || root.isDirectory) { "无法建立设备暂存目录" }
        val file = File(root, operationKey("segment",publisher,task.id,segment.kind.value))
        check(file.length() <= segment.bytes) { "本机暂存长度无效，已停止应用" }
        check(root.usableSpace > (segment.bytes - file.length()) + 64L * 1024 * 1024) { "手机暂存空间不足" }
        RandomAccessFile(file, "rw").use { local ->
            var offset = local.length()
            val buffer = ByteArray(128 * 1024)
            while (offset < segment.bytes) {
                currentCoroutineContext().ensureActive()
                val end = minOf(segment.bytes, offset + 1024 * 1024) - 1
                val etag = "\"${segment.sha256}\""
                val response = api.getDeviceApplySegment(id = task.id, kind = segment.kind, range = "bytes=$offset-$end", ifRange = etag)
                response.bodyOrThrow().use { body ->
                    check(response.code() == 206 && response.headers()["ETag"] == etag &&
                        response.headers()["Content-Range"] == "bytes $offset-$end/${segment.bytes}") { "设备包范围或 ETag 已改变，原库保留" }
                    local.seek(offset)
                    body.byteStream().use { incoming ->
                        var received = 0L
                        val length = end - offset + 1
                        while (received < length) {
                            currentCoroutineContext().ensureActive()
                            val count = incoming.read(buffer, 0, minOf(buffer.size.toLong(), length - received).toInt())
                            if (count < 0) throw java.io.IOException("设备包段中断，将续取")
                            local.write(buffer, 0, count); received += count
                        }
                        check(incoming.read() == -1) { "设备端点返回超出范围的内容" }
                    }
                    local.fd.sync()
                }
                offset = local.length()
            }
        }
        val digest = MessageDigest.getInstance("SHA-256")
        file.inputStream().use { input ->
            val buffer = ByteArray(128 * 1024)
            while (true) { currentCoroutineContext().ensureActive(); val count = input.read(buffer); if (count < 0) break; digest.update(buffer,0,count) }
        }
        check(file.length() == segment.bytes && digest.digest().joinToString("") { "%02x".format(it) } == segment.sha256) { "设备包摘要不一致，已停止应用" }
        return file
    }

    suspend fun apply(local: DefaultApi, file: File, segment: DeviceApplySegment, key: String): ReceivedSegment {
        if (segment.kind.value == "masterTxt") {
            val revision = requireNotNull(segment.expectedLocalRevision) { "作者主表缺少预期本机修订" }
            val content = file.inputStream().use { readAuthorText(it) }
            val result = local.importAuthorList("txt-$key", ImportAuthorListRequest("作者总表.txt",content,revision)).bodyOrThrow()
            val fragment = requireNotNull(result.list) { "本机服务未提供片段回执" }
            val current = local.getAuthorList(fragment.id).bodyOrThrow()
            check(current.id == fragment.id && current.name == "作者总表.txt" && current.revision >= fragment.revision) { "作者主表回读未确认" }
            return ReceivedSegment(segment.kind.value,"applied",segment.sha256,fragment.id)
        }
        check(segment.kind.value == "v2Backup") { "不支持此设备包段" }
        val source = requireNotNull(segment.sourceId) { "v2 段缺少来源安装身份" }
        val staged = local.stageV2Import("v2-$key",source,file.asRequestBody("application/octet-stream".toMediaType())).bodyOrThrow()
        check(staged.sha256 == segment.sha256 && staged.bytes == segment.bytes && staged.sourceId == source) { "本机 v2 暂存与服务器段不符" }
        val plan = local.prepareV2Migration(staged.id,"plan-$key",V2PlanCreate()).bodyOrThrow()
        if (plan.issueCount > 0 || plan.skippedCount > 0) return ReceivedSegment(segment.kind.value,"skipped", receipt = plan.id, code = "v2_review_required")
        local.commitV2Migration(plan.id,"commit-$key",CommitV2MigrationRequest(plan.fingerprint,false)).bodyOrThrow()
        val applied = local.getV2MigrationPlan(plan.id).bodyOrThrow()
        check(applied.state.value == "committed" && applied.fingerprint == plan.fingerprint && applied.checkpointId != null) { "本机没有返回可核对的迁移完成回执" }
        return ReceivedSegment(segment.kind.value,"applied",segment.sha256,applied.id)
    }
}
