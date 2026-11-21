package media.qimeng.next.screen

import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * 下拉刷新归位条件（§1「刷新后跳到未见内容」修复口径）。
 *
 * 归位只能发生在：用户手势触发的刷新、请求结束、无错误、列表已属于当前查询、渲染中的
 * 分组已是这批条目的分组。翻页（无 userRefresh）与服务器推送（silent，无 userRefresh）
 * 在任何字段组合下都不得归位。
 */
class ScrollResetGateTest {

    private fun granted(
        userRefresh: Boolean = true,
        loading: Boolean = false,
        refreshing: Boolean = false,
        error: String? = null,
        settled: Boolean = true,
        sectionsMatch: Boolean = true,
    ) = shouldResetScrollAfterRefresh(userRefresh, loading, refreshing, error, settled, sectionsMatch)

    @Test fun resetsOnlyAfterTheRefreshedListAndItsSectionsAreOnScreen() {
        assertTrue(granted())
    }

    @Test fun appendsAndServerPushesNeverResetBecauseTheyDoNotArmTheGestureFlag() {
        // 翻页：loading 在途，也没有手势标志；两种情况都不能归位。
        assertFalse(granted(userRefresh = false, loading = true))
        // silent SSE 补读：列表已完成、分组也已替换，但用户没有下拉。
        assertFalse(granted(userRefresh = false))
    }

    @Test fun anInFlightRequestBlocksTheReset() {
        assertFalse(granted(loading = true))
        assertFalse(granted(refreshing = true))
    }

    @Test fun aFailedRefreshNeverResets() {
        assertFalse(granted(error = "连接失败"))
        // 失败时即使恰好没有在途请求、分组也是新的，也不能归位。
        assertFalse(granted(loading = true, refreshing = true, error = "连接失败"))
    }

    @Test fun aSeedChangeBlocksTheResetUntilTheNewQueryHasLanded() {
        // 首页换 seed：旧列表还在屏上，loadedQuery 仍属于上一个查询。
        assertFalse(granted(settled = false))
    }

    @Test fun aPendingOnGroupingBlocksTheResetUntilTheSectionsMatchTheEntries() {
        // 异步分组还没换完：此时归位会被随后替换的分区重新锚定。
        assertFalse(granted(sectionsMatch = false))
    }
}
