# Android 客户端

独立 Kotlin / Compose 客户端，应用包名 `media.qimeng.next`。客户端不拥有媒体业务数据库或双向备份合并。用户已明确保留本机离线与远程双模式：可移除 embedded 模块运行同一个新 Go 服务，装配契约见 [本机运行模块](embedded/README.md)，当前仍在接通。业务与数据层从空目录编写；用户授权复用旧纯展示 UI 以保持界面一致，复用清单与边界见 [UI 来源](UI_SOURCES.md)。

## 分层

- `core/model`：端内展示、传输和队列模型，不计算推荐、排名、统计与命名规则。
- `sdk`：引用 `api/generated/kotlin/src/main/kotlin`，源码仅由云端按 OpenAPI 生成。
- `core/data`：生成 SDK 的唯一业务调用边界，Repository 将 DTO 映射成端内模型；媒体引擎经限定来源的认证客户端取字节。
- `core/ui`：独立于业务的主题、卡片、`SingleSceneHost`、`GlassBackdrop` 和 `LiquidNavigation`。可由其他 Compose 应用通过适配层复用。
- `app`：ViewModel 管理查询代际、会话与用户草稿；页面只调用 ViewModel，不引用 SDK。

主要导航保留首页、相册、数据、我的；上传、报告、作者、历史、收藏和设置经原类型入口进入。页面的保存状态与查询状态分别保留；换服务器清空旧实例的浏览数据，持久传输仍绑定原地址与实例身份。

2026-11-17 界面复核确认当前页面结构存在偏离，不能把功能接线称为旧版界面已保留。具体源码对照、纠偏顺序与截图缺口见 [现行界面对照](UI_PARITY.md)；后续严格恢复现行布局与操作，只加入用户授权的过渡和导航材质效果。

## 当前源码行为

登录恢复原本机 / NAS 双模式页。NAS 密码只用于交换可撤销会话，不保存密码；最近 5 个成功地址持久保留。旧默认登录偏好与成功登录跟随、服务器设置卡片顺序和本机地址预填流程已接回；预填地址在登录确认后才用于启动。本机入口启动独立运行模块，再以私有回执的临时 TLS leaf 证书认证连接；远程使用系统证书信任（或本机隧道的 loopback HTTP）。服务会话保存在 Android Keystore 加密的私有配置中，备份关闭，传输记录不保存令牌。客户端禁用重定向，不把媒体认证转送其他来源。设置中的退出会撤销服务器会话；更换连接仅清除本机选择，原目标传输保留。

