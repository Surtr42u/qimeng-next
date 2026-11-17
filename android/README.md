# Android 客户端

从空目录编写的 Kotlin / Compose 客户端，应用包名 `media.qimeng.next`。不包含手机后端、媒体业务数据库或双向备份合并。旧 Android 只用于核对产品行为与视觉约束，没有导入旧 Kotlin 源码。

## 分层

- `core/model`：端内展示、传输和队列模型，不计算推荐、排名、统计与命名规则。
- `sdk`：引用 `api/generated/kotlin/src/main/kotlin`，源码仅由云端按 OpenAPI 生成。
- `core/data`：生成 SDK 的唯一业务调用边界，Repository 将 DTO 映射成端内模型；媒体引擎经限定来源的认证客户端取字节。
- `core/ui`：独立于业务的主题、卡片、`SingleSceneHost`、`GlassBackdrop` 和 `LiquidNavigation`。可由其他 Compose 应用通过适配层复用。
- `app`：ViewModel 管理查询代际、会话与用户草稿；页面只调用 ViewModel，不引用 SDK。

主要导航保留首页、相册、数据、我的；上传、报告、作者、历史、收藏和设置经原类型入口进入。页面的保存状态与查询状态分别保留；换服务器清空旧实例的浏览数据，持久传输仍绑定原地址与实例身份。

## 当前源码行为

登录使用 HTTPS（或本机隧道的 loopback HTTP）；服务会话保存在 Android Keystore 加密的私有配置中，备份关闭，传输记录不保存令牌。客户端禁用重定向，不把媒体认证转送其他来源。设置中的退出会撤销服务器会话。

浏览支持服务器推荐/COS推荐、换 seed、各周期排行、搜索、媒体类型、常规/COS、影库、标签、排序、收藏与作者筛选、游标续页和 2–5 列网格。列表只加载版本化缩略图，缺预览显示占位。详情提供原图缩放平移、单指左右切换、标签/备注/多作者编辑、收藏、每日点赞与作者入口。视频使用 Media3 官方 `PlayerView` 的单一 AndroidView 桥接；新写 Compose 手势提供双击播放暂停、水平进度、左右区域亮度/音量、全屏退出。时间轴标记可新建、编辑、删除和跳转；旧内容版本的标记/进度保持可见但不映射到新内容。

浏览与播放携带稳定会话身份；停留用单调时钟按前台区段记录，每 30 秒、暂停或离场入持久队列，不记录不足一秒的区段，后台不计时。进度是独立事件，不当作浏览次数。切换服务器时，离场记录仍带原实例目标。统计概况、趋势、24 小时浏览回顾与兴趣份额全部消费服务器结果，推荐偏好读取服务器四预设，保存成功后才更新选中态。

SAF 批选先保存待传快照，允许改名后提交；系统分享可用已选择的目标直接提交，未登录/未选择位置时保留待传项。普通上传为默认，智能整理显式选择。可浏览/创建库内目录、选择本批多作者、组图/短剧集合、获取渠道及备注；编号只由 Go 决定。文件提供器字节先复制到应用私有空间，以固定重试输入；原件不删除。WorkManager 按原操作身份恢复批次、上传句柄及服务器 offset，再传分片；失败项不阻塞其他独立文件。取消先持久化，成功入库与等待智能整理分别报告。读取失败的输入逐项保留本机结果。清理传输记录不删除服务器媒体。私有副本存在手机可用空间预算。

管理入口包括作者创建/编辑/关注/删除/合并、标签词表、原作与角色词表、影库启停、媒体改名移动和回收站恢复；永久删除是回收站内单独确认的操作。同组入口仍逐件展示媒体。常规作者与 COS 能力分开；修改携带基础修订，冲突保持草稿与明确错误，不静默覆盖。标签草稿保留打开时的关系修订，刷新不会自动给旧整组草稿套上新修订；全局已删除标签只能通过明确的“添加”请求重建。获取渠道和原作/出处是不同字段。

