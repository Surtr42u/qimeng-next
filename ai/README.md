# AI 工具入口

本模块从零实现，使用官方 MCP Python SDK 2.3.0 处理 stdio 协议。CLI 与 MCP 共用执行路径；业务接口、参数和工具 schema 从本仓 OpenAPI 在云端生成，工具不能访问任意 URL 或运行任意脚本。

## 安装到运行 AI 的电脑

在 GitHub 打开通过的 `Cloud verification`，选与影库相同的提交，下载 **ai-first-check** 并解压。找到有 `install.lock` 和 `.whl` 的文件夹，在此文件夹打开终端。云端制品保留 7 天。下面的步骤使用 64 位 Python 3.13，安装时按随包的摘要核对客户端和依赖。[下载运行列表](https://github.com/Surtr42u/qimeng-next/actions/workflows/prepare.yml)

Windows x64 使用 PowerShell。

```powershell
py -3.13 -m venv .venv
.\.venv\Scripts\python.exe -m pip install --only-binary=:all: --require-hashes -r install.lock -r windows-recycle.lock
.\.venv\Scripts\qai.exe --help
```

Linux x64 使用终端。

```sh
python3.13 -m venv .venv
.venv/bin/python -m pip install --only-binary=:all: --require-hashes -r install.lock
.venv/bin/qai --help
```

看到 `mcp`、`worker`、`catalog` 等子命令后，在影库「设置 → AI 接入」选择允许的媒体库与动作，签发长期凭据。把下表中的地址、凭据和私有目录配置到运行执行器的账号。在 Pi、Hermes 等宿主的 MCP 配置中，`command` 填刚安装的 `qai.exe` 或 `qai` 的完整路径，`args` 填 `["mcp"]`。只需要在 App 提交后自动整理时，按下文配置 `worker` 的模型地址。[宿主配置示例](../docs/mcp-compatibility.md)

维护者使用固定版本的 Setuptools 将同提交生成的协议打入 wheel。云端安装检查从下载制品创建新环境，在源码目录外运行入口并核对内置协议摘要。[构建依赖及校验和](../tools/ai-build.lock) · [Setuptools 84.2.1](https://pypi.org/project/setuptools/84.2.1/)

## 职责

- Go：资产身份、命名编号、关联、权限、对象修订、批次、计划、研究证据与完成事实。
- 执行端：逻辑文件发现、受控文件引用、流式传输、上下文呈现、调用/时间预算、模型接入。
- 模型：理解意图、核对候选与证据，使用当前状态给出的动作。不能自算最终编号、修改授权配置或把候选写成确定事实。

业务计划保存在服务端，以计划 ID/fingerprint 提交。执行端私有SQLite保存预算、未决操作定位、派生凭据和可过期结果缓存，不作为第二份媒体库或业务任务账本。此目录须受操作系统账号保护，不放进媒体目录或公开备份。

## 配置

| 环境变量 | 用途 |
|---|---|
| `QIMENG_API_BASE` | 后端 origin；HTTP 仅回环/私有IP，远端使用HTTPS/既有隧道 |
| `QIMENG_API_TOKEN` | 首次配置的长期AI执行器凭据，或传统单任务/只读凭据；仅可信端持有 |
| `QIMENG_AI_STATE_DIR` | 执行端私有绝对状态目录，必填 |
| `QIMENG_AI_TASK_ID` | 传统单任务预算的稳定ID；默认interactive使用每日控制通道预算，新工作流按taskId另记业务预算 |
| `QIMENG_AI_PERMISSIONS` | 默认read；按需明确授予delegate/ingest/research/organize/author.profile.write/local.read/local.research |
| `QIMENG_AI_STORAGES` | JSON对象，逻辑名映射可信本地根；模型只看到逻辑名与相对路径 |
| `QIMENG_AI_RECYCLE_STORAGES` | 允许移入回收站的逻辑来源名数组，默认空。每个名称必须已经出现在来源存储配置中 |
| `QIMENG_AI_MAX_CALLS/MAX_SECONDS/MAX_UNCHANGED` | 默认100次/900秒/3次相同结果；预算持久保存 |
| `QIMENG_AI_OUTPUT_BYTES` | 默认12288，限制展示结果；不含协议外壳或宿主重复呈现 |
| `QIMENG_AI_TIMEOUT` | 单HTTP请求默认120秒；超时可能已有副作用，先回读 |
| `QIMENG_AI_ALLOW_MEDIA_TO_MODEL` | 默认false；显式true才允许artifact以MCP image形式传给模型 |

凭据不通过命令行参数输入、不进入仓库或报告。工具清单默认完整且稳定，兼容不刷新清单的宿主；任务实际上下文、规则和数据仍按需获取。可见工具不等于已获许可，服务端还会执行独立scope和批次限制。

## 自动领取 App 智能整理

希望 App 提交后直接开始整理时，在可持续运行的可信电脑上配置影库长期凭据、本机模型和 `qai worker`。已有的 stdio 聊天宿主仍可随时用 `task.start` 主动认领。关闭的聊天宿主需要另行启动，后台自动处理由运行中的 worker 与它配置的模型承担。

| 配置 | 用途 |
|---|---|
| `QIMENG_API_BASE`、`QIMENG_API_TOKEN`、`QIMENG_AI_STATE_DIR` | 影库实例、已授权接收和整理指定媒体库的长期凭据、可跨进程重启保留的私有状态目录 |
| `QIMENG_AI_PERMISSIONS` | 至少 `read,delegate,organize`。只有需要研究资料时加 `research` |
| `QIMENG_AI_WORKER_ORIGIN`、`QIMENG_AI_WORKER_MODEL` | 数值回环地址、端口和本机宿主的模型名，例如 `http://127.0.0.1:8080`。宿主须支持 `/v1/chat/completions` 的工具调用 |
| `QIMENG_AI_WORKER_TOKEN` | 模型宿主要求认证时设置其专用凭据。影库和模型使用独立凭据 |
| `QIMENG_AI_WORKER_POLL_SECONDS` | 空队列或临时离线后的轮询间隔，默认 30 秒 |
| `QIMENG_AI_WORKER_IMAGE_LOOKUP` | 默认 false。开启时仅对已授予单帧查证且该批有研究额度的任务申请。模型接收预览仍需 `QIMENG_AI_ALLOW_MEDIA_TO_MODEL=true` |

用户在 App 或 Web 选择智能整理并提交。所有选中项传输或记录失败后，Go 向相应长期授权提供可认领批次。worker 持久化认领键，签出本批的受限凭据，将分项状态和可用工具送给本机宿主。模型提供整理意图，Go 核对版本、权限和媒体资料，生成并提交命名计划。普通上传直接入库。批内的空文件或不支持的输入继续保留其原因，独立的好文件可以完成。

运行 `qai worker` 持续轮询，Ctrl+C 可停止。使用 `qai worker --once` 处理一轮。进程重启后沿原键恢复未确认操作，并从 Go 回读同一计划、待处理项和最终资产。每日新任务额度已满时，已领取的任务仍可继续。并发执行器分别使用私有状态目录，Go 只向获胜方签发任务。私有目录完全丢失或要更换宿主时，由可信端明确选择原任务并用 `task.resume` 接续。

模型结束而仍有待整理输入时，worker 报告 `needs_attention` 并保留原任务。原批次修订有变化时再交给模型。权限拒绝、预算耗尽或重复无进展时，核实授权或在可信端续期后用 `qai worker --retry-batch <批次ID>` 明确恢复。`task.read` 的 `executorStatus` 可回读最近一次等待缘由，例如本机模型离线、权限不足或操作仍待核实。worker 只报告真实停等或恢复，终态以 Go 回读的原清单、计划和资产为准。

每轮的 `batches[].modelUsage` 给出模型轮数、工具调用及重复数、耗时、序列化请求字节数、收到的应答字节数，以及本次提供的工具定义大小。输入和输出 token 分别取自宿主提供的实际用量。某项计量缺失时该字段返回 `null`，诊断摘要不保存提示词、工具参数和应答正文。

## 工作流

1. 首次由本人在可信设置中选择允许的库、资料维护范围、动作、预算和有效期，签发长期执行器凭据。配置MCP后，模型用`platform.permissions`读取真实范围，以`task.start`创建任务或认领App已有智能批次；不需要每次切回App。
2. `task.context` 查看状态、缺失条件与剩余研究预算。
3. `intake.register`从可信逻辑目录完整盘点（或接受用户完整选择的fileRefs），在传输前封存全部原始输入，返回稳定inputId与fileRef；目录变动、超100项或不安全路径会明确停止，不静默截断。`files.search`可用于浏览/研究，但不能以已上传清单代替原始选择。`upload.file`携inputId、沿稳定operationId分片上传并验证服务端摘要。普通上传直接入库，智能上传先进入批次待计划状态。
4. 登记工具在已有 `read` 许可时附上 Go 的逐项状态。只上传 `pending` 项。空文件和后缀不受支持的文件会保存失败原因，继续处理同批其他媒体即可。对已入库的项可回读返回的资产 ID。
5. 已确认资料直接复用；未知项有限研究后调用 `research.submit` 留下候选/未知/冲突与证据，不能伪装已核实。
6. `plan.prepare` 由Go生成最终命名/关联；必要时 `plan.read` 逐项核对，授权范围内用原ID/fingerprint执行 `plan.commit`。
7. `task.read` / `asset.read` 按输入清单回读，分别报告入库、未知/冲突、备份和来源清理。上传工具保留来源文件。

需要公开文本查证时，先调用 `research.context`。实例须启用公开研究，首次授权须勾选所用来源，任务凭据须拥有 `research` 和可用研究额度。`research.provider.query` 接受来源、公开标识或词语、固定操作以及稳定的 `Idempotency-Key`。批次另传当前待整理上传的 ID 与 SHA-256。相同键可回读原结果。任务重启后可从 `research.context.queries` 找到回执，再以 `research.provider.result` 阅读。提交已经计费的候选时传 `providerQueryIds` 及 `callsUsed=0`。平台记录、存档和搜索结果仅提供证据，人工已确认的选择继续生效。

单帧查证使用 `research.image.search`。实例管理员在 Go 服务配置 `QIMENG_RESEARCH_LENS_ENDPOINT` 和 `QIMENG_RESEARCH_SIDECAR_TOKEN`，首次授权开启 `canLookupImages`，创建接收或已有作品任务时显式传 `allowImageLookup=true`。在 `research.context` 检查 `imageLookupEnabled`、`imageLookupAllowed` 和剩余额度。执行器只需 `research` 权限。将预览交给模型仍由执行器上的 `QIMENG_AI_ALLOW_MEDIA_TO_MODEL` 单独授权。

1. 接收任务用 `research.materials` 指定上传和原件摘要，已有作品任务用 `asset.research.materials` 指定当前任务。从返回的 `artifacts` 中选一帧，取它的 `id` 和 `sha256`。同项的 `artifactRef` 用于获准的预览读取。
2. MCP 调用 `research.image.search`，或使用 `qai call research.image.search` 从标准输入传入相同 JSON。顶层传 `taskId` 和稳定的 `Idempotency-Key`，在 `body` 传 `frameId`、`frameSha256`。接收任务同时传 `uploadId`、`expectedHash`，已有作品任务同时传 `assetId`、`contentVersion`。Go 核对对象、版本与帧摘要后，只向管理员配置的接收服务发送所选的派生 JPEG。执行器保管任务凭据，Go 使用独立的接收服务凭据，模型读取 JSON 回执。
3. 回执的 `items` 是有链接的候选，`imageHints` 是只有域名或标题的线索。两者都需要再核实，保留现有人工归属。首次实际查证占一份研究额度。同一任务、操作键和参数可以回读同一回执。返回 `pending` 时，用原回执 ID 读 `research.provider.result`。断线丢失回执 ID 时，用 `operation.read` 传原 `key` 和 `operation=lookupResearchImage`。

未获单帧外发许可时，已授权的本地探测和服务端预览仍可使用。文本查证只发送明确选定来源需要的公开标识或搜索词。

部署明确拒绝公开查询时，执行器可以继续本地研究和入库。连接中断时，执行器保留未决操作。可信端可用 `qai admin --task-id <任务ID> reconcile <原键>` 回读该任务及原签发凭据的查询结果。只有键、操作、任务身份和终态都匹配时才解除阻塞，已中断的查询回执也会保留实际结果。

`task.start`后续调用带顶层`taskId`。执行器自动生成/保存受限任务凭据，不把秘密返回模型；服务端只保存其摘要，任务创建回执也不含秘密。继续同任务直接用原taskId；更换宿主或执行器目录时用`task.resume`，服务端保留业务任务及幂等身份，并轮换执行租约使旧执行器不能继续写入。撤销长期授权同时撤销其所有派生凭据。

原始清单封存后不能增删输入或换来源；新的选择新建批次。已封存任务换执行器后从`task.context`读取原inputId，重新发现对应来源并按同一身份续传。`task.read.inputs`包括尚未上传项，只有全部原输入确实入库才可能`completed`；未上传、重复或失败不会被成功项掩盖。执行器状态目录不得位于来源存储内或包含来源存储，配置会拒绝重叠，文件出口也拒绝私有状态。

传统“可信客户端创建单批次/资料任务并签发凭据”仍可用，直接配置该任务凭据时无需调用task.start或携带新的taskId包装参数。多宿主配置和验证边界见[兼容指引](../docs/mcp-compatibility.md)。

长期权限默认包含全库只读查询；库/作者选择约束写入范围，不是多用户私有分区。新增媒体、研究、整理和资料维护分别授予，清空资料另有选项。确定命名/编号/关联由Go执行，模型只提交意图与证据。此项目不引入家庭账户或多租户权限模型。

超长结果返回resultRef与省略声明；`platform.result.read` 用JSON Pointer、offset/limit续读。引用绑定实例、凭据和契约，24小时过期，每个32MiB、总256MiB；普通结果最多128项，文件元引用最多10000项，研究图片最多512项，字节总额仍共享。相同内容复用引用。该缓存失效不等于服务端计划丢失，需重新从原计划ID读取。

`research.probe`接收fileRef，安全快照后由ffprobe/ffmpeg读取元信息并抽取最多三帧，不要求作者提示、不自动联网、不写业务。当前本地快照上限2GiB（不限制正式上传），子进程限时、限输出、禁stdin/控制台并使用固定媒体解复用器。`platform.artifact.read`标准MCP图片输出独立授权，每图最多2MiB；普通resultRef续读不能绕过图片授权。CLI仅输出图片元信息。

输入已由手机或其他设备上传时，使用`research.materials`，传uploadId与expectedHash。Go从核验的输入生成元信息和至多三张预览；`sa_`图片引用可由另一执行器在相同任务授权下读取，不需要原执行器的本地路径/缓存。预览注明原始SHA256和有效期，不代替作者证据。服务端研究缓存独立于业务数据库，最多256MiB/10000张、24小时有效，原件不转码；缓存缺失可从仍可用的原输入重建。输入已替换且无缓存时明确无法取得旧输入，不拿新版冒充。

作者资料维护可以直接使用可信客户端签发的单任务凭据。先用 `platform.session` 读取 `authorTaskId`，再用 `author.task.context` 读取该任务的作者 ID、当前修订、允许字段与清空许可。`author.profile.update` 使用作者 ID 和 `expectedRevision`，只传本次受托修改的字段。相同写入沿用原幂等键，完毕后回读作者。授权范围外的字段（含获取渠道）会遭拒绝。AI 修改的资料标为候选。本人首次签发的是长期授权时，先读取 `platform.permissions`，按明确作者与字段通过 `task.start` 建立受限任务。

## 整理已经在库里的作品

第一次在 AI 授权页选择媒体库、是否允许研究已有作品，以及可补全的字段和是否允许改名。需要执行写入的宿主同时在 `QIMENG_AI_PERMISSIONS` 配置 `asset.metadata.write`。向模型发送预览图还需开启 `QIMENG_AI_ALLOW_MEDIA_TO_MODEL`。现有授权升级后按原来的权限继续工作。

例如，你可以在聊天里说「找出这个库里叫 source.png 的作品，查证作者，按这个目录现有格式接下一个编号」。执行器通过 `asset.query` 查找条目，用返回的修订号和内容版本创建 `kind=asset` 任务。从原生 `.qnext` 整库恢复、扫描发现和手工上传的作品使用同一流程。查询和回读沿用任务 ID，可在另一个宿主用 `task.resume` 接续。

1. `asset.task.context` 返回这件作品被授权的字段。可以先用 `asset.evidence.query` 回读同一内容版本已经采用的证据。
2. `asset.research.materials` 准备有界预览；通过 `platform.artifact.read` 将图送给已获授权的模型。无法核实时保留未知状态。
3. `asset.metadata.apply` 只补全授权的缺失项。每个字段附自己的来源和观察，主动清空的标签与作者关系受保护。人工编辑使旧修订失效，需要重新核对。
4. `naming.suggest`、`naming.preview` 由 Go 读当前目录计算续号。将同一计划、目标和目录指纹交给 `asset.rename.apply`；期间占号会返回冲突。文件身份、内容版本和作者关系随改名保留。

按用户提供的简称查资料时，`tag.vocabulary.query` 接受 `q`，返回标签的稳定 ID、正式名称、别名和修订。只需要名称的任务可继续使用 `tag.query`。`collection.query` 同时接受 `libraryId` 和 `q`。多个对象使用同一叫法时，读完需要的候选页，再用库、正式名称和用户给的线索确定对象。管理员清空别名后，下次查询立即采用新词表。三个查询均可用只读凭据执行。[接口与修订](../docs/api.md)

## 可信端维护

配置独立备份目标并取得本人授权后，`backup.protect` 保护本批已经入库的输入。用 `backup.protection.read` 核对目标、原始清单、内容版本和读回结果。要回收来源，还需给该任务单独授予 `source.cleanup`，在执行器配置 `local.read,local.recycle` 并选中允许回收的来源存储。`source.recycle` 接收原批次、输入、`fileRef` 和保护记录，校验登记时的实体文件、SHA‑256 和当前租约。文件内容相同、来源目录不同的另一文件会被拒绝。回执丢失时沿用原操作键重试。

Linux 使用同卷的 [Freedesktop 回收站](https://specifications.freedesktop.org/trash/latest/)，保留原路径和可恢复内容。Windows 的可选组件 `windows-recycle` 使用 [Windows Shell 回收操作](https://learn.microsoft.com/en-us/windows/win32/api/shobjidl_core/nf-shobjidl_core-ifileoperationprogresssink-postdeleteitem)，在执行器私有目录同卷保留临时恢复链接并核对系统返回的回收站对象。来源不满足这些条件时报告保留。若系统操作已经启动而本地缺少最终证据，执行器保留未决日志，停止再次移动。通过另一设备选择且无法核实原文件身份的输入，应在持有该来源的设备处理。

已安装经过云端验证的工具包后，`qai mcp`启动stdio，`qai catalog`查询目录；`qai call <tool>`从stdin读取JSON。`qai admin status`查询持久预算；`admin reconcile <key>`先请求服务端权威回读再核销；`admin renew --calls N --seconds N`只能提高上限，保留调用数、开始时间和未决项。这些管理能力不注册为模型工具。

长期授权的业务任务使用`qai admin --task-id <任务ID> status/reconcile/renew`选择本地任务状态。CLI的renew只提高执行器本地上限；服务端上限只能由本人会话在预算接口提高，两者取较严格者。

同一未决写入允许以完全相同的工具、参数与操作键恢复，新写入仍被阻止。分片上传沿原会话offset接续；可信端核销整次上传必须取得`:complete`回执，仅创建了上传会话不能算上传完成。SDK契约升级保留消费记录和未决操作，同时使旧契约的结果展示引用失效。

长期授权任务另有 Go 保存的调用预算与截止时间。外部查证由 Go 随回执键一次保留任务调用和研究额度，原键重放沿用该次扣费。其他任务工具沿用既有调用预算。执行器本地限制可以更严格，`platform.budget` 免费显示两层预算。每日控制通道与各业务任务分开，换宿主保留服务端计数和任务进度。只有可信本人会话可提高服务端上限，续期保留已消费次数。

开发、打包与所有测试只在云端执行，当前结果见[状态](../docs/status.md)。模型token/费用依赖宿主遥测，本模块不宣称控制外部账号全部消费。真实provider、NAS与设备验证按证据单列。
