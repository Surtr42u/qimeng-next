package media.qimeng.next.screen

import androidx.compose.foundation.background
import androidx.compose.foundation.gestures.awaitEachGesture
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.WindowInsets
import androidx.compose.foundation.layout.consumeWindowInsets
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.statusBars
import androidx.compose.foundation.layout.statusBarsPadding
import androidx.compose.material3.MaterialTheme
import androidx.compose.runtime.Composable
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.SideEffect
import androidx.compose.runtime.compositionLocalOf
import androidx.compose.runtime.getValue
import androidx.compose.runtime.key
import androidx.compose.runtime.mutableStateListOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.saveable.listSaver
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.saveable.rememberSaveableStateHolder
import androidx.compose.runtime.setValue
import androidx.compose.runtime.snapshots.SnapshotStateList
import androidx.compose.runtime.toMutableStateList
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

/** 顶层 Tab 场景键，与 AppRoot 里 `"tab:$tab"` 的取值同源（导航栏四项，模型只产生 0..3）。 */
internal val TAB_SCENE_KEYS = listOf("tab:0", "tab:1", "tab:2", "tab:3")

/** 是否顶层 Tab 场景键。pushed 路由（asset:/search/collection:…）不参与常驻层。 */
internal fun isTabSceneKey(key: String): Boolean = key.startsWith("tab:")

/** 常驻层的一层：[active] 表示它当前就是外层传入的 activeKey。 */
internal data class ResidentTabLayer(val key: String, val active: Boolean)

/**
 * 本轮要保持组合的 Tab 层（纯函数，单测锁定）。
 *
 * - [visited] 是已访问集（rememberSaveable 跨进程恢复，等价旧版 QimengNavHost.kt:220-225
 *   的 visitedTabs，以及本宿主上一版只记首页的 homeSeen）；
 * - 当前 [activeKey] 若仍是 Tab 键，即使还没入集也要立即上树：首次切到该 Tab 的同一帧
 *   就该有内容（旧版同样是首访才 add，但 add 与显示同一帧）；
 * - 按 [TAB_SCENE_KEYS] 固定顺序输出，跨帧 key 分组身份稳定，`key(tabKey)` 不会因新
 *   Tab 插入而交换 owner/remember 槽位；
 * - 非 Tab 脏键（pushed 路由或旧存档）不占常驻层；未知 Tab 键只有它自己就是活动键时才
 *   上树，避免恢复出的脏键凭空组合一屏。
 */
internal fun residentTabLayers(visited: List<String>, activeKey: String): List<ResidentTabLayer> {
    val activeTab = activeKey.takeIf(::isTabSceneKey)
    val resident = TAB_SCENE_KEYS.filter { it in visited }
    val all = if (activeTab != null && activeTab !in resident) resident + activeTab else resident
    return all.map { ResidentTabLayer(it, it == activeTab) }
}

/**
 * 当前场景是否为可见的前台场景。常驻 Tab 被其它 Tab 或 pushed 路由盖住时由
 * [ResidentSceneHost] 提供 false，用来让场景内的独立窗口（筛选面板、相册批量面板）随
 * 场景隐藏而撤下，并阻止隐藏网格续页/预取/冻结 fling——普通（非常驻）场景没有 provide，
 * 默认 true 保持原行为。
 */
internal val LocalSceneActive = compositionLocalOf { true }

