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

internal const val TRANSFER_WRONG_INSTANCE = "此地址已更换服务器实例，请重新登录"
internal const val TRANSFER_WRONG_SELECTION = "请连接任务原来的服务器"

/** Only recognized connection parking can resume upon authenticated recovery. */
internal fun Transfer.parkedForConnection(): Boolean =
    state == TransferState.WAITING && message in setOf(TRANSFER_WRONG_INSTANCE, TRANSFER_WRONG_SELECTION)

internal fun QueueSnapshot.hasReadyTransferWork(target: Session): Boolean {
    if (events.any { sameTarget(it.target, target) && it.shouldSend() }) return true
    if (transfers.any { sameTarget(it.target, target) &&
            (it.state in setOf(TransferState.QUEUED, TransferState.UPLOADING) || it.parkedForConnection() ||
                (it.state == TransferState.CANCELLED && !it.cancelConfirmed) ||
                (it.state == TransferState.SUCCEEDED && it.archiveSourceState == "pending")) }) return true
    val withTransfers = transfers.mapTo(mutableSetOf(), Transfer::batchKey)
    return batches.any { sameTarget(it.target, target) && !it.inputsChecked &&
        (it.manifestFingerprint != null || it.key !in withTransfers) }
}
