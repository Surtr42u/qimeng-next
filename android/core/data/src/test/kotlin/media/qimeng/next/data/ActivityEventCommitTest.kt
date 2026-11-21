package media.qimeng.next.data

import media.qimeng.next.model.EventDeliveryIssue
import media.qimeng.next.model.EventDeliveryKind
import media.qimeng.next.model.PendingEvent
import media.qimeng.next.model.Session
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertSame
import org.junit.Assert.assertTrue
import org.junit.Test

/** A flush writes the journal once; these are the decisions folded into that single commit. */
class ActivityEventCommitTest {
    private val nas = Session("https://nas.example/", "instance-nas", "NAS")
    private val embedded = Session("http://127.0.0.1:8420/", "instance-local", "本机")
    private val offline = EventDeliveryIssue(EventDeliveryKind.RETRYABLE, "传输中断，等待联网后重试")

    @Test fun anUnchangedIssueWithoutPendingStateSkipsTheRewrite() {
        val saved = event("unchanged", nas).copy(issue = offline)
        assertFalse(saved.needsCommit(offline))
    }

    @Test fun changedIssuesAndUnclearedRetryStateRewriteTheRow() {
        val saved = event("changed", nas)
        assertTrue(saved.needsCommit(offline))
        assertTrue(saved.copy(issue = offline, retryRequested = true).needsCommit(offline))
        assertTrue(saved.copy(issue = offline, failure = "旧版留下的失败文字").needsCommit(offline))
        assertTrue("An event the server accepted must leave the queue", saved.needsCommit(null))
    }

    @Test fun applyingOutcomesRemovesAcceptedRowsAndStoresChangedIssues() {
        val accepted = event("accepted", nas)
        val rejected = event("rejected", nas).copy(failure = "旧版失败", retryRequested = true)
        val skipped = event("skipped", nas).copy(issue = offline)
        val updated = applyOutcomes(listOf(accepted, rejected, skipped), listOf(
            EventOutcome(accepted.id, nas, null),
            EventOutcome(rejected.id, nas, EventDeliveryIssue(EventDeliveryKind.REJECTED, "原媒体已不存在", "not_found")),
        ))
        assertEquals(listOf(rejected.id, skipped.id), updated.map(PendingEvent::id))
        assertEquals(EventDeliveryKind.REJECTED, updated.first().issue?.kind)
        assertEquals("not_found", updated.first().issue?.code)
        assertNull("A stored issue clears the legacy failure text", updated.first().failure)
        assertNull("A stored issue clears the retry request", updated.first().retryRequested)
        assertEquals("A row without an outcome keeps its saved state", skipped, updated.last())
    }

    @Test fun rowsAppendedDuringTheFlushSurviveTheSingleCommit() {
        val sent = event("sent", nas)
        val appended = event("appended", nas)
        // The same event id on another server is a different row and must not be removed with it.
        val twin = event("sent", embedded).copy(issue = offline)
        val updated = applyOutcomes(listOf(sent, appended, twin), listOf(EventOutcome(sent.id, nas, null)))
        assertEquals(listOf(appended.id, twin.id), updated.map(PendingEvent::id))
        assertEquals(twin, updated.last())
    }

    @Test fun anEmptyOutcomeSetLeavesTheSnapshotIdentityUntouched() {
        val rows = listOf(event("only", nas))
        assertSame(rows, applyOutcomes(rows, emptyList()))
    }

    private fun event(name: String, target: Session) = PendingEvent(
        id = "event-$name", target = target, assetId = "asset-$name", kind = "progress", positionMs = 1234,
        contentVersion = 7, sessionId = "session-$name", occurredAt = "2026-01-01T12:30:00Z",
    )
}
