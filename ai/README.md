# AI 工具入口

本模块从零实现，使用官方 MCP Python SDK 2.3.0 处理 stdio 协议。CLI 与 MCP 共用执行路径；业务接口、参数和工具 schema 从本仓 OpenAPI 在云端生成，工具不能访问任意 URL 或运行任意脚本。

## 职责

- Go：资产身份、命名编号、关联、权限、对象修订、批次、计划、研究证据与完成事实。
- 执行端：逻辑文件发现、受控文件引用、流式传输、上下文呈现、调用/时间预算、模型接入。
- 模型：理解意图、核对候选与证据，使用当前状态给出的动作。不能自算最终编号、修改授权配置或把候选写成确定事实。

业务计划保存在服务端，以计划 ID/fingerprint 提交。执行端 SQLite 只存预算、未决操作定位与可过期结果缓存，不作为第二份媒体库或业务任务账本。

## 配置

| 环境变量 | 用途 |
|---|---|
| `QIMENG_API_BASE` | 后端 origin；HTTP 仅回环/私有IP，远端使用HTTPS/既有隧道 |
| `QIMENG_API_TOKEN` | 为目标批次签发的AI凭据，仅可信端持有 |
| `QIMENG_AI_STATE_DIR` | 执行端私有绝对状态目录，必填 |
| `QIMENG_AI_TASK_ID` | 同任务跨进程共享预算的稳定ID，默认interactive |
| `QIMENG_AI_PERMISSIONS` | 默认read；按需明确授予ingest/research/organize/author.profile.write/local.read/local.research |
| `QIMENG_AI_STORAGES` | JSON对象，逻辑名映射可信本地根；模型只看到逻辑名与相对路径 |
| `QIMENG_AI_MAX_CALLS/MAX_SECONDS/MAX_UNCHANGED` | 默认100次/900秒/3次相同结果；预算持久保存 |
| `QIMENG_AI_OUTPUT_BYTES` | 默认12288，限制展示结果；不含协议外壳或宿主重复呈现 |
| `QIMENG_AI_TIMEOUT` | 单HTTP请求默认120秒；超时可能已有副作用，先回读 |
| `QIMENG_AI_ALLOW_MEDIA_TO_MODEL` | 默认false；显式true才允许artifact以MCP image形式传给模型 |

凭据不通过命令行参数输入、不进入仓库或报告。工具清单默认完整且稳定，兼容不刷新清单的宿主；任务实际上下文、规则和数据仍按需获取。可见工具不等于已获许可，服务端还会执行独立scope和批次限制。

## 工作流

1. 可信客户端创建智能批次并签发该批次的最小凭据，模型取得任务ID。
2. `task.context` 查看状态、缺失条件与剩余研究预算。
3. `files.storages` / `files.search` 获取fileRef；`upload.file`沿稳定operationId分片上传并验证服务端摘要。普通上传直接入库，智能上传先进入批次待计划状态。
4. 已确认资料直接复用；未知项有限研究后调用 `research.submit` 留下候选/未知/冲突与证据，不能伪装已核实。
5. `plan.prepare` 由Go生成最终命名/关联；必要时 `plan.read` 逐项核对，授权范围内用原ID/fingerprint执行 `plan.commit`。
6. `task.read` / `asset.read` 按输入清单回读，分别报告入库、未知/冲突、备份和来源清理。上传工具保留来源文件。

超长结果返回resultRef与省略声明；`platform.result.read` 用JSON Pointer、offset/limit续读。引用绑定实例、凭据和契约，24小时过期，每个32MiB、总256MiB；普通结果最多128项，文件元引用最多10000项，研究图片最多512项，字节总额仍共享。相同内容复用引用。该缓存失效不等于服务端计划丢失，需重新从原计划ID读取。

`research.probe`接收fileRef，安全快照后由ffprobe/ffmpeg读取元信息并抽取最多三帧，不要求作者提示、不自动联网、不写业务。当前本地快照上限2GiB（不限制正式上传），子进程限时、限输出、禁stdin/控制台并使用固定媒体解复用器。`platform.artifact.read`标准MCP图片输出独立授权，每图最多2MiB；普通resultRef续读不能绕过图片授权。CLI仅输出图片元信息。换执行器时的服务端输入/工件接续仍须跟服务端研究产物接口一同验收，不能以本地缓存替代。

作者资料维护采用单独的明确授权任务：可信客户端用 `POST /author-tasks` 指定作者、读取修订、允许字段、是否允许清空、次数及有效期，随后签发只绑定该任务的 `read,author.profile.write` 凭据。模型通过 `author.task.context` 获取需要的信息，使用稳定幂等键执行 `author.profile.update`。正常用户界面仍直接使用本人会话编辑；只有向AI委托时才需要此范围授权。AI修改字段标为candidate，不冒充人工核实；无关字段保留。

## 可信端维护

已安装经过云端验证的工具包后，`qai mcp`启动stdio，`qai catalog`查询目录；`qai call <tool>`从stdin读取JSON。`qai admin status`查询持久预算；`admin reconcile <key>`先请求服务端权威回读再核销；`admin renew --calls N --seconds N`只能提高上限，保留调用数、开始时间和未决项。这些管理能力不注册为模型工具。

同一未决写入允许以完全相同的工具、参数与操作键恢复，新写入仍被阻止。分片上传沿原会话offset接续；可信端核销整次上传必须取得`:complete`回执，仅创建了上传会话不能算上传完成。SDK契约升级保留消费记录和未决操作，同时使旧契约的结果展示引用失效。

开发、打包与所有测试只在云端执行，当前结果见[状态](../docs/status.md)。模型token/费用依赖宿主遥测，本模块不宣称控制外部账号全部消费。真实provider、NAS与设备验证按证据单列。
