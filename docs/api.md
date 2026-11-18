# API 使用约定

唯一协议正文是 `api/openapi.yaml`（OpenAPI 3.0.3）。路径前缀 `/api/v1`，实例 API 版本 `0.1.0`。云端从该协议生成 Go 客户端与标准库服务端路由、TypeScript/Kotlin/Python 契约及来源锁。`httpapi.API` 必须实现全部生成操作才能编译。服务启动时逐条核对生成路由与内嵌协议。受保护的请求先验凭据，再绑定路径、查询和请求头。JSON 结构校验涵盖引用、组合、嵌套对象和数组元素中的必填字段与空值。修订冲突、额度和业务取值范围由用例处理。二进制上传分片与支持 Range 的响应直接使用 HTTP 正文流。

## 公开资料查证

本人首次创建 AI 授权时用 `researchProviders` 逐项选择可发查询的公开来源，同时设置 `canResearch=true` 和正数 `maxResearchCalls`。缺省和空列表均保留本地研究。迁移 0033 为已有任务设置空名单。派生任务可请求更小的来源集合或研究预算。部署者还须显式开启 `QIMENG_RESEARCH_ENABLED=true`。读取 `GET /research/context` 可查看实例开关、各来源支持的操作、当前授权及任务剩余额度。任务上下文的 `queries` 按新到旧列出已发查询及回执 ID，供执行器接续。

| 来源 | 操作 | 实际发送的内容 |
| --- | --- | --- |
| FxTwitter | `author`、`post` | 公开 X 账号名或帖文数字 ID |
| Pixiv | `author`、`work` | 作者或作品数字 ID |
| yande.re | `artist`、`tags` | 公开作者名或标签表达式 |
| e621 | `post`、`tags` | 帖文数字 ID 或标签表达式 |
| Wayback CDX | `url` | 无凭据、查询串及片段的公开 HTTP 或 HTTPS 历史页面地址，仅发送给固定的 HTTPS 存档索引 |
| Steam Workshop | `workshop` | 创意工坊作品数字 ID |
| Bing | `search` | 明确提交的公开搜索词 |

获准的任务通过 `POST /research/providers/{provider}/query` 发起一次查询。接收批次须同时指定本批待整理的 `uploadId` 和 `expectedHash`。已有选定作者的上传复用该身份，查询作者资料由该作者的维护任务承担。作品任务绑定库、哈希、内容版本和修订，作者任务绑定作者和修订。Go 在发出和返回时核实授权与对象，自动生成的外部请求只携公开查证所需的参数。搜索或存档结果标记为 `lead`，平台记录标记为 `platform_record`，都须结合实际作品和账号证据判断。

服务在联网前持久扣除一次任务调用和一次研究额度。相同幂等键及输入复用原回执，处理中返回 HTTP 202，完成后可通过 `GET /research/queries/{id}` 读取。同一实例最多两次并行查询，每个来源滚动一分钟最多 20 次。每次最多读取 2 MiB、等待 10 秒并返回 20 项。请求固定 HTTPS 端点。连接前核验该次 DNS 的所有地址，禁用代理和重定向。响应保留有界的公开名称、ID、时间和出处链接，链接剥离凭据与跟踪参数。

`not_found` 表示本次查询未找到对应记录。`blocked`、`rate_limited`、`unavailable` 和 `interrupted` 均保留未确定状态。撤销授权、取消任务或轮换执行器会结束相关在途请求。进程重启后，可以回读已结束和已中断的记录。对批次提交研究候选时，`providerQueryIds` 引用属于同一上传和哈希的已完成回执。仅汇报这些已收费查询时填 `callsUsed=0`，候选出处须在引用的结果中出现。作者归属、人工资料与命名计划分别通过各自的业务操作处理。

若实例关闭公开查证，返回 HTTP 403 和 `research_provider_disabled`，请求尚未发出，本地整理可继续。遇到查询应答丢失，可用当前任务凭据读取 `GET /operations/{key}?operation=queryResearchProvider`。原查询仍在进行时返回 HTTP 409 和 `research_query_pending`。终态回执含原键、查询 ID、任务 ID 及结果状态。同一任务续接后可核销原回执。隔离恢复后重新授权的执行器只能核销自身签发的查询。旧开发版本缺少签发记录时，管理员仍可按查询 ID 阅读留存的证据。

