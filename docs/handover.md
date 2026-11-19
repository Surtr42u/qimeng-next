# 交接：先确认主线，再交付两端安装包

2026-11-19 23:29（北京时间）的公开断点：`main` 为 `407ed698528966e73793fa1e00808ee78eb1f3af`；[PR #17](https://github.com/Surtr42u/qimeng-next/pull/17) 尚在核验，候选为 `3fc95076264dced03859ab961ab1935a8c561f2f`。接手时先查询实时状态。只有主线发布作业能给出本次交付的正式包。真实安装、后台预热和实体设备验收各自记入所有者私有交接。

## 1. 查清哪些工作已经完成

| 范围 | 能够复核的结果 |
|---|---|
| 已合入主线 | [#14](https://github.com/Surtr42u/qimeng-next/pull/14) 把封存的旧 PC v26 资料转成可恢复的 Next 原生归档。[#15](https://github.com/Surtr42u/qimeng-next/pull/15) 只合并逐组获准的图片，保存每份原件、旧身份、历史和来源。[#16](https://github.com/Surtr42u/qimeng-next/pull/16) 平衡 Go race 分组并保留原覆盖。 |
| #17 服务端 | 合成图和视频在未浏览前已由后台写出三档缓存，并从正式接口读回相同 JPEG。大图专项验证顺序及渐进 JPEG、非交错 PNG 和拒绝路径。[三档预热](https://github.com/Surtr42u/qimeng-next/actions/runs/44546981348) · [大图](https://github.com/Surtr42u/qimeng-next/actions/runs/44552284220) · [附加渐进样本](https://github.com/Surtr42u/qimeng-next/actions/runs/44553218142) |
| #17 Web 和发布包 | AI 接入改了授权列表、分步配置与窄屏排版。备份页及文件名复制按钮已有旧版可见对照。候选的正式 `web-ui` 作业通过 125 项。发布包附展示解码器的来源和许可证。[AI 专项](https://github.com/Surtr42u/qimeng-next/actions/runs/44551086609) · [旧版对照](https://github.com/Surtr42u/qimeng-next/actions/runs/44553053027) · [正式浏览器作业](https://github.com/Surtr42u/qimeng-next/actions/runs/44553147191/job/135663162270) · [随包许可](../deploy/third-party/preview-decoders/README.md) |

## 2. 合并、取包和记录证据

在检出的 Next 仓库中执行。`gh` 命令查询公开源码和 Actions 元数据；每步把所选完整 SHA、运行编号、作业结论与产物对应起来。

```powershell
gh pr view 17 --json state,headRefOid,mergeCommit,url
gh pr checks 17
gh api repos/Surtr42u/qimeng-next/commits/main --jq .sha
```

1. `OPEN` 时核对候选 SHA，再看仍运行或失败的作业。有新提交时以新提交关联的结果为准。获得合并授权且所需检查通过，按仓库流程合并。若已为 `MERGED`，保存 `mergeCommit.oid`，从这一步继续。`CLOSED` 且无合并提交，先查处置记录。
2. 找出含产品改动的主线提交和成功的 `Cloud verification`。用下列命令列出实际作业，确认 Windows 服务包、所需 Windows 桌面壳、Android 纯客户端与内嵌版。核对 artifact 未过期、下载包的 SHA-256 和适用作业。只有 Markdown 的后续提交可能跳过产品流水线，此时明确记载实际打包的主线 SHA。具体启动方式见[快速开始](quickstart.md)、[桌面](../desktop/README.md)和[Android](../android/README.md)。

```powershell
$buildSha = gh pr view 17 --json mergeCommit --jq .mergeCommit.oid
gh run list --branch main --workflow prepare.yml --commit $buildSha --json databaseId,headSha,event,status,conclusion,url
# 选定上述匹配的成功运行，将其 databaseId 赋给 $runId
gh run view $runId --json headSha,jobs,conclusion
gh run download $runId -n qimeng-next-package-windows-amd64 -D packages/windows-server
gh run download $runId -n windows-first-check -D packages/windows-desktop
gh run download $runId -n android-embedded-check -D packages/android-offline
gh run download $runId -n android-first-check -D packages/android-client
```

3. Windows 服务 ZIP 与桌面壳用途不同。手机单机选内嵌版，连接已运行的 PC/NAS 可选纯客户端。Next Android 包名为 `media.qimeng.next`；旧版为 `media.qimeng.app`。只在所有者明确指定的物理设备和允许的后台时段操作，私下核对包名、签名、目标与已有数据。锁屏时保持原状，不自动唤醒、解锁、触发前台展示或采集真实界面。保留旧应用和旧库，已有 Next 的升级保留其数据。安装证据与启动、实际后台回读分别登记；证据不足就把对应步骤留待授权。[Next 构建声明](../android/app/build.gradle.kts) · [旧版构建声明](https://github.com/Surtr42u/qimeng-media/blob/745974081716a9d69a95002a41220e35e743d764/android/app/build.gradle.kts)

## 3. 验证后台确实写出了三种 JPEG

1. 云端用普通上传加入一张合成图片和一段可解码视频；安装后的正常运行复核只在所有者指定的 Next 数据根，使用其允许的只读方式。保留授权时的磁盘余量、启用库、应有预览的当前资产和内容版本基线。后台约在启动 30 秒后开始，每页最多 256 项，先 512，再补 256 和 1024，每 10 分钟全库复核。先关闭会请求缩略图的视图，在取样结束前不访问缩略图接口。保留既有缓存。
2. 读 `cache/<资产ID>/v3-<当前内容版本>-<边长>.jpg`，校对合并的成员使用 `v4`。以资产和当前版本分别统计 256、512、1024 三档的实有数、三档齐全数以及缺项。两次带时刻的私有计数可说明后台进度。按启用库中能生成预览的资产对账，单列原件缺失、编码/资源限制、空间暂停和过期缓存。公开的两件合成输入预期每档两件、共六件；真实总数与任何标识只留本机。
3. 核对 JPEG 签名 `FF D8 FF`，再从文件解出宽高，完整解码所选样本。长边应为 `min(档位, 原长边)`，短边按原比例取整。缓存清点完成后，用带当前版本的 `/api/v1/assets/{id}/thumbnail?size=sm|md|lg&version={version}` 逐档核对 HTTP 200、JPEG 和缓存字节，原件出口 SHA-256 与基线一致。连续缺档先查当前版本、格式、后台存活、存储余量和媒体工具。只记录实际观察到的覆盖；新版本或新数据变化再安排复查。[正常入口合成用例](../server/internal/httpapi/thumbnail_warmup_test.go) · [格式和预算](api.md#写入与回放)

超过 4000 万像素的展示缩略图支持有界的顺序 JPEG、灰度或三通道渐进 JPEG、非交错 PNG。总预算为 2.56 亿像素、单边 32,768、压缩原件 256 MiB、头部 2 MiB；渐进 JPEG 另限 1.28 亿像素，PNG 中间画布限 96 MiB。大型 Adam7 PNG 或越界输入保留原件直读，展示接口可返回 422。AI 研究材料仍受独立 4000 万像素预算和外发授权约束。[展示实现](../server/internal/service/preview_large_still.go) · [研究材料](../server/internal/service/research_materials.go)

## 4. 保留恢复身份与回退点

| 所在阶段 | 验收与可执行的回退 |
|---|---|
| 封存旧库、制作原生归档 | 从只读封存 SQLite、全部明确指定的原件副本和旧回收站转换。保存旧资料、完整未合并 `.qnext`、独立 SHA-256 和私有覆盖报告；核对原资产 ID、文件字节、时间、作者和事件，字节相同的独立身份仍各在。转换或对账有差异时保留源及失败报告，在新的输出目录重做受影响步骤。[旧 PC 转换](legacy-cold.md) |
| 新目录恢复、尚未切换 | `qimeng-backup verify --archive ARCHIVE --sha256 DIGEST` 成功后，执行 `qimeng-backup restore --archive ARCHIVE --sha256 DIGEST --destination NEW_DIR`。恢复生成独立实例身份，重新授权设备和外部存储，核对原资产/内容版本、历史和关系。遇 `RESTORE_INCOMPLETE` 保留现场，用已核验归档和另一处尚不存在的目标恢复。[原生恢复](migration.md#完整归档与隔离恢复) |
| 明确选择的图片合并 | 只在尚未启动的恢复副本，用已对账的数据库摘要、归档摘要和计划摘要执行 `check`、`apply`。保存预像、每个成员及来源回执，未选条目照旧可读。出现 `ready.json` 可凭原参数续作；只有 `attempt.json` 则保留现场并另起恢复。完整撤回合并时，从未合并归档恢复到另一新目录。[精确命令与中断分支](selected-image-merge.md) |
| 已切换、需要退回 | 先停止对新实例的写入，保留切换后新增事实的归档与差异。用原软件、原独立目录或对应版本的已核验归档另起实例，回读身份和资料后再改连接。新写入另行按明确授权处理，保留两侧以供核对。[迁移与回退](migration.md) |

## 5. 交付边界与结束条件

- 普通上传直接经客户端与 Go 服务完成，断开 AI 执行器仍然可用。智能整理由用户显式选择，按既有凭据、范围和预算工作。真实资料仅在获准的私有本机环境处理。公开仓库、讨论、截图、日志和 CI 制品只使用源码或合成材料，真实媒体、路径、作品或设备标识、数量、令牌及机器信息均留在私有记录。[产品要求](requirements.md) · [安全说明](../SECURITY.md)
- 用户正在用的旧 UI 基准为 `74597408`。本轮原图详情只授权修改「复制完整文件名」按钮自身；其余布局、文字、图片及交互沿旧版。备份页和 AI 接入页按各自授权及现有证据交付。既有可见对照和浏览器回归已覆盖此次 Web 改动，所有者已明确不为细微像素差追加截图门禁。实体 NAS 的卷权限、持久化和重启，以及 Android Compose 在指定真机真实显示的材质、手势和返回，分别留待对应环境和授权。[界面对照](ui.md) · [NAS 启动](quickstart.md#nas) · [Android 对照](../android/UI_PARITY.md)
- 本轮所有者要求主会话和每位受委派代理均为 `gpt-6.2-astra`、推理 `max`。各自从本轮 rollout 的 `turn_context` 核对实际 model 与 effort 再承担工作；只有启动参数、界面名或别人的记录时，将该会话记为未核实。这是本次交接约定。使用其他维护安排的后续任务按届时授权执行。
- 产品交付以主线 SHA、对应作业成功、包的来源与摘要、各授权环境的安装/实际回读为准。安全或身份校验失败，保留原件、归档和现场，暂停受影响的操作；其他已授权步骤继续。缺物理设备、屏显许可或 NAS 时登记待验收项、所需条件及下一观察结果。已有证据通过且没有新变更或具体失败时结束该项。
