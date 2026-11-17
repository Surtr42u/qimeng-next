# Android 纯展示复用来源

用户于 2026-11-17 明确授权“UI 可以用旧的，把旧版 UI 背后代码优化，不导致未同意的界面区别”。这覆盖之前对纯展示 Kotlin 复用的限制；服务端业务、旧 ViewModel、旧 API、内嵌后端和旧客户端业务库仍不引入。

来源为 `qimeng-media` 提交 `d0215962bb1e9c29baabeed9fc9dc5c8b4b94555`，旧项目只读。下面路径均相对各自 Android 根目录。

| 旧纯展示文件（`core/ui/src/main/java/media/qimeng/app/core/ui/`） | 新文件（`core/ui/src/main/kotlin/media/qimeng/next/ui/`） | 适配 |
| --- | --- | --- |
| `theme/Color.kt`, `Dimens.kt`, `Shape.kt`, `Type.kt`, `Theme.kt`, `SoftDaylight.kt` | 同名 `theme/` 文件 | 仅包名；旧中性配色、字型、尺寸及日间阴影原值保留。根 `ui/Theme.kt` 为现有调用兼容转发。 |
| `glass/GlassColors.kt`, `GlassSurface.kt`, `GlassButtons.kt`, `PressScale.kt` | 同名 `glass/` 文件 | 包名 / 主题引用；这是旧静态展示玻璃与按压反馈，不将它宣称为新导航的背景折射。 |
| `component/QimengSegPill.kt`, `QimengSegmentedControl.kt`, `QimengSettings.kt`, `QimengPills.kt`, `QimengTitleRow.kt`, `QimengCapsuleTextField.kt` | 同名 `component/` 文件 | 包名、资源与主题引用；不含网络或业务算法。 |
| `icon/QimengIcons.kt`, `QimengManageIcons.kt`, `QimengDetailIcons.kt` | 同名 `icon/` 文件 | 包名与尺寸引用，保留原图标路径及 Material Icons 来源注释。 |
| `component/QimengTouchGuards.kt`、`QimengFormat.kt` 中计数格式函数 | `component/QimengTouchGuards.kt`、`CountFormat.kt` | 点击穿透隔离保持；计数参数拓宽为新 API 的 Long。 |

页面级已复用 / 适配：旧 `feature/detail/DetailChromeBars.kt`、`DetailChromeIcons.kt`、`DetailSections.kt` 与 `res/values/strings.xml` 放入新 app 的 `screen/` 与详情资源，参数改接新 Asset，点赞 / 收藏字段名按新模型映射；不引用旧 ViewModel。旧 `SettingsScreen` 的分组层级、`SettingsCards.CountCard` 与 `HomeScreen.HomeTopRow` 布局接入新页面；`QimengMediaGrid.AssetCard` 的 16:9 / 5dp / 24px 与仅搜索附标题呈现接入新 MediaTile。

以上说明实际来源，不代表页面已完全对齐。详情信息 / 作者编辑 / 播放器控制、相册与搜索、统计及上传仍需按 UI_PARITY 继续恢复和同数据基准比对。

旧纯展示注释中的历史工程路径仅说明来源，不转移其架构规则。新业务调用仍由生成 SDK → Repository → ViewModel → UI 完成。

为保留 `SoftDaylight` 原 `dropShadow` 阴影，将 Compose BOM 从 2025.04.01 锁至 **2025.09.01**（UI/Foundation/Runtime 1.9.2，Material3 1.4.0）。已读取 Google Maven 官方 POM 与 UI/Material3 AAR 元数据：均要求 compileSdk ≥35、AGP ≥8.6.0；Compose 1.9 lint 要求 AGP ≥8.8.2。现有 AGP8.9.2、Gradle8.11.1、JDK17、SDK35 满足，不复制旧工程构建脚本，也不升级其余依赖。

依据：[官方 Compose UI 1.9 发布说明](https://developer.android.com/jetpack/androidx/releases/compose-ui#1.9.0)、[原生阴影 API](https://developer.android.com/develop/ui/compose/graphics/draw/shadows)、[Google Maven BOM](https://dl.google.com/dl/android/maven2/androidx/compose/compose-bom/2025.09.01/compose-bom-2025.09.01.pom)。依赖兼容与页面效果仍需本提交云端构建 / 仪器 / 合成截图验证；读取元数据不等于通过构建。
