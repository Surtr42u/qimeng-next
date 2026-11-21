package media.qimeng.next.screen

import org.junit.Assert.assertEquals
import org.junit.Test

/**
 * 常驻层可见性/常驻集口径（2026-11-22 四 Tab 常驻批）。
 *
 * 对齐旧版 QimengNavHost.kt:657-733 的 per-tab 常驻层：首访才上树、此后常驻；隐藏层全部
 * 压在活动层之下；pushed 路由不占常驻层但也盖住全部 Tab。锁定这些纯条件，防止后续改动
 * 把「已访问」或「活动」算错——那会直接表现为切 Tab 重建、隐藏 Tab 仍显示或幽灵点击。
 */
class ResidentTabLayersTest {

    @Test fun coldStartStraightToAPushedRouteComposesNoTab() {
        // 冷启动直达详情/搜索：首页都没上过屏，常驻层为空（与上一版 homeSeen 的冷启动口径
        // 一致：不让隐藏首页的列表请求与当前路由争请求预算）。
        assertEquals(emptyList<ResidentTabLayer>(), residentTabLayers(emptyList(), "asset:42"))
        assertEquals(emptyList<ResidentTabLayer>(), residentTabLayers(emptyList(), "search"))
    }

    @Test fun theFirstVisitComposesOnlyThatTabAndMarksItActive() {
        assertEquals(listOf(ResidentTabLayer("tab:0", true)), residentTabLayers(emptyList(), "tab:0"))
        assertEquals(listOf(ResidentTabLayer("tab:2", true)), residentTabLayers(emptyList(), "tab:2"))
    }

    @Test fun previouslyVisitedTabsStayResidentAndHidden() {
        assertEquals(
            listOf(ResidentTabLayer("tab:0", false), ResidentTabLayer("tab:1", true)),
            residentTabLayers(listOf("tab:0"), "tab:1"),
        )
    }

    @Test fun exactlyOneLayerIsActiveAndItIsTheActiveKey() {
        val layers = residentTabLayers(listOf("tab:0", "tab:1", "tab:2"), "tab:2")
        assertEquals(listOf("tab:0", "tab:1", "tab:2"), layers.map { it.key })
        assertEquals(1, layers.count { it.active })
        assertEquals("tab:2", layers.single { it.active }.key)
    }

    @Test fun aPushedRouteHidesEveryResidentTab() {
        // pushed 路由置顶：所有已访问 Tab 都压到 CREATED，由路由层（zIndex 2f）覆盖。
        assertEquals(
            listOf(ResidentTabLayer("tab:0", false), ResidentTabLayer("tab:1", false)),
            residentTabLayers(listOf("tab:0", "tab:1"), "asset:7"),
        )
    }

    @Test fun visitedOrderIsNormalisedToTheTabOrderAndDirtyKeysAreIgnored() {
        // 恢复的常驻集可能乱序/重复，也可能夹着旧格式的 pushed 键：输出按 tab:0..3 固定
        // 排序（key(tabKey) 分组身份稳定），非 Tab 键不占常驻层。
        assertEquals(
            listOf(ResidentTabLayer("tab:1", false), ResidentTabLayer("tab:3", true)),
            residentTabLayers(listOf("tab:3", "asset:9", "tab:1", "tab:1", "search"), "tab:3"),
        )
    }

    @Test fun allFourTabsCanStayResidentWithOneActiveTab() {
        // 旧版四个 Tab 全部常驻：这里同样允许 4 层同时存在，只有当前 Tab 可见可交互。
        val layers = residentTabLayers(TAB_SCENE_KEYS, "tab:3")
        assertEquals(4, layers.size)
        assertEquals(listOf("tab:0", "tab:1", "tab:2"), layers.filter { !it.active }.map { it.key })
        assertEquals("tab:3", layers.single { it.active }.key)
    }

    @Test fun anUnknownActiveTabStillGetsAResidentLayer() {
        // 防御性口径：模型只会产生 0..3，但脏存档不能导致「活动 Tab 没有层」而整屏空白；
        // 未知 Tab 键排在固定四键之后，仍按 activeKey 标记活动。
        assertEquals(
            listOf(ResidentTabLayer("tab:0", false), ResidentTabLayer("tab:9", true)),
            residentTabLayers(listOf("tab:0"), "tab:9"),
        )
    }

    @Test fun anUnknownVisitedTabIsNotComposedOnItsOwn() {
        // 恢复出的未知 Tab 键若不在活动位，不单独组合一屏（场景内容会把未知键落到「我的」，
        // 隐藏层不该为一个不存在的 Tab 起列表/统计请求）。
        assertEquals(
            listOf(ResidentTabLayer("tab:0", true)),
            residentTabLayers(listOf("tab:9", "tab:0"), "tab:0"),
        )
    }
}
