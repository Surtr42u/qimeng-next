# API 使用约定

唯一协议正文是 `api/openapi.yaml`（OpenAPI 3.0.3）。路径前缀 `/api/v1`，实例 API 版本 `0.1.0`。生成的 TypeScript/Kotlin/Python 契约和来源锁由云端输出，不手写 SDK 或修改生成物。

## 身份与错误

`GET /instance` 返回实例身份和明确能力。客户端缓存、队列和任务绑定实例 ID，不只绑定 URL。`POST /sessions`用`password`或可信`token`二选一换取独立会话；登录密码仅在此交换，不能充当API Bearer。每台设备会话独立，单设备退出不撤销其他设备。API接受Bearer，同源Web接受HttpOnly、SameSite=Strict cookie。cookie写入检查Origin。媒体原件使用同源cookie或带Bearer的客户端流式请求，支持Range，不把token放URL。

错误为 `{code,message,currentRevision?}`。权限拒绝 403；对象或内容变化、重复、名称冲突 409；不支持格式 415；有界上传/空间不足为 429/507。错误不能被客户端转成空列表或保存成功。

分页 `{items,nextCursor?,totalMatched?}`；媒体默认 50、作者默认 20、最大 100。媒体、作者、历史返回精确 `totalMatched`，按筛选及稳定复合排序键续页；前方新增或删除不会按偏移挤动后页。搜索 SQL 参数化，`%`、`_`、反斜杠都是普通字符；多词 AND，数字编号只命中完整数字段，括号页码与裸数字区分。

相册 `/assets/facets` 使用与媒体列表相同的筛选谓词。分区和类型按固定顺序返回；作者行混合常规出处与 COS 作者，角色行混合常规角色与 COS 目录作品。客户端只回传服务给出的 `workKeys`/`roleKeys`，同一行选项 OR、行间 AND；请求某行候选时只省自身行的选项，固定作者、收藏、历史等约束仍保留。`totalOptions` 是经过 `facetQ` 的候选总数，不是媒体数，也不是本页长度。

COS 分区由实际关联的 COS 作者决定，不按库类型猜测。COS 库根中尚无目录作者的文件仍属于常规分区；COS 文件可附普通作者，但相册 COS 作者维只投影 COS 身份。列表、候选、推荐、排行和统计共用这一判定。每个 Asset 返回权威 `workKeys/roleKeys` 及带当前名称的 `workFacets/roleFacets`，`cosWorkName` 给目录作品标题；客户端不从文件路径、分隔符或已加载页面推断这些身份。`viewCount/playCount` 为服务端累计值。

固定标签路由 `tag` 始终与可选 `tags/tagMode` 组合相 AND。文件管理使用 `libraryId+directory`：显式空字符串是库根，缺省代表不过滤；默认只含直接子文件，`recursive=true` 才包括后代目录。普通浏览、历史和搜索隐藏停用库，管理浏览可明确 `includeDisabled=true`；统计库存、原件和按 ID 读取详情保持，停用不等于删除记录。

默认排序为文件日期；收藏排序使用真实 `favoritedAt`，重复收藏不会刷新时间，取消后再次收藏才重新计时。`dateCounts=true` 仅首屏返回 `{date,fileCount}` 精确文件日期分桶，包含完整筛选集，并同时返回 `unknownDateCount`。`tzOffsetMinutes` 使用客户端相对 UTC 的东向分钟数（东八区 480、西五区 −300），缺省 0；它也决定日历时间筛选边界。来源未知的文件日期不虚构日期桶，仍计入总匹配数；未知数量不受页大小影响，也不要求界面新增分组标题。翻页不重新计算日期桶。

媒体、详情及历史附带资产使用统一 `Asset.fileDate`。它优先取当前已核验原件的 `contentMtime`，仅在缺失时回退到接收线索 `sourceMtime`，不取入库时间。原件外部变化被扫描核验后，旧接收时间不压过当前文件时间；客户端不用再拼优先级。0015 只重建该查询投影，不改原件、原输入线索或统计事件。

