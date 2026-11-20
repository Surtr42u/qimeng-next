# 桌面连接壳

全新 Tauri 2 壳，只管理连接地址、窗口、托盘和导航。业务界面由 PC/NAS 同源 Web 提供；没有第二业务数据库、内嵌 Go、原生媒体规则或自动更新器。应用标识独立为 `media.qimeng.next.desktop`，不会覆盖旧客户端配置。

先准备一个已运行的影库。需要在电脑或 NAS 建库时，按[快速开始](../docs/quickstart.md)下载服务包。桌面壳的 Windows NSIS 安装包在同次 `Cloud verification` 的 `windows-first-check` 制品中。核对对应 `windows` 作业和[窗口验收记录](../docs/status.md)，再选择要试用的提交。

首次打开配置窗，输入 HTTP/HTTPS 根地址并保存。壳向该地址匿名请求 `/api/v1/instance`，确认实例身份、协议版本和能力后才写入系统应用配置目录并切换窗口。服务离线、返回其他页面、重定向或 8 秒内未完成响应时，配置窗显示原因，可修改地址或直接重试。旧连接及其原件窗在此期间可继续使用。访问令牌在 Web 登录页交换。首次配置壳与升级 Web 同时进行时，验证并沿用已部署的本机影库地址，不因安装壳改端点或新建实例。[日常覆盖升级](../docs/quickstart.md#现有-next-实例的日常覆盖升级)

正常启动时直接打开已保存的地址，当前配置文件缺失时可从上一份配置恢复。托盘提供打开、切换地址与退出，`--setup` 也能进入配置。关闭主窗口退出应用，关闭媒体子窗只影响该窗口。

主窗口恢复旧版 1200×800 无边框布局，Web 顶栏原位置的三键、空白区域拖动和双击调用当前窗口的原生操作。Tauri 按当前 WebView 标签及 HTTP(S) 完整 origin 授权主窗使用 `window_action` 和 `open_original`。Rust 再次检查活动窗口和目标来源。连接地址命令只授予内置连接页。

主窗及原件子窗逐次核对导航协议、主机与端口。外部 Web 链接由系统浏览器打开，关闭子窗只影响该子窗，切换服务器会关闭旧主窗及其原件子窗。设置窗口沿用旧 `desktop/ui/setup.html` 的 420×340 布局和颜色。连接窗与安装包使用[跨端统一图标](ui/app-icon.png)，地址、保存与权限判断由 Next 实现。

主窗中真实点击同源的新窗口链接时，控制层将地址传给专用命令。壳核对目标后，在现有 WebView2 资料目录下打开独立原件窗。打开期间若用户已切换服务器，旧子窗会关闭。云测从真实鼠标点击、原件请求和认证 Cookie 分别确认这三个环节。[WebView2 线程与回调](https://learn.microsoft.com/en-us/microsoft-edge/webview2/concepts/threading-model)

## 云端准备与验证

CLI、Tauri 和插件版本见[前端依赖](package.json)和[Rust 依赖](src-tauri/Cargo.toml)，传递版本由仓库中的 `package-lock.json`、`src-tauri/Cargo.lock` 固定。

原生壳使用 reqwest 0.13.5 请求公开的实例信息，响应上限为 64 KiB。HTTPS 使用系统证书信任，Windows 使用 Schannel。此依赖采用 MIT 或 Apache-2.0 许可证。[reqwest 0.13.5 的依赖和许可](https://github.com/seanmonstar/reqwest/blob/v0.13.5/Cargo.toml) · [TLS 后端](https://docs.rs/reqwest/0.13.5/reqwest/tls/index.html)

`windows` 作业在 Windows CI 的 `desktop/` 目录执行以下步骤。[工作流](../.github/workflows/prepare.yml)

```sh
npm ci
npm run icons
cargo test --manifest-path src-tauri/Cargo.toml --locked
npm run build
node tests/cloud-webview.mjs
```

只有手动调度完整工作流并勾选 `refresh_locks` 时，作业才先刷新 npm 和 Cargo 锁。Tauri CLI 从授权复用的品牌图在云端生成各平台尺寸。Windows 云产物还包括 Go 服务与离线恢复工具。

自动单测覆盖地址、完整 origin、本地连接页及实例响应契约。Windows 云测试安装同次 NSIS 安装包，使用空配置目录、独立的 WebView2 资料目录和回环合成站点。测试通过 Windows UI Automation 在真实连接窗口填写地址、点击保存和标题栏按钮，核对最大化、最小化、媒体分段读取与会话。打开原件窗后，测试逐次核对离线、错误服务、重定向和超时的错误提示、可重试状态、旧窗口与配置，再切换到有效实例，验证旧窗口关闭、正常重启恢复新地址、主窗退出和卸载。Windows 逐次验收见[当前状态](../docs/status.md)。[Windows UI Automation](https://learn.microsoft.com/en-us/dotnet/framework/ui-automation/ui-automation-overview)

官方依据：[Tauri capabilities](https://v2.tauri.app/security/capabilities/)、[窗口导航与新窗口](https://docs.rs/tauri/latest/tauri/webview/struct.WebviewWindowBuilder.html)、[托盘](https://v2.tauri.app/learn/system-tray/)、[Opener](https://v2.tauri.app/plugin/opener/)、[图标](https://v2.tauri.app/develop/icons/)。
