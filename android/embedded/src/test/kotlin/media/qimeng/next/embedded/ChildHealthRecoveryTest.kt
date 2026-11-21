package media.qimeng.next.embedded

import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicInteger
import java.util.concurrent.locks.ReentrantLock
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

/** Ported decision rules from the previous host: one missed probe never kills a thawing child. */
class ChildHealthRecoveryTest {

    @Test
    fun `missing or dead child is started by foreground check`() {
        for (child in listOf(null, Any())) {
            var starts = 0
            val recovery = recovery(
                current = { child },
                alive = { false },
                ensureStarted = { starts++ },
                probe = { error("no child to probe") },
                restart = { _, _ -> error("no child to kill") },
            )
            assertFalse(recovery.runOnce("foreground"))
            assertEquals(1, starts)
        }
    }

    @Test
    fun `temporary probe failure does not kill recovered child`() {
        val child = Any()
        var probes = 0
        var restarts = 0
        val recovery = recovery(
            current = { child },
            probe = { ++probes == 2 },
            restart = { _, _ -> restarts++ },
        )
        assertFalse(recovery.runOnce("foreground"))
        assertEquals(2, probes)
        assertEquals(0, restarts)
    }

    @Test
    fun `two failures restart only the captured child and keep the reason`() {
        val child = Any()
        val restarted = mutableListOf<Any>()
        val reasons = mutableListOf<String>()
        val recovery = recovery(
            current = { child },
            probe = { false },
            restart = { observed, reason -> restarted += observed; reasons += reason },
        )
        assertTrue(recovery.runOnce("watchdog"))
        assertEquals(listOf(child), restarted)
        assertEquals(listOf("watchdog"), reasons)
    }

    @Test
    fun `a child replaced during the recheck window is never killed`() {
        var current: Any? = Any()
        val replaced = recovery(
            current = { current },
            probe = { false },
            pauseBeforeRecheck = { current = Any() },
            restart = { _, _ -> error("must not kill replaced child") },
        )
        assertFalse(replaced.runOnce("foreground"))
    }

    @Test
    fun `overlapping checks collapse and release the gate afterwards`() {
        val child = Any()
        val entered = CountDownLatch(1)
        val release = CountDownLatch(1)
        val probes = AtomicInteger()
        val recovery = recovery(
            current = { child },
            probe = {
                if (probes.incrementAndGet() == 1) {
                    entered.countDown()
                    release.await(5, TimeUnit.SECONDS)
                }
                true
            },
        )
        val worker = Thread { recovery.runOnce("foreground") }.apply { start() }
        assertTrue(entered.await(5, TimeUnit.SECONDS))
        assertFalse("second check must not overlap", recovery.runOnce("foreground"))
        release.countDown()
        worker.join(5_000)
        assertFalse(worker.isAlive)
        assertEquals(1, probes.get())
        assertFalse(recovery.runOnce("foreground"))
        assertEquals(2, probes.get())
    }

    private fun <P : Any> recovery(
        current: () -> P?,
        alive: (P) -> Boolean = { true },
        ensureStarted: () -> Unit = {},
        probe: () -> Boolean = { true },
        pauseBeforeRecheck: () -> Unit = {},
        restart: (P, String) -> Unit = { _, _ -> },
    ) = ChildHealthRecovery(
        gate = ReentrantLock(),
        current = current,
        alive = alive,
        ensureStarted = ensureStarted,
        probe = probe,
        pauseBeforeRecheck = pauseBeforeRecheck,
        restart = restart,
    )
}
