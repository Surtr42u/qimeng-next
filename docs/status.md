# 当前状态

更新于 2026-11-19。本仓已按所有者授权公开，以 MIT 许可证发布。开发产物用于空目录验收，真实资料切换、用户设备安装与 NAS 实机验收尚未执行。

## 原生数据重构的交接

数据库迁移 0034–0038 分别处理续播、原件版本和位置、原生交付回执、稳定标签和集合成员。0039–0042 涵盖词条别名、出处与角色关系、混合文件清单和单帧查证授权。0043 供已授权执行器查找就绪批次，0044 保存与当前任务租约和批次修订对应的停等原因。Go 的生成接口挂在运行中的服务路由上，各端协议从同一份 OpenAPI 生成。[数据权属](architecture.md#next-原生数据与恢复) · [各版本升级说明](migration.md) · [云端生成来源锁](../api/sdk.lock)

| 验收 | 已回读的结果 |
|---|---|
| 同一件资产上独立操作 | 连续前拖、回拖、迟到事件和重发后恢复正确位置，四张关系表写入为 0。另一会话单纯开始播放会保留续播点。两个页面分别收藏、点赞、重试及取消今日赞，历史票数和收藏保持正确。[Go 全量双驱动](https://github.com/Surtr42u/qimeng-next/actions/runs/44354939835/job/134981138865) · [浏览器真实操作](https://github.com/Surtr42u/qimeng-next/actions/runs/44354939835/job/134981903063) |
| 改名、换高清版、整库恢复 | 替换前后的两份完整归档分别恢复到新目录，逐个读回原件的长度与 SHA-256。资产身份、收藏、作者和历史保留，旧时间轴和续播留在原版。组图调序和集合改名对被保护的五张资产相关表共写入 0 行。[两次恢复、集合并发、作者清空后复扫](https://github.com/Surtr42u/qimeng-next/actions/runs/44352834391/job/134973645918) |
| 目录冲突与回执 | 目录 B 新增之后，A 的可用项成功提交。A 中另一客户端占用实际目标时拒绝覆盖，处理占用后重试，已发布项保留原回执。[目录范围与实际占用](https://github.com/Surtr42u/qimeng-next/actions/runs/44349041811/job/134960104946) |
| 选择性设备交付 | 两个独立实例间传输作者资料、TXT 和只属于指定视频版本的标记。两个同哈希的视频只更新选定的一件，目标换版时保留旧标记。重复运输、应答丢失、核验名额已满时重试已成功请求均回读原结果。[真实 H.264 和逐项回执](https://github.com/Surtr42u/qimeng-next/actions/runs/44354939835/job/134981138865) · [Web 配对、发送、刷新查回执](https://github.com/Surtr42u/qimeng-next/actions/runs/44354939835/job/134981903063) |

## 维护与验收

原生数据基础合入主分支的提交为 `5538fa3`，稳定标签和集合别名合入于 `3d48afc`。两批分别通过当时的 23 项检查。单次拖动、收藏、今日赞、人工修改标签和作者都有各自的写入入口。应用详情由 Go 组装，手机离线与 PC/NAS 使用同样的写入规则。[原生数据与多端验收](https://github.com/Surtr42u/qimeng-next/pull/1) · [别名查询与回执](https://github.com/Surtr42u/qimeng-next/pull/2)

`f86bc47` 的真实 Go 入口覆盖明确清空出处与角色后再扫描、清单里有效媒体与空文件及不支持格式并存、AI 修改已入库作品的标签。独立 stdio 执行器另用合成视频检验单帧查证，重发同一请求只使用一次研究预算。[Go 双驱动与事务用例](https://github.com/Surtr42u/qimeng-next/actions/runs/44388037007/job/135098878152) · [MCP、Pi 与 CLI](https://github.com/Surtr42u/qimeng-next/actions/runs/44388037007/job/135099345899)

App 主动提交后，`b4371fa` 的正式 worker 通过真实 HTTP 认领混合批次。在脚本化模型已经准备计划时强制结束进程，重启后沿原计划完成。每日新任务名额已满仍可续办。模型离线时另一会话查到一次暂停，恢复后回读同一批。需要人工处理的一批也能让后续可办的批次先完成。[执行器、Go 与隔离宿主](https://github.com/Surtr42u/qimeng-next/actions/runs/44393250358/job/135117593505) · [真实模型的单独验收](mcp-compatibility.md#标准-cpu-上的真实模型任务)

Web 的 `8df9cd4` 在 Chromium、移动 Chromium 和 WebKit 共通过 107 项浏览器用例。浏览器目录写入演练只在支持所需 OPFS 接口的 Chromium 执行，WebKit 跳过这一项。设备配对、跨实例资料交付和用户实际拖动视频使用独立的真实服务作业验收。[浏览器回归](https://github.com/Surtr42u/qimeng-next/actions/runs/44356165266/job/134985901182)

维护者从[开发与维护](../CONTRIBUTING.md)进入。公开后的主分支 `89edb6a` 通过 Go 格式、模块引用、文档、迁移历史及密钥扫描。Web 的展示层依赖检查、浏览器测试和真实服务联调也在该提交通过。[工程检查](https://github.com/Surtr42u/qimeng-next/actions/runs/44305960995) · [Web 检查](https://github.com/Surtr42u/qimeng-next/actions/runs/44305961259/job/134808058447) · [真实服务联调](https://github.com/Surtr42u/qimeng-next/actions/runs/44305961259/job/134821281600)

数据库启动先核对已有迁移，再执行待办。`89edb6a` 的 Go 云测覆盖历史迁移表定义的备份恢复、较新数据库或校验和冲突时中止、设备分段交付以及研究回执恢复。Linux 的两个 SQLite 驱动和 Windows 原生 Go 步骤均通过。该次 Windows 桌面窗口测试止于云账号权限检查。[Linux 服务端](https://github.com/Surtr42u/qimeng-next/actions/runs/44305961259/job/134808058518) · [Windows 分步结果](https://github.com/Surtr42u/qimeng-next/actions/runs/44305961259/job/134808058701)

## 当前界面基准

旧版 PC 和 Android 最近一轮有安装记录的构建均出自 `74597408`。它与旧仓 `d0215962` 在 Web 展示源码、Android 与桌面目录的差异为空。已定位 Web 侧栏和顶栏选中效果、详情叠层及 Android 导航材质和换页过渡的实现差异。旧 Android 的四档为液态玻璃、磨砂玻璃、纯色坞、经典，默认液态玻璃。2026-11-18 起暂缓重构方案新增的动效。按已安装版本的展示源码及同场景截图核对，分别记录每个端的进度。[旧版安装记录](https://github.com/Surtr42u/qimeng-media/blob/d0215962/docs/HANDOVER.md) · [Web 与桌面对照](ui.md) · [Android 对照](../android/UI_PARITY.md)

Web 首页、相册及详情已用旧、新两个实际入口，按同一组合成媒体、主题和桌面／窄屏视口对照。六组已测布局误差均在 1 CSS 像素内，打开详情后返回原列表的位置一致。详情、图表和管理代码改为按需载入后，同套合成数据的冷首页实际下行从 3,174,379 降至 2,470,783 字节，相比调整前减少约 22%。这个数值只适用于这次 Web 首屏测量。设置页另保留用户授权所需的「AI 接入」入口。[六组画面与网络测量](https://github.com/Surtr42u/qimeng-next/actions/runs/44286855368)

Android 已接回旧版四档导航、首页间距及标签底部弹层。旧版已安装构建与新版 API 35 模拟器都有默认首页、经典首页和标签弹层原图。对照记录列出了逐区域的像素差异，以及媒体样本不同而不能直接比较的区域。其余页面和实体手机尚待按相同状态核对。[Android 新旧画面对照](../android/UI_PARITY.md) · [旧版采图](https://github.com/Surtr42u/qimeng-next/actions/runs/44286855346)

## 已取得的证据

| 范围 | 实际执行结果 |
|---|---|
| Go 与文件 | Linux 和 Windows 2025 都在独立卷上保护合成原件，核对数据库检查点与原件摘要后，经正式 stdio MCP 移入系统回收站。Windows 发出一个清理许可，重启执行器后用原键重试，得到同一回执，同名新文件和库内原件均保留。[Linux 实测](https://github.com/Surtr42u/qimeng-next/actions/runs/44354939835/job/134981903185) · [Windows 实测](https://github.com/Surtr42u/qimeng-next/actions/runs/44384433852/job/135085794715) |
| Go 扫描 | 同一云机三轮手动扫描 2,048 张小图，`d082fb0` 首扫 1,902.84 ms、原样复扫 71.22 ms、少量改动复扫 155.57 ms。安装旧版同场分别为 2,228.10、75.76、114.68 ms。首次扫描省去空分类行后，Next 写入从 107.82 降至 69.18 MiB。三个版本最终均找到 2,052 个文件。[同机原始记录与身份核对](https://github.com/Surtr42u/qimeng-next/actions/runs/44381286901) |
| 文件身份 | `ff92b48` 与安装版在同一云机执行了两种操作。换成同尺寸、同时间戳、字节不同的新图时，旧版沿用了原图身份，新版为新图分配了新身份并报告原件缺失。字节相同的原图移动且时间戳改变时，旧版分配了新身份，新版保住原身份。[两种情况的云端记录](https://github.com/Surtr42u/qimeng-next/actions/runs/44320603158) |
| 命名与作者导入 | `7600949` 的查询计划和 HTTP 回归确认，自动补全只从索引读取当前目录，再合并磁盘上尚未登记的占号文件。已验证 v2 作者资料显式覆盖前的快照、重复交付回读，以及人工清空、订阅与候选保护。外部存储整根与子目录均可注册、上传和重启恢复。[后端回归](https://github.com/Surtr42u/qimeng-next/actions/runs/44318828263/job/134852628470) |
| Android 本机 | API 35 运行内嵌 Go，实际上传合成 H.264，再从接收页复制实例 ID、目标视频 ID 和内容版本，逐一从系统剪贴板回读，并与 Go 返回值核对。上传、重新连接和接电扫描也在隔离库验收。[内嵌设备记录](https://github.com/Surtr42u/qimeng-next/actions/runs/44363806975/job/135016521774) |
| Android 客户端 | `627010c` 的 API 35 联网仪器全部通过，核心界面 6 项、真实 Go 数据 13 项、应用 4 项。首次播放、恢复全屏前方向、暂停后从 Go 回读、再次打开从保存位置播放均通过。首次和续播都取得彩色解码表面及系统窗口。[联网仪器](https://github.com/Surtr42u/qimeng-next/actions/runs/44378759221/job/135068792040) · [原图与 JUnit](https://github.com/Surtr42u/qimeng-next/actions/runs/44378759221/artifacts/14462729347) |
| Web 与桌面 | 浏览器在两个独立 Go 服务间签发资料包，回读目标实际 TXT、逐项回执，并在刷新后按设备找到原记录。Windows 的 `4012001` 在云机完成 NSIS 安装、原生窗口最大化/最小化、真实鼠标打开带认证会话的原件、HTTP Range、切换服务关闭旧窗口、退出及卸载。[Web 双实例实测](https://github.com/Surtr42u/qimeng-next/actions/runs/44354939835/job/134981903063) · [Windows 完整操作](https://github.com/Surtr42u/qimeng-next/actions/runs/44354017708/job/134978093760) |
| PC／NAS 启动包 | `89edb6a` 的 Windows x64、Linux x64、Linux ARM64 服务端包各自通过启动、登录与隔离恢复。[三个发行包的作业](https://github.com/Surtr42u/qimeng-next/actions/runs/44305961259) |
| AI 执行器与 MCP | `0325a6d` 的独立 stdio 客户端、Pi、官方 TypeScript SDK 和 Hermes 作业通过。受限合成 TLS 来源验证了研究只执行一次、重放复用回执和预算。Hermes 使用合成模型响应，核对实际宿主的工具发现、结果与拒绝写入。[独立客户端与研究](https://github.com/Surtr42u/qimeng-next/actions/runs/44309342079/job/134820037437) · [Hermes 宿主](https://github.com/Surtr42u/qimeng-next/actions/runs/44309342079/job/134820037531) |
| 数据升级 | Next 使用自己的数据库迁移、整库归档和选择性交付版本。云验收从早期 Next 结构升级，重开数据库，在新目录恢复并回读关联与历史。已有 v2 导入记录可读，新的个人旧资料迁入留给单独的一次性工具。[原生升级与恢复](migration.md) |

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

Go 的历史发布恢复与 Windows 文件占用已通过。后续设备切换使用已核对的同提交构建、协议来源锁和目标设备验收记录。

## 交付范围与切换条件

- 标签与集合别名已经通过真实 HTTP、原搜索框和只读 MCP 验收。旧称共享时分别返回每个稳定 ID，清空后刷新不再匹配，旧回执重发不会恢复已移除的别名。作者和角色继续沿用各自已有的别名规则。[Go 事务与迁移](https://github.com/Surtr42u/qimeng-next/actions/runs/44374858300/job/135051748513) · [浏览器实测](https://github.com/Surtr42u/qimeng-next/actions/runs/44374858300/job/135052234087) · [独立 MCP 会话](https://github.com/Surtr42u/qimeng-next/actions/runs/44374858300/job/135052119601)
- 设备领取 Next 原生结果包。切换前核对本机身份、接收方选择的目标视频与内容版本、人工修改保护和逐项回执。同一包重复领取应回读原结果。历史 v2 记录只读。[交付和恢复操作](migration.md)
- 七个公开文本研究来源按部署和任务授权选择。单帧查证另需管理员配置和任务授权。真实 stdio 与 CLI 已向回环测试接收器发送同一张合成视频衍生帧，核对帧摘要、回执和实际调用次数。连接真实第三方服务还取决于部署者的配置与许可。[单帧用例](../ai/tests/test_live_image_lookup.py) · [接入已有伴随服务](quickstart.md#接入已有的单帧查证服务)
- 旧 UI 的逐屏行为，尤其是返回原列表、现有材质和选中态。结合普通与智能工作流、续传、播放与筛选记录验收范围。
- 部署者首次启用来源回收时，在自己的目标卷用一份合成资料核对实际返回的保留或回收回执，再对个人资料授权。[独立卷与恢复验收](verification.md#change-evidence)
- 作者 TXT、两代备份、监听导入、历史媒体维护、发布包与新手运行说明在同一提交的执行结果。
- 确定可发布版本后再在隔离目标演练设备与 NAS，不对旧实例直接操作。覆盖表、迁移条件和运行边界分别见[产品要求](requirements.md)、[迁移说明](migration.md)、[手机本机模块](runtime-separation.md)。
