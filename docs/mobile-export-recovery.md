# 从旧手机 v2 导出封存可核对的事实

手机只剩旧版的完整 v2 JSON 和能取得的原件时，可以生成一个私有迁移资料目录。该目录保存每项已导出的业务记录，并将其中的资产身份对应到本次实际读到的原件。覆盖报告的范围是 `exported_facts_only`。原手机 SQLite、原生 Next 归档与新实例恢复是另外的交付阶段。

## 输入

1. 准备未经修改的 UTF-8 v2 JSON，以及单独保存的该文件 SHA-256。工具核对 `qimeng_backup`、`schemaVersion=2`、`com.qimeng.media`、本次导出时间和固定版本导出器的完整段集。推荐偏好可以缺省。没有关注作者时，旧导出可能写 `followedAuthorIds: null`。
2. 在可信的本地文件系统保留选定的原件目录。准备与校验期间停止更新目录及文件。原件应保留旧库相对路径、名称、字节数与修改时间至毫秒。v2 没有原件 SHA-256，只有从前期另行取得的文件摘要才能进一步证明当时的字节身份。
3. 写一份私有 UTF-8 JSON 对象，将每个出现于 `mediaFiles.libraryName` 的库显示名映射到一个绝对的本机原件根。这个选择来自操作者，旧 v2 没有提供库 ID。如果两个旧库显示名相同且无法用独立资料证明它们与根的一一对应，应保留原始资料并先解决归属。工具拒绝映射遗漏、额外根、根重叠、相对路径歧义、两个身份共用同一物理文件以及扫描范围内多出或缺少的媒体。旧扫描器排除的点文件、点目录及系统目录只计入排除数，其他普通非媒体文件也单独计数。
4. 选一个私有输出父目录，使用其下的全新子目录名，放在全部原件根以外。Windows 和 Linux 均可使用 Python 3.13，Linux 输出卷需要支持独占目录重命名。脚本仅使用标准库。

## 准备与独立回读

从本仓根目录运行。在 PowerShell 中将 `$export`、`$roots` 和 `$out` 设为上述绝对路径，将 `$exportHash` 设为原先保存的导出 SHA-256。私有内容、映射及输出仅存本地。

```powershell
$tool = 'tools/mobile-export-recovery/prepare.py'
$result = python -X utf8 $tool prepare --export $export --export-sha256 $exportHash --roots $roots --output $out
if ($LASTEXITCODE -ne 0) { throw '准备未完成' }
$receiptHash = ($result | ConvertFrom-Json).receiptSHA256
python -X utf8 $tool verify --source $out --receipt-sha256 $receiptHash --roots $roots
if ($LASTEXITCODE -ne 0) { throw '回读未通过' }
```

将首次成功返回的 `receiptSHA256` 单独保存。它绑定导出字节、逐文件 SHA-256、覆盖报告及当时选定的根。相同输入重复运行返回 `already_prepared` 并逐文件核对既有结果。现有空目录、陌生文件、内容不同或缺件的目录都会被拒绝。正常失败不发布结果。进程被强制终止可能留下只含本次生成材料的私有暂存目录，重新调用可使用原定输出名。

更换磁盘或挂载点后，可给 `verify` 提供新的私有映射。回读再次核对每一条导出身份、相对路径、大小、毫秒修改时间和 SHA-256。新的选择在成功回执中显示 `sameRootSelection=false`。最初统计的辅助文件与扫描排除数仍指向准备时的根。

## 给后续离线投影的文件

| 文件 | 内容 |
|---|---|
| `export.json` | 完整旧 JSON 的原始字节。全部段、原字段、顺序、可空值、未知扩展均可从这里读回。未知扩展只作封存。 |
| `originals.jsonl` | 每行对应一条 `data.mediaFiles`，保持导出顺序，字段为 `assetId`、`recordKey`、`libraryName`、`relPath`、`sizeBytes`、`modifiedAtMillis` 和本次实测的 `sha256`。原件字节仍由选定根保管。 |
| `coverage.json` | 逐段记下存在性、数量、已核对的引用或仅封存的内容。`sourceId` 使用 `portable-v2:<导出字节 SHA-256>` 指代本次文件。`oldInstanceId` 和 `oldLibraryIds` 保持未知。 |
| `receipt.json` | `qimeng-mobile-export-source/v1`，列出以上三份文件的 SHA-256，并用 `rootSelectionSHA256` 绑定准备时的目录选择，独立回读使用另存的收据摘要。 |

作者与标签关联按导出的 `recordKey`、`authorId` 和标签名核对。旧关系上没有记录的成立时刻继续保持缺省。历史核对逐条的 `eventId`、原会话、时刻、秒数及关联，并核对 `history` 与所有 `open` 的数量、时刻与目标。`dailyBrowse`、`mediaStats` 和 `history` 均保留作对账。喜欢保留累计数和已给出的最后日期。收藏、关注、时间轴、TXT、档案、头像和设置的导出细节保存在原始 JSON；当前步骤对它们执行的检查见逐段报告。

未导出的原库 ID、空库清单、过去版本、回收项及删除证据、逐日喜欢明细、续播进度、未被导出的孤立事件、上传与设备操作记录无法从这份材料还原。旧正式导入需要已入库的目标资产，还会为部分缺失时间与行 ID 赋新值。后续原生投影要逐项使用已核对身份、明确标注新容器身份，并区分已投影与仅封存事实。现有 PC v26 转换器的输入要求一份真正的一致冷 SQLite。[旧导出与导入语义](https://github.com/Surtr42u/qimeng-media/tree/d0215962bb1e9c29baabeed9fc9dc5c8b4b94555/server/internal/portablebackup) · [PC v26 离线转换](legacy-cold.md)
