package media.qimeng.next.embedded

import java.util.concurrent.locks.ReentrantLock

/**
 * One health decision for the owned child, ported from the previous host's measured behaviour.
 *
 * A failed probe is not enough to kill: the scheduler may simply not have been thawed yet, so
 * the check pauses for [media.qimeng.next.embedded.EmbeddedService]'s recheck window and probes
 * again. Two failures are required. A false kill only costs one clean restart, while leaving a
 * frozen child alone leaves the whole app failing until it is manually restarted.
 *
 * [gate] is shared by every caller (watchdog tick and foreground request), so overlapping checks
 * collapse into one. A skipped check is not a health verdict. The child captured before the pause
 * is re-read afterwards: a child that was replaced by a newer launch is never killed by an older
 * check.
 */
internal class ChildHealthRecovery<P : Any>(
    private val gate: ReentrantLock,
    private val current: () -> P?,
    private val alive: (P) -> Boolean,
    private val ensureStarted: () -> Unit,
    private val probe: () -> Boolean,
    private val pauseBeforeRecheck: () -> Unit,
    private val restart: (P, String) -> Unit,
) {

    /**
     * Runs at most one decision at a time.
     *
     * @param reason short diagnostic token (`watchdog`, `foreground`) carried into restart logs.
     * @return true when [restart] was invoked for the child captured by this check.
     */
    fun runOnce(reason: String): Boolean {
        if (!gate.tryLock()) return false
        try {
            val child = current()
            if (child == null || !alive(child)) {
                ensureStarted()
                return false
            }
            if (probe()) return false
            pauseBeforeRecheck()
            if (current() !== child) return false
            if (probe()) return false
            restart(child, reason)
            return true
        } finally {
            gate.unlock()
        }
    }
}
