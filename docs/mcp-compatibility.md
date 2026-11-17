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

Hermes整机交互验收与传输测试分别记录；不能用另一个SDK通过替代Hermes本身的测试证据。

## Codex、ZCode与其他宿主

选择本地stdio服务器，command填工具包的qai可执行文件，args为mcp，配置相同可信环境。宿主的字段名称和凭据保存方式按其实际版本设置；不发布猜测的私有配置。接入后先检查platform.catalog、library.query与一次明确被拒绝的越权调用，再在隔离数据执行完整工作流。

## 证据与边界

云端`mcp-compat`作业使用真实Go服务、Python官方SDK和相互独立的Pi/TypeScript客户端。测试脚本只访问临时服务与合成图片。最新通过的提交和未验证项见[当前状态](status.md)。仅源码或配置示例存在不构成通过证据。

协议依据：[MCP工具规范](https://modelcontextprotocol.io/specification/2026-07-28/server/tools)。新SDK的旧协议协商能力必须用独立客户端实测，不凭相同SDK自测推定广泛兼容。