### 单帧反向查证

`GET /research/context` 的 `imageLookupEnabled` 表示部署者已配置受信 Lens 伴随服务，`imageLookupAllowed` 表示当前授权或任务允许外发。本人创建长期授权时须指定 `canLookupImages=true`、研究权限、正数预算与媒体范围。接收或作品任务还须主动指定 `allowImageLookup=true`。旧授权、旧任务与省略字段的任务均保持单帧外发关闭。七个文本来源的名单与执行器向模型展示预览的许可分别配置。

从当前任务的研究材料取 `artifacts[i].id` 和 `artifacts[i].sha256`。调用 `POST /research/image-lookup` 时将两者传作 `frameId` 和 `frameSha256`，接收任务另传 `uploadId` 与 `expectedHash`，已有作品另传 `assetId` 与 `contentVersion`。本地预览缓存有效期为 24 小时，过期后可重新读取当前任务的材料。Go 在入账前核实任务、源版本及帧摘要，只向配置好的 `/lens` 发送一个名为 `frame.jpg` 的 multipart 文件，最长边 640 像素、最多 2 MiB。上传名、路径、作品或任务 ID 均留在本机。伴随服务收到的字节摘要须与所选帧一致。

请求与文本查证共享任务调用和研究预算。单次最长 95 秒，并受任务截止时间约束；响应上限 256 KiB。同一幂等键重复提交只取得原回执，处理中为 202。应答丢失可按 `GET /operations/{key}?operation=lookupResearchImage` 或查询 ID 核销。成功回执的 `items` 只收录真实候选链接，`imageHints` 单列无链接的域名与标题，二者最多各 20 项。空结果为 `not_found`。伴随服务的 `BUSY` 为 `rate_limited`，`BLOCKED_BOT` 为 `blocked`；浏览器未就绪、未到结果页、超时或摘要不符均为 `unavailable`。已中断的任务保留 `interrupted`。所有 Lens 线索的 `evidenceKind` 都是 `lead`，确认作品归属仍须独立核实。

## 显式设备维护包

PC 或 NAS 的管理员在 `POST /device/credentials` 登记目标设备及客户端生成的 32 字节独立随机凭据（无填充 base64url）。服务只返回凭据 ID、设备标签、显示名和到期时间。轮换须提供当前凭据 ID。设备可用 `GET /sessions/current` 核对本人的设备身份与 `device.apply` 权限，登记端的媒体目录和管理接口另需本人会话。

源实例管理员向 `POST /device/native-deliveries` 提交目标设备、接收实例 ID 和明确勾选的对象。每项给出来源稳定 ID 和预期业务修订。可选对象包括编号作者 TXT 片段、作者的 X/基本/账号资料块，以及指定视频当前内容版本上的一条时间轴标记。服务导出该修订的声明或已确认资料，排除私人备注、头像原件、候选和订阅。协议 2 的任务只含一个 `nativeSelection` 段，段内格式独立从 1 计，最大 2 MiB、最多 32 项。同一来源内容发到同一接收实例时，同样的选项产生相同的 SHA-256 包身份。

交付时间轴时，`sourceObjectId` 是来源标记 ID，`expectedRevision` 是标记修订。`sourceAssetId`、`sourceContentVersion`、`targetAssetId`、`targetContentVersion` 必须逐个指定。源服务核验视频原件并导出标记时间、标签、颜色、视频字节数和 SHA-256；段中只有这些元数据。接收端只打开明确指定的本机视频。预览和提交分别核对实际字节，写前再次检查文件身份、变化时间和目录中当前原件。相同摘要的其他资产不会参与选择。目标版本变化、同名异内容或标记已绑定别的对象时，逐项回执分别给出 `target_content_changed` 或 `target_binding_conflict`，保留原标记与人工修订。

