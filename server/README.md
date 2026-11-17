# Go 服务端

每个实例由同一套 Go 服务拥有业务写入权，可独立运行于 PC/NAS，也可由Android独立运行模块托管。业务源码重新实现；Android启动与权限适配不进入Go业务模块，独立服务不依赖安卓构建或代码。

## 运行配置

发布构建由云端提供。运行时必填 `QIMENG_DATA_DIR`（隔离业务数据库与控制数据根）与至少 32 字符的 `QIMENG_ADMIN_TOKEN`。默认仅监听 `127.0.0.1:8421`，可用 `QIMENG_LISTEN` 调整。不得连接旧项目真实库或 8420。

可选配置：

- `QIMENG_WEB_DIR`：同源 Web 发布目录，支持客户端路由。
- `QIMENG_ADMIN_PASSWORD`：可选日常登录密码，至少12个字符、至多1024字节。启动时以随机盐和PBKDF2-HMAC-SHA256派生校验值，不写入数据库/回执/日志，子进程环境移除此项。原密码输入界面交换独立设备会话；密码不是API Bearer凭据。变更配置并重启即可改变后续登录密码，已签发设备会话继续按自身撤销/过期规则管理。
- `QIMENG_STORAGE_ROOTS`：可信运维配置的 JSON 对象，键为逻辑存储 ID，值为该存储的绝对根。路径只进入运行配置，不进入源码、API 或日志。未配置时使用内置 `data` 存储；注册库只接受逻辑 ID 与根内相对目录。
- `QIMENG_TIMEZONE`：IANA 时区，在数据库启动前加载；缺省使用宿主时区，非法值阻止启动。
- `QIMENG_SECURE_COOKIES=true`：HTTPS 会话 cookie；反向代理须保持同源协议与 Host。
- `QIMENG_AI_TOKEN`：独立只读凭据，不能通过工具可见性取得写权限。
- `QIMENG_AI_CONFIGURED=true`：可信执行器已接线的运行配置声明；不代表服务内含模型。
- `QIMENG_FFMPEG` / `QIMENG_FFPROBE`：可选可信媒体工具可执行文件。未配置时从 PATH 查找；缺少工具时明确报告格式预览/探测能力缺失。
- `QIMENG_STARTUP_RECEIPT` 与 `QIMENG_LAUNCH_ID`：可选宿主启动回执，必须同时设置；私有绝对文件路径与本次随机启动身份。成功绑定监听器后原子写入实例ID和实际地址，不写凭据。
- `QIMENG_EPHEMERAL_TLS=true`：仅供受监督回环实例；为本次进程生成TLS证书，在私有回执中提供公开DER。宿主必须只信该证书并保留正常主机名校验。默认关闭，PC/NAS原运行方式不变。

设备用 `POST /sessions`的`password`或可信`token`二选一换取独立可撤销会话；空输入不获得远程权限。本机自动登录由独立宿主提供随机bootstrap并绑定本次证书，不是远程免密码入口。本人可用 `POST /ai/delegations` 一次配置长期AI操作范围，此后执行器自动创建或接续受限任务。长期凭据本身只有read/delegate，实际写入使用与批次/资料任务绑定的派生凭据；不能签发管理员scope。撤销父授权同时撤销派生凭据，接续任务轮换generation并拒绝旧执行器的在途排队写入。单批次签发方式仍可用。根凭据只应进入受信配置。

密码验证使用[Go标准库PBKDF2](https://pkg.go.dev/crypto/pbkdf2)与[OWASP建议](https://cheatsheetseries.owasp.org/cheatsheets/Password_Storage_Cheat_Sheet.html)的600,000次HMAC-SHA256，32字节随机盐。单实例每分钟最多20次登录尝试、最多2个同时进行的密码派生，防止弱密码暴力试探和不受限CPU消耗；部署仍须按同源HTTPS/可信隧道访问。

## 数据布局与恢复

SQLite 与媒体位于本机数据根，不支持把数据库置于网络共享并由多个服务打开。独立 SQLite 排他锁阻止第二进程拥有同一根；主业务库采用 WAL、外键和 FULL 同步。

内置 `data` 存储在 `libraries/<libraryId>/` 保存媒体；外部逻辑存储可直接注册现有目录，不要求先把原件复制进数据库目录。每个库的 `.qimeng-internal/` 保存上传暂存、回收站、替换提交窗口和缩略图原件快照，和该库原件位于同一根，发布不依赖跨盘硬链接。客户端不能向内部目录上传、移动或创建目录。新登记的库不能与既有库或数据库控制根重叠。

`cache/` 中最终 JPEG 可重建；`backups/<id>/` 保存一致性业务快照和原件版本清单，不代表独立备份已经完成。旧版控制根的 `uploads/`、`trash/`、`retired/` 路径仍用于已有意图恢复，不静默搬动历史文件。配置根缺失时拒绝启动，不回退到同名空目录或据此删除索引。注册目录只代表存储登记，现有文件的扫描/迁移有自己的结果。

文件先写持久意图，再无覆盖发布，最后在业务事务内提交资产、批次和幂等回执。分片字节 flush 后才推进持久偏移。启动会重放未完成意图；内容不匹配时保留文件并停止启动供核对，不猜测删除原件。Windows 不声称目录元数据具有与 Linux fsync 相同的掉电保证。

原始文件名、相对来源目录与备注保存在业务数据中，不写入日志。上传文件原样保存，不转码。缩略图只写可重建缓存。替换保留资产身份、作者集合、标签、收藏、历史和旧版本播放位置；位置注明对应内容版本，不伪造跨版本时间映射。

## 模块与验证

`internal/domain` 是纯规则；`internal/model` 是业务记录；`internal/store` 负责追加迁移和持久化；`internal/service` 实现明确用例与文件恢复；`internal/httpapi` 只适配协议；`cmd/qimeng` 管理运行配置和生命周期。SDK 由根目录云端流水线从 OpenAPI 生成，服务端不手写客户端 SDK。

仅在云端执行：

```sh
cd server
go mod tidy
gofmt -w cmd internal
go vet ./...
go test -race -count=1 ./...
go build -trimpath -o qimeng ./cmd/qimeng
go build -trimpath -o qimeng-backup ./cmd/qimeng-backup
```

测试使用云端临时根与合成 PNG，不依赖模型、旧仓服务、真实媒体或设备。各能力的实际编译、测试、发布与设备验收证据统一记录于 `docs/status.md`。本文件描述实现与契约，不替代云端通过记录。

当前构建工具链锁定 Go 1.27.4，服务模块锁定 `modernc.org/sqlite v1.36.3` 与生成协议客户端需要的 `github.com/oapi-codegen/runtime v1.9.0`。工具链与模块版本以 `go.mod` 为准，云端 tidy 产生 `go.sum` 及传递依赖锁；不由本地隐式升级。确认来源：[Go 官方发行记录](https://go.dev/dl/)、[SQLite driver 发布文档](https://pkg.go.dev/modernc.org/sqlite@v1.36.3)。

可选媒体工具契约依据 [FFmpeg protocol whitelist](https://ffmpeg.org/ffmpeg-protocols.html)、[ffprobe JSON 输出](https://ffmpeg.org/ffprobe.html)。进程不经 shell，禁止网络协议，超时与输出大小有界；运行时工具版本与真实视频格式覆盖仍须在发布环境验收。
