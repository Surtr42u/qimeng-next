# Android UI 复用来源

用户授权直接复用旧版纯展示 UI，保留实际页面与操作，只增加平滑过渡和底栏玻璃选中效果。旧版来源为 `qimeng-media@d0215962bb1e9c29baabeed9fc9dc5c8b4b94555`。下表路径相对各自的 Android 模块根；设备与截图情况见[界面对照与验收](UI_PARITY.md)。

| 旧版展示来源 | Next 落点及适配 |
| --- | --- |
| `core/ui/.../theme/{Color,Dimens,Shape,Type,Theme,SoftDaylight,ButtonColors}.kt`，`glass/{GlassColors,GlassSurface,GlassButtons,PressScale}.kt` | `core/ui/.../ui/theme/` 与 `ui/glass/` 保留颜色、尺寸、字型、日间阴影、卡面和按压反馈。 |
| `core/ui/.../component/Qimeng{SegmentedControl,SegPill,Settings,Pills,TitleRow,CapsuleTextField,TouchGuards}.kt`、`QimengScaffold.kt` 顶栏与计数格式函数 | 新 `ui/component/`；计数接 Go 的 Long，胶囊、标题、顶栏、筛选值区和分组均由这些共享组件渲染。 |
| `core/ui/.../component/QimengFilterSheet*.kt` 及筛选文字资源 | 新 `ui/component/QimengFilterSheet*.kt`、`filter_strings.xml`。保留行顺序、年份、标签长按确认、重置和应用。新草稿、分页和修订写入由客户端数据层管理。 |
| `core/ui/.../icon/Qimeng{Icons,ManageIcons,DetailIcons}.kt` | 新 `ui/icon/` 保留图形路径和来源注释。 |
| `HomeScreen.HomeTopRow`、`AllScreen`、`FavoriteScreen`、`HistoryScreen`、`QimengMediaGrid.AssetCard`、`FloatingTabDock.TabDockDefaults` | `MediaBrowser.kt`、`AlbumFilters.kt`、`GallerySections.kt` 和 `LiquidNavigation.kt`。保留 16:9、5dp 外距、24px 封面圆角、四维控件及原底栏几何。导航采样与选择动画为新实现。 |
| `feature/login/LoginScreen.kt`、`feature/settings/ServerSettingsScreen.kt`、`SettingsScreen` 与 `SettingsCards.CountCard` | 新登录/服务器/我的页面沿用双卡、候选行、密码、计数和分组。NSD、连接恢复、权限和充电任务经新独立模块接入。 |
| `feature/detail/{DetailChromeBars,DetailChromeIcons,DetailSections}.kt`、原详情资源及 `feature/search/SearchScreen.kt` | 同名详情组件、`detail_strings.xml` 与新搜索页；图像、视频、点赞与查询接新 Asset 和 SDK。播放器沿用系统 Media3 控件，横屏时保留同一解码输出。 |
| `feature/stats/{StatsScreen,StatsDetailScreen,StatsFormatters}.kt`、`QimengTrendLineChart.kt` 和 `QimengTrendMarker.kt` | 新统计页与 `core/ui/.../ui/chart/`。趋势和榜单由 Go 返回；Vico 以真实数据点实现点选。 |
| `feature/manage/{LibraryManageScreen,RegisterLibraryCard}.kt`、`feature/upload/{UploadScreen,UploadPanels,UploadDestinationSection,UploadPendingSection,UploadArchiveBatchSection}.kt` 与归档文件夹设置页 | 新库管理、上传、目录树、待传清单和归档区沿用原入口与控件。原始文件名、目标身份、目录解析、扫描调度和回执走新代码。 |
| `feature/author/{AuthorScreen,AuthorCollectionScreen,CreatorProfileCard,CreatorNewWorks,CreatorProfileDetails}.kt` | 新作者列表、作品集合、资料与维护、订阅区域；查询、编辑、候选、已确认账号与手动检查采用新 Go 服务。 |

## 依赖与模块边界

- `android/gradle/libs.versions.toml` 锁定 Compose BOM 2025.09.01、Material3 1.5.0-alpha01 与 Vico 2.2.0。原阴影和 Expressive 加载组件使用这些版本的公开接口。[Compose UI](https://developer.android.com/jetpack/androidx/releases/compose-ui#1.9.0) [Material3](https://developer.android.com/jetpack/androidx/releases/compose-material3#1.5.0-alpha01) [Vico 2.2.0 POM](https://repo.maven.apache.org/maven2/com/patrykandpatrick/vico/compose/2.2.0/compose-2.2.0.pom)
- 上传归档采用 Android SAF 的持久授权；可回收来源还需可核验的 MediaStore 条目、物理文件和版本。视频使用 Media3 官方 `PlayerView` 的纹理输出。[DocumentsContract](https://developer.android.com/reference/android/provider/DocumentsContract) [MediaStore](https://developer.android.com/reference/android/provider/MediaStore) [StructStat](https://developer.android.com/reference/android/system/StructStat) [Media3 输出视图](https://developer.android.com/media/media3/ui/surface)
- 新仓库没有引入旧 ViewModel、旧业务数据库或旧服务端。数据层消费云端从 OpenAPI 生成的 SDK；可剥离的本机运行模块托管与 NAS 相同的新 Go 后端。
