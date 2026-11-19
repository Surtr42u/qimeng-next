# 绮梦 Next

面向个人 PC / NAS 的自托管媒体库。可以用浏览器、Windows 桌面壳或 Android 客户端连接影库，也可以在 Android 上离线运行同一个 Go 服务。普通上传和浏览可独立于 AI 使用。

首次试用请选空的专用数据目录，按[快速开始](docs/quickstart.md)下载服务包、设置密码、登记媒体库并上传第一张图片。各端已验证的提交、现有缺口与真实资料切换条件见[当前状态](docs/status.md)。

## 从哪里开始

| 你要了解 | 阅读入口 |
|---|---|
| 运行、排障、升级和备份 PC/NAS 影库 | [快速开始](docs/quickstart.md)、[服务端配置](server/README.md#运行配置)、[迁移与回退](docs/migration.md) |
| 连接已有影库，或在手机上离线使用 | [Windows 桌面](desktop/README.md)、[Android 两种安装包](android/README.md) |
| 要做成什么、哪些行为必须保留 | [产品要求](docs/requirements.md) |
| 模块怎样分工、谁拥有业务真相 | [架构](docs/architecture.md) |
| 接手当前版本、取包、验收或回退 | [接手手册](docs/handover.md)、[各提交的验证结果](docs/status.md) |
| 开发、评审、同步文档和选择云端作业 | [开发与维护](CONTRIBUTING.md)、[验证与交付](docs/verification.md) |
| 报告安全问题或发现意外公开的信息 | [安全说明](SECURITY.md) |
| AI 首次配置、日常工作和 MCP 宿主 | [AI 工具入口](ai/README.md)、[Pi 与 Hermes 兼容指引](docs/mcp-compatibility.md) |
| 手机本机运行与后期剥离 | [运行模块边界](docs/runtime-separation.md) |
| AI 接手约束 | [AGENTS.md](AGENTS.md) |

编译、代码生成、自动测试和发布构建均在 GitHub Actions 执行。本地开发限于阅读、编辑、静态核对与 Git 操作。本仓以 MIT 许可证发布，重构起点、旧版界面参照及复用范围另有记录。[许可证](LICENSE) · [来源记录](docs/provenance.md)

## 产品原则

- 后端可在 PC/NAS 独立运行，也可由可移除的 Android 本机模块离线托管；两种形态共用业务核心，普通上传与浏览不依赖 AI。
- 稳定资产身份、明确内容版本；收藏、历史、作者关系与人工修正受保护。
- 智能批次逐项报告，已授权正常新增自动完成，重复/冲突/替换按实际授权处理。
- AI 工具按任务提供必要上下文，长结果与计划可引用接续，失败不能伪报成功。
- 保留旧版页面、导航、选中效果和外观选项。用新的状态和数据层支撑相同的操作。
