# 从已封存的旧版 PC v26 影库迁入 Next

Windows 工具包带有 `qimeng-legacy-convert.exe` 和同一提交的 `qimeng-backup.exe`。前者从旧资料制作原生 `.qnext`，后者校验归档、恢复到新的 Next 数据目录。`SHA256SUMS` 用于核对这两个程序。`cloud-receipt.json` 记录产生工具包的提交、运行平台及合成库验收结果。

## 准备私有输入

1. 停止旧版的扫描、写入、回收清理和服务，保持原件与回收元数据冻结，直至转换结束。使用已完成 v26 迁移的一致 SQLite 单文件冷副本。若数据库仍有 `-wal`、`-shm` 或 `-journal`，请先以 SQLite 一致备份方式重新封存。直接丢弃这些文件可能丢失已提交的历史。
2. 从旧库 `libraries.id` 列出所有库，包括已停用的库。在本机保存 UTF-8 JSON 对象，每个旧库 ID 对应其现有原件目录的绝对路径。路径使用规范形式。转换器打开这份明确选择的目录；旧数据库里的主机绝对路径只保留在封存源中。
3. 确认旧 DataDir 下的 `trash` 位置，即使 SQL 表 `trash_items` 行数为零也要检查。若旧服务从未创建过它，请填写其应有的绝对路径，确保父目录存在。真实回收项由文件和同目录的 `.meta.json` 配成一对。
4. 确认旧实例实际使用的 IANA 时区。旧时间戳采用 UTC，部分旧事件及计数需要按原来的本地日期、小时解释。将已确认的时区传给 `--source-timezone`。
5. 准备私有输出父目录和另一个尚不存在的恢复目录。输出目录须位于所有原件目录、冷数据库目录与旧回收站父目录以外。目标卷需要容纳全部原件字节、临时 Next 数据库、归档清单与保留余量。转换直接把源原件流入归档；恢复另需一套实物空间。默认保留 512 MiB，可用 `--reserve-bytes` 指定字节数。

示例映射使用虚构的库 ID 和目录。

若旧 DataDir 位于某个媒体根中，旧扫描器可能按当时的物理配置排除了整个服务数据目录。只凭已封存的 SQLite 和映射无法证明这层排除。遇到未建档的缓存或回收媒体时，转换会报告并中止；应先核对原配置与单独保存的文件清单，再明确选择与旧扫描范围一致的离线副本。转换器不会按目录名猜测哪些实物可以略过。

```json
{
  "00000001-0000-4000-8000-000000000001": "E:\\Example\\Pictures",
  "00000002-0000-4000-8000-000000000002": "F:\\Example\\Video"
}
```

## 制作、校验和恢复

在私有 PowerShell 窗口设置 `$sealedDb`、`$rootsFile`、`$oldTrash`、`$sourceTimezone`、`$newArchiveDir` 和 `$newInstanceDir`。其中路径变量均为绝对路径。

```powershell
.\qimeng-legacy-convert.exe --source-db $sealedDb --roots $rootsFile --trash-root $oldTrash --source-timezone $sourceTimezone --output $newArchiveDir
if ($LASTEXITCODE -ne 0) { throw '转换未完成，请查看私有覆盖报告' }
$archive = Join-Path $newArchiveDir 'library.qnext'
$digest = (Get-Content -LiteralPath (Join-Path $newArchiveDir 'library.qnext.sha256') -Raw).Trim()
.\qimeng-backup.exe verify --archive $archive --sha256 $digest
if ($LASTEXITCODE -ne 0) { throw '归档校验未通过' }
.\qimeng-backup.exe restore --archive $archive --sha256 $digest --destination $newInstanceDir
if ($LASTEXITCODE -ne 0) { throw '恢复未完成，请保留现场' }
```

成功目录含 `library.qnext`、独立的 `library.qnext.sha256`、`coverage.json` 与 `receipt.json`。归档是最后发布的文件。中断后保留失败现场，使用新的输出目录重试。恢复工具另行拒绝已存在的目的地。恢复成功后按 Next 的配置启动新目录，重新设置登录凭据，核对资产、关联、历史和回收站。另存旧数据库、输入映射、辅助文件、归档与两份私有报告。

进度 `checking_catalog_metadata` 表示正在从封存数据库校验并迁入用户设置、作者 TXT、档案和离线头像。这一阶段遇到无法携带的内容，会在读取媒体原件字节之前停止，原因写入私有覆盖报告。通过后进入 `checking_originals`，核对原件和回收项，再在同一事务中按已核验的资产身份写入关联。归档发布前仍会再次核对文件。

## 核对覆盖报告

