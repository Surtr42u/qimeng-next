# Web / PWA

React + TypeScript + Vite，服务端同源托管；TanStack Query 负责缓存、取消与失效，React Router 持有导航。业务代码重新编写，旧版表现层按用户于2026-11-17的明确授权复用，见 [复用来源](REUSED_UI.md)。当前源码与视觉纠偏/验证缺口见 [UI 对照](../docs/ui.md)；构建成功不等同于界面对齐、真实库切换或设备验收。

## 数据与状态

`src/api/generated/schema.d.ts` 只在云端从 `api/openapi.yaml` 生成。`openapi-fetch` 直接消费生成的 paths 类型；请求只在 `src/data`。页面经 hooks 获取数据，纯组件不导入运行时 SDK、不直接请求接口。`npm test` 的 AST 门禁覆盖页面/组件和独立视觉包。

Web 使用同源 HttpOnly 设备会话，管理密码只用于交换，不放入本地存储、URL 或媒体链接；响应中的设备令牌不留在Query缓存。媒体原件与缩略图通过同源 Cookie，视频原生支持 Range。实例 ID 与设备会话 ID 是查询缓存与队列作用域的一部分；实例改变时取消并清除旧查询，切服不复用旧状态。

查询失败不当空列表；有缓存时标记上次内容，游标续页失败保留已读项。搜索词与筛选进入 URL。详情标签使用 `tagsRevision`，作者关系使用 `authorsRevision`，冲突保留草稿。多作者不压缩为单作者。点赞消费今日状态与累计次数，不把再次点击解释为减赞。

详情恢复旧双栏、文件名复制、点赞/收藏/编辑/整理/删除及标签弹窗。列表进入时携带顺序快照，图片查看器使用原视觉坐标手势，连续换件保留已显示帧并明确加载态，混合图片/视频保留查看意图；背景inert与退出焦点均独立处理。播放器恢复ArtPlayer原控件，只把当前contentVersion的进度和标记交给播放器；时间轴在原进度条呈现，不额外添加旧Web没有的大面板。外部字幕和标记标签只写安全DOM文本。

上传选入即开始，普通模式默认且独立于 AI。完整选择先以稳定 inputId 原子保存到本地队列与批次准备记录，再在服务端封存原始输入清单；确认前不创建任何上传会话。超过100项在登记前明确拒绝，不截断选择。建批或封存回执丢失沿用原操作键及完整清单，刷新重选一部分不会缩减原始总数；未收到、名称不可读和已取消条目也留在服务器报告。批次、创建与完成各有稳定操作键；重新传输先回读服务端 offset/终态。文件在 Worker 以 4 MiB 有界读块计算完整 SHA-256，创建前以 `expectedHash` 固定到服务端会话；刷新重选核对名称/大小/mtime和完整内容。队列只保存任务元数据，不把整段视频装入内存或复制入 IndexedDB。两个传输并行且不超过服务端限制，暂停中止本地传输，取消先核对服务端已提交事实。存储退化可见，重试存储不重发入库。浏览器关闭不会继续传输，已入库/智能报告保存在服务端。替换原件从详情单独进入，明确目标与内容版本并确认，复用同一传输链。

浏览事件以稳定详情会话和事件ID持久化，15秒可见段及隐藏/离开时提交整秒停留；隐藏期间不计时。离线与本地存储退化在壳层提示，恢复连接按原身份补发。队列上限2000条，确认后只失效相关统计/历史/榜单。硬关闭或浏览器清理存储仍可能丢失未落盘尾段；从未把这些平台边界写成永久恰好一次保证。

PWA worker 只缓存公共应用壳与哈希静态资源，API/媒体明确排除。离线显示失败或已有内存内容，不伪报写入成功，不承诺离线媒体库。PWA 安装与 Service Worker 需要 HTTPS 或浏览器信任的本机上下文；HTTP NAS 地址仍可普通浏览，不承诺可安装。没有 push/通知授权，也没有隐藏的自动上传。

