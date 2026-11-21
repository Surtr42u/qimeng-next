package media.qimeng.next.screen

import androidx.compose.foundation.background
import androidx.compose.foundation.gestures.awaitEachGesture
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.material3.MaterialTheme
import androidx.compose.runtime.Composable
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.SideEffect
import androidx.compose.runtime.compositionLocalOf
import androidx.compose.runtime.getValue
import androidx.compose.runtime.key
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.saveable.rememberSaveableStateHolder
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clipToBounds
import androidx.compose.ui.graphics.graphicsLayer
import androidx.compose.ui.input.pointer.pointerInput
import androidx.compose.ui.semantics.clearAndSetSemantics
import androidx.compose.ui.zIndex
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.LifecycleEventObserver
import androidx.lifecycle.LifecycleOwner
import androidx.lifecycle.LifecycleRegistry
import androidx.lifecycle.compose.LocalLifecycleOwner

/** 首页常驻层的固定场景键；与 AppRoot 里 `"tab:$tab"` 的取值同源。 */
private const val HOME_SCENE_KEY = "tab:0"

/**
 * 当前场景是否为可见的前台场景。常驻首页被其它 Tab 或 pushed 路由盖住时由
 * [ResidentSceneHost] 提供 false，用来让首页内的独立窗口（底部筛选面板）随场景隐藏而
 * 撤下——普通（非常驻）场景没有 provide，默认 true 保持原行为。
 */
internal val LocalSceneActive = compositionLocalOf { true }

/**
 * 场景宿主（2026-11-21 首页常驻批）：只让首页 Tab 保持组合，其它场景维持既有行为。
 *
 * 旧版（qimeng-media）只有起始 Tab 在导航壳下连续组合（QimengNavHost.kt:640-651），
 * 其余 Tab 返回时重建、靠 rememberSaveable / Activity 级 ViewModel 还原。本宿主对齐这一
 * 边界：首页是唯一常驻层，切到相册/统计/我的或 pushed 路由时首页**不离树**（无新网格
 * 组合、无新 AsyncImage 请求、分区不重算、pager/滚动位置原样保留）；非首页 Tab 仍在
 * 离开时销毁、回来用 SaveableStateProvider 恢复，与改前逐帧一致。刻意不做四 Tab 全常驻：
 * 隐藏相册的批量返回键、共享 FacetViewModel 的陈旧查询、四份网格的内存都在本批风险面外。
 *
 * z 序（同一 Box 的子级，zIndex 决定绘制与命中顺序）：
 * 隐藏首页 0f（alpha=0 + clearAndSetSemantics）→ 触摸死层 0.5f → 活动 Tab 1f →
 * 活动 pushed 路由 2f。触摸死层只在上层没有接住触摸时兜底（Compose 命中测试中无
 * pointer input 的节点不拦截），把落在隐藏首页上的幽灵点击/滚动整段吞掉。
 *
 * 常驻首页包一个 [SceneLifecycleOwner]：隐藏时压到 CREATED（等价 ON_STOP 之后），
 * collectAsStateWithLifecycle、GalleryViewport 的 repeatOnLifecycle 预取、BackHandler
 * 都会像普通 Tab 被停掉一样停摆；可见时按父（Activity）状态封顶到 RESUMED，回前台
 * 仍受系统暂停约束。LocalSceneActive 则负责首页侧独立窗口的显隐。
 *
 * SaveableState 口径与 core/ui 的 SingleSceneHost 完全一致：首页用固定键
 * [HOME_SCENE_KEY] 常驻，活动场景按 activeKey 换 provider；pushed 路由键只保留最近 12 个
 * （Tab 键不参与淘汰），避免详情历史无限积累。
 */
