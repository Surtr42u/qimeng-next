package media.qimeng.next.data

import media.qimeng.next.model.EventDeliveryIssue
import media.qimeng.next.model.EventDeliveryKind
import media.qimeng.next.model.PendingEvent
import media.qimeng.next.model.Session
import media.qimeng.next.model.sameTarget
import media.qimeng.next.sdk.api.DefaultApi
import media.qimeng.next.sdk.model.EventCreate
import java.io.IOException

internal const val PENDING_EVENT_LIMIT = 1_000
internal const val REJECTED_EVENT_ALLOWANCE = 200

/** A terminal server response leaves the original fact available for individual acknowledgement. */
class ActivityEventQueue internal constructor(
    private val queue: QueueStore,
    private val settings: SecureSettings,
    private val schedule: () -> Unit,
) {
    internal suspend fun append(event: PendingEvent) {
        queue.update { snapshot ->
            val owned = snapshot.events.filter { sameTarget(it.target, event.target) }
            val rejected = owned.count { it.issue?.kind == EventDeliveryKind.REJECTED }
            // Bound the additional rejection history. Unacknowledged overflow still occupies an
            // ordinary slot until the user clears it. There is no eviction of an outstanding fact.
            check(owned.size - minOf(rejected, REJECTED_EVENT_ALLOWANCE) < PENDING_EVENT_LIMIT) {
                "「${event.target.label}」的浏览记录补传队列已满，请在资料维护中补传或处理需要确认的记录"
            }
            snapshot.copy(events = snapshot.events + event)
        }
        schedule()
    }

    suspend fun retry(target: Session, id: String? = null) {
        queue.update { snapshot ->
            checkCurrentTarget(target)
            snapshot.copy(events = snapshot.events.map { event ->
                if (!sameTarget(event.target, target) || !event.canRetry) return@map event
                val selected = if (id == null) event.issue?.kind == EventDeliveryKind.WAITING else event.id == id
                if (selected) event.copy(retryRequested = true) else event
            })
        }
        schedule()
    }

    suspend fun dismiss(target: Session, reviewed: PendingEvent) {
        queue.update { snapshot ->
            checkCurrentTarget(target)
            val event = snapshot.events.firstOrNull { it.id == reviewed.id && sameTarget(it.target, target) }
                ?: return@update snapshot
            check(event == reviewed && event.canDismiss && event.retryRequested != true) { "补传结果已变化，请重新核对该条记录" }
            snapshot.copy(events = snapshot.events.filterNot { it == event })
        }
    }

    internal suspend fun flush(target: Session, api: DefaultApi): Boolean {
        var retry = false
        val outcomes = mutableListOf<EventOutcome>()
        for (event in requireNotNull(queue.state.value).events) {
            if (!sameTarget(target, settings.session.value)) break
            if (!sameTarget(event.target, target) || !event.shouldSend()) continue
            val issue = send(api, event)
            if (issue?.kind == EventDeliveryKind.RETRYABLE) retry = true
            if (!event.needsCommit(issue)) continue
            outcomes += EventOutcome(event.id, event.target, issue)
        }
        // One durable commit per flush instead of one per event: QueueStore.update serialises the
        // whole snapshot and fsyncs it, and a reconnect flush may carry PENDING_EVENT_LIMIT rows.
        // A send that throws (401/403) skips this commit; the rows stay pending and the server
        // dedupes the resend by eventId (service.RecordEvent returns the stored row).
        if (outcomes.isNotEmpty()) queue.update { snapshot -> snapshot.copy(events = applyOutcomes(snapshot.events, outcomes)) }
        return retry
    }

    private fun checkCurrentTarget(target: Session) {
        check(sameTarget(target, settings.session.value)) { "服务器已改变，请回到该记录所属的服务器" }
    }

    private fun PendingEvent.shouldSend(): Boolean = when (issue?.kind) {
        EventDeliveryKind.REJECTED -> false
        EventDeliveryKind.RETRYABLE -> true
        EventDeliveryKind.WAITING, EventDeliveryKind.UNCONFIRMED -> retryRequested == true
        null -> failure == null || retryRequested == true
    }

    private suspend fun send(api: DefaultApi, event: PendingEvent): EventDeliveryIssue? {
        val kind = EventCreate.Kind.entries.firstOrNull { it.value == event.kind }
            ?: return EventDeliveryIssue(EventDeliveryKind.REJECTED, "本机保存的事件类型无效")
        return try {
            api.recordEvent(id = event.assetId, eventCreate = EventCreate(eventId = event.id,
                kind = kind, contentVersion = event.contentVersion, positionMs = event.positionMs,
                sessionId = event.sessionId, seconds = event.seconds, occurredAt = event.occurredAt)).bodyOrThrow()
            null
        } catch (failure: ApiFailure) {
            if (failure.status == 401 || failure.status == 403) throw failure
            deliveryIssue(failure)
        } catch (_: IOException) {
            EventDeliveryIssue(EventDeliveryKind.RETRYABLE, "传输中断，等待联网后重试")
        }
    }

    private fun deliveryIssue(failure: ApiFailure): EventDeliveryIssue = when (failure.code) {
        "asset_trashed" -> EventDeliveryIssue(EventDeliveryKind.WAITING, "媒体在回收站，恢复后可补传", failure.code)
        "event_time_invalid" -> EventDeliveryIssue(EventDeliveryKind.WAITING, "事件时间晚于服务器时间，稍后可重试", failure.code)
        "asset_merged" -> EventDeliveryIssue(EventDeliveryKind.REJECTED, "原媒体已合并，这条事件仍属于原媒体身份", failure.code)
        "not_found" -> EventDeliveryIssue(EventDeliveryKind.REJECTED, "原媒体已不存在", failure.code)
        "content_changed" -> EventDeliveryIssue(EventDeliveryKind.REJECTED, "媒体内容已替换，这条事件属于旧版本", failure.code)
        "event_conflict" -> EventDeliveryIssue(EventDeliveryKind.REJECTED, "服务器已有同一事件身份的不同记录", failure.code)
        "invalid_event", "missing_progress_position" ->
            EventDeliveryIssue(EventDeliveryKind.REJECTED, "服务器拒绝了这条事件的数据", failure.code)
        else -> if (failure.status == 408 || failure.status == 429 || failure.status in 500..599) {
            EventDeliveryIssue(EventDeliveryKind.RETRYABLE, failure.message)
        } else EventDeliveryIssue(EventDeliveryKind.UNCONFIRMED, failure.message)
    }
}

