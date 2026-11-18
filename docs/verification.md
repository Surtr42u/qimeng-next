# 验证与交付

## 执行位置

编译、SDK 生成、自动测试、文档校验与发布构建均由 GitHub Actions 执行。云端结果按实际运行的提交 SHA 记录。已取得的证据见[当前状态](status.md)。

本地允许源码阅读与编辑、静态差异检查、Git 提交/推送和读取云端日志。需要设备或 NAS 才能证明的行为单列，不能用 CI 冒充。

[Engineering checks](../.github/workflows/engineering.yml) 自动检查 `main`、`codex/**` 的推送和所有 PR。[Cloud verification](../.github/workflows/prepare.yml) 在 `main` 推送或 PR 包含 Markdown 以外的变更时自动运行。维护者也可在 Actions 里选分支手动启动完整工作流，随后核对运行的 SHA。合成界面的[Web 新旧对照](../.github/workflows/ui-reference.yml)和[Android 参考图采集](../.github/workflows/android-reference.yml)可单独调度，也会在主分支上各自列出的路径发生变化时运行。

手动运行时，`suite` 默认选择 `all`。只更新协议锁可选 `contracts`；排查单端可选 `backend`、`web`、`android` 或 `windows`。`backend` 包含独立 MCP 客户端。主分支和 PR 继续运行完整流程。

OpenAPI 生成的 Go、Kotlin、TypeScript 和 Python 契约随各组检查提供。仓库中的 `api/sdk.lock` 记录来源与每个生成文件的摘要。独立的 `lock-check` 将生成结果、Go 校验和及依赖锁与当前提交比较。Windows 使用 `npm ci` 和 Cargo 的锁定模式。收取云端产物时只提交所需的锁文件。

Kotlin SDK 在生成期间从已校验的官方模板渲染 Retrofit `@Streaming`。归档下载、作者总表、片段与可读作者资料均按字节流交付。云端会核对这些方法的实际签名，避免手机在收到大备份时先把整个响应放进内存。

需要更新 JavaScript／Rust 依赖时，在 Actions 手动运行并勾选 `refresh_locks`。协议变更可直接取同一提交的 `generated-contracts-and-locks` 更新来源锁；若锁有差异，独立的功能测试仍能给出诊断。带新锁的提交须通过完整检查才可交付。

<a id="engineering-gates"></a>
## 代码与架构检查

工程作业核对仓库内文档路径与 Markdown 章节、SQL 迁移历史、Go 模块引用以及 `server/` 下受版本控制的 Go 文件格式。已有迁移文件按基线提交比较，新增版本须高于该基线已发布的最大值。不同功能分支有编号冲突时，只重排尚未发布的迁移。

工程作业配置了固定版本并校验下载摘要的 Gitleaks，扫描本次检出引用可达的 Git 历史。内置凭据规则和项目的个人目录规则命中时，日志遮盖匹配的值。公开前还需核对提交作者信息、人工文本与可下载的日志、截图和制品。具体运行结果绑定到对应提交。[扫描配置](../.gitleaks.toml) · [安全说明](../SECURITY.md)

Go 格式有差异时，该作业失败，并提供同一提交的 `go-format-correction` 补丁供维护者核对。正常 Go 与 Web 构建直接验证提交的源码。Web 的展示层引用随 `npm test` 检查。Android 的 `verifyClientArchitecture` 读取构建系统实际解析的依赖、源码集和客户端 APK，再由真服务或模拟器用例验证业务行为。两端的边界和适用例外见[Web 开发说明](../web/README.md)和[Android 架构检查](../android/architecture/README.md)。错误处理、职责分工和实际效率通过审查与行为用例验证。

比较性能时在同一云机器使用同一套合成数据、配置、冷暖状态和操作；记录提交、运行次数、耗时及该场景相关的内存或磁盘指标。扫描进度写入次数只反映该项数据库开销。跨版本、跨平台结论要有对应的测量范围。

### 手动扫描的同机对照

