# 在电脑或 NAS 打开绮梦影库

登录 GitHub，打开本仓 Actions 中的 [Cloud verification 运行列表](https://github.com/Surtr42u/qimeng-next/actions/workflows/prepare.yml)。选定一个提交，确认对应的 `Server package` 平台作业成功，再从这次运行底部的 **Artifacts** 下载服务包。服务包制品保留 14 天。链接过期时，可选另一轮实际通过的平台作业，并按那一轮的提交核对包名和证据。

Windows x64 电脑选 `qimeng-next-package-windows-amd64`，Intel/AMD 64 位 Linux 电脑或 NAS 选 `qimeng-next-package-linux-amd64`，64 位 ARM 设备选 `qimeng-next-package-linux-arm64`。每个制品内含产品 ZIP、同名 `.sha256` 文件和平台检查结果。Windows 的 Tauri 安装包用于连接已经运行的影库。

## Windows

1. 将下载的制品解压到一个单独的文件夹，在这个文件夹打开 PowerShell。
2. 运行 `Get-FileHash -Algorithm SHA256 .\qimeng-next-windows-amd64-*.zip`，将结果和旁边 `.sha256` 文件中的值对照。一致后解压产品 ZIP。如果其属性中显示“解除锁定”，先解除该 ZIP 的锁定再解压。
3. 双击解压目录的 `start.cmd`。首次按提示设置至少 12 个字符的影库密码，输入时不会回显。就绪后浏览器打开 `http://127.0.0.1:8421/`，输入刚设置的密码登录。
4. 打开资源管理器，在地址栏输入 `%LOCALAPPDATA%\QimengNextServer\media\我的影库`。复制该文件夹的完整路径，在影库的「数据管理 → 库管理」填入库名称与服务器根路径，点「注册并扫描」。也可先将一张合成图片放到这个专用目录体验。

保持启动窗口打开，按 Ctrl+C 停止。再次双击同一个入口会使用原数据。默认业务数据和媒体在当前账号的 `QimengNextServer` 专用目录，口令由 Windows 当前用户的 DPAPI 加密保存。忘记密码可在软件目录运行 `.\start.cmd -ResetPassword`。已签发会话在其撤销或到期时失效。

## Linux 电脑

解开下载的制品，在包含产品 ZIP 和 `.sha256` 的文件夹运行 `sha256sum -c qimeng-next-linux-*.zip.sha256`。核对成功后解压产品 ZIP，进入解压出来的软件目录运行以下命令。

```sh
chmod +x qimeng-next qimeng-backup start.sh nas-entry.sh
bash start.sh
```

首次启动在终端隐藏输入密码，随后从同机浏览器进入 `http://127.0.0.1:8421/`。默认数据位置为 `$XDG_DATA_HOME/qimeng-next-server`；未设置 XDG_DATA_HOME 时为 `$HOME/.local/share/qimeng-next-server`。可登记其 `media/我的影库` 目录。按 Ctrl+C 停止，同一启动器可再次打开。Linux 把登录口令保存在该专用目录的 `private/login`，只允许当前用户读取。

## NAS

NAS 需要与包相符的 Linux CPU、Bash 和 Docker Compose。按上节核对并解压 Linux 产品 ZIP。请选择 NAS 本机持久盘上的全新专用目录，SQLite 跟随该本机磁盘保存。首次启动前从软件目录执行以下命令，将示例数据路径换成 NAS 自己的路径。

```sh
chmod +x qimeng-next qimeng-backup start.sh nas-entry.sh
export QIMENG_PACKAGE_STATE=/srv/qimeng-next-storage
bash start.sh nas
```

Compose 会拉取固定摘要的 Alpine 镜像，直接运行包中的服务程序。应用只读挂载，数据库与媒体分别映射到专用目录下的 `data` 和 `media`，登录口令以只读 secret 提供。打开「库管理」时，容器中的服务器根路径填 `/media/qimeng/我的影库`。宿主可以从持久目录的 `media/我的影库` 放入文件。同一状态目录对应同一个 Compose 项目，停止时在同一终端运行 `bash start.sh nas-stop`。

默认宿主端口仅在回环地址开放。有 SSH 登录权限的电脑可用下面的隧道连接，将 `用户@NAS` 换成自己的 SSH 登录目标。保持隧道打开，在电脑浏览器访问 `http://127.0.0.1:18421/`。

```sh
ssh -N -L 18421:127.0.0.1:8421 用户@NAS
```

## 上传第一张图片

在「数据管理 → 库管理」确认上文对应的服务器目录已登记。进入「数据管理 → 上传文件」，选中这个库，保留默认的「普通上传」，点击「选择文件并立即上传」并选一张测试图片。上传队列显示完成后，到「相册」打开图片核对结果。未完成时先看本页队列的提示，已建立的批次可到「任务与报告」查看逐项结果。

若浏览器打不开，先在服务所在电脑打开 `http://127.0.0.1:8421/healthz`。配置过端口时替换为自己的端口。返回 `{"status":"ok"}` 表示服务就绪。使用上述 NAS 隧道时可在客户端打开 `http://127.0.0.1:18421/healthz`。若服务端可达而隧道地址不可达，核对隧道连接与两端端口。若服务端也不可达，查看启动器输出，核对端口占用、数据目录归属及密码文件权限。

## 启用视频缩略图

Windows 打开 PowerShell，运行以下命令。安装后关掉启动影库的旧终端，再开一个新终端，让服务读取更新后的 PATH。

```powershell
winget install --id Gyan.FFmpeg --exact --source winget
ffmpeg -version
ffprobe -version
```

Ubuntu 或 Debian 可以用下面的命令安装同一组程序。两个版本命令都能显示版本后，停止影库并用原来的启动器重新打开。[Windows 软件包清单](https://github.com/microsoft/winget-pkgs/blob/master/manifests/g/Gyan/FFmpeg/8.1.2/Gyan.FFmpeg.installer.yaml)、[Ubuntu FFmpeg 文件清单](https://packages.ubuntu.com/noble/arm64/ffmpeg/filelist)。

```sh
sudo apt-get update
sudo apt-get install ffmpeg
ffmpeg -version
ffprobe -version
```

NAS 在同一持久目录设置下运行 `bash start.sh nas-media`。它从包内配方安装固定版本的 FFmpeg/ffprobe 并启动同一 Compose 项目。原先用基础容器启动过的影库会继续使用原持久卷。以后使用 `nas-media` 启动，`nas-stop` 停止。可在新注册的合成库上传一段短视频，打开详情查看时长、分辨率与缩略图。`linux-amd64` 制品中的 `linux-amd64-compose-media-smoke.json` 记录这次检查的预览字节数和逐张核验的研究帧数量。[Alpine FFmpeg 软件包](https://pkgs.alpinelinux.org/package/v3.24/community/aarch64/ffmpeg)。

## 接入已有的单帧查证服务

已经部署 Lens 伴随服务的管理员可将它接入同机的 Next 原生服务包。伴随服务使用自己的运行环境和凭据。安装版的 `GET /healthz` 返回 `ready: true` 后，再启动 Next。伴随服务的 Node、浏览器及启动指引见[已安装版协议与启动示例](https://github.com/Surtr42u/qimeng-media/blob/745974081716a9d69a95002a41220e35e743d764/tools/research_sidecar/README.md)。

Windows 在 Next 包目录打开 PowerShell，输入伴随服务所用私有密钥文件的位置后启动。密钥只通过环境传入。

```powershell
$env:QIMENG_RESEARCH_SIDECAR_TOKEN = (Get-Content -Raw -LiteralPath (Read-Host '伴随服务密钥文件')).Trim()
$env:QIMENG_RESEARCH_LENS_ENDPOINT = 'http://127.0.0.1:18421'
.\start.cmd
```

Linux 的原生包可从服务账号专用的权限为 0600 的配置文件加载相同的两个变量，再运行 `bash start.sh`。容器中的 `127.0.0.1` 指本容器的网络命名空间。有独立容器部署需求时，须让已安装的伴随服务与 Go 服务共享网络命名空间，并向 Go 服务单独注入这两个变量。默认 NAS Compose 包未包含伴随服务。执行器在获得研究权限后，可读 `GET /research/context` 查看现有配置以及单个任务的外发授权。任务提交方法见[单帧反向查证](api.md#单帧反向查证)。

## 更新、备份和核验

升级前，在正在运行的 Next 影库中打开「数据管理 → 资料备份与归档」，生成并下载完整 `.qnext`，保留页面显示的 SHA-256。用版本配套的离线工具核验。做恢复演练时，以尚不存在的目录为目标。下面以 Linux 为例，Windows 使用同目录的 `.\qimeng-backup.exe`。

```sh
./qimeng-backup verify --archive 影库备份.qnext --sha256 实际SHA256
./qimeng-backup restore --archive 影库备份.qnext --sha256 实际SHA256 --destination 新的独立恢复目录
```

`verify` 成功返回 `state` 为 `verified`，`restore` 成功返回 `restored` 及新的实例 ID。恢复目录是独立服务的 `QIMENG_DATA_DIR`，含恢复的业务库和媒体，旧会话和 AI 授权已撤销。需试运行恢复结果时，按服务端配置给该实例设置新凭据与独立监听端口。[服务端配置](https://github.com/Surtr42u/qimeng-next/blob/main/server/README.md#运行配置)

升级原 Next 实例时，先停进程，把新软件解压到另一目录，用原来的专用状态目录启动，再登录核对媒体库和代表性原件。如果所选服务端口已被占用，新程序会在打开业务库前退出，原数据库仍是升级前的版本。可释放端口或用 `QIMENG_PACKAGE_PORT` 选择空闲端口，再用同一状态目录启动。原项目资料另走迁移流程。首次启动器会拒绝接管未标记的非空目录。[迁移与回退](https://github.com/Surtr42u/qimeng-next/blob/main/docs/migration.md)

- `SOURCE.json` 记录完整提交 SHA 与 OpenAPI/SDK 摘要，`SHA256SUMS` 列出各文件的校验值。对应平台的 `*-smoke.json` 与 `*-home.png` 记录云端实际打开该包、密码登录、空媒体库首页和离线恢复结果。原生启动的 `upgradeProof` 还记录端口冲突后历史库的保留情况、重试前后的数据库版本，以及密码登录后读回的原实例和作者。仅以成功作业给出的 artifact 确认对应平台。
- 基础浏览和原件下载随包提供。原生基础包与 `nas` 模式可使用浏览器支持的原件格式；系统工具安装或 `nas-media` 模式为服务提供视频探测、缩略图和研究帧。
- 自定义端口用 `QIMENG_PACKAGE_PORT`，持久目录用 `QIMENG_PACKAGE_STATE`。已有受限密码文件可通过 `QIMENG_PACKAGE_PASSWORD_FILE` 提供。默认账号凭据、端口与目录独立于旧应用。

容器参考 [Docker Compose secrets](https://docs.docker.com/compose/how-tos/use-secrets/) 与 [Alpine 官方镜像](https://hub.docker.com/_/alpine/)。
