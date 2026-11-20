package media.qimeng.next.data

import media.qimeng.next.model.*
import org.junit.Assert.*
import org.junit.Test

class TransferAdmissionTest {
    private val target = Session("https://127.0.0.1:18431/", "same-library", "Local")
    private val other = target.copy(instanceId = "another-library")
    private val upload = Transfer("t", target, "l", "x.png", "image/png", 1, "t", UploadMode.ORDINARY, null, "b", "batch")
    private val event = PendingEvent("e", target, "a", "open", 0, 1, "s", occurredAt = "2026-01-01T00:00:00Z")
    private val batch = TransferBatch("b", target, "l", UploadMode.ORDINARY, "batch", emptyList())

    @Test fun skipEmptyFinishedDraftAndForeignWork() {
        assertFalse(QueueSnapshot().hasReadyTransferWork(target))
        assertFalse(QueueSnapshot(transfers = listOf(upload.copy(target = other))).hasReadyTransferWork(target))
        assertFalse(QueueSnapshot(transfers = listOf(upload.copy(state = TransferState.SUCCEEDED))).hasReadyTransferWork(target))
        assertFalse(QueueSnapshot(pendingInputs = listOf(OriginalInput("draft"))).hasReadyTransferWork(target))
        assertFalse(QueueSnapshot(events = listOf(event.copy(failure = "invalid"))).hasReadyTransferWork(target))
    }

    @Test fun eventAdmissionUsesTheSameExplicitRetryRulesAsDelivery() {
        fun admits(value: PendingEvent) = QueueSnapshot(events = listOf(value)).hasReadyTransferWork(target)
        assertTrue(admits(event.copy(failure = "legacy failure", retryRequested = true)))
        for (kind in listOf(EventDeliveryKind.WAITING, EventDeliveryKind.UNCONFIRMED)) {
            val waiting = event.copy(issue = EventDeliveryIssue(kind, "paused"))
            assertFalse(admits(waiting))
            assertTrue(admits(waiting.copy(retryRequested = true)))
        }
        assertFalse(admits(event.copy(issue = EventDeliveryIssue(EventDeliveryKind.REJECTED, "terminal"), retryRequested = true)))
        assertTrue(admits(event.copy(issue = EventDeliveryIssue(EventDeliveryKind.RETRYABLE, "transient"), failure = "legacy")))
    }

    @Test fun retainEveryDurableReplayPath() {
        assertTrue(QueueSnapshot(transfers = listOf(upload)).hasReadyTransferWork(target))
        assertTrue(QueueSnapshot(transfers = listOf(upload.copy(state = TransferState.CANCELLED))).hasReadyTransferWork(target))
        assertTrue(QueueSnapshot(transfers = listOf(upload.copy(state = TransferState.SUCCEEDED, archiveSourceState = "pending"))).hasReadyTransferWork(target))
        assertTrue(QueueSnapshot(events = listOf(event)).hasReadyTransferWork(target))
        assertTrue(QueueSnapshot(batches = listOf(batch)).hasReadyTransferWork(target))
        assertTrue(QueueSnapshot(batches = listOf(batch.copy(manifestFingerprint = "known")),
            transfers = listOf(upload.copy(state = TransferState.FAILED))).hasReadyTransferWork(target))
    }
}
