# 当前状态

更新于 2026-11-19。本仓已按所有者授权公开，以 MIT 许可证发布。开发产物用于空目录验收，真实资料切换、用户设备安装与 NAS 实机验收尚未执行。

## 原生数据重构的交接

本轮在 `codex/native-continuation` 集成。续播按资产和内容版本写入；在同一 MP4 连续拖动、回拖和重发时，触发器对作者、标签、出处和角色关系均计得 0 次写入。浏览器首轮刷新后停在零处，修复已提交至 `5180311`，浏览器端需单独检查。[续播服务端实测](https://github.com/Surtr42u/qimeng-next/actions/runs/44338071574/job/134920809910) · [浏览器复验](https://github.com/Surtr42u/qimeng-next/actions/runs/44345158616)

资产记录、当前文件位置和不可变内容版本已分表。目录范围命名使用 `19bb3d4`，双实例原生资料包使用 `c486ae5`，稳定标签身份与按事实写入使用 `0f95b36`。接手先核对最新分支、Actions 和 SDK 来源锁。其余重点为集合成员排序、同一内容版本的跨实例时间轴、新目录整库恢复，以及手机从两个独立服务领取资料。验收逐项记录实际通过的入口和提交。先前 Go 作业在整个 HTTP 包的三分钟时限中到时，未执行到的用例仍待验证。[初次原生后端云测](https://github.com/Surtr42u/qimeng-next/actions/runs/44345385405) · [数据权属与恢复](architecture.md#next-原生数据与恢复)

## 维护与验收

维护者从[开发与维护](../CONTRIBUTING.md)进入。公开后的主分支 `89edb6a` 通过 Go 格式、模块引用、文档、迁移历史及密钥扫描。Web 的展示层依赖检查、浏览器测试和真实服务联调也在该提交通过。[工程检查](https://github.com/Surtr42u/qimeng-next/actions/runs/44305960995) · [Web 检查](https://github.com/Surtr42u/qimeng-next/actions/runs/44305961259/job/134808058447) · [真实服务联调](https://github.com/Surtr42u/qimeng-next/actions/runs/44305961259/job/134821281600)

数据库启动先核对已有迁移，再执行待办。`89edb6a` 的 Go 云测覆盖历史迁移表定义的备份恢复、较新数据库或校验和冲突时中止、设备分段交付以及研究回执恢复。Linux 的两个 SQLite 驱动和 Windows 原生 Go 步骤均通过。该次 Windows 桌面窗口测试止于云账号权限检查。[Linux 服务端](https://github.com/Surtr42u/qimeng-next/actions/runs/44305961259/job/134808058518) · [Windows 分步结果](https://github.com/Surtr42u/qimeng-next/actions/runs/44305961259/job/134808058701)

## 当前界面基准

旧版 PC 和 Android 最近一轮有安装记录的构建均出自 `74597408`。它与旧仓 `d0215962` 在 Web 展示源码、Android 与桌面目录的差异为空。已定位 Web 侧栏和顶栏选中效果、详情叠层及 Android 导航材质和换页过渡的实现差异。旧 Android 的四档为液态玻璃、磨砂玻璃、纯色坞、经典，默认液态玻璃。2026-11-18 起暂缓重构方案新增的动效。按已安装版本的展示源码及同场景截图核对，分别记录每个端的进度。[旧版安装记录](https://github.com/Surtr42u/qimeng-media/blob/d0215962/docs/HANDOVER.md) · [Web 与桌面对照](ui.md) · [Android 对照](../android/UI_PARITY.md)

Web 首页、相册及详情已用旧、新两个实际入口，按同一组合成媒体、主题和桌面／窄屏视口对照。六组已测布局误差均在 1 CSS 像素内，打开详情后返回原列表的位置一致。详情、图表和管理代码改为按需载入后，同套合成数据的冷首页实际下行从 3,174,379 降至 2,470,783 字节，相比调整前减少约 22%。这个数值只适用于这次 Web 首屏测量。设置页另保留用户授权所需的「AI 接入」入口。[六组画面与网络测量](https://github.com/Surtr42u/qimeng-next/actions/runs/44286855368)

Android 已接回旧版四档导航、首页间距及标签底部弹层。`4105b7a` 纯客户端构建、JVM、Lint 和 APK 检查通过。旧版默认／经典首页与标签弹层已有实际发布 APK 的回读摘要和云端原图；完整新旧同状态对照仍需新版同场景截图。[客户端构建](https://github.com/Surtr42u/qimeng-next/actions/runs/44284583229/job/134734519028) · [旧版原图](https://github.com/Surtr42u/qimeng-next/actions/runs/44286855346)

## 已取得的证据

| 范围 | 实际执行结果 |
|---|---|
| Go 与文件 | `e9fe1df` 在 Linux 两个 SQLite 驱动及 Windows 通过 Go 检查。覆盖发布恢复、大小写改名、批次独立备份、任务取消和监听。云 Linux 验证了来源的原生回收。Windows 云环境返回来源保留的回执。 |
| Go 扫描 | `b3847e3` 经首轮优化，在同机三轮 2,048 张小图的首次入库、原样复扫和少量改动上，比优化前分别缩短 15.7%、27.0%、21.6%，耗时仍为旧版的 2.82～3.02 倍。两版均手动遍历整库。[测量条件与逐项结果](verification.md#手动扫描的同机对照) |
| 文件身份 | `ff92b48` 与安装版在同一云机执行了两种操作。换成同尺寸、同时间戳、字节不同的新图时，旧版沿用了原图身份，新版为新图分配了新身份并报告原件缺失。字节相同的原图移动且时间戳改变时，旧版分配了新身份，新版保住原身份。[两种情况的云端记录](https://github.com/Surtr42u/qimeng-next/actions/runs/44320603158) |
| 命名与作者导入 | `7600949` 的查询计划和 HTTP 回归确认，自动补全只从索引读取当前目录，再合并磁盘上尚未登记的占号文件。已验证 v2 作者资料显式覆盖前的快照、重复交付回读，以及人工清空、订阅与候选保护。外部存储整根与子目录均可注册、上传和重启恢复。[后端回归](https://github.com/Surtr42u/qimeng-next/actions/runs/44318828263/job/134852628470) |
| Android 本机 | `89edb6a` 的 ARM64 和 x86_64 原生构建、内嵌 APK 和本机 API 35 作业通过，使用隔离数据核对上传及电源变化后的扫描。[本机模拟器](https://github.com/Surtr42u/qimeng-next/actions/runs/44305961259/job/134812936078) |
| Android 客户端 | `f2b884b` 的 APK、JVM、lint、两种原生架构和本机 API 35 仪器通过。后者实际核对了普通上传取得的资产 ID、磁盘原件及断电／接电扫描。联网模拟器的视频全屏画面仍未通过。此轮仍使用修正前的 Android 外观。[本机仪器](https://github.com/Surtr42u/qimeng-next/actions/runs/44269316797/job/134686309823) |
| Web 与桌面 | `e42260c` 的浏览器与真实 Go 联调通过。Windows 在云端已生成安装包和服务端程序，真实 WebView2 窗口的完整回执仍待验证。[真实 Go 与浏览器](https://github.com/Surtr42u/qimeng-next/actions/runs/44278862765/job/134719598585) |
| PC／NAS 启动包 | `89edb6a` 的 Windows x64、Linux x64、Linux ARM64 服务端包各自通过启动、登录与隔离恢复。[三个发行包的作业](https://github.com/Surtr42u/qimeng-next/actions/runs/44305961259) |
| AI 执行器与 MCP | `0325a6d` 的独立 stdio 客户端、Pi、官方 TypeScript SDK 和 Hermes 作业通过。受限合成 TLS 来源验证了研究只执行一次、重放复用回执和预算。Hermes 使用合成模型响应，核对实际宿主的工具发现、结果与拒绝写入。[独立客户端与研究](https://github.com/Surtr42u/qimeng-next/actions/runs/44309342079/job/134820037437) · [Hermes 宿主](https://github.com/Surtr42u/qimeng-next/actions/runs/44309342079/job/134820037531) |
| 数据迁移 | 云端使用合成 v2 副本和全新目标目录验证导入、内容身份、历史记录及隔离恢复。真实资料留待独立的迁移验收。 |

可按作业查看 [e9fe1df 集成记录](https://github.com/Surtr42u/qimeng-next/actions/runs/44264832227) 和 [此前的本机启动检查](https://github.com/Surtr42u/qimeng-next/actions/runs/44238104432)。

## 接手复查

本轮由两位 GPT-6.2 Astra / max 子代理分别负责 Go 数据恢复与 AI 工作流、旧版对新版的扫描实测。Android 子代理负责手机的真实消费端和画面对照，主代理整合协议锁、跨端云测及交付证据。

| 已定位的触发条件 | 处理结果与验收范围 |
|---|---|
| Windows 只改文件名大小写时，源路径和目标路径可能指向同一原件 | 改名先保留可恢复的同卷副本，核对实际目录项后再发布。`5330c2f` 已通过 Windows 服务端检查。 |
| 系统分享与上次手选文件共用草稿，或分享期间切换服务器 | 分享按本次选择、实例和媒体库冻结输入。真实多文件分享用例另含独立手选草稿及不可读输入。 |
| 第一版被替换后，第三版恰好又与第一版字节相同 | 服务端绑定上传真实发布的内容版本。历史版本仅凭原发布意图或可信回执回填，证据不足时保留来源。 |
| 签发凭据时附带重试键，可将一次性返回的秘密写入操作回执 | 专用签发入口拒绝重试键。迁移撤销受影响的旧凭据及其派生授权，清除旧签发回执。 |
| 浏览器维护目录反复备份同一版本，可能过早替换上一代 | 服务端新增业务修订号和同一检查点的可读作者资料，客户端按实际快照衔接两代备份。 |

Go 的历史发布恢复和 Windows 文件占用修复已通过。本轮重点复核 Android 联网播放、恢复旧版展示后的手机画面以及 Windows 真窗口。协议变更后取同次云端生成锁再验收。

## 切换前还需核对

- 设备领取 v2 包时默认保留已有作者资料，可显式选择覆盖。Go 与 Android 已接入带修订和指纹的计划。切换前核对手机端实际领取、检查点回读、人工修改保护以及重复交付。[迁移说明](migration.md) · [Android 客户端验证](https://github.com/Surtr42u/qimeng-next/actions/runs/44309908039)
- 七个公开文本研究来源按部署和任务授权选择。真实 stdio 和研究回执已通过合成来源验证。旧版可选的图片外查能力单独核对。
- 旧 UI 的逐屏行为，尤其是返回原列表、现有材质和选中态。结合普通与智能工作流、续传、播放与筛选记录验收范围。
- 真实 Go、MCP 客户端、独立卷备份与来源端原生回收的端到端回执，区分回收成功和来源保留。
- 作者 TXT、两代备份、监听导入、历史媒体维护、发布包与新手运行说明在同一提交的执行结果。
- 确定可发布版本后再在隔离目标演练设备与 NAS，不对旧实例直接操作。覆盖表、迁移条件和运行边界分别见[产品要求](requirements.md)、[迁移说明](migration.md)、[手机本机模块](runtime-separation.md)。