接收端只查询 `protocolVersion=2` 的待领取任务。领取后用设备凭据按字节范围下载，核实段长度、SHA-256、已认证源实例和本机实例，再向本机 `POST /native-deliveries/imports` 提交原始字节与 `keep` 或 `replace`。返回的预览逐项给出目标、修订和处置。用户确认后提交 `POST /native-deliveries/imports/{id}/commit` 附同一指纹。已绑定来源的数据只有在 `replace` 且本机修订仍一致时才可更新。人工修改、清空、在用订阅、同名但不同的身份及迟到改动会保留，并提供逐项原因。无改动和主动选保留的项目可以正常完成。

本机 `GET /native-deliveries/imports/{id}` 返回持久结果。在另一运输任务里收到同一包，或提交后丢失响应时，按包和来源对象的稳定身份回读原结果。未提交预览保留七天，每实例最多暂存 64 个、共 64 MiB。已提交的业务结果与对象回执持续保留。设备端协议 1 的旧记录可查，历史 `v2Backup` 的新签发返回 HTTP 410。显式提交编号 `masterTxt` 的旧客户端仍须声明摘要和目标片段修订。

`POST /device/apply-tasks/{id}/result` 保存设备报告的逐段状态。已应用或部分应用的段必须附本机导入回执 ID 和匹配的摘要，终态回执可以重复读取。管理员可取消仍在等待或已领取的任务。终态与超期任务停止下载，历史保留目标、段摘要和结果。在新实例恢复时，旧设备凭据失效，尚未结束的包关闭。

## 身份与错误

「维护」读取 `GET /system/status` 的容量、系统 CPU、系统内存、进程 RSS、运行时长和服务 HTTP 正文累计流量。未取得的宿主指标留空。每两个采样点来自同一实例和启动周期时才能相减计算速率；两秒内复用同一读数。数据目录及各媒体库分别统计，共用同一磁盘的目录会显示相同的卷容量。概览最多列 128 项，`/system/storage` 支持继续分页。全部接口要求本人会话。

`GET /maintenance/jobs` 将现有扫描、接收批次、备份、保护和资料任务按创建时间排在一起，保留原类型和状态。每个任务通过原有操作入口查看详情或取消。`GET /system/client-errors` 仅提供最新 200 条错误；上报只接受固定分类、页面名称及可选 HTTP 状态码，服务端生成显示文字。客户端的 URL、文件名、堆栈与请求正文不会进入异常列表。