播放/观看范围和大小范围由 Go 解释 `playRange`/`viewRange`/`sizeRange`。`low` 为 1–5、`mid` 为 5–20，端点按界面文字包含；`<1MB` 和 `>50MB` 为严格边界，1–10 和 10–50 包含端点，MB 桶按 1,048,576 字节。客户端传枚举，不重复换算。

## 写入与回放

显式接收批次先通过`PUT /batches/{id}/inputs`一次登记完整原始选择：`expectedManifestRevision`初始为1，1–100个输入各带稳定`id`和原始文件名，已知时附长度/摘要及相对来源线索。封存后不增删，上传必须带本批`inputId`，且原名/来源/已知长度与摘要匹配；同输入绑定同一上传，重试沿原操作身份。隐式普通单文件由Go在同一事务自动登记并封存一项，不增加用户步骤。

文件提供器连名称也读取失败时，清单保留`id`并设置`nameState=unavailable`、省略原名，界面按选择序号显示读取失败，不能保存伪造文件名。默认`known`必须提供真实原名。后来来源恢复可沿原inputId上传，已知原名由Upload保留；原清单的未知状态不会改写成当时已知。

`BatchDetail.inputs`及分页任务上下文按原选择顺序报告，包括从未收到的`pending`项。完成条件为封存数量、输入映射、实际上传及入库逐项一致；只完成已经上传的子集不能标整批完成。旧开发批次不能反推真实意图，追加迁移标记`legacy_unverified`并保留旧明细，不伪造清单证据。原始选择与业务修订分别使用`manifestRevision`与`revision`。

取消尚未收到的项用`POST /batches/{id}/inputs/{inputId}/cancel`，提交原`manifestFingerprint`与稳定幂等键。清单行不删除，只记录`cancelled`；如已绑定未发布上传，同一用例取消该上传。已入库或有待恢复发布意图时拒绝倒退。取消项不算成功入库，整批保留partial和原始数量，来源继续保留。

文件提供器未知MIME用明确的`application/octet-stream`声明，不由客户端猜业务格式。Go按已支持扩展名确定标准类型、归一已知别名；明确声明不匹配仍拒绝。完整接收后继续校验魔数、内容长度/摘要和媒体探测，未知声明不能绕过实际文件校验。

`GET/PUT /settings`保留原设置页的扫描与上传语义。默认`scan.workers=0`自动、`scan.thumbEdge=800`预留、`upload.autoAccept=true`；PUT需完整三项及`expectedRevision`，遗漏不以零值替代。自动并发按可用CPU及内存保守取1–8库，手动1–32；队列最多64个活动任务，同库不能重复排队。调整只影响后续准入，不打断运行任务；等待和运行均可取消。上传开关关闭后拒绝新会话、分片与新发布，已接收内容和已完成回执保留，取消/查询仍可用。

缩略图`size=sm|md|lg`对应长边256/512/1024，缺省md。预留`thumbEdge`保存不改变此管线。缓存包含内容版本、策略与尺寸，ETag取实际缩略图字节摘要；旧版本或原件已变拒绝返回另一内容，原件出口始终逐字节直出。

浏览回顾返回服务端日历窗口`fromDay/toDay/previousFromDay/previousToDay`和本期/前期`viewCount/previousViewCount`，不让客户端猜前期。`all`起点和前期日期为空串；固定窗口前期等长且紧邻，本期或前期无浏览都不可比较。`totalSeconds/imageSeconds/animatedSeconds/videoSeconds`来自日聚合，三类型互斥，包含只有停留而没有新增浏览的日期；视频项仍是详情停留。

`/stats/top-interests`专门给原Android混合Top3卡提供作者/标签结果，Go按views降序、同分作者在前、再按稳定身份升序选出最多三项。多作者/标签各自完整计数，客户端只展示与跳转，不自行拼接截断榜单。

创建、文件动作、上传完成和 AI 提交使用稳定 `Idempotency-Key`（8–128 字符）。同凭据、同操作、同键且输入不变回放原响应；输入改变返回冲突。业务数据与回执同事务保存。`GET /operations/{key}` 只读取当前凭据的回执；键用于多个操作时加 `operation`。`not_recorded` 不证明文件副作用从未开始，应使用原输入与原键重试或读取上传/资产状态。

