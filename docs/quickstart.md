# 在电脑或 NAS 打开绮梦影库

从同一提交的私有 Actions 运行下载适合设备的服务包。Windows 普通电脑选 `windows-amd64`，Intel/AMD NAS 选 `linux-amd64`，64 位 ARM NAS 选 `linux-arm64`。包名带提交短号。Windows 的 Tauri 安装包是连接现有影库的客户端，首次在电脑上建立影库请选服务包。

## Windows

1. 打开 Actions 运行底部的 `qimeng-next-package-windows-amd64`。解开下载的 artifact，可以看到产品 ZIP、相应的 `.sha256` 与云端冒烟证据。
2. 用 PowerShell 的 `Get-FileHash -Algorithm SHA256 .\产品包名.zip` 核对旁边 `.sha256` 中的值。若 Windows 在产品 ZIP 属性中显示“解除锁定”，核对完后解除该 ZIP 的锁定，再解压到普通软件目录。
3. 双击解压目录的 `start.cmd`。首次按提示设置至少 12 个字符的影库密码，输入时不会回显。就绪后浏览器打开 `http://127.0.0.1:8421/`。在熟悉的登录页输入刚才的密码。
4. 打开资源管理器，在地址栏输入 `%LOCALAPPDATA%\QimengNextServer\media\我的影库`。复制该文件夹的完整路径，在影库的「数据管理 → 库管理」填入库名称与服务器根路径，点「注册并扫描」。也可先将一张合成图片放到这个专用目录体验。

保持启动窗口打开，按 Ctrl+C 停止。再次双击同一个入口会使用原数据。默认业务数据和媒体在当前账号的 `QimengNextServer` 专用目录，口令由 Windows 当前用户的 DPAPI 加密保存。忘记密码可在软件目录运行 `.\start.cmd -ResetPassword`。已签发会话在其撤销或到期时失效。

## Linux 电脑

打开与 CPU 对应的 Linux 产品 ZIP，核对 SHA-256 后解压。在解压出来的软件目录运行以下命令。

```sh
chmod +x qimeng-next qimeng-backup start.sh nas-entry.sh
bash start.sh
```

首次启动在终端隐藏输入密码，随后从同机浏览器进入 `http://127.0.0.1:8421/`。默认数据位置为 `$XDG_DATA_HOME/qimeng-next-server`；未设置 XDG_DATA_HOME 时为 `$HOME/.local/share/qimeng-next-server`。可登记其 `media/我的影库` 目录。按 Ctrl+C 停止，同一启动器可再次打开。Linux 把登录口令保存在该专用目录的 `private/login`，只允许当前用户读取。

## NAS

NAS 需要与包相符的 Linux CPU、Bash 和 Docker Compose。请选择 NAS 本机持久盘上的全新专用目录，SQLite 跟随该本机磁盘保存。首次启动前从软件目录执行以下命令，将示例数据路径换成 NAS 自己的路径。

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

## 更新、备份和核验

- 升级先停止旧的 Next 进程，解压新软件到另一目录，用原来的专用状态目录启动。首次启动器会拒绝接管未标记的非空位置；原版库应通过迁移流程进入新实例。
- 在「资料备份与归档」生成并下载完整 `.qnext`，保留页面显示的 SHA-256。离线工具可先核验，再恢复到尚不存在的新目录。下面以 Linux 命令为例；Windows 使用同目录的 `.\qimeng-backup.exe`。

```sh
./qimeng-backup verify --archive 影库备份.qnext --sha256 实际SHA256
./qimeng-backup restore --archive 影库备份.qnext --sha256 实际SHA256 --destination 新的独立恢复目录
```

- `SOURCE.json` 记录完整提交 SHA 与 OpenAPI/SDK 摘要，`SHA256SUMS` 列出各文件的校验值。对应平台的 `*-smoke.json` 与 `*-home.png` 记录云端原生平台实际打开该包、密码登录、空库首页和离线恢复结果。仅以成功作业给出的 artifact 确认对应平台。
- 基础浏览和原件下载随包提供。媒体探测、视频缩略图和研究预览使用服务进程能找到的 `ffmpeg` 与 `ffprobe`；本包及随附 Alpine 容器没有内置这两个程序。普通浏览器可以播放的原件格式由设备解码能力决定。
- 自定义端口用 `QIMENG_PACKAGE_PORT`，持久目录用 `QIMENG_PACKAGE_STATE`。已有受限密码文件可通过 `QIMENG_PACKAGE_PASSWORD_FILE` 提供。默认账号凭据、端口与目录独立于旧应用。

容器参考 [Docker Compose secrets](https://docs.docker.com/compose/how-tos/use-secrets/) 与 [Alpine 官方镜像](https://hub.docker.com/_/alpine/)。
