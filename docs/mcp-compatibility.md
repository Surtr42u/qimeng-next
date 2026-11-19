# MCP宿主接入与兼容验证

绮梦的业务接口与模型宿主分开。所有宿主通过相同的标准stdio MCP入口调用同一执行器；执行器调用Go的权威用例，不为某个聊天软件重写编号、资料更新或入库规则。安装与权限环境变量见[AI工具包](../ai/README.md)。

## 兼容底线

- 工具列表稳定且默认完整；权限改变不依赖宿主自动刷新列表才能生效。
- 输入为对象JSON Schema，工具名经宿主规范化后不碰撞。返回同时包含结构化对象和对应JSON文本，纯文本宿主仍能读取操作身份、状态和下一步。
- 业务失败返回标准isError与可行动错误码；宿主超时不等于后端回滚。恢复沿同一操作身份，禁止自动生成新键重试有副作用的请求。
- 长结果显式给resultRef，按指针续读。计划执行以服务端planId/fingerprint为准，不从宿主截断的文本拼回完整计划。
- 图片以标准image内容块返回，单独要求媒体外传配置。宿主或所选模型不能接收图片时，元数据仍可用，但不假称完成视觉核实。
- 工具annotations帮助宿主展示只读/写入性质，最终授权仍由执行器配置和Go服务端共同执行。核心流程不要求sampling、elicitation或私有卡片。

## Pi