/**
 * 场景宿主（2026-11-22 四 Tab 常驻批）：所有访问过的顶层 Tab 场景（tab:0..tab:3）保持组合。
 *
 * 旧版（qimeng-media）四个顶层 Tab 都是常驻屏：首次进入才加入常驻层，此后永不离树，
 * 切换只翻转可见性——zIndex + graphicsLayer alpha=0 + clearAndSetSemantics + 触摸死层
 * （QimengNavHost.kt:657-733，死层实现 ResidentTabScreen.kt:80-88）。隐藏屏保留滚动位置、
 * 已加载网格与缩略图解码，切回是零重建。本宿主逐条沿用这套隔离设计（隐藏 Tab 0f、触摸
 * 死层 0.5f、活动 Tab 1f、pushed 路由 2f），本批把上一版「只常驻首页」推广到全部已访问
 * Tab。
 *
 * 数据仍按需加载：Tab 只有在首次成为 activeKey 的那一帧才首次组合，所以冷启动不会四份
 * 推荐/榜单首屏请求同时发出（首页仍是第一个 Tab 的加载者）。已访问集用 rememberSaveable
 * 保存，进程恢复行为与上一版 homeSeen 相同：用过的 Tab 恢复后仍拿回自己的常驻层。
 *
 * 生命周期：每个常驻 Tab 一个 [SceneLifecycleOwner]——仅当它就是 activeKey 且 Activity
 * 处于前台时封顶 RESUMED；被其它 Tab 或 pushed 路由盖住压到 CREATED；宿主离树 DESTROYED。
 * `collectAsStateWithLifecycle`、`repeatOnLifecycle` 预取因此随隐藏停摆；BackHandler 由
 * activity-compose 1.10 的 lifecycle-aware 注册在 CREATED 时不会进入 dispatcher（隐藏 Tab
 * 收不到返回键），场景内再以 LocalSceneActive 显式门控面板与续页。
 *
 * 顶部安全区：外壳不再按路由加减 statusBarsPadding——每个常驻 Tab 层自己
 * sceneTopInset()，被任何路由盖住时测量尺寸都不变，网格不会按旧 key 停到另一行；[chrome]
 * 只渲染在活动场景内部（pushed 路由页头），Tab 层没有页头。详情（asset:）是沉浸式场景，
 * 自管系统栏，外壳不给它加顶部避让。
 *
 * SaveableState 口径与 core/ui 的 SingleSceneHost 一致：每个常驻 Tab 用固定键
 * SaveableStateProvider("tab:N")，pushed 路由按 activeKey 换 provider、只保留最近 12 键
 * （Tab 键不参与淘汰）。
 *
 * 内存代价（接受）：旧版同样让四个 Tab 屏常驻，隐藏 Tab 仍参与测量并保留各自的网格、
 * facet 实例与已解码缩略图；这正是「切换零重建」的前提。隐藏层自身不发起新的续页/视口
 * 预取/图片请求（生命周期压到 CREATED），但场景里已在途的请求会跑完。
 */
