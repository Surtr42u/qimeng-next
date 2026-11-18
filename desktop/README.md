# 桌面连接壳

全新 Tauri 2 壳，只管理连接地址、窗口、托盘和导航。业务界面由 PC/NAS 同源 Web 提供；没有第二业务数据库、内嵌 Go、原生媒体规则或自动更新器。应用标识独立为 `media.qimeng.next.desktop`，不会覆盖旧客户端配置。

先准备一个已运行的影库。需要在电脑或 NAS 建库时，按[快速开始](../docs/quickstart.md)下载服务包。桌面壳的 Windows NSIS 安装包在同次 `Cloud verification` 的 `windows-first-check` 制品中。核对对应 `windows` 作业和[窗口验收记录](../docs/status.md)，再选择要试用的提交。

首次打开配置窗，输入 HTTP/HTTPS 根地址，保存后连接影库；访问令牌在 Web 登录页交换。地址写入系统应用配置目录，失败保留旧配置，启动时可从上一份配置恢复。托盘提供打开、切换地址与退出，`--setup` 也能进入配置。关闭主窗口退出应用；媒体子窗口关闭只关闭该窗口。

主窗口恢复旧版 1200×800 无边框布局，Web 顶栏原位置的三键、空白区域拖动和双击调用当前窗口的原生操作。Tauri 运行时 capability 按每次连接唯一的 WebView 标签和所选 HTTP(S) 完整 origin 建立，只允许 `window_action`。Rust 选择目标并检查当前活动窗口。连接地址命令仅限内置连接页，媒体子窗不能调用标题栏或地址命令。

主窗及原件子窗逐次核对导航协议、主机与端口。外部 Web 链接由系统浏览器打开，关闭子窗只影响该子窗，切换服务器会关闭旧主窗及其原件子窗。设置窗口复用旧 `desktop/ui/setup.html` 的 420×340 布局、颜色及 `desktop/ui/app-icon.png` 品牌图；地址、保存与权限判断由 Next 实现。

主窗打开原件时，壳先核对请求的地址，将同一浏览器环境中新建的空白子窗交给 WebView2，再由 WebView2 导航至原件。这样原件请求沿用登录会话。子窗随后继续检查所选服务器的完整 origin。[WebView2 新窗口约定](https://learn.microsoft.com/en-us/microsoft-edge/webview2/reference/win32/icorewebview2newwindowrequestedeventargs#put_newwindow)

## 云端准备与验证

CLI、Tauri 和插件版本见[前端依赖](package.json)和[Rust 依赖](src-tauri/Cargo.toml)，传递版本由仓库中的 `package-lock.json`、`src-tauri/Cargo.lock` 固定。

`windows` 作业在 Windows CI 的 `desktop/` 目录执行以下步骤。[工作流](../.github/workflows/prepare.yml)

```sh
npm ci
npm run icons
cargo test --manifest-path src-tauri/Cargo.toml --locked
npm run build
node tests/cloud-webview.mjs
```

只有手动调度完整工作流并勾选 `refresh_locks` 时，作业才先刷新 npm 和 Cargo 锁。Tauri CLI 从授权复用的品牌图在云端生成各平台尺寸。Windows 云产物还包括 Go 服务与离线恢复工具。

自动单测覆盖地址、完整 origin 和本地连接页边界。Windows 云测试安装同次 NSIS 安装包，使用独立的 WebView2 资料目录。测试通过 Windows UI Automation 在真实连接窗口填写地址、点击保存和标题栏按钮，用两个回环合成站点核对媒体分段读取与会话，再从原生窗口核对最大化、最小化、切换服务器后关闭旧窗口，以及主窗退出和卸载。Windows 逐次验收见[当前状态](../docs/status.md)。[Windows UI Automation](https://learn.microsoft.com/en-us/dotnet/framework/ui-automation/ui-automation-overview)

官方依据：[Tauri capabilities](https://v2.tauri.app/security/capabilities/)、[窗口导航与新窗口](https://docs.rs/tauri/latest/tauri/webview/struct.WebviewWindowBuilder.html)、[托盘](https://v2.tauri.app/learn/system-tray/)、[Opener](https://v2.tauri.app/plugin/opener/)、[图标](https://v2.tauri.app/develop/icons/)。
