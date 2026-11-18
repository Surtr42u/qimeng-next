package media.qimeng.next.data

import android.content.Context
import android.net.Uri
import kotlinx.coroutines.CancellationException
import media.qimeng.next.model.*
import media.qimeng.next.sdk.api.DefaultApi
import media.qimeng.next.sdk.model.BatchProtection
import media.qimeng.next.sdk.model.IssueSourceCleanupPermitRequest
import media.qimeng.next.sdk.model.SourceCleanupPermit
import media.qimeng.next.sdk.model.SourceCleanupReceiptInput
import java.io.IOException

internal class ArchiveProtection(context: Context, private val store: ArchiveStore, private val provider: ApiProvider, private val queue: QueueStore) {
    private val trash = ArchiveMediaTrash(context)

    suspend fun run(target: Session): Boolean {
        val journal = queue.state.value ?: return false
        val tasks = journal.transfers.filter { sameTarget(it.target, target) && it.state == TransferState.SUCCEEDED && it.archiveSourceState == "pending" }
        val active = journal.transfers.filter { it.state in setOf(TransferState.QUEUED, TransferState.UPLOADING) }.mapTo(mutableSetOf(), Transfer::batchKey)
        val ready = tasks.filterNot { it.batchKey in active }
        var later = ready.size > 24
        val protections = mutableMapOf<String, BatchProtection>()
        val api = provider.api(target)
        for (task in ready.take(24)) {
            val source = store.cleanup(task.id) ?: error("归档来源清单缺失，已停止来源处理")
            try {
                if (!process(api, task, source, protections)) later = true
            } catch (cancelled: CancellationException) { throw cancelled }
            catch (failure: IOException) {
                val transient = failure !is ApiFailure || failure.status == 429 || failure.status >= 500
                if (transient || store.cleanup(task.id)?.stage in setOf("armed", "observed")) {
                    publish(task, "pending", "已入库；归档保护或来源回执等待恢复")
                    later = true
                } else {
                    store.block(task.id, "protection_unavailable")
                    publish(task, "blocked", "已入库；独立备份或回收许可尚未确认，归档来源保留。可重新核对。")
                }
            } catch (failure: IllegalStateException) {
                if (store.cleanup(task.id)?.stage in setOf("armed", "observed")) {
                    publish(task, "pending", "已入库；来源结果等待服务器核对")
                    later = true
                } else {
                    store.block(task.id, "binding_mismatch")
                    publish(task, "blocked", "已入库；归档输入与服务端凭据不一致，已停止来源处理")
                }
            }
        }
        return later
    }

    private suspend fun process(api: DefaultApi, task: Transfer, src: ArchiveStore.Cleanup,
                                snapshots: MutableMap<String, BatchProtection>): Boolean {
        val id = task.id
        if (src.stage == "done") { publish(task, "done", label(src.outcome, src.code)); return true }
        if (src.stage == "blocked") { publish(task, "blocked", "已入库；本批尚未取得可用的独立备份，归档来源保留。"); return true }
        if (src.local) { store.finish(id, "same_device"); publish(task, "done", label(null, "same_device")); return true }
        val batch = task.batchId ?: error("归档批次尚未取得服务器回执")
        val hash = task.sourceHash ?: error("归档来源摘要缺失")
        val permitId = src.permit ?: run {
            val proof = snapshots[task.batchKey] ?: (if (src.protection != null) api.getBatchProtection(batch, src.protection)
                else api.protectBatchInputs(batch, "protect-${task.batchKey}")).bodyOrThrow().also {
                check(it.instanceId == task.target.instanceId && it.batchId == batch) { "独立备份绑定不一致，来源保留" }
                store.protection(task.batchKey, it.id); snapshots[task.batchKey] = it
            }
            if (proof.state.value in setOf("queued", "running")) {
                publish(task, "pending", "已入库；正在等待本批独立备份")
                return false
            }
            val item = proof.items.find { it.inputId == id }
            if (proof.state.value != "completed" || item == null || item.state.value != "protected" || item.assetId != task.assetId ||
                item.hash != hash || item.propertySize != task.bytes || proof.proofHash.isNullOrBlank()) {
                store.block(id, "input_unprotected"); publish(task, "blocked", "已入库；独立备份尚未覆盖此输入的具体版本，归档来源保留。")
                return true
            }
            val value = api.issueSourceCleanupPermit(id = batch, protectionId = proof.id, idempotencyKey = "permit-$id",
                issueSourceCleanupPermitRequest = IssueSourceCleanupPermitRequest(inputId = id, expectedHash = hash, sourceSize = task.bytes)).bodyOrThrow()
            check(value.contentVersion == item.contentVersion && value.proofHash == proof.proofHash) { "许可与本批保护凭据不一致" }
            store.permit(id, value.id)
            value.id
        }
        val permit = api.getSourceCleanupPermit(permitId).bodyOrThrow()
        check(bindingMatches(permit, task, hash)) { "归档回收许可与原输入不一致" }
        permit.receipt?.let {
            store.observe(id, it.outcome.value, it.code.orEmpty(), it.providerReceipt)
            store.finish(id, it.code.orEmpty()); publish(task, "done", label(it.outcome.value, it.code))
            return true
        }
        if (src.stage != "observed") {
            val outcome = when {
                src.stage == "armed" -> observeSource(src, task, hash, permitId)
                permit.state.value != "issued" -> SourceResult("retained", "permit_inactive")
                else -> inspectAndRecycle(api, task, src, hash, permit)
            }
            store.observe(id, outcome.outcome, outcome.code, outcome.receipt)
        }
        val observed = requireNotNull(store.cleanup(id))
        val input = SourceCleanupReceiptInput(outcome = SourceCleanupReceiptInput.Outcome.entries.first { it.value == observed.outcome },
            code = observed.code, providerReceipt = observed.receipt)
        val result = api.recordSourceCleanupReceipt(id = permitId, idempotencyKey = "receipt-$permitId", sourceCleanupReceiptInput = input).bodyOrThrow()
        check(result.receipt?.outcome?.value == observed.outcome && result.receipt?.providerReceipt == observed.receipt) { "服务器尚未确认真实来源回执" }
        store.finish(id, observed.code.orEmpty()); publish(task, "done", label(observed.outcome, observed.code))
        return true
    }

