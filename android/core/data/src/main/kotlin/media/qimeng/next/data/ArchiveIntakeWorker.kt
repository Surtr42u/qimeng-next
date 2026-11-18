package media.qimeng.next.data

import android.content.Context
import android.net.Uri
import androidx.work.CoroutineWorker
import androidx.work.WorkerParameters
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import media.qimeng.next.model.*

class ArchiveIntakeWorker(context: Context, parameters: WorkerParameters) : CoroutineWorker(context, parameters) {
    private val graph = DataGraph.get(context)

    override suspend fun doWork(): Result = withContext(Dispatchers.IO) {
        val id = inputData.getString("plan") ?: return@withContext Result.failure()
        val archive = graph.archives
        val plan = archive.store.overview(id) ?: return@withContext Result.success()
        if (plan.state !in setOf("confirmed", "preparing") || !sameTarget(plan.target, graph.settings.session.value)) return@withContext Result.success()
        try {
            graph.awaitRecovery()
            archive.store.state(id, "preparing")
            archive.changed()
            while (!isStopped && sameTarget(plan.target, graph.settings.session.value)) {
                val group = archive.store.nextGroup(id)
                if (group == null) {
                    archive.store.state(id, "queued")
                    archive.changed()
                    graph.uploads.schedule()
                    return@withContext Result.success()
                }
                val journal = requireNotNull(graph.queue.state.value)
                if (journal.batches.none { it.key == group.id }) {
                    val remaining = group.inputs.count { input -> journal.pendingInputs.none { it.id == input.id } }
                    val active = journal.drafts.size + journal.transfers.count { it.state !in setOf(TransferState.SUCCEEDED, TransferState.CANCELLED) }
                    if (active + remaining > 200) {
                        archive.store.state(id, "preparing", "正在等待传输队列腾出空间")
                        archive.changed()
                        return@withContext Result.retry()
                    }
                    val inputs = graph.uploads.receive(group.inputs.map { Uri.parse(it.uri) }, intakeGroup = group.id, archive = group.inputs)
                    graph.uploads.enqueue(group.library, group.directory, UploadMode.ORDINARY, target = plan.target, inputIds = inputs, batchKey = group.id)
                }
                val copied = requireNotNull(graph.queue.state.value).transfers.filter { it.batchKey == group.id }.mapTo(mutableSetOf(), Transfer::id)
                group.inputs.filterNot { it.id in copied }.forEach { archive.store.finish(it.id, "intake_failed") }
                archive.store.queued(group.id)
                archive.changed()
            }
            Result.success()
        } catch (cancelled: CancellationException) { throw cancelled }
        catch (failure: Exception) {
            archive.store.state(id, "preparing", safeFailure(failure))
            archive.changed()
            // The exact input IDs, prepared snapshots and target survive an explicit retry.
            Result.failure()
        }
    }
}
