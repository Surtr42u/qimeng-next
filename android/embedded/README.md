# 可移除的 Android 本机运行模块

用户于 2026-11-17 明确保留本机离线与 PC/NAS 远程双模式。两种模式使用同一个新 Go 服务、OpenAPI、生成 SDK 和 Repository；本模块只负责 Android 进程、安装包、目录、权限及后台生命周期，不拥有媒体业务规则。

## 装配边界

- `:runtime-api` 只定义本机运行能力、生命周期状态和一次启动连接信息。
- `:embedded` 提供本机实现、Foreground Service 和受包管理的原生程序；不依赖 `core:data` 或业务 ViewModel。
- `app/src/embedded` 装配本机运行实现；`app/src/client` 装配只支持远程连接的实现。
- `core:data` 和 `sdk` 完全不引用 embedded / runtime-api。连接方式的选择只发生在 App 的连接入口，进入 Repository 后统一使用服务器会话。
- 计划两 flavor 为 `embedded` / `client`，使用同一新 applicationId；云端隔离验证，不在真实旧应用上切换。client-only 构建不得打包本机服务、媒体 CLI、Foreground Service 或本机存储权限。

## 原生产物契约

CI 从同一提交的新 Go `server/cmd/qimeng` 构建，不能复制旧服务二进制。所有原生产物均为目标 ABI 的 PIE ELF，校验 16 KiB LOAD 对齐。

| ABI 目录 | 文件 |
| --- | --- |
| `src/main/jniLibs/arm64-v8a/` | `libqimeng.so`, `libqimeng_runner.so`, `libffmpeg_cli.so`, `libffprobe_cli.so` |
| `src/main/jniLibs/x86_64/` | 同名、真正 x86_64 产物；不将 arm64 复制后依赖模拟器翻译 |