## 材质和动效

`captureGlassBackdrop` 只录制导航下面的页面，避免导航递归采样。API 33+ 且硬件加速时，导航重绘真实场景图层，AGSL 根据移动选中胶囊位移采样坐标，实现折射；选中态用弹簧位移与拖动反馈。API 31–32 使用实时采样模糊，不称为折射；API 26–30、软件渲染及关闭玻璃时使用高对比实色材质。

`SingleSceneHost` 同时只保留一个可交互页面，按页面键从首帧初始化入场位移，避免快速切页保留旧页面与其触摸语义；保存容器恢复页面草稿和滚动。系统动画关闭或应用减少动效设置会停用位移及选择弹簧。导航有 Tab/选中语义，触摸目标不小于 48 dp，文字随系统字号，胶囊空隙消费触摸。

这描述源码设计，不替代实际 GPU 截图、帧时长、手势或设备验收。云端模拟器与真机验收须分别记录。

## 云端构建

仅云端执行：JDK 17、Gradle 8.11.1、Android SDK 35 / Build Tools 35.0.0。先运行仓库 SDK 生成脚本，再运行：

```sh
gradle -p android :app:assembleDebug :app:testDebugUnitTest :core:model:test :core:data:testDebugUnitTest :core:ui:testDebugUnitTest :app:lintDebug --stacktrace
```

云端专用 API 35 模拟器再运行 `:core:ui:connectedDebugAndroidTest :core:data:connectedDebugAndroidTest`。这些测试覆盖快速切换只有一个活动页面、草稿恢复、减少动效、选择语义、大字/深色导航，以及队列重建、坏记录不伪装空队列、认证同源/重定向/断开边界；合成背景截图写入测试应用 external files 目录。JVM 测试另覆盖前后台停留区段、目标实例与查询代际。不得使用本机默认模拟器或手机，也不能访问旧 8420 实例。

锁定版本见 `gradle/libs.versions.toml`。选择已知兼容的稳定版本；官方依据：[AGP 8.9 兼容矩阵](https://developer.android.com/build/releases/agp-8-9-0-release-notes)、[Compose 编译器与 Kotlin](https://developer.android.com/jetpack/androidx/releases/compose-kotlin)、[AndroidX 稳定发布](https://developer.android.com/jetpack/androidx/versions/stable-channel)、[Media3 发布说明](https://developer.android.com/jetpack/androidx/releases/media3)、[Coil 更新记录](https://coil-kt.github.io/coil/changelog/)、[Retrofit](https://square.github.io/retrofit/)、[OpenAPI Kotlin 生成器](https://openapi-generator.tech/docs/generators/kotlin/)。

## 验证和仍在接通的能力

本文件首次编写时尚未取得本提交的云端编译或设备结果。当前完整状态以仓库 `docs/status.md` 的提交与 CI 证据为准。

作者 TXT、完整备份导入/导出与重复/替换/研究冲突的具体决定入口仍待后续正式协议；不是取消需求。手机不重建旧内嵌或双向合并体系，当前普通传输队列固定字节输入并保留来源。来源端回收尚未接通，必须具备本批独立备份证据与可回收的来源能力；界面不会宣称已清理来源。推荐/统计/缩略图等源码接线不等于已通过当前版本端到端验证。真实系统文件提供器、分享授权撤回、进程终止恢复、服务器切换、GPU 折射、TalkBack 与厂商后台限制仍需独立验收。

实际读取资料：新仓 `AGENTS.md`、`README.md`、`docs/verification.md`、`docs/requirements.md`、`api/openapi.yaml`；规划需求 R01–R23 / 验收对照；旧 Android README 与 `GUIDE_UI` Android 相关段落（仅产品行为参考）；上列官方依赖与图形 API 文档。实现分工与模型证据由仓库 `docs/provenance.md` 统一记录。