/** A terminal send result for one journal row; a null [issue] means the server accepted it. */
internal data class EventOutcome(val id: String, val target: Session, val issue: EventDeliveryIssue?)

/**
 * True when the row must be rewritten after a send attempt. An accepted event is always removed;
 * an issue is stored only when it differs from the saved one or an old retry flag / legacy failure
 * text still has to be cleared. An unchanged outcome therefore never touches the journal.
 */
internal fun PendingEvent.needsCommit(issue: EventDeliveryIssue?): Boolean =
    issue == null || issue != this.issue || retryRequested == true || failure != null

/**
 * Fold send outcomes into the rows read inside the single commit. Rows appended by another
 * coroutine while the sends were in flight have no outcome and come back untouched, which keeps a
 * batched commit from dropping them. A null [EventOutcome.issue] removes the accepted row.
 */
internal fun applyOutcomes(events: List<PendingEvent>, outcomes: List<EventOutcome>): List<PendingEvent> {
    if (outcomes.isEmpty()) return events
    val byKey = outcomes.associateBy { EventKey(it.id, it.target.baseUrl, it.target.instanceId) }
    return events.mapNotNull { saved ->
        val outcome = byKey[saved.key()] ?: return@mapNotNull saved
        outcome.issue?.let { saved.copy(issue = it, failure = null, retryRequested = null) }
    }
}

/** Rows are identified the way [sameTarget] compares servers: the same fact can target two instances. */
private data class EventKey(val id: String, val baseUrl: String, val instanceId: String)

private fun PendingEvent.key() = EventKey(id, target.baseUrl, target.instanceId)