| 数据 | 如何复核 |
|---|---|
| 来源和库 | `sourceId` 绑定整份封存 SQLite 的 SHA-256。`rootSelectionSHA256` 绑定本次库 ID 与目录的选择。新库保留原库 ID、名称、分类、禁用状态和创建时间。恢复后的库根位于新数据目录。 |
| 原件 | 按旧资产 ID 写入版本 1。用数据库里的相对路径、大小、毫秒级修改时间核对磁盘文件，逐项取 SHA-256；写归档时再次校验字节，重新核对文件身份、大小与修改时间。清单记录实际修改时间。两个旧身份即使字节相同仍各有原件记录。 |
| 回收站 | 旧 `trash/<19位时间戳>/<资产ID>/<原相对路径>` 中的成对文件成为 Next 库内回收项，保留恢复路径与删除时间。正在恢复的旧条目、缺件、身份冲突或有内容的废弃 SQL 回收表需要先处理。 |
| 作者、标签与行为 | 在同一新事务内投影旧作者、词表、关联、收藏、喜欢、浏览和播放等有据事实。来源原文或结构化留证按独立冷库身份进入 `migration_*_origins`。旧续播、标记、事件只有对应版本证据才能绑定原件。详见 `coverage.json` 的逐表处置和留证计数。 |
| 已完成或待处理的旧操作 | AI 请求保留 HTTP 方法、状态和时间，上传回执保留录入的字节数、时间与可定位的旧资产行。维护任务及设备操作保留状态、时间、数量和父行关系。完整旧记录由封存 SQLite 留存。旧命令、主机路径、原始请求、租约和令牌均在旧源中保管。 |
| 旧凭据、索引和文件 | 报告分别统计已排除的用户、会话、AI 客户端与可重建索引。库内非媒体文件计入 `unarchivedNonMediaFiles`；旧扫描器排除的隐藏或系统目录条目计入 `excludedOldScannerEntries`。这些文件、目录与外置作者文档须另行私下备份。工具不会把 TXT 或其他附件猜成一个媒体资产。 |

作者档案中的公开归档链接可以在路径段中引用另一个 HTTP(S) 页面。转换器逐层检查嵌入网址的主机、路径、查询和片段，识别字面或百分号编码的公开引用，并保留原链接的拼写。整个检查离线完成。带登录信息的地址、本机或私有主机、封存库根和无法明确解析的多重编码会中止转换。

旧作者 TXT 正文还可使用一种 HTTPS 中继链接，其整段查询只有一个百分号编码的完整公共 HTTPS 引用。转换器单独核对内层引用，保留 TXT 原字节。键值式查询和其他设置继续按各自字段校验。

旧档案只有四个描述性简介字段享有查询参数处理规则，即 `/biography`、`/candidate/biography`、`/xProfile/biography`、`/xProfile/biographyZh`。如果其中的公开 HTTP(S) 链接带凭据类查询参数，转换器仅移除对应的参数对，保留周围文字、普通参数及其原有顺序和编码。作者核心资料、公开块、候选、档案和来源留证全部使用同一份可携值。主页、头像、结构化来源、备注、动态标题和设置仍要求其内容本身可以携带。

每个经过处理的字段在 `cold26/profile-text-disposition` 留一行。其 `decision.field` 是 JSON Pointer，`decision.action` 为 `remove_sensitive_url_query_parameters`，`decision.removedParameters` 是该字段移除的参数数。对应的 `cold26/author-profile` 行将 `valueDisposition` 记为 `portable_query_redacted`，`fields` 汇总以上处置，`value` 保存处理后的档案。这两类行的 `sealedSource` 都保存 `sourceId`、表名 `kv_settings` 和源记录键。需核对原文时，按这个定位读取摘要匹配的私有封存库。

`problem.table` 与 `problem.rowId` 可在匹配摘要的封存源里定位失败行。磁盘问题的 `locatorSHA256` 采用 UTF-8 编码；未建档原件以 `旧库ID/原相对路径` 取 SHA-256，回收站问题以 `trash` 下的相对路径取 SHA-256。报告接受新发现的空表并标记其状态。未知非空表、无法核对的实际原件与未解释的真实业务记录会阻止归档完成。原生格式上限为 50 万个文件和 64 MiB 清单。

云端验证使用旧仓固定提交的 26 个公开迁移脚本生成合成库，再调用正式程序。真实冷数据库、媒体、主机路径、覆盖报告和运行收据仅保存在操作者的私有位置。[旧版表结构](https://github.com/Surtr42u/qimeng-media/tree/d0215962bb1e9c29baabeed9fc9dc5c8b4b94555/server/migrations) · [同提交云端验证](../.github/workflows/legacy-cold.yml)
