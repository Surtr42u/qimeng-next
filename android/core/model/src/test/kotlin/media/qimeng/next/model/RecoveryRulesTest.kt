package media.qimeng.next.model

import org.junit.Assert.*
import org.junit.Test

class RecoveryRulesTest {
    @Test fun sameUrlWithDifferentInstanceCannotResumeOldWork() {
        val old = Session("https://example.test/", "old", "电脑")
        assertFalse(sameTarget(old, old.copy(instanceId = "replacement")))
        assertFalse(sameTarget(old, null))
        assertTrue(sameTarget(old, old.copy(label = "新显示名称")))
    }

    @Test fun aLateSearchCannotOverwriteOrFinishItsSuccessor() {
        val generation = RequestGeneration()
        val first = generation.next()
        val second = generation.next()
        assertFalse(generation.isCurrent(first))
        assertTrue(generation.isCurrent(second))
    }

    @Test fun committedAndCancelledTransfersNeverBecomeAutomaticRetries() {
        assertFalse(canRetry(TransferState.SUCCEEDED))
        assertFalse(canRetry(TransferState.CANCELLED))
        assertFalse(canRetry(TransferState.UPLOADING))
        assertTrue(canRetry(TransferState.WAITING))
        assertTrue(canRetry(TransferState.FAILED))
    }
}
