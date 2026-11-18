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

2026-11-18 在提交 `d288c98` 的 Windows 云机器上，OSV 从全部六份锁清单中找到七条原始公告，均来自 Cargo.lock 的六个 crate，glib 各有一条 RustSec 和 GitHub 公告。Cargo.lock 涵盖 474 个包版本。以 rustc 1.99.1 核对后，Windows 发布图选中 234 个包版本，六个被标记的 crate 全部在此图之外，本次阻断数为零。[云端审计结果](https://github.com/Surtr42u/qimeng-next/actions/runs/44377287143/job/135059709367) · [完整清单与反向依赖树](https://github.com/Surtr42u/qimeng-next/actions/runs/44377287143/artifacts/14461792042)

`glib 0.18.5` 的公告记录迭代器存在内存安全问题，修复版本从 0.20.0 起。全目标反向树显示 Tauri 2.12.6 通过 GTK/WebKitGTK 引入 glib，`proc-macro-error` 来自同一图中的 glib-macros 和 gtk3-macros。Tauri 针对 Linux/BSD 的 GTK 0.18 约束以及 GTK 0.18.2 的 glib 0.18 约束仍在生效。迁往修复版本依赖上游 GTK 绑定升级。[glib 公告](https://rustsec.org/advisories/RUSTSEC-2024-0429.html) · [Tauri 的目标约束](https://crates.io/api/v1/crates/tauri/2.12.6/dependencies) · [GTK 的版本约束](https://crates.io/api/v1/crates/gtk/0.18.2/dependencies)

`chrono 0.4.45`、`proc-macro-error 1.0.4`、`zbus_names 4.3.5`、`zvariant 5.15.1` 和 `zvariant_derive 5.15.1` 的 RustSec 记录均属停止维护，均未列同名 crate 的修复版本。`chrono` 来自 serde_with 的可选依赖，当前特性的全目标普通及构建图也没有选中它。其余三个 zbus 子 crate 在 Linux/BSD 由 Opener 2.7.3 经 zbus 5.19.0 引入，后续实现已并入 zbus 6，超出 Opener 当前版本约束。[serde_with 的可选依赖](https://crates.io/api/v1/crates/serde_with/3.24.0/dependencies) · [chrono 公告](https://rustsec.org/advisories/RUSTSEC-2026-0543.html) · [proc-macro-error 公告](https://rustsec.org/advisories/RUSTSEC-2024-0370.html) · [zbus_names 公告](https://rustsec.org/advisories/RUSTSEC-2026-0371.html) · [zvariant 公告](https://rustsec.org/advisories/RUSTSEC-2026-0370.html) · [zvariant_derive 公告](https://rustsec.org/advisories/RUSTSEC-2026-0372.html) · [Opener 的目标约束](https://crates.io/api/v1/crates/tauri-plugin-opener/2.7.3/dependencies)