    private fun bindingMatches(value: SourceCleanupPermit, task: Transfer, hash: String) =
        value.instanceId == task.target.instanceId && value.batchId == task.batchId && value.inputId == task.id &&
            value.assetId == task.assetId && value.hash == hash && value.propertySize == task.bytes

    private suspend fun inspectAndRecycle(api: DefaultApi, task: Transfer, source: ArchiveStore.Cleanup,
                                         hash: String, permit: SourceCleanupPermit): SourceResult {
        val binding = try {
            val (found, disposition) = trash.inspect(Uri.parse(source.uri), task.bytes, hash, source.original)
            disposition?.let { return it }
            requireNotNull(found)
        } catch (cancelled: CancellationException) { throw cancelled }
        catch (_: Exception) { return SourceResult("unavailable", "source_unavailable") }
        val current = api.getSourceCleanupPermit(permit.id).bodyOrThrow()
        if (!bindingMatches(current, task, hash) || current.state.value != "issued") return SourceResult("retained", "permit_inactive")
        store.arm(task.id, binding.uri.toString(), binding.generation)
        return try {
            if (!trash.move(binding, task.bytes)) SourceResult("source_changed", "source_changed")
            else trash.observe(binding, task.bytes, hash, permit.id, restoreChanged = true)
        } catch (cancelled: CancellationException) { throw cancelled }
        catch (_: Exception) { observeSource(requireNotNull(store.cleanup(task.id)), task, hash, permit.id) }
    }

    private suspend fun observeSource(source: ArchiveStore.Cleanup, task: Transfer, hash: String, permit: String): SourceResult = try {
        trash.observe(MediaBinding(Uri.parse(requireNotNull(source.media)), requireNotNull(source.generation), requireNotNull(source.original).fileIdentity), task.bytes, hash, permit)
    } catch (cancelled: CancellationException) { throw cancelled }
    catch (_: Exception) { SourceResult("unavailable", "recycle_unconfirmed") }

    private fun label(result: String?, code: String?) = when {
        code == "same_device" -> "已入库；本机上传保留手机归档"
        result == "recycled" -> "已入库并取得独立备份；手机归档已移入系统回收站"
        code == "recycle_unconfirmed" -> "已入库；系统回收状态未能核实，请在手机核对"
        result == "unavailable" -> "已入库；系统未提供可核验的来源回收能力，未执行回收"
        result == "source_changed" -> "已入库；归档来源已改变，已停止来源处理"
        else -> "已入库；系统未确认回收，归档来源保留"
    }
    private suspend fun publish(task: Transfer, state: String, text: String) {
        if (queue.state.value?.transfers?.find { it.id == task.id }?.let { it.message == text && it.archiveSourceState == state } == true) return
        queue.update { journal -> journal.copy(transfers = journal.transfers.map {
            if (it.id == task.id && sameTarget(task.target, it.target)) it.copy(archiveSourceState = state, message = text) else it
        }) }
    }
}
