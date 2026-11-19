# Go 服务端

每个实例由同一套 Go 服务拥有业务写入权，可独立运行于 PC/NAS，也可由Android独立运行模块托管。业务源码重新实现；Android启动与权限适配不进入Go业务模块，独立服务不依赖安卓构建或代码。

## 运行配置

发布构建由云端提供。运行时必填 `QIMENG_DATA_DIR`（隔离业务数据库与控制数据根）与至少 32 字符的 `QIMENG_ADMIN_TOKEN`。默认仅监听 `127.0.0.1:8421`，可用 `QIMENG_LISTEN` 调整。不得连接旧项目真实库或 8420。

可选配置：

- `QIMENG_WEB_DIR` 指定支持客户端路由的同源 Web 发布目录。PC/NAS 包为首页直接引用的指纹 JS、CSS 附带 gzip 文件，二者与原文件均列入包校验清单。启动时校验当前入口压缩文件的解码内容，请求时按 `Accept-Encoding` 选择。单一区间按所选表示的字节位置返回，gzip 多区间请求返回完整压缩文件。代理自行做 Brotli 时可向上游请求 `identity`。自备 Web 目录可直接使用普通构建文件。
- `QIMENG_ADMIN_PASSWORD`：可选日常登录密码，至少12个字符、至多1024字节。启动时以随机盐和PBKDF2-HMAC-SHA256派生校验值，不写入数据库/回执/日志，子进程环境移除此项。原密码输入界面交换独立设备会话；密码不是API Bearer凭据。变更配置并重启即可改变后续登录密码，已签发设备会话继续按自身撤销/过期规则管理。
- `QIMENG_STORAGE_ROOTS`：可信运维配置的 JSON 对象，键为逻辑存储 ID，值为该存储的绝对根。路径不进入源码、日志或AI工具。本人管理界面保留原来的绝对根目录输入框，经专用解析接口转换为已授权根内的位置；注册库仍只保存逻辑 ID 与根内相对目录。未配置时使用内置 `data` 存储。
- `QIMENG_TIMEZONE`：IANA 时区，在数据库启动前加载；缺省使用宿主时区，非法值阻止启动。
- `QIMENG_SCANNER_WATCH_ENABLED` 控制已登记媒体库的原生文件通知，默认 `true`。关闭后仍按 `QIMENG_SCANNER_POLL_INTERVAL` 对账，默认 `5m`。网络挂载漏事件、系统拒绝通知或超过监听目录上限时，轮询继续。设置在启动时读取。
- `QIMENG_LOCAL_SYNC_ROOT` 启用服务器目录投放。服务按 `QIMENG_LOCAL_SYNC_INTERVAL`（默认 30s）轮询；文件大小、修改时间与文件身份跨轮稳定且达到 `QIMENG_LOCAL_SYNC_STABLE_AGE`（默认 60s）后接收。根目录下的 TXT 导入作者清单，一级目录名匹配已启用常规库，其下的媒体按相对目录入库。上传开关暂停媒体接收，TXT 仍可处理。每次成功后保留来源，服务重启仍可识别已入库版本。管理员可在上传页查看失败原因和最近成功记录，手动触发下一轮。同步根须存在，且与媒体库、备份及服务数据根分开。
- `QIMENG_DISCOVERY=true`：在已启用的私有局域网接口发布 `_qimeng._tcp` DNS-SD 服务，缺省关闭；可用 `QIMENG_DISCOVERY_INTERFACE` 选择单网卡、`QIMENG_DISCOVERY_NAME` 指定不含私人信息的显示名。端口取实际监听地址，不固定猜测8421。组播不可用时服务继续运行并给出不含网络地址的警告。
- `QIMENG_PUBLIC_ORIGIN`：反向代理的明确 HTTPS origin，用于发现候选，例如 `https://library.example`；不得包含账号、路径、查询参数或令牌。客户端仍执行正常证书及实例身份核验。只绑定回环且没有公开origin的独立实例不广播，Android受监督TLS实例始终不广播。
- `QIMENG_SECURE_COOKIES=true`：HTTPS 会话 cookie；反向代理须保持同源协议与 Host。
- `QIMENG_AI_TOKEN`：独立只读凭据，不能通过工具可见性取得写权限。
- `QIMENG_AI_CONFIGURED=true`：可信执行器已接线的运行配置声明；不代表服务内含模型。
- `QIMENG_RESEARCH_ENABLED=true` 开启固定端点的公开文本查证，缺省关闭。AI 任务另须获得具体来源、对象和预算授权。部署状态、来源矩阵及剩余额度由 `GET /research/context` 提供。本地取证与预览可独立使用。外发字段、限流和回执规则见 [API 使用约定](../docs/api.md#公开资料查证)。
- `QIMENG_RESEARCH_LENS_ENDPOINT` 为可选的单帧反向查证服务地址，例如 `http://127.0.0.1:18421`。须使用同一网络命名空间里的数字回环地址、明确端口和与伴随服务一致的 `QIMENG_RESEARCH_SIDECAR_TOKEN`。服务固定请求 `/lens`，只传一张授权的研究 JPEG。省略地址即关闭。程序读入密钥后会从自身的进程环境移除。启动原生服务包的示例见[快速开始](../docs/quickstart.md#接入已有的单帧查证服务)。
- `QIMENG_FFMPEG` / `QIMENG_FFPROBE`：可选可信媒体工具可执行文件。未配置时从 PATH 查找；缺少工具时明确报告格式预览/探测能力缺失。
- `QIMENG_STARTUP_RECEIPT` 与 `QIMENG_LAUNCH_ID`：可选宿主启动回执，必须同时设置；私有绝对文件路径与本次随机启动身份。服务在获取数据目录独占锁和执行迁移前绑定 TCP 端口，保留同一监听器提供 HTTP。实例初始化完成后才原子发布回执，包含本次启动身份、持久实例 ID 和实际监听地址。端口冲突时保留原数据库与已有回执。
- `QIMENG_EPHEMERAL_TLS` 接受 `true` 或 `false`，缺省关闭。启用时须使用受监督回环监听器，服务为本次进程生成 TLS 证书，在私有回执中提供公开 DER。证书覆盖实际绑定的回环 IP、`localhost`、`127.0.0.1` 和 `::1`。宿主只信该证书并保留正常主机名校验。无效值会在打开数据库前阻止启动。

设备用 `POST /sessions`的`password`或可信`token`二选一换取独立可撤销会话；空输入不获得远程权限。本机自动登录由独立宿主提供随机bootstrap并绑定本次证书，不是远程免密码入口。本人可用 `POST /ai/delegations` 一次配置长期AI操作范围，此后执行器自动创建或接续受限任务。长期凭据本身只有read/delegate，实际写入使用与批次/资料任务绑定的派生凭据；不能签发管理员scope。撤销父授权同时撤销派生凭据，接续任务轮换generation并拒绝旧执行器的在途排队写入。单批次签发方式仍可用。根凭据只应进入受信配置。

密码验证使用[Go标准库PBKDF2](https://pkg.go.dev/crypto/pbkdf2)与[OWASP建议](https://cheatsheetseries.owasp.org/cheatsheets/Password_Storage_Cheat_Sheet.html)的600,000次HMAC-SHA256，32字节随机盐。单实例每分钟最多20次登录尝试、最多2个同时进行的密码派生，防止弱密码暴力试探和不受限CPU消耗；部署仍须按同源HTTPS/可信隧道访问。

已显式提交的智能批次由可信执行器以长期凭据读取 `GET /ai/delegations/current/ready-batches`。Go 在本地业务库按授权库及可用预算查询清单、上传和现有任务，至多返回最旧的 16 个可开始的批次。读取不会占用名额，也不会打开媒体或发起研究。执行器用返回的清单摘要原子认领，随后按受限任务的上下文工作。停机期间仍由影库保存上传和任务，重连沿用原认领或提交回执。[发现、认领与恢复约定](../docs/api.md#已提交智能批次的发现与认领)

## 数据布局与恢复

所有者校对的重复图片在本次新恢复的离线工作副本上合组。专用命令校验完整未合并归档、恢复收据、计划与原件，封存选中成员的独立预像后同卷发布到新目录。普通服务接管发布成功的数据根。[逐项核对、合组与中断恢复](../docs/selected-image-merge.md)

已启用的库注册监听后先扫描一次。外部新增、删除、改名和目录移入会触发整库对账。已有媒体的写通知按路径核对实际内容。每库在 300 毫秒静默期后处理一批，连续写入最多合并 30 秒。每批最多保存 128 个路径，超过即扫描整库。每实例至多监听 64 个库、8192 个目录，每库至多 2048 个目录。停用或注销库会撤销监听。管理者可通过 `GET /libraries/{id}/monitoring` 查看当前模式、登记目录数及失败说明。

整库遍历成功后，服务按索引中的目录逐页核对原件，涵盖已从磁盘移出的子树。每页读取至多 256 条索引记录，每个目录最多枚举 4096 项。稳定目录内已见的普通文件可以直接确认，其余逐路径读取文件状态。报告缺件前再次核实当前资产身份、有效路径和实际磁盘状态。缺件保留资产、标签、作者和观看记录，目录读取错误会反映在扫描结果中。

SQLite 与媒体位于本机数据根，不支持把数据库置于网络共享并由多个服务打开。独立 SQLite 排他锁阻止第二进程拥有同一根；主业务库采用 WAL、外键和 FULL 同步。

内置 `data` 存储在 `libraries/<libraryId>/` 保存媒体；外部逻辑存储可直接注册现有目录，不要求先把原件复制进数据库目录。每个库的 `.qimeng-internal/` 保存上传暂存、回收站、替换提交窗口和缩略图原件快照，和该库原件位于同一根，发布不依赖跨盘硬链接。按上传 ID 从目录数据库回读暂存位置，兼容旧控制根布局及已解绑库的留存输入。客户端不能向内部目录上传、移动或创建目录。新登记的库不能与既有库或数据库控制根重叠。

`cache/` 中最终 JPEG 可重建；`backups/<id>/` 保存一致性业务快照和原件版本清单，不代表独立备份已经完成。旧版控制根的 `uploads/`、`trash/`、`retired/` 路径仍用于已有意图恢复，不静默搬动历史文件。配置根缺失时拒绝启动，不回退到同名空目录或据此删除索引。注册目录只代表存储登记，现有文件的扫描/迁移有自己的结果。

批次独立保护用检查点内已经入库的输入和版本签发逐项证明。原键重试继续使用先前发布的快照，后来完成的输入需新建保护。原实例上的旧保护仅丢失本地检查点时，服务可从原独立目标核验并回读同一份快照。[中断后的批次保护与回收许可](../docs/migration.md#批次独立保护中断后怎样继续)

文件先写持久意图，再无覆盖发布，最后在业务事务内提交资产、批次和幂等回执。意图落盘时，同一凭据、同一操作的幂等键即绑定原请求。中断后可用原键和原输入继续，改用其他输入返回冲突。分片字节 flush 后才推进持久偏移。启动会重放未完成意图；内容不匹配时保留文件并停止启动供核对，不猜测删除原件。Windows 不声称目录元数据具有与 Linux fsync 相同的掉电保证。

普通新增上传在提交资产前遇到停库或暂停接收，会保留暂存原件、发布目标和待办意图。重启会继续处理其他库。重新启用库和上传后，用原上传、原请求与原键重试即可完成并取得回执。原设备凭据失效或旧意图缺少可核实的授权时，可由有效的管理会话显式完成同一上传。待办期间全库备份等待发布收尾。旧数据若已记录上传或批次取消，启动会保留暂存原件，把已核实的临时发布文件移入本库 `.qimeng-internal/publications/`，记录取消并释放备份占用。

智能计划逐项使用与普通上传共用的校验名额。暂存摘要、实际发布原件和提交后的清理可以与独立业务写入并行。服务在落库前再核对原件对象及版本、批次修订、目标占用和当前授权。请求在预检中取消时可从原暂存继续；已保留发布意图的项目沿原计划核验和恢复。重试使用同一计划和幂等键，已提交项沿用原回执。

Android 普通应用在私有数据目录也会遇到硬链接被拒绝。文件发布在库内建立独立暂存副本，记录其文件身份、校验大小和 SHA-256 后，使用同卷不覆盖重命名发布。业务提交前保留原件，清理时再次核对两端身份。临时空间不足时保留来源并返回错误，重试使用同一上传和幂等键。可恢复的发布错误附带阶段码，涉及时间属性、媒体探测、文件发布或数据库提交时可据此区分。

原始文件名、相对来源目录与备注保存在业务数据中，不写入日志。上传文件原样保存，不转码。缩略图只写可重建缓存。替换保留资产身份、作者集合、标签、收藏、历史和旧版本播放位置；位置注明对应内容版本，不伪造跨版本时间映射。

命名续号和自动补全按媒体库与所在目录查询活动文件索引，再盘点该目录中实际存在的文件和目录。尚未扫描的同名项也占用名称，最终发布时再次核对目标路径。

编辑标签或集合的别名仅写对应词条。搜索可以按别名找回关联媒体。改别名后，既有文件名、组内序号和已按集合 ID 准备的命名计划仍然有效。数据表、迁移和冲突处理见[原生数据架构](../docs/architecture.md#next-原生数据与恢复)。

## 模块与验证

云端使用 oapi-codegen v2.8.2 从 `api/openapi.yaml` 生成 Go 类型、客户端、标准库服务端接口和内嵌协议。`internal/httpapi.API` 在编译时实现全部接口，运行服务使用生成的路由。kin-openapi v0.153.0 校验 JSON 请求。文件上传与 Range 下载保留流式读写，由真实入口的契约测试覆盖。[生成器](https://github.com/oapi-codegen/oapi-codegen/tree/v2.8.2) · [kin-openapi 版本与 MIT 许可证](https://github.com/getkin/kin-openapi/tree/v0.153.0)

`internal/sourcerules` 从独立 JSON 读取经用户审定的 133 个出处组和 1,394 行角色检索词。原词条来自旧版本 d0215962 的 `server/internal/sourcematcher/source_groups_data.go` 纯数据表。仅转录规范名、变体和角色别名。新匹配代码按归一化最长前缀定位出处，优先采用出处内的角色别名，表未命中时再按文件名和停用词提取。遇到多个同等匹配候选时保留候选名称，并允许以后人工核对。

管理员通过 `/sources/custom-groups` 读取或按修订整体保存追加词层。存储修订、重算游标与进度一并落库，启动后续做未结束部分。普通上传、扫描和文件改名使用同一规则。存量仅更新空白或由规则生成的归属。人工填写与明确清空在单文件修订和持久保护记录中保留，原件和内容版本不参与词表重算。

`internal/domain` 是纯规则；`internal/model` 是业务记录；`internal/store` 负责追加迁移和持久化；`internal/service` 实现明确用例与文件恢复；`internal/httpapi` 只适配协议；`cmd/qimeng` 管理运行配置和生命周期。SDK 由根目录云端流水线从 OpenAPI 生成，服务端不手写客户端 SDK。

一次性的 `cmd/qimeng-legacy-convert` 通过 `internal/legacycold` 只读访问已封存的旧 PC v26 资料，在私有新目录制作原生备份。普通服务和 Android 均独立于这个命令。归档由 `cmd/qimeng-backup` 验证和恢复。[操作与私有覆盖报告](../docs/legacy-cold.md)

`cmd/qimeng-merge-selected` 在已核验的恢复工作副本中处理所有者明确选定的同内容图片，保存数据库和选中原件的合并前副本，再将结果发布到新目录。内容版本、旧地址、关系、逐条历史与来源的复核步骤见[图片合并交接说明](../docs/selected-image-merge.md)。

数据库启动时先将已记录的迁移逐个与本程序核对，检查通过才运行待执行的 SQL。每项迁移与其校验记录在同一事务提交。启动与备份验证共享迁移定义。SQLite 会保留建表 SQL 的文本，因此迁移记录表沿用原始定义，以便旧备份通过相同的完整性检查。[架构边界](../docs/architecture.md#module-boundaries) · [工程检查](../docs/verification.md#engineering-gates)

以下检查使用云端环境。日常验证直接读取提交的源码。

```sh
cd server
go mod tidy
git ls-files -z -- '*.go' | xargs -0 -r gofmt -l
go vet ./...
go test -race -count=1 ./...
go build -trimpath -o qimeng ./cmd/qimeng
go build -trimpath -o qimeng-backup ./cmd/qimeng-backup
```

测试使用云端临时根与合成 PNG，不依赖模型、旧仓服务、真实媒体或设备。各能力的实际编译、测试、发布与设备验收证据统一记录于 `docs/status.md`。本文件描述实现与契约，不替代云端通过记录。

PC/NAS 使用 Go 1.27.4、modernc.org/sqlite v1.62.0 与该版本指定的 libc v1.80.1。SQLite 3.54.1 包含上游修复的 WAL 并发检查点问题。Android 使用由 NDK 编译的 go-sqlite3，所有平台共用相同迁移和业务事务。依赖与校验和由云端锁定。[驱动与平台](https://pkg.go.dev/modernc.org/sqlite@v1.62.0) · [SQLite WAL 修复说明](https://sqlite.org/wal.html#the_wal_reset_bug)

可选媒体工具契约依据 [FFmpeg protocol whitelist](https://ffmpeg.org/ffmpeg-protocols.html)、[ffprobe JSON 输出](https://ffmpeg.org/ffprobe.html)。进程不经 shell，禁止网络协议，超时与输出大小有界；运行时工具版本与真实视频格式覆盖仍须在发布环境验收。

大幅顺序 JPEG 使用 2025-12-18 发布的 go-scaled-jpeg v1.2.0（MIT，含 Go BSD-3-Clause 与 IJG 部分）在反变换中降采样。非交错 PNG 使用 2026-03-25 发布的 go-scaled-png v1.0.0（MIT，含 Go BSD-3-Clause 部分）按行缩放。两者用 Go 构建，版本与校验和由模块锁定。渐进 JPEG 的预览只保留各块的 DC 系数，所需内存与块数成正比。具体输入和工作区预算见 [API 使用约定](../docs/api.md#写入与回放)。[JPEG 实现与许可证](https://github.com/m8rge/go-scaled-jpeg/tree/v1.2.0) · [PNG 实现与许可证](https://github.com/m8rge/go-scaled-png/tree/v1.0.0) · [JPEG 编码规范 T.81](https://www.itu.int/rec/T-REC-T.81/en)

局域网发现适配位于 `cmd/qimeng`，锁定 [hashicorp/mdns v1.0.7](https://github.com/hashicorp/mdns/tree/v1.0.7)（MIT），不进入业务服务/数据库。TXT契约为 `txtvers=1, api=0.1.0, id=<实例ID>, scheme=http|https, path=/api/v1`，反向代理可附 `origin`；服务名称或TXT均不是可信身份。Android按[官方DNS-SD机制](https://developer.android.com/develop/connectivity/wifi/use-nsd)解析候选，再回读 `/instance`。单测验证记录和禁用/回环边界，真实路由器/NAS组播另行验收。
