# 验证与交付

## 执行位置

编译、SDK 生成、自动测试、文档校验与发布构建均由 GitHub Actions 执行。云端结果按实际运行的提交 SHA 记录。已取得的证据见[当前状态](status.md)。

本地允许源码阅读与编辑、静态差异检查、Git 提交/推送和读取云端日志。需要设备或 NAS 才能证明的行为单列，不能用 CI 冒充。

[Engineering checks](../.github/workflows/engineering.yml) 自动检查 `main`、`codex/**` 的推送和所有 PR。[Cloud verification](../.github/workflows/prepare.yml) 在 `main` 推送或 PR 包含 Markdown 以外的变更时自动运行。维护者也可在 Actions 里选分支手动启动完整工作流，随后核对运行的 SHA。合成界面的[Web 新旧对照](../.github/workflows/ui-reference.yml)和[Android 参考图采集](../.github/workflows/android-reference.yml)可单独调度，也会在主分支上各自列出的路径发生变化时运行。

手动运行时，`suite` 默认选择 `all`。只更新协议锁可选 `contracts`；排查单端可选 `backend`、`web`、`android` 或 `windows`。`backend` 包含独立 MCP 客户端。主分支和 PR 继续运行完整流程。

普通云测直接使用已有 Next 数据行核对旧回执、未分日票数与已退役写入口。维护者需要复现早期导入行为时可单独选择 `archived-import`。这个测试适配器只编入指定测试，发布二进制和原生验收均独立于它。

OpenAPI 生成的 Go、Kotlin、TypeScript 和 Python 契约随各组检查提供。仓库中的 `api/sdk.lock` 记录来源与每个生成文件的摘要。独立的 `lock-check` 将生成结果、Go 校验和及依赖锁与当前提交比较。Windows 使用 `npm ci` 和 Cargo 的锁定模式。收取云端产物时只提交所需的锁文件。

Kotlin SDK 在生成期间从已校验的官方模板渲染 Retrofit `@Streaming`。归档下载、作者总表、片段与可读作者资料均按字节流交付。云端会核对这些方法的实际签名，避免手机在收到大备份时先把整个响应放进内存。

需要更新 JavaScript／Rust 依赖时，在 Actions 手动运行并勾选 `refresh_locks`。协议变更可直接取同一提交的 `generated-contracts-and-locks` 更新来源锁；若锁有差异，独立的功能测试仍能给出诊断。带新锁的提交须通过完整检查才可交付。

### Ubuntu 验证作业的媒体工具

