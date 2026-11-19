# 在空的手机 Next 实例绑定现有原件

手机已授予内嵌服务共享存储权限，逻辑存储名为 `phone`。现装 Next 已建立实例与登录凭据，数据库迁移至 0045。此时可以在隔离 PC 上保留一个已核验归档里的资产 ID、版本、历史与关联，并让手机继续使用共享存储中的每个物理文件。PC 输出约一份数据库大小；准备过程中还需要几份临时 SQLite 的空间。完整归档、原始还原目录、手机原控制库及其外部凭据由所有者单独保管。

## 封存输入

1. 用正式工具核对整份原生归档的独立 SHA-256，并恢复到全新私有目录。使用已验收且从未启动服务的这份 `qimeng.sqlite`，保存恢复时 stdout 的 UTF-8 JSON 收据和数据库 SHA-256。旧版 v26 资料先按整库转换工具说明制作原生归档。
2. 停止目标内嵌服务，并保持手机资料冻结。取得目标 SQLite 的一致单文件副本及其 SHA-256。输入数据库旁出现 `-wal`、`-shm` 或 `-journal` 时，应重新取得完整的一致副本。直接丢弃这些文件可能丢失已提交的修改。
3. 所有者将每个来源库分配到受信 `phone` 存储内的一个独立、互不嵌套的相对目录，对每个当前原件明确指定一次源资产与手机实物的对应。库内改名须保持相同扩展名。每个已回收的原件还须有一件独立实物，目标为该库根的 `.qimeng-internal/trash/<资产ID>/<原文件名>`，以便现装服务按原来的还原路径恢复。需要从隔离归档补入少量回收实物时，使用下述指定方式。
4. 从已授权的同一台手机独立采集物理清单。每条记录均须从该 `phone` 根逐层核对路径，拒绝全部链接和重解析点，打开普通文件并对完整字节取 SHA-256。哈希前后核对打开文件的设备号、inode、长度、mtime、ctime，并确认命名路径仍指向同一对象。保留采集时钟的精度与原值。设备号与 inode 使用十进制字符串，两个时间字段是 Unix 纪元起算的整数纳秒。清单只列本次绑定的实物，保管另存的现场清点记录。PC 以这份冻结清单作为对手机实物的见证。
5. 分别对已批准的计划和采集清单原始字节取 SHA-256。各输入与输出使用本机私有、无链接的规范绝对路径。输出是恢复数据目录以外、尚不存在的新目录。

计划的路径均使用 `/`。以下是虚构的一件资产。

```json
{
  "format": "qimeng-phone-relink-v1",
  "libraries": [{"libraryId":"example-library","rootRelativePath":"Pictures/Example"}],
  "bindings": [{
    "assetId":"example-asset","libraryId":"example-library",
    "sourceRelativePath":"album/first.png","phoneRelativePath":"Pictures/Example/album/first.png"
  }]
}
```

与这项计划独立采集的清单格式如下。示例数值仅说明字段表示。

```json
{
  "format":"qimeng-phone-files-v1","storageId":"phone",
  "files":[{
    "phoneRelativePath":"Pictures/Example/album/first.png","size":128,
    "sha256":"0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
    "device":"100","inode":"2001","mtimeUnixNs":1700000000000000000,"ctimeUnixNs":1700000000000000000
  }]
}
```

## 生成与复核

若少量原生回收实物需要补入，先在冻结手机上对其目标路径逐层排除链接，确认最终文件确实缺失。计划的 `bindings` 仍列出每个回收资产，同时在可选数组 `restoreTrashAssetIds` 中列出本次批准复制的每个回收资产 ID。采集清单把这些已确认缺失的目标路径放进可选数组 `absentTrashPaths`，已有文件仍放在 `files`。在命令末尾追加 `--trash-budget-bytes N`，其中 N 为批准的总字节上限。命令从已还原目录按库、资产 ID、原文件名找到各自独立的回收实物，逐件核对大小、SHA-256，拷贝后再次读取校验。补入操作只适用于原生库内回收布局 2。

将以下占位符替换为本次冻结的输入、对应的 64 位小写十六进制 SHA-256，以及私有目标目录。

```text
qimeng-backup phone-relink --archive ARCHIVE --sha256 ARCHIVE_SHA --restored-db RESTORED_SQLITE --restored-sha256 RESTORED_SHA --restore-receipt RESTORE_JSON --target-db PHONE_COLD_SQLITE --target-sha256 PHONE_COLD_SHA --plan APPROVED_PLAN --plan-sha256 PLAN_SHA --inventory PHONE_CENSUS --inventory-sha256 CENSUS_SHA --destination NEW_DIRECTORY
```

程序重新读取原生归档里的每个字节，只提取临时 SQLite。正式恢复策略撤销源会话、历史租约及原存储位置，然后按恢复收据中的实例身份重演。各持久表要与提供的恢复库逐项一致。目标库中允许正常初始化的占位作者、索引、计数和目标自己的独立凭据与已知设置。目标已有的设置与源库同键异值、存在活动/任务或其他业务行时会中止。当前版本另会中止尚有文件的未完成上传、归档中正在执行的设备任务或研究请求，以及无法在目标按原生布局验证的回收实物。

成功目录含 `qimeng.sqlite` 和 `phone-relink.json`，后者记录原生归档、已还原源库、目标冷库、计划、物理清单及成品数据库各自的摘要，以及源、隔离恢复和目标三个实例身份。原件在手机原地读取。每份字节相同、inode 不同的原件保持各自的资产和版本。缺项、重复路径或 inode、跨库、摘要冲突都会在发布前中止。重复运行会拒绝已存在的目标。进程被强制终止或提示 `private_scratch_cleanup_required` 时，私有父目录下可能留有 `.qimeng-phone-relink-*` 临时副本，其中含私有数据库。请保留现场并核对归属；若输出目录已经出现，先核对完整回执和成品 SHA-256。

部署人员需再次核对手机仍为原目标实例、控制库与冻结副本相符，并复核授予的共享根与每件实物。保持服务完全停止，保留完整可回退的目标旧控制状态。若回执有 `stagedTrash`，输出另含 `phone-files/<phoneRelativePath>`；对每项仅向已确认缺失的位置以独占新建方式写入，回读核对 SHA-256 和大小，并按回执恢复 mtime。所有必需实物均已就绪后安装校验过的新 `qimeng.sqlite`，保留手机现有的私有凭据、宿主配置、权限及登录存储。正常启动后用原手机会话回读实例 ID、媒体内容、收藏和事件，并在有回收项时验证其可回读或重新归档。工具包中的 `phone-relink-cloud-receipt.json` 记录该平台合成验收中实际执行的检查。
