# Go 服务端

PC/NAS 单一 Go 服务拥有业务写入权。源码从空目录重新实现，领域行为参考已确认产品要求；无 Android 内嵌服务。

## 运行配置

发布构建由云端提供。运行时必填 `QIMENG_DATA_DIR`（隔离数据根）与至少 32 字符的 `QIMENG_ADMIN_TOKEN`。默认仅监听 `127.0.0.1:8421`，可用 `QIMENG_LISTEN` 调整。不得连接旧项目真实库或 8420。

可选配置：

- `QIMENG_WEB_DIR`：同源 Web 发布目录，支持客户端路由。
- `QIMENG_TIMEZONE`：IANA 时区，在数据库启动前加载；缺省使用宿主时区，非法值阻止启动。
- `QIMENG_SECURE_COOKIES=true`：HTTPS 会话 cookie；反向代理须保持同源协议与 Host。
- `QIMENG_AI_TOKEN`：独立只读凭据，不能通过工具可见性取得写权限。
- `QIMENG_AI_CONFIGURED=true`：可信执行器已接线的运行配置声明；不代表服务内含模型。
- `QIMENG_FFMPEG` / `QIMENG_FFPROBE`：可选可信媒体工具可执行文件。未配置时从 PATH 查找；缺少工具时明确报告格式预览/探测能力缺失。

首次配置凭据不是业务密码注册；设备用 `POST /sessions` 换取独立可撤销会话。AI 写凭据由管理员对特定接收批次签发，24 小时有效，不能取得管理员 scope。根凭据只应进入受信配置。

## 数据布局与恢复

SQLite 与媒体位于本机数据根，不支持把数据库置于网络共享并由多个服务打开。独立 SQLite 排他锁阻止第二进程拥有同一根；主业务库采用 WAL、外键和 FULL 同步。

`libraries/<libraryId>/` 保存媒体，`uploads/` 保存接收分片，`trash/` 保存回收站原件，`retired/` 仅供已授权替换的提交窗口，`cache/` 可重建。`backups/<id>/` 保存一致性业务快照和原件版本清单。它们不是独立备份已经完成的证明。

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
```

测试使用云端临时根与合成 PNG，不依赖模型、旧仓服务、真实媒体或设备。各能力的实际编译、测试、发布与设备验收证据统一记录于 `docs/status.md`。本文件描述实现与契约，不替代云端通过记录。

当前构建工具链锁定 Go 1.27.4，服务模块锁定 `modernc.org/sqlite v1.36.3` 与生成协议客户端需要的 `github.com/oapi-codegen/runtime v1.9.0`。工具链与模块版本以 `go.mod` 为准，云端 tidy 产生 `go.sum` 及传递依赖锁；不由本地隐式升级。确认来源：[Go 官方发行记录](https://go.dev/dl/)、[SQLite driver 发布文档](https://pkg.go.dev/modernc.org/sqlite@v1.36.3)。

可选媒体工具契约依据 [FFmpeg protocol whitelist](https://ffmpeg.org/ffmpeg-protocols.html)、[ffprobe JSON 输出](https://ffmpeg.org/ffprobe.html)。进程不经 shell，禁止网络协议，超时与输出大小有界；运行时工具版本与真实视频格式覆盖仍须在发布环境验收。