普通元数据使用 `expectedRevision`；标签集合写入使用 `Asset.tagsRevision`，空数组是明确清空。作者编辑集合为 `authorIds`，可附 `expectedAuthorsRevision`；COS 的固定目录作者由后端保留，用户编辑替换普通作者集合，清空普通作者不会删除目录身份。有真实作者就解除未知占位。点赞 `liked=true` 请求当天一次累计，`false` 不撤销已存点赞；`liked` 响应指有明确当天记录，`likeCount` 包含已迁入的真实累计基线。无日期基线不编造日事件，只影响累计推荐输入及全时排行，不塞进日/周/月排行。

标签整组另有 `tagSetUpdatedAt` 与 `tagSetSource`。只有真正标签编辑、词表改名/删除造成的关联变化及明确迁入整组会修改它们，喜欢、改名或移动不会充当标签时钟。未知/相同时间的不同整组保持冲突；迁移不取并集来掩盖先后不明。词表墓碑也适用于迁入关系，不因旧候选重新关联而复活词条。

管理接口提供库启停、目录创建和分页目录读取、标签词表维护、单条标签移除、作者删除和明确合并。全局词表改名/删除与资产关联在一个事务内收敛。`sourceIds` 与 `characterIds` 是原作与角色的稳定关联；`source`/`characters` 为兼容展示。`acquisitionSources` 是单独的用户获取渠道。

作者也有单独的 `acquisitionSources` 全量集合，以作者修订保护清空/替换。`/acquisition-vocabulary` 保存用户维护的组合渠道词表，`sources` 保留组合项与顺序，`suggestions` 由 Go 按空白拆词并保序去重。公开主页和作品出处不会被当作获取渠道；当前资料任务 scope 未授权此字段，AI 不能通过夹带新字段修改它。

`GET /storages` 只返回可信逻辑根 ID，不泄露本机绝对路径。创建库可指定 `storageId` 和 `rootRelativePath` 原地登记目录；内置 `data` 存储始终分配隔离目录。登记不等于扫描已完成。媒体原件与缩略图请求可传 `version`，与当前内容版本不符返回 409，不把新版字节填入旧版缓存地址。

视频时间轴通过 `/assets/{id}/timeline` 管理；修改携带标记修订与当前内容版本。旧版标记继续可读，客户端应明确标示版本差异，不自动跳转到推测的新位置。

旧 v2 事件和时间轴未携带来源内容版本，迁入后读接口使用 `contentVersion=0` 表示未知；不能将它绑定到当前文件版本或自动跳转。写接口仍要求当前版本至少为 1。历史响应的原事件 ID 按读取约束保存，不套用新上报 ID 的长度限制；累计统计不因此重放或删改。

## 上传与智能批次

本人库管理界面的原有“根目录路径”输入框通过 `POST /storages/resolve-library-root` 解析，body 为 `{rootPath}`，返回 `{storageId,rootRelativePath,rootPath}`。它只读取和验证，不创建目录；必须落在已配置的媒体根内，拒绝控制目录、链接及越界。创建库仍使用现有逻辑位置字段。`GET /libraries/{id}/location` 给本人回显登记根；两个接口均要求 admin，不加入MCP工具映射，一般库列表不暴露本机绝对路径。

先 `POST /uploads` 固定目标、原名、MIME、声明长度、模式、可用来源相对目录/时间与批次。`PUT /uploads/{id}/content` 传原始 bytes，头 `Upload-Offset`，每片最多 8 MiB。`GET /uploads/{id}` 是恢复时的权威偏移。重复分片须字节一致；已封存上传拒绝后续写入。声明总长度参与空间与并发资源预算，没有退回单文件业务大小上限。

客户端具备完整源快照时传 `expectedHash`（SHA-256），与创建操作身份绑定。服务端在 complete 封存和发布前复算，不匹配时返回 `source_hash_mismatch`，不新增资产。未提供该字段时，接收摘要只能证明服务收到的字节，不能证明断点恢复前后客户端源文件未变化。