同一台 Ubuntu 24.04、4 vCPU 运行器按轮换顺序启动三个版本，每版三轮。每轮从新进程、空媒体索引开始，使用 16 个目录中的 2,048 张不同的 32×32 PNG，共 6,496,256 字节。每阶段先读热原件，再从手动 POST 发出计时到首次观察到成功终态，测试客户端以 10 毫秒间隔轮询。原样复扫后，原位改写 16 张、移动 8 张、新增 4 张，再次扫描。三个版本均核对全部路径、大小、修改时间与已有身份，最终各有 2,052 项。两版 Next 还逐项核对 SHA-256。[最新三版同机原始数据与报告](https://github.com/Surtr42u/qimeng-next/actions/runs/44323980865)

| 三轮中位数 | 已安装版源码 7459740 | Next 小批提交 efa6234 | Next 小批提交及目录核对 fd38a66 |
|---|---:|---:|---:|
| 首次入库 | 1,262.49 ms | 828.53 ms | 785.61 ms |
| 原样复扫 | 37.08 ms | 115.84 ms | 93.79 ms |
| 少量外部改动后复扫 | 61.25 ms | 173.99 ms | 142.11 ms |
| 首扫期间进程写入 | 135.11 MiB | 58.22 MiB | 58.18 MiB |

这批小 PNG 上，fd38a66 首扫比安装版缩短 37.8%，原样复扫和少量变动分别仍需安装版的 2.53、2.32 倍时间。相对同场 efa6234，fd38a66 使原样复扫缩短 19.0%，少量改动缩短 18.3%。缺件阶段按索引父目录分页核对，对目录列表未确认的条目查原件，报告前再复核现存资产与路径。五项小批写入回归和三项缺件回归均在此次云作业运行。[目录核对](https://github.com/Surtr42u/qimeng-next/blob/fd38a66cbc929392e8e7b3c6d5d36126608af939/server/internal/service/scan_missing.go#L152) · [缺件确认](https://github.com/Surtr42u/qimeng-next/blob/fd38a66cbc929392e8e7b3c6d5d36126608af939/server/internal/service/scan_missing.go#L217) · [运行记录](https://github.com/Surtr42u/qimeng-next/actions/runs/44323980865)

此前两场各自轮换三个版本，分别定位两类开销。在第一场，b3847e3 消除重复路径、资产和作者查询后，首扫、原样复扫及少量改动相比 8e225bc 分别缩短 15.7%、27.0%、21.6%。在第二场，从每个资产独立提交改为有界的小批事务后，首扫从 5,296.18 降至 1,146.53 毫秒，写入从 350.84 降至 58.12 MiB，分别减少 78.4% 和 83.4%。第二场原样复扫接近持平，少量改动慢 5.0%。各比例均使用同一场测量。[逐项写入优化](https://github.com/Surtr42u/qimeng-next/actions/runs/44316230983) · [小批事务单独比较](https://github.com/Surtr42u/qimeng-next/actions/runs/44322029440)

最新一场截至首扫完成的进程峰值 RSS 中位数按表中顺序为 120.6、29.1、28.3 MiB，包含启动和鉴权。安装版在扫描前设置密码时执行了 64 MiB 的 Argon2id 派生。Next 使用配置令牌。两种鉴权方式影响进程高水位。[安装版口令派生参数](https://github.com/Surtr42u/qimeng-media/blob/745974081716a9d69a95002a41220e35e743d764/server/internal/auth/password.go)

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

按上述条件和固定样本推算，Next 首扫送入 SHA-256 的字节数为 6,496,256，原样手动复扫为 0，小批变动的 28 个路径合计 88,816 字节。此处按文件长度计算哈希输入量，操作系统页缓存会影响物理磁盘读取。试验采集了进程写入字节，没有逐次追踪 fsync，也未将端到端差额拆成哈希、目录系统调用和 SQL 各自的耗时。旧版可借鉴的做法包括按窄字段批量读取、用目录差集筛小缺件候选、将耗时探测放在写锁外，以及在提交时复核原件和当前记录。[合成数据与计时脚本](../tools/scan-comparison/compare.py)

<a id="change-evidence"></a>
## 按改动选择云端证据

完整工作流一次调度多个作业。审查时从受影响的入口找结果，再按共享协议、数据和平台的影响扩大范围。

| 改动范围 | 先核对什么 |
|---|---|
| 纯 Markdown | `Engineering checks` 的文档链接、章节和相关工程检查。操作步骤另与当次启动器、界面或工作流核对 |
| 协议、迁移、Go 业务、文件恢复 | `generate` 与 `lock-check` 核对协议来源，`server` 核对 Linux／SQLite 行为。文件语义涉及 Windows 时看 `windows`。协议变动也要看直接消费端 |
| Web、桌面、Android | Web 的 `web` 作业和真实 Go 联调 `live`，Windows 连接壳的 `windows` 作业。Android 依变更看 `android`、`android-ui` 或本机运行链上的 `android-native`、`android-embedded`、`android-local-ui`。新旧画面对照单列 |
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
- 旧 v2 数据迁移只在隔离副本演练，核对身份、事件、标签空集合与人工来源，计数不能替代集合/摘要核验。

## 报告规则

分别记录源码完成、编译通过、自动测试、真实接入、已安装与设备验收。失败和跳过保留原因。发布包含同提交源码、协议锁、测试结果和产物摘要；没有完成必需检查时不标成熟可切换。

若 GitHub 在分配运行器之前结束作业，记录候选提交、GitHub 的作业提示和上一次真正执行过的结果。运行器可用后，为候选提交核对工程检查，运行覆盖对应行为的完整工作流或单独的界面对照。协议锁从该提交的生成产物取回并复核。按实际作业结果更新交付状态。