锁定 NDK **r28c / 28.2.13676358**，API 26 编译器分别为 `aarch64-linux-android26-clang`、`x86_64-linux-android26-clang`。Go 的 CGO / external link 与 modernc Android 兼容性须由云端实际编译和 App UID 下执行验证，不能用 Linux shell 成功替代。NDK 依据：[官方 NDK r28c](https://github.com/android/ndk/wiki/unsupported-downloads)。

APK 设置 `useLegacyPackaging=true`，安装后**仅执行** `ApplicationInfo.nativeLibraryDir` 内文件。禁止向 filesDir 解包程序再 chmod/exec。Android 对可写应用目录的执行限制见 [官方 Android 10 行为变化](https://developer.android.com/about/versions/10/behavior-changes-10#execute-permission)。

runner 只安装父死亡信号并 exec 同目录 Go：设置 `PR_SET_PDEATHSIG=SIGTERM` 前记录 `getppid()`、设置后复核，关闭父进程已退出的竞态。Java 创建与等待子进程在一个专用持久在线程内完成，避免父线程退出过早触发信号。runner 不承载业务、鉴权、数据库或网络规则。

## 启动与私有数据

- 默认回环地址为 `127.0.0.1:18431`，避开旧应用的 18430，不监听局域网。旧本机地址入口可配置 1024–65535 的端口；实际连接统一使用核验本次证书的 HTTPS。配置变化须停止自己持有的服务再启动，不能复用占用新端口的未知进程。
- 控制根：`context.filesDir/embedded-v1/control`；独立媒体根：`context.filesDir/embedded-v1/media`，注册为受信逻辑存储 `local`。初始化只创建本应用的新空目录，不触碰旧项目数据。
- `QIMENG_DATA_DIR=control`，`QIMENG_STORAGE_ROOTS={"local":"<media绝对路径>"}`，`QIMENG_LISTEN=127.0.0.1:18431`，`QIMENG_TIMEZONE=系统IANA时区`。
- `QIMENG_ADMIN_TOKEN` 是一次生成、Keystore 加密保存的随机 bootstrap 凭据，只交子进程环境和同 API 的会话交换，不放命令行、日志、SavedStateHandle 或备份。
- 媒体工具路径经 `QIMENG_FFMPEG` / `QIMENG_FFPROBE` 指定到受包目录。缺失工具必须报告实际格式能力不足，不把原件假作缩略图。
- 默认 `data` 存储仍由 Go 管理；`local` 是可供正常库注册流程选择的独立存储。本模块不通过直接写库代替 `POST /libraries`。

获系统“所有文件访问”授权后，宿主将规范化的主存储根作为逻辑 `phone` 一同交给 Go；尚未授权时不交出该根。权限 Activity、API 26–29 的旧存储授权与 API30+ 特殊授权清单均只存在 embedded 模块。`LocalFiles` 只提供授权态与一级目录浏览（包括点前缀），拒绝链接 / 越界，按 200 项有界分页；不扫描媒体或写图库。旧服务器设置的授权卡已接此能力。授权变化后下次恢复连接会先停止自己持有的旧服务，再以相同私有数据根重启并刷新 TLS pin。库管理的本机选择、注册、充电待扫和接收路径现已接入这些端口。

启动不能只依据端口已开放。同 Go `cmd` 在监听成功后原子写私有回执，环境为 `QIMENG_STARTUP_RECEIPT=<runtime>/ready.json`、`QIMENG_LAUNCH_ID=<本次随机ID>` 和 `QIMENG_EPHEMERAL_TLS=true`；内容为 `launchId`、`instanceId`、`listenAddress`、公开 `certificateDerBase64`，不含私钥或令牌。本机地址为 `https://127.0.0.1:18431/`，证书 SAN 必须包含 IP127.0.0.1。

宿主核验本次 ID、活的子进程、回执与 TLS 实例身份后，再走生成 SDK 的正常会话交换。`core:transport` 仅信任回执中的完整 leaf DER，HTTP 客户端的默认主机名校验保留；远程默认系统信任不变。这样即使 Go 在核验后退出、其他应用重绑端口，也无法收到 bootstrap 凭据。新启动刷新公开证书策略，旧 HTTP 客户端在 pin 改变后拒发 token；队列只保存目标实例、不保存证书副本或凭据。云端基础仪器已验证回执、本机实例身份与重启后的证书更新。

已认证本机实例更换端口后，App 在应用级作用域完成会话与队列交接：仅迁移同一实例且地址曾由私有启动回执确认的队列项，保留上传 ID、分片偏移、输入清单与事件 ID。其他地址的记录不因相同实例声明而移动；迟到的旧端点 Worker 不能覆盖新端点记录。此交接不依赖登录页面的生命周期。

原生输出默认直接排空，release 不保留输出。debug 也默认关闭诊断，只有合成仪器用例显式开启后才在内存暂存最多 32 KiB，并在读取时遮盖 bootstrap 凭据和私有路径；测试结束清空。云端 `44108516882 @ 14cb2ae` 已定位启动退出码 2：SQLite 开库时 modernc libc 发出 x86_64 的 lstat 系统调用 6，被 Android App seccomp 以 SIGSYS 拒绝。主 Go 构建边界负责驱动适配，不解除沙箱；此故障随后在 `35b8950` 的实际本机仪器中修复。

release 不保留子进程输出，因此启动失败的可读原因走回执旁的私有文件：`run()` 失败且受监督时，同 Go `cmd` 原子写入 `<runtime>/ready.json.failed`（0600、临时文件改名），只含本次 `launchId` 与已脱敏、截断到 400 字符的原因。配置的数据根、bootstrap 凭据、媒体工具路径以及任何 `/data`、`/storage` 路径都替换为占位符。宿主只在子进程先于回执退出、文件大小受限且 `launchId` 与本次启动一致时读取，把原因接在「本机程序启动失败（退出码 N）」之后；缺失或无效时保持原提示。这样控制目录的数据库损坏、所有权锁冲突或恢复未完成会直接显示，而不是通用的连接失败。

控制目录里的索引数据损坏时，重启运行层不会自愈。首页连接失败横幅在该状态（本机端点、运行层 `FAILED`、文案带退出码）补充第二动作「重建本机索引」，先弹确认对话框说明：原 `control` 目录改名为同父目录的 `control.bak-<时间戳>` 备份、不删除；媒体目录与原件保留；旧本机元数据、标签与整理记录不沿用；重建后可重新扫描导入。确认后停止自己持有的 host、改名备份（改名失败则保持原状并恢复启用标记）、清除已确认实例与端口（保留 Keystore bootstrap 与启用标记），再启动新实例，并以强制本机登录保存新实例会话。取消不做任何事，任何路径都不会自动重建或删除媒体。

## 充电扫描调度

`LocalScans` 的宿主记录只保存实例 / 库身份、预期修订、稳定操作 ID 和待提交结果，每库一条、最多 100 条；不保存媒体清单或业务数据库。默认仅充电时提交，WorkManager 持有约束与恢复；App 组合根提供执行器，以私有启动回执的 TLS pin 和实例 ID 核验同一 Go，再经生成 SDK 调用已有扫描 API。后台提交使用宿主 bootstrap 的正常 owner Bearer，不改变前台正在使用的 NAS 会话。

服务器返回 202 后宿主保存持久 scan ID，后续进度归 Go；拔电不取消已接受的扫描。提交中断沿同一幂等 key 核对，不创建新扫描。用户明确停止本机服务、系统不允许后台启动、实例身份不一致或本机记录写入失败时，保留待扫记录与明确状态；不会声称已扫描。重新启用本机服务恢复仍未提交的记录。旧设置开关已接通；库管理扫描按钮按正式库投影继续接入。仪器源码新增合成 PNG 与拔电 / 充电 / 提交后拔电流程，须待实际云端执行后确认。

## 生命周期与验证要求

前台服务拥有唯一 Process 句柄；开始、停止、失败与重试状态明确。用户停止发送 SIGTERM，并给 Go 现有 20 秒 graceful shutdown 留出余量；只对自己的进程句柄做超时终止，禁止用陈旧 PID 或占用端口杀任意进程。切换远程连接不复制 / 合并两端数据库；队列仍按原实例身份补传。

显式停止会关闭自动恢复标记，单纯回到 Activity 不会再次启动。失败提示与子进程退出分开：快速重试须先等待仍在清退的旧宿主结束，再开始下一次启动，避免把失败窗口当成已释放端口。

### 冻结自愈

系统把壳进程与子进程一起冻结后，子进程可能保持“进程活着、端口在听、调度停摆”，`Process.isAlive` 探不出。服务在就绪后每 5 秒探测一次免鉴权的 `GET /api/v1/instance`（连接与读取各 3 秒上限，不读媒体、不带请求体、不查数据库），并核对启动时记录的实例 ID；一次失败只等 2 秒复核，两次都无响应或身份不符才 `SIGKILL` 并由宿主线程原位重拉。误杀的代价只是这次干净重启；不杀则用户会一直面对加载失败。每次重启在 logcat 的 `QimengEmbedded` 标签下记录原因（`watchdog` / `foreground`）、尝试序号与上一进程存活时长。

壳层 `MainActivity.onStart` 在当前会话属于本机已拥有端点时，经 `LocalRuntime.requestHealthCheck()` 向服务发一次前台探测，覆盖“服务与子进程一起被冻结、看护任务当时跑不动”的情况。显式停止已清除自动恢复标记，回到前台不会重新拉起。看护调度器、当前子进程与看护句柄都带身份核对，`onDestroy` 取消看护并关闭调度器，旧看护不会杀掉新子进程。

为什么不用 `/healthz`：其处理器当前调用 `CheckReadiness`（SQLite ping），按“存活探针不查依赖”的旧口径不适合每 5 秒执行——长写事务会让它就绪等待，反而可能被误判为冻结。本模块改用同协议面已公开、从内存回答、启动握手时本就用于实例核对的 `/api/v1/instance`。若后续要求以 `/healthz` 为准，需要服务端把存活与就绪拆开，属于 `server/internal` 的独立改动。

云端需分别验证 client-only 依赖 / APK 内容、embedded 两 ABI 装配、App UID 下启动与就绪、SQLite 建库 / 重启恢复、合成图片视频 / 探测、SIGTERM、宿主退出、端口占用、连续启停与后台恢复。设备固定为 `qimeng_api35` / `emulator-5556`；不能把 arm64 编译成功当成 x86_64 或真实手机运行成功。冻结自愈的判定逻辑由 `:embedded:testDebugUnitTest` 覆盖；真实冻结/解冻与前台重拉仍须在模拟器或真机上单独执行并记录。

未来剥离步骤是关闭 embedded flavor / 移除装配模块，保留相同 API 数据层与 UI 远程模式。切换前先走权威备份 / 导出迁移，不能删除本机数据目录冒充完成剥离。本节是当前装配契约，源码与云端完成情况仍记录在根状态页。

库管理页已接通本机文件夹选择、授权根解析与充电扫描。注册确认后先用 Go 回执建立影库，再将待扫写入本机调度队列。注销使用 Go 的条件写接口，遇到可恢复项或活动上传会显示原因。云端 44238104432 的 android-local-ui 已通过基本启停、临时 TLS 与持久化；增强的目录注册、合成图片、充电及注销保留原件用例另行核验。
