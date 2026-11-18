# 桌面连接壳

全新 Tauri 2 壳，只管理连接地址、窗口、托盘和导航。业务界面由 PC/NAS 同源 Web 提供；没有第二业务数据库、内嵌 Go、原生媒体规则或自动更新器。应用标识独立为 `media.qimeng.next.desktop`，不会覆盖旧客户端配置。

首次打开配置窗，输入 HTTP/HTTPS 根地址，保存后连接影库；访问令牌在 Web 登录页交换。地址写入系统应用配置目录，失败保留旧配置，启动时可从上一份配置恢复。托盘提供打开、切换地址与退出，`--setup` 也能进入配置。关闭主窗口退出应用；媒体子窗口关闭只关闭该窗口。

主窗口恢复旧版 1200×800 无边框布局，Web 顶栏原位置的三键、空白区域拖动和双击调用当前窗口的原生操作。Tauri 运行时 capability 按每次连接唯一的 WebView 标签和所选 HTTP(S) 完整 origin 建立，只允许 `window_action`。Rust 选择目标并检查当前活动窗口。连接地址命令仅限内置连接页，媒体子窗不能调用标题栏或地址命令。

主窗及原件子窗逐次核对导航协议、主机与端口。外部 Web 链接由系统浏览器打开，关闭子窗只影响该子窗，切换服务器会关闭旧主窗及其原件子窗。设置窗口复用旧 `desktop/ui/setup.html` 的 420×340 布局、颜色及 `desktop/ui/app-icon.png` 品牌图；地址、保存与权限判断由 Next 实现。

## 云端准备与验证

精确依赖来自 2026-11-17 官方 npm / crates.io 元数据：CLI 2.12.5；tauri 2.12.6、tauri-build 2.7.5、opener 2.7.3、serde 1.0.229、serde_json 1.0.154、url 2.5.8。依赖的传递锁必须由云端生成并回收，不能据精确直接版本声称全部已锁。

在 Windows CI 执行，禁止本机运行：

```sh
cd desktop
npm install --package-lock-only --ignore-scripts
npm ci
npm run icons
cargo generate-lockfile --manifest-path src-tauri/Cargo.toml
npm test
npm run build
```

后续云端安装使用已生成 lock；Cargo 使用 `--locked`。Tauri CLI 从授权复用的品牌图在云端生成各平台尺寸。Windows 云产物包括 NSIS 安装包和 Go 服务/恢复工具。

自动单测覆盖地址规范化、凭据/路径拒绝、完整 origin 和本地连接页边界。Windows 云冒烟从同次 NSIS 安装，使用独立 WebView2 资料目录、两个临时合成站点和原生窗口状态，核验 Cookie、Range、子窗换服、标题栏三键、退出与卸载。05d9da5 产物已完成构建，首次窗口检查停在读取连接页。后续运行的窗口收据与用户设备、真实托盘菜单另行记录。[微软 WebView2 分发说明](https://learn.microsoft.com/en-us/microsoft-edge/webview2/concepts/distribution) [Playwright WebView2 连接办法](https://playwright.dev/docs/webview2)

官方依据：[Tauri capabilities](https://v2.tauri.app/security/capabilities/)、[窗口导航与新窗口](https://docs.rs/tauri/latest/tauri/webview/struct.WebviewWindowBuilder.html)、[托盘](https://v2.tauri.app/learn/system-tray/)、[Opener](https://v2.tauri.app/plugin/opener/)、[图标](https://v2.tauri.app/develop/icons/)。