`originalFileName` 保存接收时真实原名，`sourceRelativePath` 保存可用的相对来源线索，二者与目标 `fileName` 分离。用户选名、Go 规范命名或替换都不抹掉输入映射。原名可保留坏编码线索，最终文件名仍必须通过命名校验；绝对来源路径、SAF URI 和穿越路径不进入这些字段。

普通 complete 返回已入库资产；智能 complete 返回 staged。精确重复返回输入的 `duplicate` 状态和明确原因，保持原件，不能当普通成功或替换。明确替换只允许设备管理员，须指定 `replaceAssetId` 与 `expectedContentVersion`，不因查重再创建新资产。

智能流程由批次拥有权威状态：`getTaskContext → submitResearch（需要时）→ preparePlan → getPlan → commitPlan → getBatch`。上下文分页且说明未读内容，完整计划有稳定服务端引用。模型不自行计算作品编号、猜页序或改写用户备注。剩余研究预算与逐项状态可回读；未知作者不阻塞正常新增，冲突只处理受影响项。

手动上传与 AI 共同使用 `/libraries/{id}/naming/context`、`suggestions` 和 `preview`。预览不占号，返回作用域摘要和规范名称；最终发布再次核验。空组图使用同一首作品分配器，补页从最大页之后追加。当前普通上传的同集合条目须按选定顺序 complete，前项失败待处理时后项不越过；可并发传输字节，客户端不计算页码。

## 媒体与备份

长期 AI 授权的 `canRecycleSources` 缺省关闭，只在同时允许新增且选定媒体库时可启用。派生接收任务仅在这一明确授权下获得 `source.cleanup`；作者资料任务没有该权限。服务器许可与执行器本地回收授权必须同时成立，不以 `ingest` 或 `organize` 推导删除权。

资产作者编辑整体替换可编辑的常规作者集合。COS 目录建立的作者身份固定保留；COS 文件可以附加常规作者，提交空常规集合只解除常规关联，不能从此编辑路径添加别的目录 COS 身份。作者修订和资产修订继续拒绝迟到草稿；分区由实际 COS 作者关联判定。

`contentUrl` 指原件，`thumbnailUrl` 指真实有界缩略图。PNG/JPEG/GIF 使用标准图片解码器；其他格式预览/元数据依赖服务端媒体工具。`probeState` 区分 ready、unavailable 和 failed；旧版进度通过 `positionContentVersion` 与当前文件版本区分。

`POST /backups` 创建一致性快照与版本清单，返回 `awaiting_external_backup`。管理员调用 `POST /backups/{id}/export` 后，通过 `GET /backups/{id}` 回读持久导出状态；只有 `exportState=completed` 才能从 `GET /backups/{id}/archive` 下载完整 `.qnext`，支持 Range 与 SHA-256 校验。失败或中断按原记录重试；检查点原件已经替换或缺失时明确失败，需新检查点，不把新版字节放进旧版本备份。

完整归档使用有界清单和顺序流式载荷，包含 SQLite 业务事实、作者离线头像与迁移原始证据、现存及回收站原件，以及所有未入库上传的已提交前缀。派生缩略图和临时研究缓存可重建，不纳入归档；历史备份记录保留，但不递归打包其他归档。下载与本机归档均不构成独立备份证明，来源清理仍须另有可信目标实际读回的证据。

批次独立保护由 `POST /batches/{id}/protections` 启动，状态从同名列表/详情回读，失败通过原 `protectionId` 的 `/retry` 继续。它绑定 sealed 原始清单指纹；每个原输入都在报告中，成功项保护准确的 assetId/contentVersion/hash/size，未接收、取消或版本不可得的输入保留并说明原因。服务端向可信默认目标复制完整业务快照及这些内容对象，关闭文件后重新读取 SHA-256；同内容对象可以复用，但复用前仍核验。它是本批增量保护，不宣称包含全库所有原件；全库便携恢复使用 `.qnext`。

