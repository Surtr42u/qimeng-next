# Android UI 复用来源

用户授权直接复用旧版纯展示 UI，默认展示以旧版实际代码为准，包含其已有的四档材质与选中动效。源码核对提交 `d0215962bb1e9c29baabeed9fc9dc5c8b4b94555` 与已安装原包提交 `745974081716a9d69a95002a41220e35e743d764` 的 Android Git 树均为 `56927453ab399db4d4ec403193995850ff6ea1dd`。下表路径相对各自的 Android 模块根。实际窗口及覆盖范围见[界面对照与验收](UI_PARITY.md)。

| 旧版展示来源 | Next 落点及适配 |
| --- | --- |
| `core/ui/.../theme/{Color,Dimens,Shape,Type,Theme,SoftDaylight,ButtonColors}.kt`，`glass/{GlassColors,GlassSurface,GlassButtons,PressScale}.kt` | `core/ui/.../ui/theme/` 与 `ui/glass/` 保留颜色、尺寸、字型、日间阴影、卡面和按压反馈。 |
| `core/ui/.../component/Qimeng{SegmentedControl,SegPill,Settings,Pills,TitleRow,CapsuleTextField,TouchGuards}.kt`、`QimengScaffold.kt` 顶栏与计数格式函数 | 新 `ui/component/`；计数接 Go 的 Long，胶囊、标题、顶栏、筛选值区和分组均由这些共享组件渲染。 |
| `core/ui/.../component/QimengFilterSheet*.kt` 及筛选文字资源 | 新 `ui/component/QimengFilterSheet*.kt`、`filter_strings.xml`。保留行顺序、年份、标签长按确认、重置和应用。新草稿、分页和修订写入由客户端数据层管理。 |
| `core/ui/.../icon/Qimeng{Icons,ManageIcons,DetailIcons}.kt` | 新 `ui/icon/` 保留图形路径和来源注释。 |
| `HomeScreen.HomeTopRow`、`AllScreen`、`FavoriteScreen`、`HistoryScreen`、`QimengMediaGrid.AssetCard`、`FloatingTabDock.TabDockDefaults` | `MediaBrowser.kt`、`AlbumFilters.kt`、`GallerySections.kt` 。封面为 16:9、5dp 外距、24px 圆角。网格顶距位于视口外，左右边距为 8dp。首页顶行使用原 BackdropGlassPanel 与 BackdropGlassIconButton。 |
| `app/navigation/{QimengNavHost,ClassicBottomBar}.kt`、`core/ui/glass/{FloatingTabDock,QimengBackdrop,BackdropGlassPanel}.kt`、`feature/settings/AppearanceScreen.kt` | Next 的 AppRoot 按原材质选择两套底栏，通过原采样库渲染。外观页恢复三个主题及四个材质。路由瞬切，首页点按芯片瞬时对齐，双击主标签回顶。 |
| `feature/login/LoginScreen.kt`、`feature/settings/ServerSettingsScreen.kt`、`SettingsScreen` 与 `SettingsCards.CountCard` | 新登录/服务器/我的页面沿用双卡、候选行、密码、计数和分组。NSD、连接恢复、权限和充电任务经新独立模块接入。 |
| `feature/detail/{DetailChromeBars,DetailChromeIcons,DetailSections}.kt`、原详情资源及 `feature/search/SearchScreen.kt` | 同名详情组件、`detail_strings.xml` 与新搜索页；图像、视频、点赞与查询接新 Asset 和 SDK。图片缩放与详情操作使用现有舞台。 |
| `74597408` 的 `feature/search/SearchScreen.kt` 及 `app/navigation/QimengNavHost.kt` 的 `onOpenTagSearch` | `app/.../screen/SearchScreen.kt` 恢复结果返回清词和点输入框保留查询的两条路径。统计标签的初始词、历史记录和详情返回由 Next 的查询与导航状态接入。 |
| `feature/detail/VideoStage.kt` 的海报和横滑，`video/{BiliPlayerView,PlayerMath}.kt`，`InstantSystemBars.kt` 与 `res/drawable/ic_player_*.xml` | Next `app/.../screen/VideoStage.kt`、`screen/video/` 和 `app/src/main/res/drawable/`。复用自绘双排播控、画质/倍速/比例/循环弹窗、手势和图标。固定横屏与排版态共用一个 Media3 播放器和表面。播放、进度与标记使用 Next 的版本化接口，手势松手才提交定位。 |
| `feature/stats/{StatsScreen,StatsDetailScreen,StatsFormatters}.kt`、`QimengTrendLineChart.kt` 和 `QimengTrendMarker.kt` | 新统计页与 `core/ui/.../ui/chart/`。趋势和榜单由 Go 返回；Vico 以真实数据点实现点选。 |
| `feature/manage/{LibraryManageScreen,RegisterLibraryCard}.kt`、`feature/upload/{UploadScreen,UploadPanels,UploadDestinationSection,UploadPendingSection,UploadArchiveBatchSection}.kt` 与归档文件夹设置页 | 新库管理、上传、目录树、待传清单和归档区沿用原入口与控件。原始文件名、目标身份、目录解析、扫描调度和回执走新代码。 |
| `feature/manage/{DataManageScreen,ThumbnailCacheScreen}.kt` 的缩略图缓存入口、双卡、占用行和进度样式 | `ManagementScreens.kt` 与 `ThumbnailCacheScreen.kt` 恢复相关提示与控件，由新数据层提供各实例的磁盘统计、清空和预取状态。 |
| `feature/manage/DeviceApplyScreen.kt` 的作者覆盖行及操作按钮 | `app/.../screen/DeviceDeliveryScreen.kt` 沿用「应用时覆盖作者资料」开关、默认关闭和保存草稿的操作。说明文字采用新 Go 预检的修订与人工保护口径。 |
| `feature/manage/BackupScreen.kt` 的「同步浏览数据」卡 | `MaintenanceScreen.kt` 与上传页共用 `ActivityEventCard.kt`，从同一个 Next 持久队列查看本连接的状态和单条处理操作。 |
| `feature/manage/{VocabularyEditScreen,VocabularyEditGroupsCard,VocabularyEditDialogs}.kt` | `ExtractionVocabularyScreen.kt` 恢复停用词卡、可展开的出处组及胶囊词条对话框，保存与重算使用 Next 当前连接的词表修订。 |
| `feature/author/{AuthorScreen,AuthorCollectionScreen,CreatorProfileCard,CreatorNewWorks,CreatorProfileDetails}.kt` | 新作者列表、作品集合、资料与维护、订阅区域；查询、编辑、候选、已确认账号与手动检查采用新 Go 服务。 |

