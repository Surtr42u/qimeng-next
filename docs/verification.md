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

[安装版与 Next 扫描对照](../.github/workflows/scan-comparison.yml)在同一台 Ubuntu 24.04 运行器上交替启动两版服务，每版三轮。每轮使用空数据库和同一批 2,048 张合成 PNG，分别通过 HTTP 触发首次入库、原样复扫和少量外部改动后的复扫。计时前读热原件并核对内容，计时结束后逐项检查索引和身份。运行摘要与 JSON 记录每次请求至终态的耗时、后端计时、进程写入字节及 RSS 高水位。旧版此前两万行的测量覆盖内存索引或 SQL 装载，单独列在其[扫描说明](https://github.com/Surtr42u/qimeng-media/blob/745974081716a9d69a95002a41220e35e743d764/server/README.md#媒体库后台扫描)。

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
