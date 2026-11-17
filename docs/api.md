# API 使用约定

唯一协议正文是 `api/openapi.yaml`（OpenAPI 3.0.3）。路径前缀 `/api/v1`，实例 API 版本 `0.1.0`。生成的 TypeScript/Kotlin/Python 契约和来源锁由云端输出，不手写 SDK 或修改生成物。

## 身份与错误

`GET /instance` 返回实例身份和明确能力。客户端缓存、队列和任务绑定实例 ID，不只绑定 URL。`POST /sessions` 用受信设备配置凭据换取独立会话；API 接受 Bearer，同源 Web 接受 HttpOnly、SameSite=Strict cookie。cookie 写入检查 Origin。媒体原件使用同源 cookie 或带 Bearer 的客户端流式请求，支持 Range，不把 token 放 URL。

错误为 `{code,message,currentRevision?}`。权限拒绝 403；对象或内容变化、重复、名称冲突 409；不支持格式 415；有界上传/空间不足为 429/507。错误不能被客户端转成空列表或保存成功。

分页 `{items,nextCursor?}`；媒体默认 50、作者默认 20、最大 100。游标绑定筛选条件，条件变化须重新加载。搜索 SQL 参数化并转义 LIKE 通配符。资产可按库、常规/COS、作者关系、集合、标签、收藏、累计赞过状态及媒体类型过滤。

## 写入与回放

创建、文件动作、上传完成和 AI 提交使用稳定 `Idempotency-Key`（8–128 字符）。同凭据、同操作、同键且输入不变回放原响应；输入改变返回冲突。业务数据与回执同事务保存。`GET /operations/{key}` 只读取当前凭据的回执；键用于多个操作时加 `operation`。`not_recorded` 不证明文件副作用从未开始，应使用原输入与原键重试或读取上传/资产状态。

普通元数据使用 `expectedRevision`；标签集合写入使用 `Asset.tagsRevision`，空数组是明确清空。作者完整集合为 `authorIds`，可附 `expectedAuthorsRevision`；常规/COS 作者不交叉关联，有真实作者就解除未知占位。点赞 `liked=true` 请求当天一次累计，`false` 不撤销已存点赞；`liked` 响应指当天已赞，`likeCount` 为累计数。

## 上传与智能批次

先 `POST /uploads` 固定目标、原名、MIME、声明长度、模式、可用来源相对目录/时间与批次。`PUT /uploads/{id}/content` 传原始 bytes，头 `Upload-Offset`，每片最多 8 MiB。`GET /uploads/{id}` 是恢复时的权威偏移。重复分片须字节一致；已封存上传拒绝后续写入。声明总长度参与空间与并发资源预算，没有退回单文件业务大小上限。

普通 complete 返回已入库资产；智能 complete 返回 staged。精确重复返回输入的 `duplicate` 状态和明确原因，保持原件，不能当普通成功或替换。明确替换只允许设备管理员，须指定 `replaceAssetId` 与 `expectedContentVersion`，不因查重再创建新资产。

智能流程由批次拥有权威状态：`getTaskContext → submitResearch（需要时）→ preparePlan → getPlan → commitPlan → getBatch`。上下文分页且说明未读内容，完整计划有稳定服务端引用。模型不自行计算作品编号、猜页序或改写用户备注。剩余研究预算与逐项状态可回读；未知作者不阻塞正常新增，冲突只处理受影响项。

## 媒体与备份

`contentUrl` 指原件，`thumbnailUrl` 指真实有界缩略图。PNG/JPEG/GIF 使用标准图片解码器；其他格式预览/元数据依赖服务端媒体工具。`probeState` 区分 ready、unavailable 和 failed；旧版进度通过 `positionContentVersion` 与当前文件版本区分。

备份创建只产生一致性快照与版本清单，返回 `awaiting_external_backup`。它不允许来源清理，也不表示具备恢复证据。旧数据迁移和真实 NAS/设备验收不从这个 API 响应推导完成。