## 依赖与模块边界

- `android/gradle/libs.versions.toml` 锁定 旧展示使用的 Compose BOM 2026.09.00、Material3 1.5.0-alpha28 与 Backdrop 2.0.1。图表继续使用 Vico 2.2.0。原阴影和 Expressive 加载组件使用这些版本的公开接口。[Compose UI](https://developer.android.com/jetpack/androidx/releases/compose-ui) [Material3](https://developer.android.com/jetpack/androidx/releases/compose-material3#1.5.0-alpha28) [Vico 2.2.0 POM](https://repo.maven.apache.org/maven2/com/patrykandpatrick/vico/compose/2.2.0/compose-2.2.0.pom)
- 上传归档采用 Android SAF 的持久授权；可回收来源还需可核验的 MediaStore 条目、物理文件和版本。视频的 AndroidView 桥接托管旧版自绘控件及 Media3 `PlayerView` 的 `SurfaceView`。播放器始终通过 Next 的同源认证传输读取对应版本。[DocumentsContract](https://developer.android.com/reference/android/provider/DocumentsContract) [MediaStore](https://developer.android.com/reference/android/provider/MediaStore) [StructStat](https://developer.android.com/reference/android/system/StructStat) [Media3 输出视图](https://developer.android.com/media/media3/ui/surface)
- 新仓库没有引入旧 ViewModel、旧业务数据库或旧服务端。数据层消费云端从 OpenAPI 生成的 SDK；可剥离的本机运行模块托管与 NAS 相同的新 Go 后端。
