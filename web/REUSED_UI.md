# 旧版表现层复用记录

授权日期：2026-11-17。用户明确要求“ui可以用旧的，你把旧版ui的背后代码优化一下就行了，不要导致ui有区别只有我同意的那些”。本记录只涉及布局、CSS、图标和纯展示 JSX；业务状态、API、存储及服务端实现不从旧项目迁入。

实际视觉基准为旧仓 d0215962 的云端合成截图，run 43879314935 / artifact web-browser-evidence，已只读保存到 build/reference-ui/d0215962。该目录不是应用素材，不打包进产品。已观察 settings-desktop-actions、creator-profile-card 与 authors-mobile；其余页面按改动逐项对照。

| 旧项目来源 | 新项目位置 | 复用范围与适配 |
|---|---|---|
| web/src/tokens.css | ui/react/tokens.css | 原始完整视觉 token；保留旧版深浅色值、字号、间距与层次 |
| web/src/styles/glass.css | web/src/styles/legacy-ui.css | 原始完整表现层 CSS，含 1.1 倍整体缩放、72px 侧栏、64px 顶栏、16:9 封面 |
| web/src/components/shell/icons.tsx | web/src/components/LegacyShellIcons.tsx | 原内联 SVG 纯展示图标 |
| web/src/components/media/BrowseToolbar.tsx | web/src/components/LegacyBrowseToolbar.tsx | 紧凑浏览标题 JSX，补键盘进入标题标记 |
| web/src/components/shell/MineContentTabs.tsx | web/src/components/LegacyMineTabs.tsx | 原三分区展示与键盘交互，类型改为新本地展示契约 |
| web/src/components/shell/Sidebar.tsx、TopBar.tsx、AppShell.tsx | web/src/app/App.tsx | 复用四项侧栏、原顶栏与悬浮操作的位置/文案/DOM；路由、刷新、主题持久化和返回逻辑由新实现提供 |
| web/src/components/media/MediaCard.tsx | web/src/components/MediaGrid.tsx | 原封面/时长/标题/作者/日期的展示结构；缩略图 URL 和内容版本由新 data 层提供 |
| web/src/pages/HomePage.tsx 的展示片段 | web/src/pages/HomePage.tsx | 为你推荐/COS 推荐/内容热榜的紧凑页面，读取新推荐/榜单 SDK |
| web/src/pages/MinePage.tsx 的展示片段 | web/src/pages/MinePage.tsx | 原库存摘要、关注/收藏/历史布局；查询与受影响事件数据重新接线 |
| web/src/pages/CollectionPage.tsx、components/author/CreatorProfileCard.tsx | web/src/pages/AuthorPage.tsx | 作者主页、精简X主卡、中文/原文、原本地/新作分区；真实资料读取改走新SDK |
| web/src/pages/CreatorProfilePage.tsx、components/author/CreatorProfileDetails.tsx、CreatorProfileEditor.tsx | web/src/pages/ProfileDetails.tsx、ProfileBlockEditor.tsx | 独立资料子页与四块只读概览、单块编辑、X单框语言切换；旧后端hook未复用，独立修订与候选保存用新服务 |
| web/src/pages/AuthorsPage.tsx、components/manage/AuthorEditDialog.tsx、SourceSelectField.tsx | web/src/pages/AuthorsPage.tsx、AuthorEditDialog.tsx、components/AcquisitionSourceField.tsx | 原作者行列表/分类弹窗/来源快捷选项；排序与精确总数来自服务端，来源通过新修订契约整体保存 |
| web/src/pages/AlbumsPage.tsx、SearchPage.tsx、components/filters/PanelFilters.tsx | web/src/pages/BrowsePage.tsx、components/LegacyFacetFilter.tsx、LegacyPanelFilters.tsx | 原四维胶囊/七行更多筛选/日期组/搜索类型行；Go提供opaque候选键、区间边界与精确计数 |
| web/src/components/ui/select.tsx、switch.tsx、confirm-dialog.tsx、loading-hint.tsx、skeleton-grid.tsx、page-heading.tsx | web/src/components/LegacySelect.tsx、LegacySwitch.tsx、ConfirmDialog.tsx、LegacyLoading.tsx、LegacyPageHeading.tsx | 原Radix展示行为及纯DOM骨架，不导入旧业务hooks |
| web/src/pages/DataPage.tsx、BrowsingReviewPage.tsx、components/data/DonutCard.tsx、TrendChart.tsx、BrowsingReviewContent.tsx、ReviewInterestCard.tsx | web/src/pages/DataPage.tsx、components/LegacyDonutCard.tsx、LegacyTrendChart.tsx、LegacyReviewContent.tsx、LegacyReviewInterestCard.tsx | 保留指标/趋势库存/回顾占比/三榜的排列与图表；消费新SDK字段，客户端只做图形缩放与格式化 |
| web/src/pages/SettingsPage.tsx、components/manage/SourceVocabularyCard.tsx | web/src/pages/SettingsPage.tsx、AcquisitionVocabularyPage.tsx | 原设置卡与保存/退出位置，原四推荐预设点选即保存；新设置请求保留在途新草稿 |
| web/src/pages/LibraryManagePage.tsx、components/manage/ManagePageHeader.tsx | web/src/pages/ManagePage.tsx、components/LegacyManageHeader.tsx | 原三组八入口与管理返回标题，不增加主导航 |
| web/src/pages/LibraryRegistryPage.tsx | web/src/pages/LibrariesPage.tsx | 原库卡、注销确认与常驻注册表单；根路径映射受信存储，登记和扫描分别记账，活动文件数与扫描状态由 Go 下发。 |
| web/src/pages/BackupImportExportPage.tsx、components/manage/MaintenanceFolderCard.tsx、BackupCard.tsx、TxtAuthorImportCard.tsx、AuthorMirrorCard.tsx | web/src/pages/BackupPage.tsx、V2ImportCard.tsx、AuthorListsCard.tsx、AuthorMirrorCard.tsx | 原维护根目录卡、备份恢复/作者 TXT/上传归档三个页签、导入区、冲突操作和镜像配置。FSA 双代写入、快照、TXT 与 v2 迁入均重新接线。 |
| web/src/components/manage/DirTree.tsx、ServerImportPanel.tsx 与 components/author/CreatorNewWorks.tsx、CreatorProfileEditor.tsx 的订阅片段 | web/src/components/DirectoryTree.tsx、pages/ServerImportPanel.tsx、CreatorSubscription.tsx | 恢复分层目录行/计数、服务器目录投放区与主动订阅呈现。目录按视口/分页读取，作者订阅只有明确点击检查才访问来源。 |
| web/src/pages/LibraryUploadPage.tsx、components/manage/UploadWorkbench.tsx、UploadDestinationCard.tsx、UploadMetadataCard.tsx、AuthorSuggestField.tsx、UploadQueueList.tsx | web/src/components/AuthorSuggestField.tsx、pages/UploadPage.tsx、UploadDestinationCard.tsx、UploadQueueList.tsx | 原上传位置与目标库胶囊、作者与来源折叠、拖放和全宽队列。新 data 层封存每批目标及输入，Go 在入库事务内追加已选常规作者来源。 |
| web/src/components/manage/DirBrowser.tsx、DirFileList.tsx、MoveDialog.tsx、CreateDirDialog.tsx、components/ui/multi-select-bar.tsx、batch-ops-panel.tsx | web/src/pages/FileManagementPage.tsx、FileManagementDialogs.tsx | 原目录/文件行、单项和批量移动/回收展示；状态和幂等编排全新，树形行可见时分页读取下层，Go 返回每行直接文件数 |
| web/src/pages/AssetDetailPage.tsx、components/detail/AssetFileName.tsx、AuthorCard.tsx、UpNextList.tsx、AssetTagRow.tsx、FileOpsButton.tsx | web/src/pages/AssetPage.tsx、AssetSidebar.tsx、AssetTagDialog.tsx、components/AssetFileName.tsx | 原媒体舞台/分页/标题与文件名/互动行/标签弹窗/作者与推荐右栏；计数、版本、关注、推荐与幂等写入经新data层 |
| web/src/components/media/image-viewer.tsx、lib/image-viewer-math.ts | web/src/components/ImageViewer.tsx、lib/viewer-geometry.ts | 原查看器DOM与视觉坐标手势，保留混合媒体查看意图；补背景inert、换件后的焦点归还与实际读取错误 |
| web/src/components/media/video-player.tsx、lib/player-labels.ts、player-fullscreen.ts | web/src/components/VideoPlayer.tsx、lib/player-controls.ts、player-seek.ts、player-fullscreen.ts | 原ArtPlayer控件、画质/倍速文案、全屏chrome与zoom坐标修正；不复制旧engagement-reporting/use-progress；内容版本进度与事件重新接线，外部字幕/标记通过安全DOM写入 |
| web/src/pages/AssetEditPage.tsx、components/manage/AuthorSuggestField.tsx | web/src/pages/AssetEditPage.tsx | 原独立编辑、联想、待新建清单与逐作者来源区；新协议在关联事务里创建或复用作者，保留 COS 固定关联，并按来源的独立回执保留失败草稿。 |
| web/src/lib/format.ts 的formatBytes/formatCount | web/src/lib/format.ts | 原大小单位与万计数展示；未知值仍明确未知，不以0填充 |

