package media.qimeng.next.embedded

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * 启动失败横幅文案（纯函数，单测锁定）：退出码必留，详情单行、有界；空详情不能把
 * 「退出码」这个后续判据吞掉，否则首页不会再提供任何本机恢复出口。
 */
class StartupFailureTest {

    @Test fun keepsTheExitCodeWithoutADetail() {
        assertEquals("本机程序启动失败（退出码 1）", startupFailureMessage(1, null))
        assertEquals("本机程序启动失败（退出码 2）", startupFailureMessage(2, "  \n\t "))
    }

    @Test fun appendsTheDetailOnOneLine() {
        assertEquals(
            "本机程序启动失败（退出码 1）：database disk image is malformed",
            startupFailureMessage(1, "database disk image is malformed\r\n"),
        )
        assertEquals(
            "本机程序启动失败（退出码 1）：another service owns this data directory",
            startupFailureMessage(1, "another service owns\nthis data directory"),
        )
    }

    @Test fun boundsAPathologicalDetail() {
        val message = startupFailureMessage(7, "启动失败 ".repeat(2_000))
        assertTrue("message is too long: ${message.length}", message.length <= 19 + 1 + 400)
        assertFalse(message.contains("\n"))
        assertTrue(message.startsWith("本机程序启动失败（退出码 7）："))
    }
}
