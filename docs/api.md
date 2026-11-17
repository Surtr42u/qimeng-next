# API 使用约定

唯一协议正文是 `api/openapi.yaml`（OpenAPI 3.0.3）。路径前缀 `/api/v1`，实例 API 版本 `0.1.0`。生成的 TypeScript/Kotlin/Python 契约和来源锁由云端输出，不手写 SDK 或修改生成物。

## 身份与错误

`GET /instance` 返回实例身份和明确能力。客户端缓存、队列和任务绑定实例 ID，不只绑定 URL。`POST /sessions` 用受信设备配置凭据换取独立会话；API 接受 Bearer，同源 Web 接受 HttpOnly、SameSite=Strict cookie。cookie 写入检查 Origin。媒体原件使用同源 cookie 或带 Bearer 的客户端流式请求，支持 Range，不把 token 放 URL。

错误为 `{code,message,currentRevision?}`。权限拒绝 403；对象或内容变化、重复、名称冲突 409；不支持格式 415；有界上传/空间不足为 429/507。错误不能被客户端转成空列表或保存成功。

分页 `{items,nextCursor?,totalMatched?}`；媒体默认 50、作者默认 20、最大 100。媒体、作者、历史返回精确 `totalMatched`，按筛选及稳定复合排序键续页；前方新增或删除不会按偏移挤动后页。搜索 SQL 参数化，`%`、`_`、反斜杠都是普通字符；多词 AND，数字编号只命中完整数字段，括号页码与裸数字区分。

相册 `/assets/facets` 使用与媒体列表相同的筛选谓词。分区和类型按固定顺序返回；作者行混合常规出处与 COS 作者，角色行混合常规角色与 COS 目录作品。客户端只回传服务给出的 `workKeys`/`roleKeys`，同一行选项 OR、行间 AND；请求某行候选时只省自身行的选项，固定作者、收藏、历史等约束仍保留。`totalOptions` 是经过 `facetQ` 的候选总数，不是媒体数，也不是本页长度。

默认排序为文件日期；收藏排序使用真实 `favoritedAt`，重复收藏不会刷新时间，取消后再次收藏才重新计时。`dateCounts=true` 仅首屏返回 `{date,fileCount}` 精确文件日期分桶，包含完整筛选集。`tzOffsetMinutes` 使用客户端相对 UTC 的东向分钟数（东八区 480、西五区 −300），缺省 0；它也决定日历时间筛选边界。来源未知的文件日期不虚构日期桶，仍计入总匹配数。翻页不重新计算日期桶。

播放/观看范围和大小范围由 Go 解释 `playRange`/`viewRange`/`sizeRange`。`low` 为 1–5、`mid` 为 5–20，端点按界面文字包含；`<1MB` 和 `>50MB` 为严格边界，1–10 和 10–50 包含端点，MB 桶按 1,048,576 字节。客户端传枚举，不重复换算。

## 写入与回放

创建、文件动作、上传完成和 AI 提交使用稳定 `Idempotency-Key`（8–128 字符）。同凭据、同操作、同键且输入不变回放原响应；输入改变返回冲突。业务数据与回执同事务保存。`GET /operations/{key}` 只读取当前凭据的回执；键用于多个操作时加 `operation`。`not_recorded` 不证明文件副作用从未开始，应使用原输入与原键重试或读取上传/资产状态。

普通元数据使用 `expectedRevision`；标签集合写入使用 `Asset.tagsRevision`，空数组是明确清空。作者完整集合为 `authorIds`，可附 `expectedAuthorsRevision`；常规/COS 作者不交叉关联，有真实作者就解除未知占位。点赞 `liked=true` 请求当天一次累计，`false` 不撤销已存点赞；`liked` 响应指当天已赞，`likeCount` 为累计数。

标签整组另有 `tagSetUpdatedAt` 与 `tagSetSource`。只有真正标签编辑、词表改名/删除造成的关联变化及明确迁入整组会修改它们，喜欢、改名或移动不会充当标签时钟。未知/相同时间的不同整组保持冲突；迁移不取并集来掩盖先后不明。词表墓碑也适用于迁入关系，不因旧候选重新关联而复活词条。

管理接口提供库启停、目录创建和分页目录读取、标签词表维护、单条标签移除、作者删除和明确合并。全局词表改名/删除与资产关联在一个事务内收敛。`sourceIds` 与 `characterIds` 是原作与角色的稳定关联；`source`/`characters` 为兼容展示。`acquisitionSources` 是单独的用户获取渠道。

作者也有单独的 `acquisitionSources` 全量集合，以作者修订保护清空/替换。`/acquisition-vocabulary` 保存用户维护的组合渠道词表，`sources` 保留组合项与顺序，`suggestions` 由 Go 按空白拆词并保序去重。公开主页和作品出处不会被当作获取渠道；当前资料任务 scope 未授权此字段，AI 不能通过夹带新字段修改它。