自动发现先匿名核对最多 5 个已记住地址（每个至多 2 秒），再进行 12 秒有界 DNS-SD，最多保留 32 个候选。正式服务类型 `_qimeng._tcp`，使用 SRV 实际端口和 `txtvers=1/api=0.1.0/id/scheme/path=/api/v1`；可选 `origin` 必须是无凭据 / 查询 / 路径的 HTTPS 根地址。发现不发送密码或保存令牌，不改变 TLS 信任；选中候选后登录仍先回读实例 ID。HTTP 局域网候选不能绕过 HTTPS / 隧道登录约束。API34+ 使用服务更新回调，较旧版本顺序 resolve；离开登录页或退后台停止发现并释放监听。暂不把带接口作用域的 IPv6 地址去掉作用域后使用。依据：[Android NSD](https://developer.android.com/reference/android/net/nsd/NsdManager)、[服务 TXT 与地址 API](https://developer.android.com/reference/android/net/nsd/NsdServiceInfo)。解析与候选接线已写，云端真实发现和界面对照仍待验证。

浏览支持服务器推荐/COS推荐、换 seed、各周期排行、搜索、媒体类型、常规/COS、影库、标签、排序、收藏与作者筛选、游标续页和 2–5 列网格。列表只加载版本化缩略图，缺预览显示占位。详情提供原图缩放平移、单指左右切换、标签/备注/多作者编辑、收藏、每日点赞与作者入口。视频使用 Media3 官方 `PlayerView` 的单一 AndroidView 桥接；新写 Compose 手势提供双击播放暂停、水平进度、左右区域亮度/音量、全屏退出。时间轴标记可新建、编辑、删除和跳转；旧内容版本的标记/进度保持可见但不映射到新内容。

浏览与播放携带稳定会话身份；停留用单调时钟按前台区段记录，每 30 秒、暂停或离场入持久队列，不记录不足一秒的区段，后台不计时。进度是独立事件，不当作浏览次数。切换服务器时，离场记录仍带原实例目标。统计概况、趋势、24 小时浏览回顾与兴趣份额全部消费服务器结果，推荐偏好读取服务器四预设，保存成功后才更新选中态。

SAF 批选先保存待传快照，允许改名后提交；系统分享可用已选择的目标直接提交，未登录/未选择位置时保留待传项。普通上传为默认，智能整理显式选择。可浏览/创建库内目录、选择本批多作者、组图/短剧集合、获取渠道及备注；编号只由 Go 决定。文件提供器字节先复制到应用私有空间，以固定重试输入；原件不删除。WorkManager 按原操作身份恢复批次、上传句柄及服务器 offset，再传分片；失败项不阻塞其他独立文件。取消先持久化，成功入库与等待智能整理分别报告。读取失败的输入逐项保留本机结果。清理传输记录不删除服务器媒体。私有副本存在手机可用空间预算。

原始选择在读取字节前逐项取得稳定 inputId 并持久保存；每批至多 100 项，超出时完整拒绝，不截断。提交时把完整清单（包括读取失败项）先封存到 Go，再创建上传，清单指纹与输入次序一同持久化。无法读取原名的输入使用 nameState=unavailable 并省略原名，序号提示仅用于显示。可得的 SAF 原始修改时间随上传保留；无法得知时省略，不拿入队时间冒充。取消通过服务器输入接口确认，尚未拿到 uploadId 也可取消；发布已开始时保留待核对状态。取消确认前不清掉本机记录，任务报告按原始清单展示未到达输入，不以收到的文件数量伪报整批完成。

管理入口包括作者创建/编辑/关注/删除/合并、标签词表、原作与角色词表、影库启停、媒体改名移动和回收站恢复；永久删除是回收站内单独确认的操作。同组入口仍逐件展示媒体。常规作者与 COS 能力分开；修改携带基础修订，冲突保持草稿与明确错误，不静默覆盖。标签草稿保留打开时的关系修订，刷新不会自动给旧整组草稿套上新修订；全局已删除标签只能通过明确的“添加”请求重建。获取渠道和原作/出处是不同字段。

作者资料按 X / 基本资料 / 其他账号分别读取和保存，候选需用户载入草稿并明确保存；部分候选中缺失的字段保留当前值，显式空值表示清除。修改简介原文时不沿用未核对的旧译文。头像只加载当前实例认证的缓存地址，远程网址交由服务器获取；本地头像经系统文件选择上传。合并与删除保留档案为只读入口。AI 接入设置提供最小默认只读权限、明确写入范围和预算、一次显示凭据及撤销；凭据不进入保存状态、备份或日志。

## 材质和动效

`captureGlassBackdrop` 只录制导航下面的页面，避免导航递归采样。API 33+ 且硬件加速时，导航重绘真实场景图层，AGSL 根据移动选中胶囊位移采样坐标，实现折射；选中态用弹簧位移与拖动反馈。API 31–32 使用实时采样模糊，不称为折射；API 26–30、软件渲染及关闭玻璃时使用高对比实色材质。

`SingleSceneHost` 同时只保留一个可交互页面，按页面键从首帧初始化入场位移，避免快速切页保留旧页面与其触摸语义；保存容器恢复常驻页与最近 12 个详情页的草稿/滚动，作者和集合查询缓存也有数量上限。系统动画关闭或应用减少动效设置会停用位移及选择弹簧。导航有 Tab/选中语义，触摸目标不小于 48 dp，文字随系统字号，胶囊空隙消费触摸。

这描述源码设计，不替代实际 GPU 截图、帧时长、手势或设备验收。云端模拟器与真机验收须分别记录。

## 云端构建

仅云端执行：JDK 17、Gradle 8.11.1、Android SDK 35 / Build Tools 35.0.0。先运行仓库 SDK 生成脚本，再运行：

```sh
gradle -p android -Pqimeng.embedded=false :app:assembleClientDebug :app:testClientDebugUnitTest :core:model:test :core:data:testDebugUnitTest :core:ui:testDebugUnitTest :app:lintClientDebug --stacktrace
```

云端专用 API 35 模拟器再运行 `:core:ui:connectedDebugAndroidTest :core:data:connectedDebugAndroidTest`。这些测试覆盖快速切换只有一个活动页面、草稿恢复、减少动效、选择语义、大字/深色导航，以及队列重建、坏记录不伪装空队列、认证同源/重定向/断开边界；合成背景截图写入测试应用 external files 目录。JVM 测试另覆盖前后台停留区段、目标实例与查询代际。不得使用本机默认模拟器或手机，也不能访问旧 8420 实例。

真实链路仪器类为 `media.qimeng.next.test.AndroidRealServerTest`：只允许 runner 参数 `qimengServerUrl=http://127.0.0.1:18481/`，`qimengToken` 来自同 SHA 隔离 Go 服务的临时测试凭据。云端将短 H.264 合成视频放到设备 `/data/local/tmp/qimeng-next-test.mp4`；测试通过仅存在于测试 APK 的 ContentProvider 和真实 WorkManager 验证 PNG/视频入库、标签、收藏、推荐及播放历史，不 mock 服务。完整窗口截图写到目标 APK 的 `getExternalFilesDir("screenshots")`，包含实际 GPU 合成和视频表面。运行命令、显式设备选择及产物回收由云端工作流负责。

client 仪器任务为 `:app:connectedClientDebugAndroidTest`。本机原生产物按 embedded README 云端装配后构建 `:app:assembleEmbeddedDebug`；隔离清空合成应用数据，再单独运行 `:app:connectedEmbeddedDebugAndroidTest`，限定类 `media.qimeng.next.test.EmbeddedRuntimeTest` 且显式参数 `qimengEmbedded=true`。该测试验证受包 Go 在 App UID 下启动、TLS pin 与正常 SDK 登录、同一实例的库数据经停止 / 重启仍在，并要求新启动更换临时证书。不能用这条基础测试代替全部本机媒体 / 后台 / 迁移验收。

锁定版本见 `gradle/libs.versions.toml`。选择已知兼容的稳定版本；官方依据：[AGP 8.9 兼容矩阵](https://developer.android.com/build/releases/agp-8-9-0-release-notes)、[Compose 编译器与 Kotlin](https://developer.android.com/jetpack/androidx/releases/compose-kotlin)、[AndroidX 稳定发布](https://developer.android.com/jetpack/androidx/versions/stable-channel)、[Media3 发布说明](https://developer.android.com/jetpack/androidx/releases/media3)、[Coil 更新记录](https://coil-kt.github.io/coil/changelog/)、[Retrofit](https://square.github.io/retrofit/)、[OpenAPI Kotlin 生成器](https://openapi-generator.tech/docs/generators/kotlin/)。

## 验证和仍在接通的能力

本文件首次编写时尚未取得本提交的云端编译或设备结果。当前完整状态以仓库 `docs/status.md` 的提交与 CI 证据为准。

作者 TXT、完整备份导入/导出与重复/替换/研究冲突的具体决定入口仍待后续正式协议；不是取消需求。手机本机运行层按新授权独立实现，复用同一新 Go 服务，不引入旧业务或日常双向合并。当前普通传输队列固定字节输入并保留来源。来源端回收尚未接通，必须具备本批独立备份证据与可回收的来源能力；界面不会宣称已清理来源。推荐/统计/缩略图等源码接线不等于已通过当前版本端到端验证。真实系统文件提供器、分享授权撤回、进程终止恢复、服务器切换、GPU 折射、TalkBack 与厂商后台限制仍需独立验收。

实际读取资料：新仓 `AGENTS.md`、`README.md`、`docs/verification.md`、`docs/requirements.md`、`api/openapi.yaml`；规划需求 R01–R23 / 验收对照；旧 Android README 与 `GUIDE_UI` Android 相关段落（仅产品行为参考）；上列官方依赖与图形 API 文档。实现分工与模型证据由仓库 `docs/provenance.md` 统一记录。