`server`、`live`、`mcp-compat`、`android-ui` 和 `android-local-ui` 共用[宿主 FFmpeg 安装步骤](../.github/actions/ubuntu-ffmpeg/action.yml)。运行器可执行 FFmpeg 和 FFprobe 时直接继续。需要安装时，APT 刷新运行器的 Ubuntu 签名索引，在空归档目录解析 `ffmpeg` 和必需依赖。缓存键包含 Ubuntu 版本、原生架构和本次全部归档名与 SHA-256 的摘要。每个归档名由 APT 按包名、版本和架构生成。[APT 归档身份与既有文件判定](https://github.com/Debian/apt/blob/2.7.14/apt-pkg/acquire-item.cc#L3274-L3325)

缓存只保存这次解析到的 `.deb`。每次安装都将候选归档复制到新的临时目录，对照本次签名索引逐件计算 SHA-256。缺件或摘要有异的归档交回 Ubuntu 下载，校验整批后由 APT 离线安装。首次并发的作业可以各自取包，安装成功后按同一键保存，后续作业可复用先保存的副本。缓存命中仍需更新索引和执行系统安装。[实现](../.github/actions/ubuntu-ffmpeg/prepare.sh) · [GitHub 缓存作用域与匹配规则](https://docs.github.com/en/actions/reference/workflows-and-actions/dependency-caching)

<a id="engineering-gates"></a>
## 代码与架构检查

工程作业核对仓库内文档路径与 Markdown 章节、SQL 迁移历史、Go 模块引用以及 `server/` 下受版本控制的 Go 文件格式。已有迁移文件按基线提交比较，新增版本须高于该基线已发布的最大值。不同功能分支有编号冲突时，只重排尚未发布的迁移。

工程作业配置了固定版本并校验下载摘要的 Gitleaks，扫描本次检出引用可达的 Git 历史。内置凭据规则和项目的个人目录规则命中时，日志遮盖匹配的值。公开前还需核对提交作者信息、人工文本与可下载的日志、截图和制品。具体运行结果绑定到对应提交。[扫描配置](../.gitleaks.toml) · [安全说明](../SECURITY.md)

Go 格式有差异时，该作业失败，并提供同一提交的 `go-format-correction` 补丁供维护者核对。正常 Go 与 Web 构建直接验证提交的源码。Web 的展示层引用随 `npm test` 检查。Android 的 `verifyClientArchitecture` 读取构建系统实际解析的依赖、源码集和客户端 APK，再由真服务或模拟器用例验证业务行为。两端的边界和适用例外见[Web 开发说明](../web/README.md)和[Android 架构检查](../android/architecture/README.md)。错误处理、职责分工和实际效率通过审查与行为用例验证。

比较性能时在同一云机器使用同一套合成数据、配置、冷暖状态和操作；记录提交、运行次数、耗时及该场景相关的内存或磁盘指标。扫描进度写入次数只反映该项数据库开销。跨版本、跨平台结论要有对应的测量范围。

### 手动扫描的同机对照

同一台 Ubuntu 24.04、4 vCPU 运行器按轮换顺序启动三个版本，每版三轮。每轮从新进程、空媒体索引开始，使用 16 个目录中的 2,048 张不同的 32×32 PNG，共 6,496,256 字节。每阶段先读热原件，再从手动 POST 发出计时到首次观察到成功终态，测试客户端以 10 毫秒间隔轮询。原样复扫后，原位改写 16 张、移动 8 张、新增 4 张，再次扫描。三个版本逐项核对路径、大小、mtime 和原有 ID，末次计时结束后回读 2,052 份物理原件核对长度与 SHA-256。两版 Next 另核对目录中的内容摘要与版本，16 个改写项各加一版，8 个移动项及其余 2,024 个原有项保持原版本，4 个新增项从第一版开始。独立的小样本用例在移动前通过 API 加标签和收藏，再按原资产 ID 回读。[合成数据与计时脚本](../tools/scan-comparison/compare.py) · [身份与人工资料用例](../tools/scan-comparison/identity_check.py)

包含原生内容版本、文件位置、标签身份和集合成员的 `034d2aa` 与分表前的 `b972bec`、已安装版在同场分三轮轮换。计时、目录核对、身份反例与扫描回归均通过。[本轮原始数据和回归日志](https://github.com/Surtr42u/qimeng-next/actions/runs/44351821418)

| 三轮中位数 | 已安装版 7459740 | 分表前 Next b972bec | 原生模型 Next 034d2aa |
|---|---:|---:|---:|
| 首次入库 | 5,639.48 ms | 1,983.57 ms | 2,905.30 ms |
| 原样复扫 | 66.45 ms | 220.02 ms | 88.57 ms |
| 少量改动后复扫 | 127.60 ms | 467.24 ms | 166.20 ms |
| 首扫期间进程写入 | 135.14 MiB | 55.70 MiB | 79.69 MiB |
| 截至首扫的进程峰值 RSS | 122.7 MiB | 34.0 MiB | 34.8 MiB |

新增 2,048 项时，原生模型比同场分表前版本多用 921.73 ms（46.5%），进程多写 23.99 MiB（43.1%）。比已安装版首扫缩短 48.5%，原样复扫和少量改动仍分别比安装版多用 22.12 和 38.60 ms。安装版三次首扫为 9,082.04、5,639.48、2,177.90 ms，原生版为 2,905.30、3,382.91、2,099.75 ms。本轮覆盖多个原生数据改动，耗时与写入差额反映整套变更。两版 Next 均逐项核对原件摘要，移动与缺件身份另用反例验证。[逐次耗时、写入与身份记录](https://github.com/Surtr42u/qimeng-next/actions/runs/44351821418/job/134969228999)

下表记录此前仅调整目录扫描路径时的同机测量。[目录扫描优化的原始数据](https://github.com/Surtr42u/qimeng-next/actions/runs/44326987188)

| 三轮中位数 | 已安装版源码 7459740 | Next 目录核对 fd38a66 | Next 目录内签名核对 5b558c7 |
|---|---:|---:|---:|
| 首次入库 | 1,286.11 ms | 800.66 ms | 794.34 ms |
| 原样复扫 | 37.25 ms | 94.51 ms | 79.96 ms |
| 少量外部改动后复扫 | 61.42 ms | 152.53 ms | 127.97 ms |
| 首扫期间进程写入 | 135.13 MiB | 58.09 MiB | 58.18 MiB |

这批小 PNG 上，5b558c7 首扫比安装版缩短 38.2%，原样复扫和少量变动分别仍需安装版的 2.15、2.08 倍时间。相对同场 fd38a66，按列读取签名与受控目录内核对使原样复扫缩短 15.4%，少量改动缩短 16.1%。手动普通库只对已登记的普通文件尝试这条路径，开始和结束都检查原目录的身份和修改时间，发生目录交换时回到实际注册路径核验。自动写通知、COS、新文件和内容变化仍按各自的复核流程处理。[三版实测与目录交换回归](https://github.com/Surtr42u/qimeng-next/actions/runs/44326987188) · [目录内签名核对](https://github.com/Surtr42u/qimeng-next/blob/5b558c7783a2193654d77767efcee220b6794149/server/internal/service/scan_known.go#L26)

在上一场独立对照中，fd38a66 按索引父目录分页核对缺件，对目录列表未确认的条目查原件，报告前再复核存续资产与路径。相对该场 efa6234，原样复扫从 115.84 降至 93.79 毫秒，少量改动从 173.99 降至 142.11 毫秒。五项小批写入与三项缺件回归在同一云作业执行。[缺件优化同机实测](https://github.com/Surtr42u/qimeng-next/actions/runs/44323980865) · [目录核对](https://github.com/Surtr42u/qimeng-next/blob/fd38a66cbc929392e8e7b3c6d5d36126608af939/server/internal/service/scan_missing.go#L152) · [缺件确认](https://github.com/Surtr42u/qimeng-next/blob/fd38a66cbc929392e8e7b3c6d5d36126608af939/server/internal/service/scan_missing.go#L217)

此前两场各自轮换三个版本，分别定位两类开销。在第一场，b3847e3 消除重复路径、资产和作者查询后，首扫、原样复扫及少量改动相比 8e225bc 分别缩短 15.7%、27.0%、21.6%。在第二场，从每个资产独立提交改为有界的小批事务后，首扫从 5,296.18 降至 1,146.53 毫秒，写入从 350.84 降至 58.12 MiB，分别减少 78.4% 和 83.4%。第二场原样复扫接近持平，少量改动慢 5.0%。各比例均使用同一场测量。[逐项写入优化](https://github.com/Surtr42u/qimeng-next/actions/runs/44316230983) · [小批事务单独比较](https://github.com/Surtr42u/qimeng-next/actions/runs/44322029440)

目录优化那场截至首扫完成的进程峰值 RSS 中位数按表中顺序为 120.7、28.1、28.2 MiB，包含启动和鉴权。安装版在扫描前设置密码时执行了 64 MiB 的 Argon2id 派生。Next 使用配置令牌。两种鉴权方式影响进程高水位。[安装版口令派生参数](https://github.com/Surtr42u/qimeng-media/blob/745974081716a9d69a95002a41220e35e743d764/server/internal/auth/password.go)

在只完成 0035 的早期独立对照中，首扫进程写入从分表前 55.78 增至 70.36 MiB，首扫中位数从 1,874.71 增至 2,089.07 ms。计时和身份核验已完成，该场末段的另一项测试因 Go 测试代码编译出错而未运行。[早期 0035 原始记录](https://github.com/Surtr42u/qimeng-next/actions/runs/44340327562)

三版均关闭文件监听、设 24 小时轮询，Next 的注册对账在投放原件前完成。三轮均在 30 秒缩略图静默期内结束，缓存无产物。运行器缺少 ffmpeg/ffprobe，安装版启动时的工具检查在计时前。Next 扫描 PNG 时会读取图片头并计算内容哈希。冷磁盘、大图、视频、有历史数据的库、NAS 与手机性能尚待对应环境测量。性能工作流可手动填写较早 Next 提交的完整 SHA，在同一台运行器上重做三方比对。[复跑工作流](../.github/workflows/scan-comparison.yml) · [优化前 CPU 样本](https://github.com/Surtr42u/qimeng-next/actions/runs/44312664840)

旧版此前两万行的测量覆盖内存索引构建或 SQL 快照装载，完整扫描的耗时还包括文件遍历、变更处理与持久化。[旧版扫描说明](https://github.com/Surtr42u/qimeng-media/blob/745974081716a9d69a95002a41220e35e743d764/server/README.md#媒体库后台扫描)

#### 手动扫描逐段核对

| 阶段 | 安装版 7459740 | Next |
|---|---|---|
| 遍历与复扫 | 手动首扫、复扫都遍历整个目录树。先按库一次读取 ID、相对路径、大小、mtime、行更新时间五列，在内存中记录哪些路径见过。已知路径大小和按毫秒格式化的 mtime 相同便跳过，无需逐文件 SQL。[快照、遍历与短路](https://github.com/Surtr42u/qimeng-media/blob/745974081716a9d69a95002a41220e35e743d764/server/internal/scanner/scanner.go#L271) | 手动请求同样遍历整库，目录每次读取 128 项，批量取该页路径签名，在受信根内核对文件元数据和保存的 mtime。普通库未变文件跳过内容读取，COS 目录归属不完整时仍需处理。[目录遍历](https://github.com/Surtr42u/qimeng-next/blob/efa62347b256e5a30488aae3e30e2e9d82272101/server/internal/service/scans.go#L291) · [签名查询](https://github.com/Surtr42u/qimeng-next/blob/efa62347b256e5a30488aae3e30e2e9d82272101/server/internal/service/scan_progress.go#L101) |
| 文件内容与元信息 | 扫描路径没有媒体内容哈希。图片和动图按路径、大小与 mtime 入库，尺寸交给后台回填；视频在库锁外调用 ffprobe。入库前重新核对大小和 mtime。[单文件入库](https://github.com/Surtr42u/qimeng-media/blob/745974081716a9d69a95002a41220e35e743d764/server/internal/scanner/scanner.go#L451) · [尺寸回填](https://github.com/Surtr42u/qimeng-media/blob/745974081716a9d69a95002a41220e35e743d764/server/internal/thumbnailwarmup/service.go#L259) | 新路径或大小、mtime 有变化时，将该文件从头读到 EOF 计算 SHA-256，各种大小使用同一规则。随后读取 PNG/JPEG/GIF 图片头，其他受支持格式在工具可用时探测。发布前还核对文件身份、大小和 mtime。[触发条件](https://github.com/Surtr42u/qimeng-next/blob/efa62347b256e5a30488aae3e30e2e9d82272101/server/internal/service/scans.go#L398) · [哈希读取](https://github.com/Surtr42u/qimeng-next/blob/efa62347b256e5a30488aae3e30e2e9d82272101/server/internal/service/files.go#L77) · [媒体探测](https://github.com/Surtr42u/qimeng-next/blob/efa62347b256e5a30488aae3e30e2e9d82272101/server/internal/service/thumbnail.go#L68) |
| 改写、移动与缺件 | 同路径覆盖元信息，保留首次分配的资产 ID。对已消失路径，将本轮新增项按“大小 + mtime”分组，复核后选择最新有效候选合并；找不到候选时删除消失资产的索引和相应 TXT 作品行。已记录事件独立保留。[路径更新](https://github.com/Surtr42u/qimeng-media/blob/745974081716a9d69a95002a41220e35e743d764/server/internal/store/queries/assets.sql#L21) · [匹配、复核与删除](https://github.com/Surtr42u/qimeng-media/blob/745974081716a9d69a95002a41220e35e743d764/server/internal/scanner/reconcile.go#L23) | 同路径保留 ID，哈希变化才增加内容版本。遇新路径按同库哈希和大小查候选，恰有一个原路径消失时沿用它的 ID；仍在场的相同内容可保留各自身份，多个失踪候选或超出候选上限时记录歧义。孤立缺件记录问题并保留资产。[候选、版本与修订](https://github.com/Surtr42u/qimeng-next/blob/efa62347b256e5a30488aae3e30e2e9d82272101/server/internal/service/scans.go#L466) |
| 事务、进度与写入 | 常规库按单资产事务写媒体及角色，进度通过事件总线节流发送。WAL 使用 FULL 同步。[事务](https://github.com/Surtr42u/qimeng-media/blob/745974081716a9d69a95002a41220e35e743d764/server/internal/scanner/enrich.go#L155) · [进度](https://github.com/Surtr42u/qimeng-media/blob/745974081716a9d69a95002a41220e35e743d764/server/internal/scanner/scanner.go#L516) · [数据库配置](https://github.com/Surtr42u/qimeng-media/blob/745974081716a9d69a95002a41220e35e743d764/server/internal/store/store.go#L115) | b3847e3 也逐资产提交，并将资产、关系和进度一起落库。efa6234 对常规库中新发现且不超过 1 MiB 的 PNG/JPEG/GIF，最多先准备 32 项。写入阶段遇到 20 毫秒预算边界便在文件间释放写锁，保留 WAL/FULL。文件哈希与图片探测在事务外完成。已知路径、同内容候选、未决发布走逐项复核，故障回滚失败项并保存已完成前缀。[原逐项提交](https://github.com/Surtr42u/qimeng-next/blob/b3847e36cd0b6ef38389bd0b0ce14497a64c8285/server/internal/service/scans.go#L550) · [小批提交](https://github.com/Surtr42u/qimeng-next/blob/efa62347b256e5a30488aae3e30e2e9d82272101/server/internal/service/scan_batch.go#L87) · [数据库配置](https://github.com/Surtr42u/qimeng-next/blob/efa62347b256e5a30488aae3e30e2e9d82272101/server/internal/store/sqlite_portable.go#L15) |
| 自动处理 | 已有路径的写通知可局部处理，大小和 mtime 都相同直接结束。未知路径、删除、改名、目录变化或局部处理失败会请求整库对账，另有独立轮询。[局部更新](https://github.com/Surtr42u/qimeng-media/blob/745974081716a9d69a95002a41220e35e743d764/server/internal/scanner/watch_update.go#L17) · [轮询](https://github.com/Surtr42u/qimeng-media/blob/745974081716a9d69a95002a41220e35e743d764/server/internal/scanner/background.go#L118) | 已有文件写通知也可按路径处理，大小与 mtime 相同时仍读内容核对哈希。新增、改名、删除和目录变化合并整库对账，缩略图另由后台补齐。对照试验关闭监听，手动复扫不承担监控事件的实测含义。[通知分类](https://github.com/Surtr42u/qimeng-next/blob/efa62347b256e5a30488aae3e30e2e9d82272101/server/internal/service/library_watch.go#L248) · [写通知哈希](https://github.com/Surtr42u/qimeng-next/blob/efa62347b256e5a30488aae3e30e2e9d82272101/server/internal/service/scans.go#L403) · [缩略图主管](https://github.com/Surtr42u/qimeng-next/blob/efa62347b256e5a30488aae3e30e2e9d82272101/server/internal/service/preview_maintenance.go#L19) |

Next 扫描 PNG、JPEG 和 GIF 时，用同一受控文件句柄读至 EOF 计算 SHA-256，再从该句柄的起点解码图片头及 JPEG 方向。提交时按库内路径复核原件身份、长度和 mtime，包含批量提交里的逐件复核。原样手动复扫继续按目录签名跳过未变项。外部工具探测使用现有的平台路径。[扫描读取](../server/internal/service/scan_inspection.go) · [批量发布](../server/internal/service/scan_batch.go) · [内置格式与方向用例](../server/internal/service/scan_inspection_test.go)

监听收到写入与目录结构变化时，Next 会同时盘点整库并核对相应路径的内容摘要。队列繁忙时，新到通知与待重试的路径一起保留。待核验路径超过 128 个或操作系统报告通知溢出时，该次扫描逐件重算摘要。合成回归覆盖等长原位改写后还原 mtime、同批新增、排队冲突和通知溢出，并把变更后的原件、标签与备注从导出归档恢复到独立实例。[通知合并与重试](../server/internal/service/library_watch.go) · [扫描与恢复用例](../server/internal/service/scan_watch_test.go)

按上述条件和固定样本推算，Next 首扫送入 SHA-256 的字节数为 6,496,256，原样手动复扫为 0，小批变动的 28 个路径合计 88,816 字节。此处按文件长度计算哈希输入量，操作系统页缓存会影响物理磁盘读取。试验采集了进程写入字节，没有逐次追踪 fsync，也未将端到端差额拆成哈希、目录系统调用和 SQL 各自的耗时。旧版可借鉴的做法包括按窄字段批量读取、用目录差集筛小缺件候选、将耗时探测放在写锁外，以及在提交时复核原件和当前记录。[合成数据与计时脚本](../tools/scan-comparison/compare.py)

分类关系独立存储后，在同一云机扫描 2,048 张合成小图。新资产没有文字线索或候选证据时省去空行，首次扫描三轮中位数从 2,424.91 降至 1,902.84 毫秒，进程写入从 107.82 降至 69.18 MiB。原样复扫为 71.22 毫秒。少量修改、移动和新增后的复扫为 155.57 毫秒，同机安装旧版为 114.68 毫秒。三版均逐项核对身份和路径，Next 另核对内容 SHA-256。以上只描述该组合成文件与该台云机。[同机三版、身份与逐轮数据](https://github.com/Surtr42u/qimeng-next/actions/runs/44381286901)

`c3aa424` 与交付基线 `8f4321a`、安装版 `74597408` 又在同一台 Linux 云机轮换三轮。下表是手动请求至观察到成功终态的中位数，单位为毫秒。[逐轮耗时、写入量与身份核对](https://github.com/Surtr42u/qimeng-next/actions/runs/44452369764/job/135319171439)

| 操作 | 安装版 | 交付基线 | 共用原件句柄后 |
|---|---:|---:|---:|
| 首扫 2,048 张 PNG | 1,384.67 | 1,104.25 | 1,069.77 |
| 原样复扫 | 37.50 | 57.69 | 55.78 |
| 改写 16 张、移动 8 张、新增 4 张后复扫 | 62.24 | 112.23 | 112.90 |

在这台机器上，少量变动复扫仍比安装版慢约 51 毫秒。末轮计时后重新读取了全部 2,052 份原件并比较长度与 SHA-256，两版 Next 还逐项校验了资产 ID 和内容版本。独立移动用例保留了收藏和人工标签。这次测量使用热文件缓存，关闭了目录监听，覆盖的是手动扫描路径。

文件意图和幂等回执的改动集成后，`f573310` 在另一台云机与相同两个参照版本各跑三轮。按安装版、交付基线、集成版的顺序，首扫中位数分别为 2,491.90、2,429.97、2,325.01 毫秒，原样复扫为 79.75、90.98、91.07 毫秒，少量变动复扫为 122.49、186.75、190.60 毫秒。少量变动阶段各进程写入中位数为 2.21、2.55、4.11 MiB。末轮全部原件、身份、版本及独立移动用例均通过。[集成候选逐轮记录](https://github.com/Surtr42u/qimeng-next/actions/runs/44457609793/job/135336361587)

### 上传完成期间的独立编辑

同一台 Ubuntu 运行器以 FFmpeg 生成 132,718,466 字节、1280×720、4 秒的 AVI。每版三轮，两台独立设备会话通过真实 HTTP 操作。第一台分片上传视频并提交完成，第二台在服务进程读数增长 8 MiB 后修改另一张图片的标签，又在目标视频出现在媒体目录时取消该图片的收藏。空闲时对同一张图作对照，上传端同时重放完成请求。每轮均核对视频完整 SHA-256、ffprobe 返回的尺寸与时长、最终标签修订与收藏、两项资产以及一致的原回执。[两时点同机实测](https://github.com/Surtr42u/qimeng-next/actions/runs/44332887189)

| 三轮中位数或完成次数 | 原全局写锁 2a890af | 核验、发布分阶段 05d3847 |
|---|---:|---:|
| 空闲时改标签 | 3.26 ms | 2.97 ms |
| 视频哈希过程中改标签 | 597.17 ms | 2.92 ms |
| 空闲时改收藏 | 2.60 ms | 2.05 ms |
| 视频目标出现后改收藏 | 224.97 ms | 3.15 ms |
| 视频完成请求 | 604.22 ms | 497.29 ms |
| 标签、收藏分别先于视频完成 | 0/3、0/3 | 3/3、3/3 |

每个实例同时核验一个 HTTP 上传完成请求，其他完成请求可取消地等待。服务用打开的文件检查容器、哈希和原生变化标记，Linux/Android 的 ffprobe 接收同一文件描述符。提交阶段重新检查当前凭据、上传与批次、来源、目标和占用。新增资产有持久化文件意图保护，服务可放开业务写锁做发布 I/O，最后在锁内提交索引。替换仍将旧文件名切换与新内容版本的提交放在同一写入区间。取消、保留 mtime 的等长改写、重试，以及安装文件后、提交索引前中断的两种恢复布局均有回归。表中时延来自 Linux 普通新增上传。Windows 沿用受保护路径调用外部探测器，Android 内嵌、不同编解码器以及线上磁盘延迟尚待对应环境测量。[核验与恢复回归](https://github.com/Surtr42u/qimeng-next/actions/runs/44332887189) · [复跑工作流](../.github/workflows/upload-contention.yml)

原生数据迁移后的 `5704687` 又用同一段 132,718,466 字节合成视频复测。哈希期间独立标签请求的三轮中位数为 2.30 毫秒，目标文件出现后取消收藏为 2.82 毫秒。同机使用原服务级写锁的 `2a890af` 分别为 388.27 和 142.80 毫秒。新版本两种编辑均三次先于上传完成，视频摘要、媒体参数、最终标签和收藏及重复请求回执均通过。[合成视频与并发编辑记录](https://github.com/Surtr42u/qimeng-next/actions/runs/44384217014)

### 原生资产的版本与整库恢复

视频演练使用 FFmpeg 生成 160×90 与 1280×720 的两个 MP4，另有一张全程保留的 PNG。先走普通分片上传，挂靠作者、收藏、写入时间轴和续播，再改名并导出第一份整库归档。随后用预期内容版本明确替换视频，重放完成请求，再导出第二份归档。两个归档分别恢复到空目录，以恢复后实例的正式内容接口读出每个原件，逐项比较长度与 SHA-256。第一份可取低分辨率原件，第二份可取高清原件，保留两个版本的内容事实。旧时间轴和续播保留原版本号，跨版本改写标记、提交旧播放进度或按旧版本号取当前文件都应返回 409。[视频上传、改名、替换与恢复用例](../server/internal/httpapi/native_restore_test.go)

升级用例从 0034 SQLite 结构开始，分别放入已提交文件记录、只有摘要和大小的已发布上传、未决文件操作、相互矛盾的证据，以及没有历史记录的版本号。它逐项核对可证版本、未决写保护、只读投影及恢复时对缺少文件位置的资产的检查。扫描用例对当前版和已退任版本执行相同的查重查询，并用 SQLite 的执行计划检查路径、摘要加大小、资产 ID 的索引检索。[升级与不完整快照](../server/internal/store/asset_file_test.go) · [扫描查重与索引](../server/internal/service/scan_version_index_test.go)

标签写入用例预先填充作者、出处、角色、当前位置和内容版本，在五张表分别为新增、更新、删除安装行级触发器。两次真实 HTTP 标签修改后读取各表写入次数。另一用例禁止任何标签关联行变动，再执行词表改名和幂等重放，按原标签 ID 回读资产关系与原显示顺序。收藏和日赞用例在真实移动原件后分别核对收藏列、当日记录及 API 展示，并核对业务 JSON 中这些字段的副本数。[关系表写入与稳定词条用例](../server/internal/httpapi/asset_tags_write_test.go)

<a id="change-evidence"></a>
## 按改动选择云端证据

Windows 作业使用同一提交的 Go 服务和正式 stdio MCP 执行器登记一张合成图片。可用独立卷时，验收重读备份原件、SQLite 检查点和证明文件，再调用 Windows Shell 回收。新进程使用原操作键回读同一许可和回执，并检查原位置的新文件及库内原件。若 Shell 拒绝回收，验收核实原文件身份、字节和保留原因。没有可用独立卷时，验收核实备份拒绝、来源保留以及零清理许可。`windows-first-check` 中的 `windows-source-recovery.json` 记录该次运行实际经过的分支。[验收程序](../tools/cloud_windows_recovery.py)

`da811b7` 在 Windows 2025 标准云机上取得独立卷。Go 和 MCP 跑通备份、原件与检查点回读，Shell 实际将选中的来源移入系统回收站。服务只签发一个许可。新的 stdio 进程重发原操作后得到同一回执，重新建立的同名文件和库内原件均通过字节核对。[Windows 作业](https://github.com/Surtr42u/qimeng-next/actions/runs/44384433852/job/135085794715) · [回收结果报告](https://github.com/Surtr42u/qimeng-next/actions/runs/44384433852/artifacts/14465882480)

完整工作流一次调度多个作业。审查时从受影响的入口找结果，再按共享协议、数据和平台的影响扩大范围。

| 改动范围 | 先核对什么 |
|---|---|
| 纯 Markdown | `Engineering checks` 的文档链接、章节和相关工程检查。操作步骤另与当次启动器、界面或工作流核对 |
| 协议、迁移、Go 业务、文件恢复 | `generate` 与 `lock-check` 核对协议来源，`server` 核对 Linux／SQLite 行为。文件语义涉及 Windows 时看 `windows`。协议变动也要看直接消费端 |
| Web、桌面、Android | `web` 编译并做单元测试，`web-ui` 在桌面与移动视口运行浏览器测试，`live` 使用同一份 Web 构建连接真实 Go。Windows 连接壳看 `windows`。Android 依变更看 `android`、`android-ui` 或本机运行链上的 `android-native`、`android-embedded`、`android-local-ui`。新旧画面对照单列 |
| AI、MCP 宿主 | `ai` 的执行器用例、`live` 的真实 Go 调用，以及受影响的 `mcp-compat`、`hermes` 客户端结果 |
| PC/NAS 打包、启动与恢复 | `server-packages` 中对应 CPU 和操作系统的作业、产品 ZIP、SHA-256、登录页与隔离恢复记录。设备安装和 NAS 实机另记 |

## 必须覆盖的实际行为

- 普通上传独立于 AI，重试沿用操作身份；断线、并发重放、换输入及源变化可区分。
- 智能批次每项有去向；独立条目继续推进，未知与冲突不同，已授权正常步骤不重复确认。
- Go 决定编号与命名，图/视频分序、组图页序、最小空号与占用复核；改名保留身份和关联。
- 人工修改与删除关系不被迟到请求或自动研究覆盖；标签清空、版本冲突与重放真实可见。
- 研究证据有适用对象、版本、来源；候选不伪称已确认；外部能力缺失不伪报无结果。
- 有界查询与上下文、完整计划引用、授权、预算、无进展停止和中断后回读。
- 路径穿越/链接、MIME/长度、磁盘故障、替换/回收站与备份清理边界。
- Web、桌面和 Android 分别对照已安装旧版的展示代码。在同一页面、视口、主题、滚动位置及数据状态下比较画面。交互单独核对，包括列表上打开详情后返回原位置、导航选中态、Android 原有四档材质及系统减少动效设置。分别标注源码对照和实际同屏验证的范围。
- 数据库升级使用历史 Next 隔离快照。整库恢复逐项比较原件摘要、关系和事件，设备交付在两个独立的 Next 实例间验证回执与重传。旧 v2 写入口退役后，普通服务仍须能读取已存在的历史记录。

## 报告规则

Android 的全屏播放使用同提交 Go 供应的合成 H.264，在指定 API 35 模拟器的生产窗口采图，同时回读播放计数和浏览历史。`730f8ad` 的普通应用 4/4、核心数据 13/13、核心界面 6/6 通过，零跳过。系统软件 AVC 解码的 640×320 整窗含彩条，规定网格中 1,697 / 2,340 点达到色差阈值。此前相同解码路径也出现过黑窗；此记录的范围是这次云模拟器和样本。[作业与设备报告](https://github.com/Surtr42u/qimeng-next/actions/runs/44357630219/job/134993469508) [实际 PNG、JUnit 与 logcat](https://github.com/Surtr42u/qimeng-next/actions/runs/44357630219/artifacts/14453719209)

联网仪器分别从宿主和指定模拟器探测两个 Go 实例的 `/healthz`，在 `transport-health.txt` 记录各阶段的 HTTP 状态与端口转发情况。播放用例同时保存解码表面、首次和再次打开后的系统窗口，核对全屏前后的方向策略、实际方向和 Go 中的续播点。统计图用一次点击显示同一日期的两条数值，比较点选前后的实际窗口，再点一次取消。[Android 证据和已知边界](../android/UI_PARITY.md)

分别记录源码完成、编译通过、自动测试、真实接入、已安装与设备验收。失败和跳过保留原因。发布包含同提交源码、协议锁、测试结果和产物摘要；没有完成必需检查时不标成熟可切换。

若 GitHub 在分配运行器之前结束作业，记录候选提交、GitHub 的作业提示和上一次真正执行过的结果。运行器可用后，为候选提交核对工程检查，运行覆盖对应行为的完整工作流或单独的界面对照。协议锁从该提交的生成产物取回并复核。按实际作业结果更新交付状态。
