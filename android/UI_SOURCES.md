# Android 纯展示复用来源

用户于 2026-11-17 明确授权“UI 可以用旧的，把旧版 UI 背后代码优化，不导致未同意的界面区别”。这覆盖之前对纯展示 Kotlin 复用的限制；服务端业务、旧 ViewModel、旧 API、内嵌后端和旧客户端业务库仍不引入。

来源为 `qimeng-media` 提交 `d0215962bb1e9c29baabeed9fc9dc5c8b4b94555`，旧项目只读。下面路径均相对各自 Android 根目录。

| 旧纯展示文件（`core/ui/src/main/java/media/qimeng/app/core/ui/`） | 新文件（`core/ui/src/main/kotlin/media/qimeng/next/ui/`） | 适配 |
| --- | --- | --- |
| `theme/Color.kt`, `Dimens.kt`, `Shape.kt`, `Type.kt`, `Theme.kt`, `SoftDaylight.kt`, `ButtonColors.kt` | 同名 `theme/` 文件 | 仅包名；旧中性配色、字型、尺寸及日间阴影原值保留。根 `ui/Theme.kt` 为现有调用兼容转发。 |
| `glass/GlassColors.kt`, `GlassSurface.kt`, `GlassButtons.kt`, `PressScale.kt` | 同名 `glass/` 文件 | 包名 / 主题引用；这是旧静态展示玻璃与按压反馈，不将它宣称为新导航的背景折射。 |
| `component/QimengSegPill.kt`, `QimengSegmentedControl.kt`, `QimengSettings.kt`, `QimengPills.kt`, `QimengTitleRow.kt`, `QimengCapsuleTextField.kt` | 同名 `component/` 文件 | 包名、资源与主题引用；不含网络或业务算法。 |
| `icon/QimengIcons.kt`, `QimengManageIcons.kt`, `QimengDetailIcons.kt` | 同名 `icon/` 文件 | 包名与尺寸引用，保留原图标路径及 Material Icons 来源注释。 |
| `component/QimengTouchGuards.kt`、`QimengFormat.kt` 中计数格式函数 | `component/QimengTouchGuards.kt`、`CountFormat.kt` | 点击穿透隔离保持；计数参数拓宽为新 API 的 Long。 |

页面级已复用 / 适配：旧 `feature/detail/DetailChromeBars.kt`、`DetailChromeIcons.kt`、`DetailSections.kt` 与 `res/values/strings.xml` 放入新 app 的 `screen/` 与详情资源，参数改接新 Asset，点赞 / 收藏字段名按新模型映射；不引用旧 ViewModel。旧 `SettingsScreen` 的分组层级、`SettingsCards.CountCard` 与 `HomeScreen.HomeTopRow` 布局接入新页面；`QimengMediaGrid.AssetCard` 的 16:9 / 5dp / 24px 与仅搜索附标题呈现接入新 MediaTile。

旧 `feature/login/LoginScreen.kt` 的双模式卡、地址卡、胶囊密码与提交区域复用到新 `screen/LoginScreen.kt`，原登录文案进入 `login_strings.xml`；私有本机启动与 NAS 密码交换均走新接口。自动发现尚未接通，在原信息位置明确显示进度说明，不伪报已搜索或放无效刷新操作。旧 `feature/search/SearchScreen.kt` 的顶行、三态布局、词条与结果筛选形式及结果输入触摸呈现接到新的 SearchViewModel；不复制旧查询业务。

以上说明实际来源，不代表页面已完全对齐。详情信息 / 作者编辑 / 播放器控制、相册、搜索日期分组、统计及上传仍需按 UI_PARITY 继续恢复和同数据基准比对。

旧纯展示注释中的历史工程路径仅说明来源，不转移其架构规则。新业务调用仍由生成 SDK → Repository → ViewModel → UI 完成。

为保留 `SoftDaylight` 原 `dropShadow` 阴影，将 Compose BOM 从 2025.04.01 锁至 **2025.09.01**（UI/Foundation/Runtime 1.9.2，Material3 1.4.0）。已读取 Google Maven 官方 POM 与 UI/Material3 AAR 元数据：均要求 compileSdk ≥35、AGP ≥8.6.0；Compose 1.9 lint 要求 AGP ≥8.8.2。现有 AGP8.9.2、Gradle8.11.1、JDK17、SDK35 满足，不复制旧工程构建脚本，也不升级其余依赖。

