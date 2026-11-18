# Android 模块门禁

GitHub Actions 在生成 OpenAPI Kotlin SDK 后运行以下命令。相同 Gradle 调用可以继续附加已有的 JVM、Lint 任务。门禁所需的 `:app:assembleClientDebug` 会加入同一任务图。

```sh
gradle --no-daemon -p android -Pqimeng.embedded=false -I android/architecture/verify.gradle verifyClientArchitecture
```

| 边界 | 执行检查 |
| --- | --- |
| 独立客户端 | Gradle settings 排除 `:embedded`。检查客户端 debug、release 的实际编译与运行依赖，校验每个项目依赖方向。应用经 `:runtime-api` 获得宿主能力，客户端生产源集与静态资源根均须与 embedded 实现分离。最后打开实际 debug APK 检查四个本机程序。 |
| 公共模型和界面 | 对 AGP 注册的生产 Java/Kotlin 输入和 JVM main 源集检查直接引用，包括各 flavor 和生成源码。SDK 手写调用归 `:core:data`，`:sdk` 只容纳从 OpenAPI 生成的实现。可复用 UI 仅依赖 `:core:model`。页面及 Compose 组件经 ViewModel 获取业务状态和操作，直接引用仓储、Retrofit、SQLite、Room、DataStore 或发 HTTP 业务请求会令门禁失败。 |
| 设备能力 | 系统选文件、SAF 文档标识、系统分享、图片展示以及 Media3/Coil 读取经授权媒体字节都属于平台接口。Android 归档暂存、可恢复传输和本地授权记录位于数据适配层。 |
| 服务决策 | `RecoveryBoundaryTest.libraryReadsPreserveTheServersOrderScoresCursorsAndNames` 让实际 Repository 经生成 SDK 接收两条有意打乱名称顺序的推荐和排名，核对返回次序、分数及不透明翻页游标。命名检查提交原名，并原样接收远端给出的示例、下一个工作编号和完整目标路径。云联网与内嵌仪器另外连接真正的同源 Go 服务，验证媒体入库、实例身份和回读。 |

Gradle 将项目、逐变体依赖边和实际源目录写入 `android/build/reports/architecture/gradle-inputs.json`，路径均相对于仓库。源码异常给出相对文件及行，依赖异常给出配置、来源和目标项目。设备行为断言随 `:core:data:connectedDebugAndroidTest` 在指定 `qimeng_api35` 云模拟器运行。新增 UI 数据访问或 Gradle 模块时应先核对这里的边界及相应 Go 用例。业务公式本身由 Go 领域测试验证。
