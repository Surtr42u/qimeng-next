# 已安装 Next 的后台迁移验收

此伴包匹配 embedded debug 基线 `407ed698528966e73793fa1e00808ee78eb1f3af`。目标应用须已有确认过的本机会话与共享主存储授权。将云端产物 `migration-acceptance.apk` 使用与目标 APK 相同的所有者签名签署。在已选设备上只安装这个 `media.qimeng.next.test` 伴包。云端合成准备程序有自己的源集，下载产物只声明 `media.qimeng.next.test/.MigrationAcceptance` 这一项 instrumentation。[云端打包和 Android 35 实验](../../.github/workflows/mobile-migration-check.yml)

在每次调用前确认 `adb -s DEVICE shell pidof -s media.qimeng.next` 返回主进程。若进程已回收，以下定向广播可启动目标 Application。AndroidX 接收器看到这个操作值就返回，宿主不打开 Activity。用实际选定的 USB 设备代替 `DEVICE`。[AndroidX 接收器](https://android.googlesource.com/platform/frameworks/support/+/520af3dd047dd8fd631529efcf79714f971ca97f/profileinstaller/profileinstaller/src/main/java/androidx/profileinstaller/ProfileInstallReceiver.java)

```shell
adb -s DEVICE shell am broadcast --user current --include-stopped-packages -a androidx.profileinstaller.action.SKIP_FILE --es EXTRA_SKIP_FILE_OPERATION MIGRATION_NO_OP -n media.qimeng.next/androidx.profileinstaller.ProfileInstallReceiver
adb -s DEVICE shell pidof -s media.qimeng.next
```

`--no-restart` 将验收器附到已有主进程，避免常规 instrumentation 在测试起止时强停应用。`quiesce` 先确认没有可并发启动服务的待执行扫描，再在应用私有偏好里记一次原先的启停布尔值，等待正式停止接口完成。重复调用沿用第一次的记录。设备端迁移工具只在 `ok=true`、`stopped=true` 和结果码 `-1` 后开始交接。[Android 15 ActivityManager](https://android.googlesource.com/platform/frameworks/base/+/refs/heads/android15-release/services/core/java/com/android/server/am/ActivityManagerService.java)

```shell
adb -s DEVICE shell am instrument --user current --no-restart --always-check-signature -w -r -e command quiesce media.qimeng.next.test/.MigrationAcceptance
```

所有者把 UTF-8 核对清单临时放在目标应用自己的 `cache/qimeng-migration-check.json`。以下是假定实例和原件的格式示例。选择实际目录位于已授权共享主存储的 1–3 个可见代表资产，每件上限 64 MiB。实例 ID、当前资产 ID、字节数和完整小写 SHA-256 为必填。填写 `authorIds`、`tags` 时按完整集合核对，空数组可验证确实没有该关系。`counts` 支持产品 `/stats` 的 `assets`、`authors`、`events`、`images`、`videos`、`favorites`、`bytes` 和 `trash`，只比较清单提供的字段。

```json
{
  "instanceId": "synthetic-instance",
  "counts": {"assets": 1, "authors": 1, "events": 0},
  "originals": [{
    "id": "synthetic-asset",
    "bytes": 100,
    "sha256": "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
    "authorIds": ["synthetic-author"],
    "tags": ["synthetic-tag"]
  }]
}
```

```shell
adb -s DEVICE shell am instrument --user current --no-restart --always-check-signature -w -r -e command accept media.qimeng.next.test/.MigrationAcceptance
```

`accept` 检查已落盘目录库、App 运行上下文的权限、受信启动回执和普通会话。它从正式 Repository 读实例、计数、作者、资产标签与库位置，再通过同一认证客户端读取带当前内容版本的完整原件，比较大小、强 ETag 及实际流的 SHA-256。原件读前读后各取一次事件总数。结果仅含计数与布尔值。成功须同时看到结果码 `-1`、`ok=true`、`originalsVerified` 等于清单件数、`eventsUnchanged=true` 和 `lifecycleRestored=true`。

本机服务按产品设计显示系统前台服务通知，并在新启动时更新临时 TLS 证书。应用协调器按原选择刷新普通会话，已有工作队列及扫描会遵循原有恢复逻辑。原先启用的宿主在完成后继续运行，原先停用的宿主在核验后停止。验收失败保留原启停记录，修正数据或清单后可重试。所有者检查完回执后移除私有临时清单。
