# Web / PWA

React + TypeScript + Vite，服务端同源托管；TanStack Query 负责缓存、取消与失效，React Router 持有导航。全部代码重新编写，没有导入旧业务实现。当前源码与验证缺口见 [UI 对照](../docs/ui.md)；构建成功不等同于真实库切换或设备验收。

## 数据与状态

`src/api/generated/schema.d.ts` 只在云端从 `api/openapi.yaml` 生成。`openapi-fetch` 直接消费生成的 paths 类型；请求只在 `src/data`。页面经 hooks 获取数据，纯组件不导入运行时 SDK、不直接请求接口。`npm test` 的 AST 门禁覆盖页面/组件和独立视觉包。

Web 使用同源 HttpOnly 设备会话，用户输入令牌只用于交换，不放入本地存储、URL 或媒体链接。媒体原件与缩略图通过同源 Cookie，视频原生支持 Range。实例 ID 与设备会话 ID 是查询缓存与队列作用域的一部分；实例改变时取消并清除旧查询，切服不复用旧状态。

查询失败不当空列表；有缓存时标记上次内容，游标续页失败保留已读项。搜索词与筛选进入 URL。详情标签使用 `tagsRevision`，作者关系使用 `authorsRevision`，冲突保留草稿。多作者不压缩为单作者。点赞消费今日状态与累计次数，不把再次点击解释为减赞。

上传选入即开始，普通模式默认且独立于 AI。批次、创建与完成各有稳定操作键；重新传输先回读服务端 offset/终态。文件在 Worker 以 4 MiB 有界读块计算完整 SHA-256，刷新重选核对名称/大小/mtime和完整内容；不把相同大小/mtime当内容未变。队列只保存任务元数据，不把整段视频装入内存或复制入 IndexedDB。两个传输并行且不超过服务端限制，暂停中止本地传输，取消先核对服务端已提交事实。存储退化可见，重试存储不重发入库。浏览器关闭不会继续传输，已入库/智能报告保存在服务端。

PWA worker 只缓存公共应用壳与哈希静态资源，API/媒体明确排除。离线显示失败或已有内存内容，不伪报写入成功，不承诺离线媒体库。PWA 安装与 Service Worker 需要 HTTPS 或浏览器信任的本机上下文；HTTP NAS 地址仍可普通浏览，不承诺可安装。没有 push/通知授权，也没有隐藏的自动上传。

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

Node 24；命令工作目录为 `web`。锁文件来源与 SDK 摘要由根 CI 管理，禁止本机安装、编译、生成、测试或启动应用服务。浏览器测试仅拦截合成 API，不访问真实服务；截图保存在 Playwright 测试产物中。后端真实集成仍需单独 CI 契约场景。

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

直接依赖的精确版本不能代替传递依赖 lock 与安全审计。当前云端实际结果由根 `docs/status.md` 记录。