`GET /storages` 只返回可信逻辑根 ID，不泄露本机绝对路径。创建库可指定 `storageId` 和 `rootRelativePath` 原地登记目录；内置 `data` 存储始终分配隔离目录。登记不等于扫描已完成。媒体原件与缩略图请求可传 `version`，与当前内容版本不符返回 409，不把新版字节填入旧版缓存地址。

视频时间轴通过 `/assets/{id}/timeline` 管理；修改携带标记修订与当前内容版本。旧版标记继续可读，客户端应明确标示版本差异，不自动跳转到推测的新位置。

## 上传与智能批次

先 `POST /uploads` 固定目标、原名、MIME、声明长度、模式、可用来源相对目录/时间与批次。`PUT /uploads/{id}/content` 传原始 bytes，头 `Upload-Offset`，每片最多 8 MiB。`GET /uploads/{id}` 是恢复时的权威偏移。重复分片须字节一致；已封存上传拒绝后续写入。声明总长度参与空间与并发资源预算，没有退回单文件业务大小上限。

客户端具备完整源快照时传 `expectedHash`（SHA-256），与创建操作身份绑定。服务端在 complete 封存和发布前复算，不匹配时返回 `source_hash_mismatch`，不新增资产。未提供该字段时，接收摘要只能证明服务收到的字节，不能证明断点恢复前后客户端源文件未变化。

`originalFileName` 保存接收时真实原名，`sourceRelativePath` 保存可用的相对来源线索，二者与目标 `fileName` 分离。用户选名、Go 规范命名或替换都不抹掉输入映射。原名可保留坏编码线索，最终文件名仍必须通过命名校验；绝对来源路径、SAF URI 和穿越路径不进入这些字段。

普通 complete 返回已入库资产；智能 complete 返回 staged。精确重复返回输入的 `duplicate` 状态和明确原因，保持原件，不能当普通成功或替换。明确替换只允许设备管理员，须指定 `replaceAssetId` 与 `expectedContentVersion`，不因查重再创建新资产。

智能流程由批次拥有权威状态：`getTaskContext → submitResearch（需要时）→ preparePlan → getPlan → commitPlan → getBatch`。上下文分页且说明未读内容，完整计划有稳定服务端引用。模型不自行计算作品编号、猜页序或改写用户备注。剩余研究预算与逐项状态可回读；未知作者不阻塞正常新增，冲突只处理受影响项。

手动上传与 AI 共同使用 `/libraries/{id}/naming/context`、`suggestions` 和 `preview`。预览不占号，返回作用域摘要和规范名称；最终发布再次核验。空组图使用同一首作品分配器，补页从最大页之后追加。当前普通上传的同集合条目须按选定顺序 complete，前项失败待处理时后项不越过；可并发传输字节，客户端不计算页码。

## 媒体与备份

`contentUrl` 指原件，`thumbnailUrl` 指真实有界缩略图。PNG/JPEG/GIF 使用标准图片解码器；其他格式预览/元数据依赖服务端媒体工具。`probeState` 区分 ready、unavailable 和 failed；旧版进度通过 `positionContentVersion` 与当前文件版本区分。

备份创建只产生一致性快照与版本清单，返回 `awaiting_external_backup`。它不允许来源清理，也不表示具备恢复证据。旧数据迁移和真实 NAS/设备验收不从这个 API 响应推导完成。

v2 迁移使用管理员专用 `/migration/v2/imports` 接收原始文件（最多 64 MiB）。`sourceId` 是可信端为来源安装保存的稳定标识，不能拿应用包名代替安装身份。原文件逐字留存并可认证下载；非 v2、缺事件数组和非法身份在写入业务前拒绝。

先登记并扫描原件，再创建迁移计划。唯一源身份优先，文件名、相对路径与库名只能用于唯一消歧提案；显式映射可修正提案。所有源记录通过计划的 `items`/`authors` 分页回读，歧义、多对一和失效映射不会取首条。提交绑定指纹并明确是否接受跳过，重新核验资产修订、内容版本与当前原件摘要，生成导入前数据库快照后原子写入元数据及回执。旧 v2 没有源原件 SHA-256，`sourceHashesAvailable=false` 始终如实保留这一边界。

迁移保留原事件 ID、会话、可空秒数和原日键，按身份去重；不把聚合统计重新造为事件。作者档案与离线头像经正式档案导入用例补缺，人工资料保持；原 TXT 片段和关系溯源完整保存。点赞只能恢复有原日键的一票，累计总数不能凭空还原逐日身份，原载荷继续保留。推荐权重仅恢复未设置的目标，其余源运行配置不启动任务、不改写本机存储配置。独立原件备份、正式停服回退和迁移后实库验收仍是另行验证的边界。
