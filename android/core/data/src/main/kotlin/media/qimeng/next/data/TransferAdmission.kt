package media.qimeng.next.data

import android.content.Context
import media.qimeng.next.model.QueueSnapshot
import media.qimeng.next.model.Session
import media.qimeng.next.model.Transfer
import media.qimeng.next.model.TransferState
import media.qimeng.next.model.sameTarget

/** Optional app host admits the verified embedded transport, never an arbitrary loopback service. */
interface LocalTransferHost {
    fun transferConnectionReady(target: Session): Boolean
}

internal fun transferConnectionReady(context: Context, target: Session): Boolean =
    (context.applicationContext as? LocalTransferHost)?.transferConnectionReady(target) ?: true

internal fun QueueSnapshot.hasReadyTransferWork(target: Session): Boolean {
    if (events.any { sameTarget(it.target, target) && it.shouldSend() }) return true
    if (transfers.any { sameTarget(it.target, target) &&
            (it.state in setOf(TransferState.QUEUED, TransferState.UPLOADING) ||
                (it.state == TransferState.CANCELLED && !it.cancelConfirmed) ||
                (it.state == TransferState.SUCCEEDED && it.archiveSourceState == "pending")) }) return true
    val withTransfers = transfers.mapTo(mutableSetOf(), Transfer::batchKey)
    return batches.any { sameTarget(it.target, target) && !it.inputsChecked &&
        (it.manifestFingerprint != null || it.key !in withTransfers) }
}
