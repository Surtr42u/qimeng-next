# 在原生恢复副本合并已校对的图片

先用转换器以独立 ID 迁入全部旧资产。保管完整的未合并 `.qnext`、独立 SHA-256、旧封存库、原件和私有覆盖报告。合并只接受所有者逐组明确选择的原生图片。每组指定一个代表 ID、实际目标文件名、每个成员的身份以及对应来源帖。当前内容必须在同一库、同一目录，大小、原生类型、当前版本与完整字节摘要须匹配。[旧 PC 整库迁入](legacy-cold.md) · [原生恢复](migration.md#完整归档与隔离恢复)

## 准备与执行

1. 在本机同一文件系统的私有父目录 `P` 工作。私有计划和归档保存在 `work`、`preimage`、`data` 三个子目录之外。以下各参数使用绝对路径。`P/data` 可以换成同父目录下尚不存在的其他名字。
2. 用正式 `qimeng-backup verify --archive ARCHIVE --sha256 DIGEST` 核对原始归档，再运行 `qimeng-backup restore --archive ARCHIVE --sha256 DIGEST --destination P/work`。把这次恢复的 stdout 保存为 UTF-8 `P/restore.json`。Windows PowerShell 5 可将输出交给 `Out-File -Encoding utf8`。`P/work` 必须是本次新恢复、从未启动服务的专用工作副本。
3. 在该副本完成原始 ID、实际相对路径、每件原件完整哈希及业务历史的基线核验。离线读取 SQLite 时用只读 immutable 连接，读取结束后关闭。保留该次基线明细。可将这时 `P/work/qimeng.sqlite` 的完整摘要作为 `--catalog-sha256` 传入，绑定连同历史和关系在内的已核数据库。对已校对的私有计划文件计算完整 SHA-256，以其原始字节的 64 位小写十六进制摘要作为 `PLAN_DIGEST`。
4. 运行以下命令核对整库和所有决定。`check` 只在临时 SQLite 副本上试算，并逐项读取归档和工作副本。成功后运行相同参数的 `apply`。两次调用各自读取完整输入，大归档可能需要较长时间。

```text
qimeng-merge-selected check --archive ARCHIVE --sha256 DIGEST --stage P/work --restore-receipt P/restore.json --plan PLAN --plan-sha256 PLAN_DIGEST --preimage P/preimage --destination P/data
qimeng-merge-selected apply --archive ARCHIVE --sha256 DIGEST --stage P/work --restore-receipt P/restore.json --plan PLAN --plan-sha256 PLAN_DIGEST --preimage P/preimage --destination P/data
```

私有计划是 UTF-8 JSON。顶层 `baselineSHA256` 使用完整未合并归档摘要，`sourceId` 使用转换报告中的来源标识，`groups` 为授权组数组。每组字段如下。

| 字段 | 含义 |
|---|---|
| `representativeId`、`libraryId`、`relativePath`、`sha256` | 明确保留的原资产 ID、库 ID、带实际文件名的目标相对路径，以及组内原件的共同完整摘要。目标可取同组成员原来占用的名字。 |
| `members` | 每项提供 `assetId`。可填写 `expectedRelativePath`、`expectedSize`、`expectedVersion` 固定人工核对时的路径、字节数和版本。`sourceKeys` 列出这份物理原件对应的来源键。 |
| `sources` | 每条记录提供组内唯一的 `key` 与原帖 `url`。有证据时填写 RFC 3339 格式 `at`、文字 `caption`、具体 `mediaUrl`。保留不同来源的各自记录。 |

`apply` 使用普通服务同款 SQLite 独占锁和 `RESTORE_INCOMPLETE` 标记认领工作副本。受管文件和控制路径逐层排查链接与 Windows 重解析点。所有原件与归档和当前版本逐一比对大小、摘要、修改时间，检测来源与目标占用变化。目标以同卷无覆盖改名发布。Linux 文件系统须支持 `RENAME_NOREPLACE`。默认在复制时保留 512 MiB 空间，可通过 `--reserve-bytes` 调整。Windows 刷新文件，并使用带 `WRITE_THROUGH` 的移动操作完成交接。

## 完成、回执与还原

`P/data` 是新普通服务的数据根。代表保留其物理资产 ID 与选定名字。其他成员保持各自独立的文件实体，使用原生虚拟路径 `library-trash/<libraryId>/<assetId>/<fileName>`，本机对应 `libraries/<libraryId>/.qimeng-internal/trash/<assetId>/<fileName>`。域内的专门关系记录合并决定、每个物理成员、原身份和多份证据。完整归档继续携带成员字节与这些关系。

`P/preimage/qimeng.sqlite` 是合组前的原生封存数据库。`catalog-fingerprint.json` 保存长度、修改时间、摘要和文件身份。`baseline-files.json` 保存完整的逐项原生清单和实测文件证据，`selected-files.json` 保存每个所选成员的原路径、新路径与独立副本名，`originals/` 中的每份副本均已回读核验。只有所选原件获得额外副本，未选媒体随整个恢复目录一次改名。归档、私有计划和预像含私人内容，须由所有者保管。

`apply` 的成功 JSON 回执给出实例、归档和计划摘要，以及已核验资产数、组数、成员数和 donor 数。用原参数重试已成功的交接时，命令从 `preimage/published.json` 与目标目录的 `SELECTED_IMAGE_MERGE.json` 核对并回放原回执。恢复已合并的原生归档时，使用它自己的 `--archive`、`--sha256` 和新还原回执，再提交原计划。只有该计划的全部决定已经有一致的原生记录，才能接受新的归档摘要。

| 停止位置 | 处理 |
|---|---|
| `check` 或 `apply` 在认领前停止 | 核对归档、恢复收据、私有计划指纹、空间和目标占用，然后在原样工作副本重新检查。 |
| 有 `ready.json`，交接期间中断 | 保留工作目录和预像，用原参数重试。工具核对私有交接标识与最终目录状态，再回读全部原件及数据库。第三方同名目标必须由所有者自行处置。 |
| 已有 `attempt.json`，尚无完整 `ready.json` | 工作副本仍属这次私有尝试。保留诊断和预像，先核验未合并归档，再从它恢复至新的专用工作目录。如果磁盘仅够一份工作库，所有者可先处置这次有标记的失败工作副本后重新恢复。 |
| 需要恢复所有原身份、位置和原始历史 | 使用完整未合并 `.qnext` 和独立摘要恢复至新的数据目录，可按逐项基线再核对。 |

工具与同提交的恢复程序由双平台合成工作流打包，下载后核对 `SHA256SUMS`。[Windows 与 Linux 工作流](../.github/workflows/selected-image-merge.yml)