有外部采集器时，可凭管理员 Bearer 令牌读取根路径 `/metrics`。其中的 HTTP 请求数、已收正文和已发正文计数覆盖 API 与同源静态 Web，支持部分读取、Range、HEAD 和 304。主机指标使用 [gopsutil v4.26.10](https://pkg.go.dev/github.com/shirou/gopsutil/v4@v4.26.10)，首次 CPU 样本只有累计时钟，第二次有效采样才能得到百分比。

本人在 `POST /credentials` 和 `POST /ai/delegations` 签发的秘密只显示一次。这两个端点拒绝幂等键，从而避免通用操作回执保存秘密。开发版中曾经带键签发且确有明文回执的凭据在升级时撤销，日常无键签发的凭据继续按原期限使用。

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

年份筛选保留旧版的双向选择。用户先点较晚年份时，服务把两个有效年份按先后顺序应用到同一日历区间。越界或缺失的年份返回参数错误。

显式接收批次先通过`PUT /batches/{id}/inputs`一次登记完整原始选择：`expectedManifestRevision`初始为1，1–100个输入各带稳定`id`和原始文件名，已知时附长度/摘要及相对来源线索。封存后不增删，上传必须带本批`inputId`，且原名/来源/已知长度与摘要匹配；同输入绑定同一上传，重试沿原操作身份。隐式普通单文件由Go在同一事务自动登记并封存一项，不增加用户步骤。

文件提供器连名称也读取失败时，清单保留`id`并设置`nameState=unavailable`、省略原名，界面按选择序号显示读取失败，不能保存伪造文件名。默认`known`必须提供真实原名。后来来源恢复可沿原inputId上传，已知原名由Upload保留；原清单的未知状态不会改写成当时已知。

已知来源长度可以是零。Go 在封存时给每个空文件记下 `failed / empty_input`，给已知后缀不受支持的文件记下 `failed / unsupported_media`。结果保存在原输入 ID 下，可从批次详情或任务上下文回读。这些项没有上传或资产 ID，来源保持原状。其余待接收项继续上传，整批状态显示 `partial`。不知道名称或长度时仍可等待实际媒体，后续上传照常核实命名、长度、魔数和内容。换宿主后沿用原批次、输入与幂等键，已完成的上传返回原回执。

`BatchDetail.inputs`及分页任务上下文按原选择顺序报告，包括从未收到的`pending`项。完成条件为封存数量、输入映射、实际上传及入库逐项一致；只完成已经上传的子集不能标整批完成。旧开发批次不能反推真实意图，追加迁移标记`legacy_unverified`并保留旧明细，不伪造清单证据。原始选择与业务修订分别使用`manifestRevision`与`revision`。

取消尚未收到的项用`POST /batches/{id}/inputs/{inputId}/cancel`，提交原`manifestFingerprint`与稳定幂等键。清单行不删除，只记录`cancelled`；如已绑定未发布上传，同一用例取消该上传。已入库或有待恢复发布意图时拒绝倒退。取消项不算成功入库，整批保留partial和原始数量，来源继续保留。

文件提供器未知MIME用明确的`application/octet-stream`声明，不由客户端猜业务格式。Go按已支持扩展名确定标准类型、归一已知别名；明确声明不匹配仍拒绝。完整接收后继续校验魔数、内容长度/摘要和媒体探测，未知声明不能绕过实际文件校验。

`GET/PUT /settings`保留原设置页的扫描与上传语义。默认`scan.workers=0`自动、`scan.thumbEdge=800`预留、`upload.autoAccept=true`；PUT需完整三项及`expectedRevision`，遗漏不以零值替代。自动并发按可用CPU及内存保守取1–8库，手动1–32；队列最多64个活动任务，同库不能重复排队。调整只影响后续准入，不打断运行任务；等待和运行均可取消。上传开关关闭后拒绝新会话、分片与新发布，已接收内容和已完成回执保留，取消/查询仍可用。

管理员通过 `GET /libraries/{id}/monitoring` 查看库的原生监听、周期对账和最近故障。新扫描回执的 `trigger` 表示手动、文件通知或轮询，`scope` 表示整库或受影响的路径。手动要求整库对账时，会复用已经排队或运行的自动整库任务。服务保留每库最近的自动扫描记录以及手动任务。停用库会取消其自动任务。外部删除发现原件缺失后，记录缺失路径并保留已有元数据及历史。

缩略图`size=sm|md|lg`对应长边256/512/1024，缺省md。预留`thumbEdge`保存不改变此管线。缓存包含内容版本、策略与尺寸，ETag取实际缩略图字节摘要；旧版本或原件已变拒绝返回另一内容，原件出口始终逐字节直出。

JPEG 预览按 EXIF 方向显示，尺寸探测和缩略图使用同一方向规则。视频优先用内嵌封面，其次从时长 35% 处取代表帧。如果全部 100 个采样像素都接近黑色或白色，再依次尝试 25%、45%、15%、55%、5%、65% 和起点。时长未知时只读起点。服务在同一超时和进程预算内完成抽取，保留可读取的第一张极端帧供全黑或全白作品使用。

中尺寸缩略图在运行 30 秒后开始按页准备。原件写入或扫描后按受影响的作品或库补齐，每十分钟核对一次全库。前后台对同一表示共用一次解码，取消一个请求不会撤销其他请求等待的结果。大库按 256 个 ID 分页，队列合并重复变化；多核保留一个前台位置。清理只识别本服务产生的过期缩略图，当前版本会保留。写缓存也要保留原件所需的磁盘余量。

浏览回顾返回服务端日历窗口`fromDay/toDay/previousFromDay/previousToDay`和本期/前期`viewCount/previousViewCount`，不让客户端猜前期。`all`起点和前期日期为空串；固定窗口前期等长且紧邻，本期或前期无浏览都不可比较。`totalSeconds/imageSeconds/animatedSeconds/videoSeconds`来自日聚合，三类型互斥，包含只有停留而没有新增浏览的日期；视频项仍是详情停留。

`/stats/top-interests`专门给原Android混合Top3卡提供作者/标签结果，Go按views降序、同分作者在前、再按稳定身份升序选出最多三项。多作者/标签各自完整计数，客户端只展示与跳转，不自行拼接截断榜单。

创建、文件动作、上传完成和 AI 提交使用稳定 `Idempotency-Key`（8–128 字符）。同凭据、同操作、同键且输入不变回放原响应；输入改变返回冲突。业务数据与回执同事务保存。`GET /operations/{key}` 只读取当前凭据的回执；键用于多个操作时加 `operation`。`not_recorded` 不证明文件副作用从未开始，应使用原输入与原键重试或读取上传/资产状态。

普通元数据使用 `expectedRevision`。标签集合写入使用 `Asset.tagsRevision`，空数组是明确清空。作者编辑集合为 `authorIds`，可以附带 `expectedAuthorsRevision`。Go 保留 COS 固定目录作者，并在关联真实作者时解除未知占位。

每个标签有稳定 `Tag.id`。改词条名后，同一资产的关联 ID 和显示顺序保留。组图与短剧通过 `GET /collections/{id}/members` 分页读取成员。改集合名称使用 `PATCH /collections/{id}` 并提交集合修订。调整一页或一集使用 `PUT /collections/{id}/members/{assetId}/position`，提交成员修订和正整数位置。实际位置已有成员或已被在途发布占用时返回 409 和 `collection_position_occupied`。早期 Next 库里冲突的旧编号作为 `reportedPosition` 展示，便于核对。

管理员用 `PUT /tag-vocabulary/{id}/aliases` 或 `PUT /collections/{id}/aliases` 修改指定标签或集合的全部别名。请求须有 `Idempotency-Key`，正文包含对象的 `expectedRevision` 和完整 `aliases` 数组。传 `[]` 明确清空。服务按输入顺序保留清理后的别名，同一组值按同样顺序再次保存沿用当前修订。过期修订返回 409 和当前修订号。同一键、同一输入可回读原回执。编辑别名只写所选对象。

`GET /tag-vocabulary?q=...` 返回标签的稳定 ID、正式名、别名和修订，`GET /tags?q=...` 延用正式名字符串分页。`GET /collections?libraryId=...&q=...` 在指定库中搜索，省略 `libraryId` 可以跨库查询。三个查询都把 `q` 当作字面子串，分别匹配正式名和每个别名。一个对象即使命中多次也只出现一次。同一拼法可对应多个对象，客户端应保留各候选的稳定 ID，按用户选定的对象保存。

媒体浏览的 `GET /assets?q=...` 也会匹配资产所用标签的别名、所属集合名称和别名，沿用资产搜索的数字边界与分页。多个标签或集合同时命中时，每项资产只计一次。移除某个别名后，其他对象使用的同一拼法仍可命中。

使用原幂等键重试 `POST /tags` 或 `PATCH /tags/{tag}` 会取得当时保存的 `TagWriteReceipt`。历史回执可能只有正式名、修订和两个时间戳。后续需要按 ID 编辑时，从当前标签词表重新选择对象；当前词表和编辑别名的响应都带稳定 ID 和完整别名数组。

普通播放与拖动分开上报到 `POST /assets/{id}/events`。`play` 增加该会话的播放事实，`progress` 随 `contentVersion` 和 `positionMs` 写入续播行。位置为 0 表示回到开头。旧版本的标记和位置始终带原版本号，换版后播放器只对当前版本自动续播。[事件口径](analytics.md#事件与排行)

**收藏和当天的赞分别写入。** `PUT /assets/{id}/favorite` 只接受 `{"favorite":true}` 或 `{"favorite":false}`。`PUT /assets/{id}/like/today` 只接受 `{"liked":true}` 或 `{"liked":false}`。两个入口均使用 `Idempotency-Key`，返回当前资产。对同一次操作重发时使用原键。回读旧回执后，查 `GET /assets/{id}` 可看到其后其他设备的修改。

日界线取服务器配置的时区。当日已赞时再次点击会撤销当天这一票，收藏、此前的票和迁入的未分日累计保留。`liked` 表示当天有一条已知投票记录，`likeCount` 包含真实历史基线。无日期的基线只参与累计推荐和全时排行。旧版 Next 的组合接口 `/reaction` 已弃用；旧格式的点赞请求附带与当前值冲突的收藏时返回 409，升级客户端后可单独提交本次操作。

标签整组另有 `tagSetUpdatedAt` 与 `tagSetSource`。只有真正标签编辑、词表改名/删除造成的关联变化及明确迁入整组会修改它们，喜欢、改名或移动不会充当标签时钟。未知/相同时间的不同整组保持冲突；迁移不取并集来掩盖先后不明。词表墓碑也适用于迁入关系，不因旧候选重新关联而复活词条。

管理接口提供库启停、目录创建和分页目录读取、标签词表维护、单条标签移除、作者删除和明确合并。全局词表改名/删除与资产关联在一个事务内收敛。`sourceIds` 与 `characterIds` 是原作与角色的稳定关联；`source`/`characters` 为兼容展示。`acquisitionSources` 是单独的用户获取渠道。

`PATCH /assets/{id}` 只需发送改动的维度。ID 数组按传入顺序保存，空数组表示解除这一维的明确选择。保留角色时，作品仍会显示该角色当前所属的出处。在没有关联词条的维度，兼容文字按原文保存。已有词条时只提交不同的展示文字会得到 `409 taxonomy_ids_required`，本次其他修改也会回滚。使用稳定 ID 更换词条，或先明确清空这组 ID 并在同一请求提供文字线索。详情、列表、回执及单件作品的分面引用按同一套关系回读。

作者也有单独的 `acquisitionSources` 全量集合，以作者修订保护清空/替换。`/acquisition-vocabulary` 保存用户维护的组合渠道词表，`sources` 保留组合项与顺序，`suggestions` 由 Go 按空白拆词并保序去重。公开主页和作品出处不会被当作获取渠道；当前资料任务 scope 未授权此字段，AI 不能通过夹带新字段修改它。

`GET /storages` 返回可信逻辑根 ID。创建库时选择外部 `storageId`，将 `rootRelativePath` 留空可使用配置的整座目录，填写相对路径可使用其中的子目录。已有库和新库的目录不能重叠。内置 `data` 存储始终分配隔离目录。扫描结果通过扫描作业回读。媒体原件与缩略图请求可传 `version`，与当前内容版本不符返回 409。

视频时间轴通过 `/assets/{id}/timeline` 管理；修改携带标记修订与当前内容版本。旧版标记继续可读，客户端应明确标示版本差异，不自动跳转到推测的新位置。

旧 v2 事件和时间轴未携带来源内容版本，迁入后读接口使用 `contentVersion=0` 表示未知；不能将它绑定到当前文件版本或自动跳转。写接口仍要求当前版本至少为 1。历史响应的原事件 ID 按读取约束保存，不套用新上报 ID 的长度限制；累计统计不因此重放或删改。

## 上传与智能批次

标准 CLI 与 MCP 的 `task.context` 将 Go 给出的下一步转换为当前执行器和当前任务凭据均获准使用的工具名。`nextActions` 中的名称可原样传给 `platform.tool.describe`，例如收到智能上传后可按提示读取 `plan.prepare` 或 `research.submit` 的参数。`actionWaits` 单独列出等待事项。重复输入由用户决定，仍在传输的字节由原发送端续传，观察方可以用 `upload.status` 读到当前偏移。缺少许可时会列出对应的本地 scope、服务 scope 或任务绑定。混合批次可以同时有等待项和可执行的下一步。对外 HTTP 的任务状态、修订及写入校验继续以 Go 为准。

本人库管理界面的原有“根目录路径”输入框通过 `POST /storages/resolve-library-root` 解析，body 为 `{rootPath}`，返回 `{storageId,rootRelativePath,rootPath}`。它只读取和验证，不创建目录；必须落在已配置的媒体根内，拒绝控制目录、链接及越界。创建库仍使用现有逻辑位置字段。`GET /libraries/{id}/location` 给本人回显登记根；两个接口均要求 admin，不加入MCP工具映射，一般库列表不暴露本机绝对路径。

先 `POST /uploads` 固定目标、原名、MIME、声明长度、模式、可用来源相对目录/时间与批次。`PUT /uploads/{id}/content` 传原始 bytes，头 `Upload-Offset`，每片最多 8 MiB。`GET /uploads/{id}` 是恢复时的权威偏移。重复分片须字节一致；已封存上传拒绝后续写入。声明总长度参与空间与并发资源预算，没有退回单文件业务大小上限。

客户端具备完整源快照时传 `expectedHash`（SHA-256），与创建操作身份绑定。服务端在 complete 封存和发布前复算，不匹配时返回 `source_hash_mismatch`，不新增资产。未提供该字段时，接收摘要只能证明服务收到的字节，不能证明断点恢复前后客户端源文件未变化。

`originalFileName` 保存接收时真实原名，`sourceRelativePath` 保存可用的相对来源线索，二者与目标 `fileName` 分离。用户选名、Go 规范命名或替换都不抹掉输入映射。原名可保留坏编码线索，最终文件名仍必须通过命名校验；绝对来源路径、SAF URI 和穿越路径不进入这些字段。

普通 complete 返回已入库资产；智能 complete 返回 staged。精确重复返回输入的 `duplicate` 状态和明确原因，保持原件，不能当普通成功或替换。明确替换只允许设备管理员，须指定 `replaceAssetId` 与 `expectedContentVersion`，不因查重再创建新资产。

智能流程由批次拥有权威状态：`getTaskContext → submitResearch（需要时）→ preparePlan → getPlan → commitPlan → getBatch`。上下文分页且说明未读内容，完整计划有稳定服务端引用。模型不自行计算作品编号、猜页序或改写用户备注。剩余研究预算与逐项状态可回读；未知作者不阻塞正常新增，冲突只处理受影响项。

手动上传与 AI 共同使用 `/libraries/{id}/naming/context`、`suggestions` 和 `preview`。预览不占号，返回作用域摘要和规范名称；最终发布再次核验。空组图使用同一首作品分配器，补页从最大页之后追加。当前普通上传的同集合条目须按选定顺序 complete，前项失败待处理时后项不越过；可并发传输字节，客户端不计算页码。

例如，同一系列已经有图片 `作品 01.png`、`作品 03.jpg` 和视频 `作品 02.mp4`。新增一张图片时，Go 会建议 `作品 02.png`。如果提交的是同一部新作品的两页，预览会给出 `作品 02 (1).png` 和 `作品 02 (2).png`。AI 只需告诉服务哪些图片属于同一组。

## 媒体与备份

已索引作品使用 `POST /ai/delegated-tasks` 的 `kind=asset`，请求指定作品 ID、当前修订与内容版本。任务可申请长期授权列出的 `assetFields`，管理员单独选择是否提供研究预览。每个任务只覆盖一个作品。可在 `/asset-tasks/{id}` 回读任务，在 `/materials` 读取当前内容版本的帧，在 `/evidence` 分页查看已采用的来源。研究缓存有同一套大小、时限和版本校验，命名使用普通客户端的 Go 规则。

调用 `PATCH /asset-tasks/{id}/metadata` 时，显式字段分别附出处和观察。来源和角色通过现有词表 ID 解析。Go 检查作品版本、库是否仍可用、长期授权、任务写入预算与人工保护记录。作品改名另走 `POST /asset-tasks/{id}/rename`，在同一发布意图中保存任务进度、幂等回执与实际目录项。

浏览器两代资料目录以 `GET /backups/change-state` 返回的实例身份与 `businessRevision` 判断是否有新业务内容。版本为十进制字符串，创建快照、导出和下载可反复执行。备份记录中的同名版本读取自该份快照，保存成功时应记录此值，避免覆盖快照之后发生的更改。`GET /backups/{id}/author-documents` 从该快照一次导出 UTF-8 的《作者资料.md》，包括作者、简介原文/译文、来源、核实状态与关联作品，供维护目录直接保存。

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

旧 v2 的暂存、建计划和提交操作已退役，管理员按旧方法调用将收到 HTTP 410 和 `v2_import_retired`。早期 Next 中已经保存的输入、原始文档、计划及逐项结果仍可通过对应 GET 读取。正常的跨实例资料交付使用[原生设备包](#显式设备维护包)。历史缺少源摘要、内容版本或事件日期时如实保留未知值。将来的一次性工具使用独立迁入流程。[数据升级与隔离恢复](migration.md)
