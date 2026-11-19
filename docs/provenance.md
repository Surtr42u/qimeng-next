# 来源与实现纪律

2026-11-20 的 Android 对照和 `55d4963` 审查修复按所有者指定，向新子代理显式派发 GPT-6.2 Sol、max 推理。主代理协调共享导航、缩略图与视频的接线，复核不同提交的测试结果。旧发行包的合成测量包括首页、统计、作者、缓存与视频。[旧发行包测量](https://github.com/Surtr42u/qimeng-next/actions/runs/44588916908) · [Android 集成](https://github.com/Surtr42u/qimeng-next/pull/19) · [事件与扫描修复](https://github.com/Surtr42u/qimeng-next/pull/18)

2026-11-19 的维护交接与文档复核由独立文档代理完成，已从该代理本轮 `turn_context` 核对 `gpt-6.2-astra` 与 `max`。公开手册引用源码、合成作业和产品合并提交。真实交付状态由所有者私下保存。[接手手册](handover.md) · [产品合并记录](https://github.com/Surtr42u/qimeng-next/pull/17)

图片合并先由独立代理核对已有能力、身份、统计与备份边界，随后分派事务与来源、文件与发布、普通服务读写保护。主代理整合，独立审查代理复核。参与代理均以各自当轮运行记录核实 GPT-6.2 Astra、max 推理。公开验证使用虚构的身份和媒体。[双驱动事务验证](https://github.com/Surtr42u/qimeng-next/actions/runs/44515875860) · [双平台发布与恢复](https://github.com/Surtr42u/qimeng-next/actions/runs/44516923052)

旧 PC v26 离线迁移先复核旧版备份和封存数据库能分别提供哪些事实。三位实施代理分别负责文件与归档、作者与目录资料、互动历史，另一位独立核对旧版写入格式与 Next 恢复流程。主代理负责身份与时区规则、分支整合和交付验收。各代理按自己的运行记录确认 GPT-6.2 Astra 和 max 推理。公开测试使用旧仓固定提交的 schema 与合成数据。[旧版 schema 来源](https://github.com/Surtr42u/qimeng-media/tree/d0215962bb1e9c29baabeed9fc9dc5c8b4b94555/server/migrations) · [目录双驱动回归](https://github.com/Surtr42u/qimeng-next/actions/runs/44503664071) · [历史活动与 HTTP 回读](https://github.com/Surtr42u/qimeng-next/actions/runs/44501702272) · [整库归档与双平台恢复](https://github.com/Surtr42u/qimeng-next/actions/runs/44504237179)

2026-11-18 公开前复核了 7 个远端分支和待发布的 Git 内容、当时全部 52 次云运行的日志及 293 件保留产物的目录。测试报告、网络跟踪和合成图片按内容及来源复核，构建二进制按来源和样本核验。两份带临时测试会话的旧附件已在保存校验副本后撤下。公开时 GitHub 密钥扫描和推送保护均已开启。新制品的范围与提交前隐私检查见[安全说明](../SECURITY.md)。

本仓库 2026-11-17 从空目录初始化，依据用户已确认产品要求重新实现。参考项目为 [Surtr42u/qimeng-media，d0215962](https://github.com/Surtr42u/qimeng-media/tree/d0215962)，只作业务规则、公式、使用教训、界面布局与验收依据，不复制业务源码或历史补丁实现。

工程规范参考旧项目在代码所有权、按风险验证和专题文档维护上的经验。新仓采用当前实际模块、云端流程和用户选择，规则正文位于[开发与维护](../CONTRIBUTING.md)，实际依赖约束位于[架构](architecture.md#module-boundaries)。[旧仓协作规范](https://github.com/Surtr42u/qimeng-media/blob/d0215962/AI_README_FIRST.md) · [旧仓模块边界](https://github.com/Surtr42u/qimeng-media/blob/d0215962/docs/ARCHITECTURE.md)

## UI复用的明确授权

初轮实现重画了部分页面。2026-11-17 用户明确允许直接使用旧 UI，要求优化背后的代码。2026-11-18 用户指出截图仍有差异，要求将拟新增的平滑过渡、玻璃材质和选中效果暂缓。采用旧版的实际展示代码和交互，包括 Android 已有的四档导航材质，业务逻辑与 API 则在新仓实现。

旧项目交接记录列出最近一次 PC 和 Android 安装，PC 对应 `74597408`。复核该提交至 `d0215962`，`web/src`、`android` 和 `desktop` 三个目录的源码差异为空。Web 和 Android 分别记录复用位置与适配范围。外观对照使用同场景云端合成数据截图，分别记录源代码、画面、操作已覆盖的范围。[旧版安装记录](https://github.com/Surtr42u/qimeng-media/blob/d0215962/docs/HANDOVER.md)

## 手机本机模式的明确授权

2026-11-17用户了解成本后明确要求本机能力“一起重构”，并要求后期容易剥离、不影响独立后端。此前纯客户端方案因此被替代。采用相同新Go服务的Android内嵌运行模块和独立客户端构建；安卓宿主适配与业务核心分离，后续迁移数据与移除模块各有独立验收。该授权不允许复制旧Go业务实现。

规则来源包括 R01–R23 的 Next 讨论，以及现项目 11-14 至 11-16 已记录的权限默认值、MCP宿主兼容、结果续读、预算恢复、命名与实际批次操作。它们在 [产品要求](requirements.md) 收敛为当前约束。旧项目通过的测试不能作为新项目证据。

新项目必须使用官方来源的工具链、依赖和 SDK 生成器。第三方基础模板（例如 Gradle wrapper）记录官方来源与校验和，不通过旧项目复制。生成代码由 CI 从本仓 OpenAPI 产生并锁定来源。

初期三个实现子代理为 GPT-6 Astra / max，分别负责 Go/API、Web/桌面、Android。2026-11-18 用户将主会话切换到 GPT-6.2 Astra 并要求更换子代理。原三个代理已停止，新三个均以 GPT-6.2 Astra / max 启动，沿用对应分工，接续代码并复查实际用户流程。云端结果按具体提交列在[当前状态](status.md)。

公开仓库后，后端进一步分工。Go/API 子代理负责备份恢复、研究回执和 v2 作者资料的受控覆盖，另派的同型号子代理在同台云机器上测量两个版本的实际扫描。Android 子代理核验手机消费端，主代理收取同提交生成的协议锁，复核云端结果并整合。

2026-11-19 起，维护者按所有者要求继续在桌面会话协调开发，编译、测试、依赖核对和性能测量都由 GitHub Actions 执行。三位 GPT-6.2 Astra / max 子代理分别复核并实现协议与混合文件清单、数据库和研究网关、Android 数据层与 MCP 消费。维护者整合改动，用云端生成物和实际用户操作核对每一批。该阶段暂停界面改版。[原生数据与多端验收](https://github.com/Surtr42u/qimeng-next/pull/1) · [别名与稳定身份](https://github.com/Surtr42u/qimeng-next/pull/2) · [出处角色、清单、图片查证](https://github.com/Surtr42u/qimeng-next/pull/3)

随后按原讨论逐项核对 22 条产品要求和 36 个场景，补上 App 提交后的自动认领、可执行的下一步和跨端停等原因。三位 GPT-6.2 Astra / max 子代理负责 Go 的授权与并发、执行器的认领与中断恢复、标准 MCP 及真实模型验证。Qwen3-4B 在隔离云机上接收中文短任务并自行选择工具，验收端独立回读 Go 的实际结果。[受限任务与真实模型](https://github.com/Surtr42u/qimeng-next/pull/4) · [App 智能批次与恢复](https://github.com/Surtr42u/qimeng-next/pull/5)

以 `8f4321a` 为起点的系统审查分两轮进行。四个只读审查分别检查数据与恢复、三端客户端、AI 与 MCP、测试与性能。另一组代理实施修复和回归用例，云端验收代理核对实际运行，主代理负责排序、归并与复核。主代理和每位子代理均从各自当轮的运行记录确认了 GPT-6.2 Astra、max 推理。性能对照使用独立数据库及合成原件。[同机扫描记录](https://github.com/Surtr42u/qimeng-next/actions/runs/44452369764) · [协议来源锁的生成作业](https://github.com/Surtr42u/qimeng-next/actions/runs/44454623724/job/135326529499)

从主线 `21b4b6d` 接续的迭代以四个只读方向定位复扫写入、恢复后作者身份与备份凭证、播放器续播和 worker 长清单。实施代理按这些行为分别提交，验收代理从云端真实作业核对。各次派工及续工均核实为 GPT-6.2 Astra、max 推理。两个扫描对照分别测量耗时与非计时数据库写入。[两场三版对照](verification.md#普通库少量变动复扫) · [50 项清单与换宿主接续](https://github.com/Surtr42u/qimeng-next/actions/runs/44471246374/job/135381725569) · [快速返页与暂停定位](https://github.com/Surtr42u/qimeng-next/actions/runs/44471416242/job/135382443911)

原生包升级与云端 FFmpeg 安装由独立实施代理处理，验收代理核对了三种正式 ZIP 和五个使用该安装步骤的作业。主代理在合入时核对了受测文件树。复扫的时钟探针和 Web 次页加载方案在各自的证据分支运行。主代理、只读审查、实施与验收代理均从自己的当轮记录核实 GPT-6.2 Astra、max 推理。[原生包升级](https://github.com/Surtr42u/qimeng-next/pull/9) · [五项云端作业](https://github.com/Surtr42u/qimeng-next/pull/10) · [扫描专项](https://github.com/Surtr42u/qimeng-next/actions/runs/44484716169/job/135425198607)