web/src/styles/legacy-base.css 补回旧UI原本消费的浏览器重置规则；legacy-adapter.css 连接新变量与原样式、处理类名冲突，并停用被新连续过渡接管的旧入场动画。没有新增 Web 底栏。独立 GlassNavigation 不改变宿主导航布局；新增 SelectionLens 只在既有侧栏/顶栏的实际选中元素下移动。

为恢复相同字形，@fontsource-variable/geist 精确锁为旧项目实际使用的 5.3.0（OFL-1.1）；为复用原页面图标，lucide-react 精确锁为 1.59.0（ISC，React 19 peer 可兼容）。两者已读取官方 npm registry 元数据，锁文件只由云端生成。

Radix使用旧实际版本 [1.6.7](https://registry.npmjs.org/radix-ui/1.6.7)（MIT）；图表使用旧实际版本 [Recharts 3.10.1](https://registry.npmjs.org/recharts/3.10.1)（MIT），其peer使用与React同版本的 [react-is 19.3.1](https://registry.npmjs.org/react-is/19.3.1)。2026-11-17已读取官方元数据，未本机安装。复制的包许可位于 public/licenses，云端负责传递依赖锁与构建验证。

播放器恢复旧实际 [ArtPlayer 5.4.0](https://registry.npmjs.org/artplayer/5.4.0)（MIT，传递依赖option-validator ^2.0.6），2026-11-17读取官方registry及[官方options](https://artplayer.org/document/en/start/option)、[实例属性](https://artplayer.org/document/en/advanced/property)。上游LICENSE保存在public/licenses/artplayer-MIT.txt。进度标记按固定5.4 DOM构造data-text/data-time，避免其option.highlight文本插入HTML；字幕标题同样用textContent。依赖锁与播放器实际浏览器验证仍由CI执行。

页面复用范围及同提交验证记录见 [UI 对照](../docs/ui.md)。设置、相册、详情、作者、上传和两代资料维护均有云端合成截图或交互用例。系统资源/服务流量、存储明细、客户端异常等维护页继续由真实协议接入。