上传工作台恢复原有「上传位置」「作者与来源」「添加文件」与全宽队列，目标库需手动选择。未确认的联想词和服务端暂停接收会阻止入队，折叠后仍保留指定作者与渠道。入队冻结作者身份及追加渠道；渠道由 Go 随实际入库处理。详情编辑的常规作者与待新建名使用同一资产修订和操作键保存，COS 固定关系由服务器保留。真实服务回读与两种屏宽截图由对应 CI 验证。

## 云端命令

以下命令只允许 CI 执行：

```sh
# 首次依赖准备，由 CI 生成并归档 lock；之后使用 npm ci
npm install --package-lock-only --ignore-scripts
npm ci
npm run generate:api
npm run build
npm test
npx playwright install --with-deps chromium webkit
npm run test:e2e
npm audit --audit-level=high
```

Node 24；命令工作目录为 `web`。锁文件来源与 SDK 摘要由根 CI 管理，禁止本机安装、编译、生成、测试或启动应用服务。浏览器mock项目阻止Service Worker以保证全部合成API可被拦截，不访问真实服务；真实live项目仍运行PWA worker。截图保存在Playwright测试产物中。后端真实集成仍需单独 CI 契约场景。

另有 `npm run test:live` 真实服务闭环：仅在 `CI=true`、`QIMENG_E2E_BASE_URL=http://127.0.0.1:18481` 且提供一次性 `QIMENG_E2E_TOKEN` 时运行。CI先启动同提交Go服务并托管同提交`web/dist`，PATH需ffmpeg/ffprobe，并通过`QIMENG_E2E_LIBRARY_ROOT`提供已加入服务受信storage配置的云端临时根。测试只在该根创建每次独立子目录，从原服务器根路径文本框注册；路径不放查询串或日志。测试在云端生成合成PNG与短视频，实际登录、建库、上传、编辑、推荐回读、播放和时间轴持久化；该项目不使用route mock，不连接8420或任何既有库。执行结果仍以根状态页中的提交SHA与CI日志为准。

## 依赖来源与固定版本

2026-11-17 通过官方 npm registry 实际读取精确版本和 peerDependencies。首次云端准备发现 openapi-typescript 只支持 TypeScript 5.x，已使用 5.9.3；不以 force 或 legacy-peer-deps 绕过。

| 用途 | 精确版本 | 官方依据 |
|---|---|---|
| UI | React / React DOM 19.3.1 | [React 版本](https://react.dev/versions) |
| 类型 | TypeScript 5.9.3，React types 19.3.0 | [TypeScript registry](https://registry.npmjs.org/typescript/5.9.3) |
| 构建 | Vite 8.3.8，plugin-react 6.3.0 | [Vite 官方指南](https://vite.dev/guide/) |
| 查询与路由 | TanStack Query 5.107.1，React Router DOM 7.18.5 | [Query取消](https://tanstack.com/query/latest/docs/framework/react/guides/query-cancellation)、[Router](https://reactrouter.com/) |
| 协议 | openapi-typescript 7.13.0，openapi-fetch 0.17.0 | [生成器](https://openapi-ts.dev/introduction)、[类型客户端](https://openapi-ts.dev/openapi-fetch/) |
| 文件核验 | @noble/hashes 2.4.0 | [上游流式 SHA256](https://github.com/paulmillr/noble-hashes) |
| 自动验证 | Vitest 5.1.0，Playwright 1.65.1 | [Vitest](https://vitest.dev/)、[Playwright](https://playwright.dev/docs/intro) |
| 旧UI表现层 | Geist 5.3.0、lucide-react 1.59.0、radix-ui 1.6.7、Recharts 3.10.1、react-is 19.3.1、ArtPlayer 5.4.0 | [复用来源、官方元数据与许可证](REUSED_UI.md) |

直接依赖的精确版本不能代替传递依赖 lock 与安全审计。当前云端实际结果由根 `docs/status.md` 记录。
