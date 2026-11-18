# 已安装版本的 Android 截图基准

基准使用绮梦影库 0.2.3/code38 的生产首页、详情、标签弹层和外观设置。安装记录对应源码 `745974081716a9d69a95002a41220e35e743d764`，旧仓的发布作业为 43873579739。应用通过自己的登录页连接同提交的 Go 服务。服务在 GitHub 临时主机的回环端口 18483 运行。数据只有一张双色圆形 PNG、一段 12 秒 H.264 视频和一个标签。 [旧版源码](https://github.com/Surtr42u/qimeng-media/tree/745974081716a9d69a95002a41220e35e743d764/android) · [发布作业](https://github.com/Surtr42u/qimeng-media/actions/runs/43873579739)

| 产物 | 可核对的状态 |
| --- | --- |
| `provenance.json` | 源提交、GitHub 采集作业、参考制品 ZIP 和原版 APK 的 SHA-256，以及从模拟器安装目录回读的 APK SHA-256 |
| `installed-home-liquid.png` | 全屏首页，首次登录、系统浅色、原始默认液态玻璃、首页选中，合成 PNG 和视频均已列出 |
| `installed-tag-sheet.png` | 从首页打开合成 PNG 的详情，展开「标签」，勾选「安卓云端标签」，画面显示「已选 1 个」 |
| `installed-home-classic.png` | 同一会话经「我的 → 外观 → 经典」返回首页，两份媒体与首页选中状态相同 |
| `installed-navigation-liquid.png`、`installed-navigation-classic.png` | 从各自全屏原图无损截取底部 132 像素，包含底栏、系统手势区和上方留白 |
| `manifest.json` | 源码树、系统镜像、屏幕参数、合成原件和截图的 SHA-256，以及首页两个媒体项与四个底栏标签的可访问性矩形 |

`installed-release` 表示模拟器实际安装了发布制品中的原包。GitHub 制品编号为 14228034776，ZIP 的 SHA-256 为 `5337e94e60a582b27cb25a888363fa1d1c58c7490f50f00b00c4b80e0d111add`。APK 的 SHA-256 为 `72cd0c695e888877a8f07f27486c308b1b0bf8179e57e90dabf19adb384dc84a`，与旧仓交接记录中的手机回读值相同。原包只含 arm64。制品超过保留期、工作流无法读取该制品或镜像缺少 arm64 支持时，`pinned-source-debug` 表示由同一提交原样构建的 x86_64 兼容 debug 包。此时以清单中实际 APK 摘要标识截图。下载成功后，两级校验中任一不符都会使作业失败。

## GitHub Actions 采集条件

1. 使用独立的 Ubuntu 24.04 GitHub-hosted runner、Go 1.27.4、Node 24、JDK 21、Android SDK 37.0、Make、FFmpeg 和系统 Python 包 `python3-pil`。以仅可读的 GitHub token 调用 `bash android/baseline/prepare.sh`，它在 runner 临时目录拉取固定提交、生成旧 Go SDK、编译旧服务并取得原 APK。
2. 以和 Next 联网仪器相同的配置启动全新 API 35、Google APIs、x86_64、SwiftShader AVD。固定名称 `qimeng_api35`、端口 5556，显式设置 `ANDROID_SERIAL=emulator-5556`，在 emulator runner 内调用 `bash android/baseline/capture.sh`。脚本核对 AVD 名、系统版本、包是否为首次安装，将窗口设为 320×640、160 dpi、字体比例 1、系统浅色。
3. 以 `always()` 上传 `android/build/installed-ui-baseline/`。完成条件为 `manifest.json` 的 `state=complete` 和表内全部 PNG。若截图时状态断言失败，`unverified-*.png` 及 `failedStage` 供定位。登录凭据、服务器原始日志和完整设备窗口转储只存在于临时作业环境。

登录采集核对手填地址，并等待可点击的「连接影库」按钮连续两次出现在相同位置。旧版发现服务期间会禁用按钮，输入法收起时表单位置也会变化。`44305961206` 读回了原发布 APK，但当次最终窗口仍在登录页，没有产出同场景对照图。[当次诊断制品](https://github.com/Surtr42u/qimeng-next/actions/runs/44305961206/artifacts/14429915949)

Next 的同场景来自联网仪器产物 `real-server-home.png`、`real-server-home-classic.png`、`real-server-tag-sheet.png`。两个首页缩略图的顺序由各自 Go 推荐服务确定。比较栏高、图标、卡片、字体和边距时以同尺寸全屏图及语义矩形为准。媒体名、画幅和主要色块对应；两个服务各自编码缩略图，系统时钟、JPEG 压缩和模拟 GPU 的背景采样可能产生像素差异。库组件的 `navigation-liquid.png`、`navigation-classic.png` 使用合成示例布局，其宿主在仪器清单内单独标识。