`QIMENG_BACKUP_TARGETS` 是受信部署配置，形如逻辑 ID 映射到 `{path,failureDomain}`，路径必须为绝对目录；`QIMENG_DEFAULT_BACKUP_TARGET` 指定批次默认目标。模型/API 不提供任意路径。服务同时拒绝目标与本地业务数据或任一媒体库位于同卷；配置者仍须确认不同物理故障域，卷号不能证明 NAS 硬件独立。`GET /backup-targets` 只向管理员返回逻辑名称、可用状态与不同卷检测结果。

来源回收许可与回执仅给可信执行器，不能直接暴露成模型工具。`source.cleanup` 是独立权限：`POST /batches/{id}/protections/{protectionId}/cleanup-permits` 绑定 inputId、来源 SHA-256/长度、备份证明、内容版本、credentialId/generation，签发前重读独立目标及当前原件；许可最长 20 分钟且不超过凭据有效期。执行器须核对当前实例/租约、原文件对象与摘要，并可靠移入回收站；不支持回收、只读、离线或来源改变都保留。未决许可不得换 ID 再执行，先用 `/batches/{id}/cleanup-permits` 或 `/cleanup-permits/{id}` 回读。

`POST /cleanup-permits/{id}/receipt` 只记录实际结果。`recycled` 必须附不含本机路径的稳定提供方回执；新租约可为同一 credentialId 回报旧许可已经发生但丢失回执的事实，旧 worker 仍被统一 generation 校验拒绝。回执允许晚于许可期限，因其不重新授权文件副作用。来源去向仅在服务器收到事实回执后成为 recycled；一个输入的成功不把原始清单中其他输入改为已清理。

离线工具 `qimeng-backup verify --archive <文件> --sha256 <服务端摘要>` 校验清单、逐项摘要、数据库迁移及实际结构、外键和载荷完整覆盖。`qimeng-backup restore --archive <文件> --sha256 <服务端摘要> --destination <不存在的新目录>` 在隔离目录恢复；不覆盖现有实例或沿原配置写 NAS 路径。失败保留 `RESTORE_INCOMPLETE` 标记且拒绝启动服务。成功保留资产、作者、关系、事件及上传身份，全部库改登记在新目录下；实例 ID 更新，旧会话、幂等凭据回执及 AI 授权撤销，需用新配置登录。历史备份记录明确 `snapshot_not_included`，不能冒充当前可恢复档案。自动化验证与实际恢复演练的证据分别记录。

v2 迁移使用管理员专用 `/migration/v2/imports` 接收原始文件（最多 64 MiB）。`sourceId` 是可信端为来源安装保存的稳定标识，不能拿应用包名代替安装身份。原文件逐字留存并可认证下载；非 v2、缺事件数组和非法身份在写入业务前拒绝。

先登记并扫描原件，再创建迁移计划。唯一源身份优先，文件名、相对路径与库名只能用于唯一消歧提案；显式映射可修正提案。所有源记录通过计划的 `items`/`authors` 分页回读，歧义、多对一和失效映射不会取首条。提交绑定指纹并明确是否接受跳过，重新核验资产修订、内容版本与当前原件摘要，生成导入前数据库快照后原子写入元数据及回执。旧 v2 没有源原件 SHA-256，`sourceHashesAvailable=false` 始终如实保留这一边界。

迁移保留原事件 ID、会话、可空秒数和原日键，按身份去重；不把聚合统计重新造为事件。作者档案与离线头像经正式档案导入用例补缺，人工资料保持；原 TXT 片段和关系溯源完整保存。旧点赞累计完整保留为 lifetime 基线，存在事实的最后一日单独参与日去重，其余以 `unattributedLikeCount` 明示未分日；不虚构每日历史。相同来源与相同累计重导不重复相加，无法证明与目标已有票数不重叠时 `likeAction=conflict` 并保留双方证据。原最后日未知时，迁入当日不接受可能重复的新票；次日的新操作继续在保留基线上累计。推荐权重仅恢复未设置的目标，其余源运行配置不启动任务、不改写本机存储配置。真实 NAS 与迁移后实库验收仍需独立证据。