@Composable
internal fun ResidentSceneHost(
    activeKey: String,
    modifier: Modifier = Modifier,
    content: @Composable (String) -> Unit,
) {
    val holder = rememberSaveableStateHolder()
    var recentDetailKeys by rememberSaveable { mutableStateOf(emptyList<String>()) }
    // 首页是否曾经上过屏：冷启动直达 pushed 路由（activeKey = "asset:…"）时首页从未展示，
    // 先不组合它——否则隐藏首页的普通 LaunchedEffect（如 browse 列表请求）会与当前路由
    // 争同一份请求预算。用 rememberSaveable 跨进程还原：用过首页的用户恢复后仍拿回常驻层。
    var homeSeen by rememberSaveable { mutableStateOf(false) }
    val homeActive = activeKey == HOME_SCENE_KEY
    val parentLifecycle = LocalLifecycleOwner.current.lifecycle
    val homeOwner = remember(parentLifecycle) { SceneLifecycleOwner(parentLifecycle, homeActive) }
    DisposableEffect(homeOwner) {
        homeOwner.attach()
        onDispose { homeOwner.dispose() }
    }
    SideEffect {
        // 可见即视为「展示过」：首次组成首页的同一帧就记下，之后退回 pushed 路由保留常驻层。
        if (homeActive) homeSeen = true
        homeOwner.setVisible(homeActive)
        // 与 SingleSceneHost 同款：只有 pushed 路由键（含 ':' 且不是 tab:）参与最近 12 个淘汰。
        if (':' in activeKey && !activeKey.startsWith("tab:") && recentDetailKeys.lastOrNull() != activeKey) {
            val keys = recentDetailKeys.filterNot { it == activeKey } + activeKey
            keys.dropLast(12).forEach(holder::removeState)
            recentDetailKeys = keys.takeLast(12)
        }
    }
    Box(modifier.fillMaxSize().clipToBounds().background(MaterialTheme.colorScheme.background)) {
        // 第一层：首页常驻。key 固定，切 Tab / pushed 路由都只改外层可见性，内容子树不重建。
        // 首页从未上过屏时（冷启动直达 pushed 路由）不组合：等它第一次成为活动场景再上树，
        // 避免隐藏首页的首次 LaunchedEffect 抢先发起列表请求；一旦展示过就按常驻层保留。
        if (homeSeen || homeActive) {
            key(HOME_SCENE_KEY) {
                CompositionLocalProvider(
                    LocalLifecycleOwner provides homeOwner,
                    LocalSceneActive provides homeActive,
                ) {
                    Box(
                        Modifier.fillMaxSize().zIndex(0f).then(
                            if (homeActive) {
                                Modifier
                            } else {
                                // 隐藏首页三重隔离之一、之二：不绘制 + 语义树不可达（对齐旧版 hide 的 GONE）。
                                Modifier.graphicsLayer { alpha = 0f }.clearAndSetSemantics { }
                            },
                        ),
                    ) {
                        holder.SaveableStateProvider(HOME_SCENE_KEY) { content(HOME_SCENE_KEY) }
                    }
                }
            }
        }
        if (!homeActive) {
            // 之三：触摸死层。声明在隐藏首页之后、活动场景之前，zIndex 把它压在两者之间。
            Box(Modifier.fillMaxSize().zIndex(0.5f).blockTouches())
            // 第二层：活动场景。不透明底色保证隐藏首页不会从活动页的留白处透出。
            val activeZ = if (activeKey.startsWith("tab:")) 1f else 2f
            Box(Modifier.fillMaxSize().zIndex(activeZ).background(MaterialTheme.colorScheme.background)) {
                holder.SaveableStateProvider(activeKey) { content(activeKey) }
            }
        }
    }
}

/**
 * 场景生命周期 owner：把父（Activity）生命周期按「本场景是否可见」收窄成场景自身的
 * 生命周期。隐藏固定 CREATED，让依赖 LocalLifecycleOwner 的收集器真正停摆；可见按父
 * 状态封顶到 RESUMED（Activity 进后台时仍会跟着降到 CREATED）。dispose 置 DESTROYED
 * 并摘掉父观察者，避免宿主离树后残留监听。
 *
 * attach 单独暴露而不在构造器里挂观察者：本类在 remember 计算期创建，挂观察者属于副作用，
 * 放到 DisposableEffect 里执行；无论 SideEffect 的可见性更新与 attach 谁先执行，attach
 * 都会按当时的 visible 同步一次，两种顺序状态一致。
 */
internal class SceneLifecycleOwner(
    private val parent: Lifecycle,
    initiallyVisible: Boolean,
) : LifecycleOwner {
    private val registry = LifecycleRegistry(this)
    private val parentObserver = LifecycleEventObserver { _, _ -> sync() }
    private var visible = initiallyVisible
    private var attached = false
    private var disposed = false

    override val lifecycle: Lifecycle get() = registry

    /** 组合应用后挂到父生命周期并按当前可见性同步一次。 */
    fun attach() {
        if (attached || disposed) return
        attached = true
        parent.addObserver(parentObserver)
        sync()
    }

    /** 可见性翻转：隐藏 → CREATED；可见 → 父状态封顶到 RESUMED（父已销毁则 DESTROYED）。 */
    fun setVisible(value: Boolean) {
        if (visible == value) return
        visible = value
        if (attached) sync()
    }

    /** 退出组合：先摘父观察者，再把自身收敛到 DESTROYED。 */
    fun dispose() {
        if (disposed) return
        disposed = true
        if (attached) parent.removeObserver(parentObserver)
        registry.currentState = Lifecycle.State.DESTROYED
    }

    private fun sync() {
        if (disposed) return
        val parentState = parent.currentState
        val target = when {
            parentState == Lifecycle.State.DESTROYED -> Lifecycle.State.DESTROYED
            // 可见只封顶到 RESUMED：父在后台（CREATED/STARTED）时保持父状态，不能自升。
            visible -> when {
                parentState.isAtLeast(Lifecycle.State.RESUMED) -> Lifecycle.State.RESUMED
                parentState.isAtLeast(Lifecycle.State.STARTED) -> Lifecycle.State.STARTED
                parentState.isAtLeast(Lifecycle.State.CREATED) -> Lifecycle.State.CREATED
                else -> Lifecycle.State.INITIALIZED
            }
            else -> Lifecycle.State.CREATED
        }
        if (registry.currentState != target) registry.currentState = target
    }
}

/**
 * 触摸死层 modifier：把落在本层的触摸整段（按下→移动→抬起）全量消费。服务「保持组合但
 * 不可交互」的隔离场景（常驻首页隐藏时的兜底层）——命中测试层面它带 pointer input 节点，
 * 触摸沿 z 序命中即止（不再下探）；事件层面 consume 保证无漏网。只承接「上层没接住的
 * 穿透触摸」，这类触摸在改前同样无处可去（落到页面背景），吞掉即行为等价。
 */
private fun Modifier.blockTouches(): Modifier = pointerInput(Unit) {
    awaitEachGesture {
        while (true) {
            val event = awaitPointerEvent()
            event.changes.forEach { change -> change.consume() }
            if (event.changes.none { it.pressed }) break
        }
    }
}