@Composable
internal fun ResidentSceneHost(
    activeKey: String,
    modifier: Modifier = Modifier,
    chrome: @Composable (String) -> Unit = {},
    content: @Composable (String) -> Unit,
) {
    val holder = rememberSaveableStateHolder()
    var recentDetailKeys by rememberSaveable { mutableStateOf(emptyList<String>()) }
    // 已访问 Tab 集：只增不减。rememberSaveable 让进程恢复/旋转后直接恢复常驻集，
    // 恢复帧即显示正确 Tab（与旧版 visitedTabs 的 listSaver 同口径）。
    val visitedTabs = rememberSaveable(
        saver = listSaver<SnapshotStateList<String>, String>(
            save = { it.toList() },
            restore = { it.toMutableStateList() },
        ),
    ) { mutableStateListOf<String>() }
    val layers = residentTabLayers(visitedTabs, activeKey)
    val parentLifecycle = LocalLifecycleOwner.current.lifecycle
    SideEffect {
        // 与 SingleSceneHost 同款：只有 pushed 路由键（含 ':' 且不是 tab:）参与最近 12 个淘汰。
        if (':' in activeKey && !activeKey.startsWith("tab:") && recentDetailKeys.lastOrNull() != activeKey) {
            val keys = recentDetailKeys.filterNot { it == activeKey } + activeKey
            keys.dropLast(12).forEach(holder::removeState)
            recentDetailKeys = keys.takeLast(12)
        }
    }
    Box(modifier.fillMaxSize().clipToBounds().background(MaterialTheme.colorScheme.background)) {
        // 第一层：全部已访问 Tab 常驻。每层固定 key，切 Tab / pushed 路由只改可见性，
        // 内容子树（网格、facets、pager）不重建。
        layers.forEach { layer ->
            key(layer.key) {
                val tabActive = layer.active
                // per-tab 场景 owner：隐藏时压到 CREATED（等价 ON_STOP 之后），
                // collectAsStateWithLifecycle、GalleryViewport 的 repeatOnLifecycle 预取、
                // BackHandler 都会像普通 Tab 被停掉一样停摆；可见时按父（Activity）状态
                // 封顶到 RESUMED，回前台仍受系统暂停约束。
                val owner = remember(parentLifecycle) { SceneLifecycleOwner(parentLifecycle, tabActive) }
                DisposableEffect(owner) {
                    owner.attach()
                    onDispose { owner.dispose() }
                }
                SideEffect {
                    // 可见即视为「展示过」：首次组成该 Tab 的同一帧就记下，之后退回其它 Tab
                    // 或 pushed 路由都保留常驻层。写入触发一次重组，层集合不变。
                    if (tabActive && layer.key !in visitedTabs) visitedTabs += layer.key
                    owner.setVisible(tabActive)
                }
                CompositionLocalProvider(
                    LocalLifecycleOwner provides owner,
                    // 场景内独立窗口与隐藏 Tab 的交互门控（见 LocalSceneActive KDoc）。
                    LocalSceneActive provides tabActive,
                ) {
                    Box(
                        Modifier.fillMaxSize().zIndex(if (tabActive) 1f else 0f).then(
                            if (tabActive) {
                                Modifier
                            } else {
                                // 隐藏 Tab 三重隔离之一、之二：不绘制 + 语义树不可达（对齐旧版 hide 的 GONE）。
                                Modifier.graphicsLayer { alpha = 0f }.clearAndSetSemantics { }
                            },
                        ),
                    ) {
                        // 常驻层自己避开顶部安全区，且与当前被盖路由无关：外壳不再按路由加减
                        // statusBarsPadding，隐藏层的高度因此不随覆盖它的页头/沉浸式详情变化。
                        Box(Modifier.fillMaxSize().then(Modifier.sceneTopInset())) {
                            holder.SaveableStateProvider(layer.key) { content(layer.key) }
                        }
                    }
                }
            }
        }
        // 之三：触摸死层。只要存在隐藏 Tab 就声明在它们之后、活动场景之前，zIndex 把它压在
        // 隐藏 Tab（0f）与活动 Tab（1f）/ pushed 路由（2f）之间。它只在上层没有接住触摸时
        // 兜底（Compose 命中测试中无 pointer input 的节点不拦截），把落在隐藏 Tab 上的幽灵
        // 点击/滚动整段吞掉。
        if (layers.any { !it.active }) {
            Box(Modifier.fillMaxSize().zIndex(0.5f).blockTouches())
        }
        // 第二层：活动 pushed 路由。不透明底色保证隐藏 Tab 不会从活动页的留白处透出。
        if (!isTabSceneKey(activeKey)) {
            Box(Modifier.fillMaxSize().zIndex(2f).background(MaterialTheme.colorScheme.background)) {
                // chrome 只属于活动场景：外壳页头/提示条不作为常驻层的兄弟占高度，
                // 常驻 Tab 的测量尺寸因此固定。详情路由是沉浸式场景，自管系统栏，不做顶部避让。
                Column(
                    Modifier.fillMaxSize().then(if (isImmersiveScene(activeKey)) Modifier else Modifier.sceneTopInset()),
                ) {
                    chrome(activeKey)
                    holder.SaveableStateProvider(activeKey) { content(activeKey) }
                }
            }
        }
    }
}

/**
 * 顶部安全区：在外壳场景层统一避让，并声明这部分 inset 已被消费。自管顶部栏的场景
 * （QimengTopBar 内部按 TopAppBar 默认 insets 避让）读到的剩余 statusBar inset 为 0，
 * 不会二次让位；外壳因此不必再按路由区分「谁自己避让」。
 */
@Composable
private fun Modifier.sceneTopInset(): Modifier =
    statusBarsPadding().consumeWindowInsets(WindowInsets.statusBars)

/**
 * 沉浸式场景：详情（asset:）会临时隐藏系统栏、自管避让，外壳不给它加顶部安全区
 * （与改前的渲染一致）；其余场景都在外壳层避让。
 */
private fun isImmersiveScene(key: String): Boolean = key.startsWith("asset:")

/**
 * 场景生命周期 owner：把父（Activity）生命周期按「本场景是否可见」收窄成场景自身的
 * 生命周期。隐藏固定 CREATED，让依赖 LocalLifecycleOwner 的收集器真正停摆；可见按父
 * 状态封顶到 RESUMED（Activity 进后台时仍会跟着降到 CREATED）。dispose 置 DESTROYED
 * 并摘掉父观察者，避免宿主离树后残留监听。
 *
 * 每个常驻 Tab 各持一个实例；activity-compose 1.10 的 BackHandler 以 LocalLifecycleOwner
 * 注册，隐藏 Tab（CREATED）的回调不会进入 OnBackPressedDispatcher。
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
 * 不可交互」的隔离场景（常驻层隐藏 Tab 的兜底层）——命中测试层面它带 pointer input 节点，
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
