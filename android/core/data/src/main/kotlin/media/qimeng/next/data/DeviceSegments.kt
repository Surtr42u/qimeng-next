package media.qimeng.next.data

import kotlinx.coroutines.currentCoroutineContext
import kotlinx.coroutines.ensureActive
import media.qimeng.next.model.DevicePackageBinding
import media.qimeng.next.sdk.api.DefaultApi
import media.qimeng.next.sdk.model.*
import okhttp3.MediaType.Companion.toMediaType
import okhttp3.RequestBody.Companion.asRequestBody
import java.io.File
import java.io.IOException
import java.io.RandomAccessFile
import java.security.MessageDigest

/** Byte-range transport is device-local. All interpretation, revisions, conflicts and mutations belong to the selected Go. */
internal object DeviceSegments {
    suspend fun fetch(root: File, publisher: String, task: DeviceApplyTask, segment: DeviceApplySegment, api: DefaultApi): File {
        require(task.state.value == "claimed" && segment.kind == DeviceSegmentKind.nativeSelection && segment.bytes in 1..2L * 1024 * 1024 &&
            segment.sha256.matches(Regex("[0-9a-f]{64}"))) { "原生设备包段清单无效" }
        check(root.mkdirs() || root.isDirectory) { "无法建立设备暂存目录" }
        val file = File(root, operationKey("segment",publisher,task.id,segment.kind.value))
        if (file.length() > segment.bytes) discardDamagedCopy(file)
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
        if (file.length() != segment.bytes || digest.digest().joinToString("") { "%02x".format(it) } != segment.sha256) discardDamagedCopy(file)
        return file
    }

    private fun discardDamagedCopy(file: File): Nothing {
        check(file.delete()) { "损坏的设备暂存副本未能清理，已停止应用" }
        throw IOException("设备暂存与清单不符，下次将从头下载")
    }

    fun nativeBinding(task: DeviceApplyTask, publisher: String, destination: String): DevicePackageBinding {
        val segment = checkNotNull(task.segments.singleOrNull()) { "设备任务应有一份原生 Next 资料段" }
        check(task.protocolVersion == 2 && segment.kind == DeviceSegmentKind.nativeSelection) { "设备任务没有原生 Next 资料段" }
        check(segment.sourceInstanceId == publisher && segment.targetInstanceId == destination && segment.packageId == segment.sha256) {
            "原生包的来源实例、目标实例或摘要与本次领取不符"
        }
        return DevicePackageBinding(publisher, destination, segment.sha256)
    }

    suspend fun applyNative(local: DefaultApi, file: File, binding: DevicePackageBinding, replace: Boolean): ReceivedSegment {
        val policy = if (replace) NativeDeliveryConflictPolicy.replace else NativeDeliveryConflictPolicy.keep
        val key = operationKey("native-stage", binding.source, binding.target, binding.packageId, policy.value)
        val plan = local.stageNativeDelivery(idempotencyKey = key, sourceInstanceId = binding.source, targetInstanceId = binding.target,
            packageId = binding.packageId, conflictPolicy = policy, body = file.asRequestBody("application/octet-stream".toMediaType())).bodyOrThrow()
        checkBinding(plan, binding)
        val kind = DeviceSegmentKind.nativeSelection.value
        if (plan.state == NativeDeliveryPlanState.prepared) {
            if (plan.conflictPolicy != policy) return ReceivedSegment(kind, "skipped", receipt = plan.id, code = "native_policy_changed")
            val commitKey = operationKey("native-commit", binding.target, plan.id, plan.fingerprint)
            try {
                local.commitNativeDelivery(plan.id, commitKey, NativeDeliveryCommit(plan.fingerprint)).bodyOrThrow()
            } catch (failure: ApiFailure) {
                if (failure.status != 409) throw failure
                val current = local.getNativeDelivery(plan.id).bodyOrThrow()
                checkBinding(current, binding)
                if (current.state == NativeDeliveryPlanState.prepared) {
                    return ReceivedSegment(kind, "skipped", receipt = plan.id, code = failure.code?.takeIf { it.matches(Regex("[a-z0-9_]{1,80}")) } ?: "native_commit_conflict")
                }
            }
        }
        val receipt = local.getNativeDelivery(plan.id).bodyOrThrow()
        checkBinding(receipt, binding)
        check(receipt.id == plan.id && receipt.fingerprint == plan.fingerprint && receipt.state != NativeDeliveryPlanState.prepared &&
            receipt.items.isNotEmpty() && receipt.items.all { it.state != NativeDeliveryItemState.ready && !it.receiptId.isNullOrEmpty() }) { "本机尚未确认原生包的逐项回执" }
        val code = if (receipt.state == NativeDeliveryPlanState.partial) "native_partial" else null
        return ReceivedSegment(kind, receipt.state.value, binding.packageId, receipt.id, code)
    }

    fun checkBinding(plan: NativeDeliveryPlan, binding: DevicePackageBinding) {
        check(plan.sourceInstanceId == binding.source && plan.targetInstanceId == binding.target && plan.packageId == binding.packageId) {
            "本机回执不属于本次原生包"
        }
    }
}