2026-11-17核对[Pi官方MCP说明](https://github.com/earendil-works/pi/blob/main/packages/coding-agent/docs/mcp.md)：当前版本支持stdio。用户级mcp.json和项目级配置分开；个人凭据使用用户级配置，项目配置须被信任。示例中qai必须是已安装工具包的实际可执行文件；凭据通过宿主继承的环境传入，不写项目配置。

```json
{
  "mcpServers": {
    "qimeng": {
      "command": "qai",
      "args": ["mcp"],
      "timeout": 180,
      "description": "绮梦媒体库：查询、确定性命名与授权任务执行"
    }
  }
}
```

云端兼容测试使用Pi维护方的独立客户端包`@earendil-works/pi-mcp@1.5.2`，覆盖真实stdio发现、调用、图片转为模型内容、错误与长结果续读。这能验证传输和内容适配，不等于已验证每个模型的任务判断。

## Hermes

2026-11-17核对[Hermes官方MCP说明](https://github.com/NousResearch/hermes-agent/blob/main/website/docs/user-guide/features/mcp.md)：当前版本提供stdio接入并在启动时发现工具。使用其用户级配置的mcp_servers；不把凭据提交到仓库。工具筛选应保留目录、任务context、结果续读和操作回读，避免只保留写入动作而移除必要前置能力。

```yaml
mcp_servers:
  qimeng:
    command: qai
    args: [mcp]
```

云端 `hermes` 作业使用 [Hermes 0.23.2](https://github.com/NousResearch/hermes-agent/releases/tag/v0.23.2) 的实际 CLI、独立配置目录和标准输入输出执行器。测试只用回环的脚本化模型服务，检查模型实际收到的实例身份、库列表和越权拒绝。Pi、Hermes 与真实模型的任务判断分别记录在[当前状态](status.md)。

## Codex、ZCode与其他宿主

选择本地stdio服务器，command填工具包的qai可执行文件，args为mcp，配置相同可信环境。宿主的字段名称和凭据保存方式按其实际版本设置；不发布猜测的私有配置。接入后先检查platform.catalog、library.query与一次明确被拒绝的越权调用，再在隔离数据执行完整工作流。

## 证据与边界

云端 `mcp-compat` 的“智能整理工具目录实测”摘要记录两个入口。标准 `qai mcp` 的 `tools/list` 返回长期凭据默认发现的完整清单。生产 `qai worker` 的脚本化宿主则记录智能批次的首轮请求，以及展开 `authors` 后收到的下一轮请求。每行给出工具数量和全部输入 Schema 组成的数组的 UTF-8 紧凑 JSON 字节数。worker 两行另计实际发送的完整工具数组，包含名称、说明和参数。该用例检查未获准的图片工具、维护操作及其他未展开域，独立从 Go 回读所选作者与原件摘要。宿主省略用量，token 和费用保留空值。这两种入口各有自己的任务范围，字节数仅用于核对各自呈现的目录。[云端用例](../ai/tests/test_live_worker.py)

### 标准 CPU 上的真实模型任务

`289d173` 的 Pi 宿主使用 Qwen3‑4B 实际完成下表四场。每场模型只发起一次业务工具调用，并收到两轮推理应答。耗时和 token 只对应这台云机、这两项任务及该模型。[作业](https://github.com/Surtr42u/qimeng-next/actions/runs/44399206761/job/135137285836) · [逐轮测量和 HTTP 观测](https://github.com/Surtr42u/qimeng-next/actions/runs/44399206761/artifacts/14473398185)

| 首轮工具目录 | 用户任务 | 输入 token | 输出 token | 耗时 |
|---|---|---:|---:|---:|
| 领域目录 | 用共享别名查所有词条并给出正式名和 ID | 4,600 | 160 | 93 秒 |
| 领域目录 | 只改获准的作者简介并回读原文 | 4,242 | 94 | 73 秒 |
| 完整目录 | 同一别名查询 | 24,627 | 184 | 764 秒 |
| 完整目录 | 同一作者修改 | 24,267 | 92 | 595 秒 |

两场别名查询都返回了两条正确的名称和稳定 ID。两场作者任务都只写入一次。执行器在写成功后独立读取 Go，观察器验证同一作者的 PATCH 和 GET 均返回 200，修订均为 3，还核对了模型实际收到的读数摘要。报告中 `verifiedByExecutor=true`、`modelIssuedRead=false`。同提交的正式 stdio 检查另覆盖写后断线、人工修改、同键重放、越权和达到服务端预算上限。[MCP 与故障回归](https://github.com/Surtr42u/qimeng-next/actions/runs/44399220924/job/135138942408)

手动运行 GitHub Actions 的 `Real CPU model and MCP task proof`。公开仓的 `ubuntu-24.04` 标准机提供 4 个 vCPU 和 16 GB 内存。工作流在联网准备阶段校验官方 Qwen3‑4B Q4_K_M 权重和 llama.cpp b12521 CPU 发行件的 SHA‑256。随后新建仅有回环接口的网络命名空间，运行本地推理、同提交 Go 服务、正式 stdio 执行器和 Pi MCP 客户端。模型只接收简短用户任务、当前可见的工具 Schema 和工具返回的合成数据。[运行器规格](https://docs.github.com/en/actions/reference/runners/github-hosted-runners) · [权重与许可证](https://huggingface.co/Qwen/Qwen3-4B-GGUF/tree/bc640142c66e1fdd12af0bd68f40445458f3869b) · [推理器发行件](https://github.com/ggml-org/llama.cpp/releases/tag/b12521)

查标签时，两条合成词条共享同一别名，模型需要给出所有匹配的正式名及稳定 ID。维护作者时，凭据只允许补一次指定作者的简介，核对正式回读、候选状态、原别名和人工清空的主页。每场都以空对话开始，宿主转交 MCP 初始化说明，推理器自行决定工具与参数。每场上限是七轮生成、十二次工具调用，每次最多生成 512 个 token。领域配置单题十二分钟，首轮最多 660 秒。完整目录的 CPU 冷启动单独分配二十五分钟，首轮最多 1,200 秒。

`domain` 先呈现资料与词表工具，模型可以通过正式工具说明请求其他工具的 Schema。`full` 首轮呈现完整 MCP 清单。两种配置各用独立合成数据库。`real-cpu-model-evidence/results.json` 记录短指令、模型和推理器版本、逐轮真实输入和输出 token、请求与结果字节数、实际工具调用、重复输入、终止原因及独立业务核对结果。某一任务失败时仍保存其结果与已经完成的其他场次。[工作流](../.github/workflows/model-proof.yml) · [宿主与判定](../tools/mcp-compat/model-proof.mjs)

同一工作流的 `intake` 选项测试 App 主动提交的后台任务。提交方经正式 HTTP 选择一张合成 PNG、一段 H.264 短片和一份不支持的文本附件，只授予本库的新增与整理权限。正式 `qai worker --once` 接入同一份模型权重。独立验证端从 Go 读回三项去向、两件资产与原件字节，逐项核对 SHA-256，再启动一个新执行器检查会不会重复入库。运行制品 `intake.json` 保存实际状态、使用量及通过的断言。[混合入库与原件回读](../tools/model-proof/intake.mjs)

`adb008a` 的混合任务通过六项验收。验收器经 App 使用的正式 API 提交三项输入，worker 自动认领。Qwen 自行选择按原名准备预览，再明确提交这份计划，两轮共 2 次调用、0 次重复。模型输入 1,938、输出 39 token。文件接收完毕后，从启动 worker 到返回用时 27.1 秒，其中任务处理用时 26.3 秒。Go 为图片和视频分别建立资产，验收器按各自的内容版本读回原件，大小、SHA-256 和类型均与提交相符，作者保持未知。文本附件单独保留 `unsupported_media`，整批显示部分完成。研究调用为 0，新执行器再次轮询时得到空队列和原有入库结果。[实际作业](https://github.com/Surtr42u/qimeng-next/actions/runs/44401162182/job/135144232294) · [逐项验收和模型用量](https://github.com/Surtr42u/qimeng-next/actions/runs/44401162182/artifacts/14474124632)

首轮混合入库 `cf02691` 实际唤起 worker 与模型。模型 10 轮生成、调用 10 次工具，7 次重复，输入 45,162、输出 573 token，触发无进展停止。Go 保留两项已暂存媒体和文本附件的拒绝原因，研究用量为零，两项媒体都未入库。[首轮混合任务与业务回读](https://github.com/Surtr42u/qimeng-next/actions/runs/44391206965)

`0d5f034` 改为按原选择序号表达命名意图，同题 7 轮生成、7 次调用中有 4 次重复，输入 9,613、输出 330 token。Go 三次返回 `plan.prepare` 所引用的对象不存在，媒体仍在暂存区。仅有工具名和错误码的诊断不能确定哪一个可选字段填错了。后续版本将作者、出处、角色和集合的查询与选取放到明确请求该类信息的步骤，普通命名只需选中输入并提供基名。[第二次混合任务与工具结果](https://github.com/Surtr42u/qimeng-next/actions/runs/44394947396)

`0046221` 同题遇到三次待处理输入选择错误和命名查询失败。14 次调用中有 4 次重复，模型使用 28,893 输入和 465 输出 token，两个原件继续暂存。后续工具把当前可处理的输入序号列入选择范围，混选时回报每个被排除的已知附件。使用原名的操作先生成预览，提交仍需明确选择这份计划。[第三次混合任务与工具结果](https://github.com/Surtr42u/qimeng-next/actions/runs/44396195614)

首轮 `27f71b3` 的领域配置呈现 15 个工具（7,920 字节）。一次词表查询返回 387 字节，模型正确给出两组正式名和 ID，两轮合计输入 5,311、输出 124 个 token。作者任务中，模型两次提交相同的写请求，两次均被拒绝，实际资料未变，三轮合计输入 8,130、输出 287 个 token。完整配置呈现 62 个工具（47,559 字节），两个任务都在收到第一条推理应答前达到 240 秒上限，token 用量未测。该轮宿主尚未转交 MCP 初始化说明。后续报告将未收到的用量记为 `null`。[首轮作业](https://github.com/Surtr42u/qimeng-next/actions/runs/44384690363/job/135085894125) · [逐轮报告](https://github.com/Surtr42u/qimeng-next/actions/runs/44384690363/artifacts/14466926397)

第二轮 `f2baba8` 转交了 MCP 初始化说明。领域配置呈现 16 个工具、9,410 字节。两个任务各调用一次工具，却都额外传入了本机并未持有的 `taskId`，返回 `TASK_NOT_BOUND`。标签检索共用 6,034 个输入、54 个输出 token，作者维护为 6,130 个输入、141 个输出 token。完整目录呈现 63 个工具、49,191 字节。标签题首轮在 660 秒超时，用量未测。作者题收到了应答，却把显示名填入稳定 ID 字段并给出与授权基线不符的修订，Go 返回 `author_task_stale`。该题共用 30,719 个输入、126 个输出 token。四场的独立回读均未通过任务判定，作者资料和词表保持原值。[第二轮作业](https://github.com/Surtr42u/qimeng-next/actions/runs/44388930776/job/135100958217) · [逐轮报告](https://github.com/Surtr42u/qimeng-next/actions/runs/44388930776/artifacts/14467952143)

第三轮 `c0dffcc` 按单任务凭据收敛输入。两种目录下，模型都在一次词表查询后答全两个名称和稳定 ID。领域配置输入/输出 4,601/182 token，完整配置为 24,633/190，完整配置首轮实际耗时 421 秒。两场作者题各执行一次有效更新，只传稳定操作键和简介。Go 最终读数确认作者修订只前进一次、简介按候选保存，其他字段保持原值。但模型都从写入响应引用了原文并声称已经回读，没有再调用读取工具，两场作者任务均未通过回读判定。两种配置的作者题输入/输出分别为 4,093/98 和 24,126/91 token。[第三轮作业](https://github.com/Surtr42u/qimeng-next/actions/runs/44394371554/job/135120193150) · [逐轮报告](https://github.com/Surtr42u/qimeng-next/actions/runs/44394371554/artifacts/14471262088)

此后的同题验收分别记录 `modelIssuedRead`、`verifiedByExecutor`。前者要求模型自己追加读取工具。后者由回环观察器核对同一工具调用内，成功的作者 PATCH 后确有独立 GET，且对象、修订与简介摘要相符。宿主发给模型的下一次推理请求须实际包含这份读数并收到推理应答。观察器原样转发 Go 的响应，仅保存方法、状态、修订及摘要。模型自行声称回读、单独的写回显或验收者事后读取都不足以满足条件。[独立观察器](../tools/model-proof/author-observer.mjs)

云端`mcp-compat`作业使用真实Go服务、Python官方SDK和相互独立的Pi/TypeScript客户端。测试脚本只访问临时服务与合成图片。最新通过的提交和未验证项见[当前状态](status.md)。仅源码或配置示例存在不构成通过证据。

协议依据：[MCP工具规范](https://modelcontextprotocol.io/specification/2026-07-28/server/tools)。新SDK的旧协议协商能力必须用独立客户端实测，不凭相同SDK自测推定广泛兼容。
