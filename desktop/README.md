# 桌面连接壳

全新 Tauri 2 壳，只管理连接地址、窗口、托盘和导航。业务界面由 PC/NAS 同源 Web 提供；没有第二业务数据库、内嵌 Go、原生媒体规则或自动更新器。应用标识独立为 `media.qimeng.next.desktop`，不会覆盖旧客户端配置。

首次打开配置窗，输入 HTTP/HTTPS 根地址，保存后连接影库；访问令牌在 Web 登录页交换。地址写入系统应用配置目录，失败保留旧配置，启动时可从上一份配置恢复。托盘提供打开、切换地址与退出，`--setup` 也能进入配置。关闭主窗口退出应用；媒体子窗口关闭只关闭该窗口。

主窗口采用系统标题栏；最小化、最大化、关闭和拖动均由系统实现。远程 Web 无原生 capability；只有内置连接窗口可调用读写地址命令。导航检查完整 origin（协议、主机、端口），拒绝带凭据及非 HTTP(S) 地址。同源原件可打开独立媒体窗口，外部主页通过系统浏览器打开；禁止 file/shell 协议。Web 不需要标题栏注入脚本和通配远程 IPC。

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

后续云端安装使用已生成 lock；Cargo 使用 `--locked`。图标来源是本仓新写的 `web/public/icon.svg`，Tauri CLI 云端生成平台图标。源码没有旧图标、安装包、真实地址或凭据。

自动单测覆盖地址规范化、凭据/路径拒绝及 origin 隔离。Windows WebView2 的实际连接、Cookie、媒体子窗口、Range、托盘、系统标题栏与退出行为仍需同提交构建产物验收；编译不能代替这些结论。当前没有安装到用户设备，也未连接真实媒体库。

官方依据：[Tauri capabilities](https://v2.tauri.app/security/capabilities/)、[窗口导航与新窗口](https://docs.rs/tauri/latest/tauri/webview/struct.WebviewWindowBuilder.html)、[托盘](https://v2.tauri.app/learn/system-tray/)、[Opener](https://v2.tauri.app/plugin/opener/)、[图标](https://v2.tauri.app/develop/icons/)。
