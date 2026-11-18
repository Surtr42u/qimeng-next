# 安全与隐私

绮梦 Next 是自托管影库。GitHub 保存项目源码、维护文档和使用合成资料的测试。你在 PC、NAS 或手机上部署的影库使用自己的存储目录和授权，运行时数据由该实例管理。[实例与存储边界](docs/architecture.md#module-boundaries)

## 反馈问题时怎样准备资料

普通问题可以写明版本或提交号、平台、操作步骤、预期和实际结果。用一张新生成的图片、临时库和虚拟作者复现即可。要说明布局差异时，优先贴合成资料的截图。

真实照片、视频、作者档案、库备份、账户口令、AI 或设备令牌、签名密钥、带认证的链接，以及可识别个人的路径和设备序列号都应留在本机。分享日志前先查看内容，只摘取与故障有关的脱敏行。完整运行目录、浏览器跟踪和系统导出可能包含额外信息。

## 维护者发布前核对什么

核对将进入 Git 的文件、提交历史和发布附件。历史 Actions 日志、测试报告和截图在仓库公开后也可能被读取。使用隔离云机器、合成输入和临时凭据采集证据，归档步骤列出允许上传的具体路径。将本地配置保存在仓库外或受忽略的文件中。[忽略规则](.gitignore) · [云端工程检查](docs/verification.md#engineering-gates)

工程检查使用固定版本的 Gitleaks，在输出中遮蔽疑似秘密。SDK 文件校验和、生成器固定摘要的例外限定到对应文件和文本形状。机器检查无法识别所有私人影像、文件名和自由文本，维护者仍须核对其来源和实际可见内容。[扫描规则](.gitleaks.toml) · [Gitleaks 官方项目](https://github.com/gitleaks/gitleaks)

若发现已暴露的有效凭据，先在签发端撤销或轮换，然后核对包含它的提交、日志和附件。后续提交删除一行不能撤回已公开的旧副本。公开问题中只说明受影响的版本和信息类别，避免再次贴出原值。[GitHub 敏感数据处理说明](https://docs.github.com/en/authentication/keeping-your-account-and-data-secure/removing-sensitive-data-from-a-repository)

## 锁定依赖公告

GitHub Actions 对 Go、Python、Node 和 Rust 的完整锁清单查询 OSV，保留原始 JSON、两份 Cargo 依赖树、命中 crate 的反向路径和门禁结果。桌面当前发布 Windows x64 NSIS。审计在原生 Windows 主机按 `x86_64-pc-windows-msvc`、项目配置和 Tauri 发布特性 `tauri/custom-protocol` 取普通依赖与构建依赖，包含过程宏。另一份树使用相同特性枚举其他目标。Cargo 树描述所选构建依赖，具体函数是否执行需另有证据。[检查工作流](.github/workflows/dependencies.yml) · [Cargo 依赖树说明](https://doc.rust-lang.org/cargo/commands/cargo-tree.html) · [Tauri CLI 的发布选项](https://github.com/tauri-apps/tauri/blob/tauri-cli-v2.12.5/crates/tauri-cli/src/interface/rust.rs#L501)

Go、Python、Node 的命中继续阻断。Windows 构建图中的 Rust 安全公告、内存安全问题及未分类命中同样阻断。上游明确标为 `unmaintained` 的 RustSec 记录会作为维护事项列出。其他目标或当前特性未选中的 crate 仍出现在完整报告中。新增发布目标时，一并加入该目标的构建图。Android 系统组件和设备厂商补丁另行核对。[OSV 输出格式](https://google.github.io/osv-scanner/output/) · [RustSec 公告库](https://rustsec.org/advisories/)

首轮完整清单命中六个 Rust crate。`glib 0.18.5` 的公告记录迭代器存在内存安全问题，修复版本从 0.20.0 起。Tauri 2.12.6 的 Linux/BSD 分支使用 GTK 0.18，GTK 0.18.2 要求 glib 0.18 系列。迁往修复版本依赖上游 GTK 绑定升级。[glib 公告](https://rustsec.org/advisories/RUSTSEC-2024-0429.html) · [Tauri 的目标约束](https://crates.io/api/v1/crates/tauri/2.12.6/dependencies) · [GTK 的版本约束](https://crates.io/api/v1/crates/gtk/0.18.2/dependencies)

`chrono 0.4.45`、`proc-macro-error 1.0.4`、`zbus_names 4.3.5`、`zvariant 5.15.1` 和 `zvariant_derive 5.15.1` 的 RustSec 记录均属停止维护，均未列同名 crate 的修复版本。Opener 2.7.3 的 Linux/BSD 分支要求 zbus 5，后三项的后续实现由 zbus 6 提供。[chrono 公告](https://rustsec.org/advisories/RUSTSEC-2026-0543.html) · [proc-macro-error 公告](https://rustsec.org/advisories/RUSTSEC-2024-0370.html) · [zbus_names 公告](https://rustsec.org/advisories/RUSTSEC-2026-0371.html) · [zvariant 公告](https://rustsec.org/advisories/RUSTSEC-2026-0370.html) · [zvariant_derive 公告](https://rustsec.org/advisories/RUSTSEC-2026-0372.html) · [Opener 的目标约束](https://crates.io/api/v1/crates/tauri-plugin-opener/2.7.3/dependencies)