依据：[官方 Compose UI 1.9 发布说明](https://developer.android.com/jetpack/androidx/releases/compose-ui#1.9.0)、[原生阴影 API](https://developer.android.com/develop/ui/compose/graphics/draw/shadows)、[Google Maven BOM](https://dl.google.com/dl/android/maven2/androidx/compose/compose-bom/2025.09.01/compose-bom-2025.09.01.pom)。依赖兼容与页面效果仍需本提交云端构建 / 仪器 / 合成截图验证；读取元数据不等于通过构建。

云端 `011ace6` 证明稳定 Material3 1.4.0 不公开旧登录页使用的 Expressive `LoadingIndicator`。为保留原加载表现，单独锁 Material3 **1.5.0-alpha01**，不整套升级旧构建：Google Maven AAR 元数据要求 compileSdk35 / AGP8.6，POM 使用 Kotlin2.0.21、Compose UI1.8.2 / Runtime1.9.0-beta03，现有锁定版本均可覆盖。官方 sources 确认该版公开对应注解与组件。依据：[Material3 发布说明](https://developer.android.com/jetpack/androidx/releases/compose-material3#1.5.0-alpha01)、[该版官方 POM](https://dl.google.com/dl/android/maven2/androidx/compose/material3/material3-android/1.5.0-alpha01/material3-android-1.5.0-alpha01.pom)。新的云端编译与视觉结果仍待验证。

统计页与详情页的原布局、尺寸、系列颜色、数字格式来自旧 `feature/stats/StatsScreen.kt`、`StatsDetailScreen.kt`、`StatsFormatters.kt`；新 Repository 消费服务端汇总与 `getTopInterests`，不复制旧榜单混合算法。旧 `QimengTrendLineChart.kt` 的线 / 平涂面积 / 空心点 / 坐标标签形态与 `QimengTrendMarker.kt` 气泡呈现进入独立 `core/ui/chart/`。Vico 锁 **2.2.0**：官方 Maven POM 的 core1.16.0 / Kotlin2.1.21 与本工具链兼容，较新版本的 SDK / Kotlin 要求不在此次顺带升级。为适配该版无 markerController 的公开 API，点击直接取 Vico `CartesianLayer.markerTargets` 的真实布局坐标，再以 persistentMarkers 切换气泡；无自绘折线、无推算点击坐标。多系列放在一个图层模型中，保留原系列色。仪器测试覆盖点选 / 再点隐藏和多系列值，生成合成趋势截图；未运行前不称通过。

依据：[Vico 官方 POM](https://repo.maven.apache.org/maven2/com/patrykandpatrick/vico/compose/2.2.0/compose-2.2.0.pom)、[官方图层与 marker 说明](https://guide.vico.patrykandpatrick.com/android/core/cartesian-charts/cartesianmarker)；确切 2.2.0 API 另对照 Maven 官方 sources。

后续导航纠偏只引用旧 `FloatingTabDock.TabDockDefaults` 的 62dp 高、16dp 左右距、10dp 底距、18dp 槽距、24dp 图标及原文字色 / 字重 / 1.12 图标选中比例；新的采样折射与选择滑块独立实现，不引入旧 renderer 或状态层。列表底部让位恢复旧 80dp 基线加系统 inset；大字号时按实际标签高度增加，默认字号不改变原几何。应用减少动效贯通按压反馈、详情 chrome 和图表；原 34dp 胶囊输入保留默认高度，大字号允许自然增高。

旧 `feature/settings/ServerSettingsScreen.kt` 的服务器 / 本机 / 默认登录 / 仅充电扫描 / 存储权限卡顺序、文案、16dp 圆角与 16×12dp 内边距现用于新 `ServerSettingsScreen`；纯 `QimengScaffold.kt` 的 TopAppBar 进入 `core/ui/component/QimengTopBar.kt`。登录的发现状态、重新寻找与候选行恢复自旧 `LoginScreen` 的纯展示。未复制旧设置 ViewModel、地址推断、权限启动或扫描实现；新默认端持久化、NsdManager 发现、权限模块和充电待扫队列均独立编写。

首页与相册的万能筛选采用旧 `core/ui/component/QimengFilterSheet*.kt` 的布局、标签按钮和资源，接入新草稿、分页词表与带修订的删除动作。相册和收藏直接使用旧 `QimengTitleRow`、`QimengChipRow`、`QimengValuePillBlock`。计数、过滤和 COS 推荐来自新 Go 查询。
