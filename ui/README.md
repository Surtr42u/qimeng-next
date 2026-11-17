# 独立视觉组件

本目录的交互组件从空白编写，只依赖 React；没有 SDK、业务模型、路由或 Android 实现。视觉 token 按用户2026-11-17的明确授权直接复用旧版，来源见 [复用记录](../web/REUSED_UI.md)，以保持原外观。旧业务实现不导入。

## 消费契约

| 组件 | 输入 / 行为 | 宿主职责 |
|---|---|---|
| `GlassNavigation` | `items` 含稳定 ID、文字、href、图标；`activeId` 由宿主路由决定；普通点击调用 `onNavigate`，修饰键保持真实链接行为 | 保留原导航含义；提供有效路由；在安全区内定位外层容器 |
| `SegmentedControl` | 受控值与稳定候选；左右 / Home / End 操作和选中语义 | 数据加载、过滤、URL同步；不改变候选顺序 |
| `RouteSurface` | 同一实例接收路由键；每次取消前一动画，只渲染一个实时页面 | 提供页面内容、焦点和滚动恢复，不在过渡结束回调提交业务 |
| `GlassOptics` | 一次挂载的 SVG 位移滤镜 | 与导航处于同一文档；不能插入截图或复制媒体作为背景 |
| `SelectionLens` | 宿主ref、选中元素selector与selectionKey；只测量并移动背景透镜 | 在既有导航根上加qm-selection-host，保留原尺寸/布局/标签；当前Web侧栏和顶栏采用此组件 |

Next 导入 `react/tokens.css` 与 `react/glass.css`；`.dark` 使用旧深色角色，其他情况为旧浅色角色，宿主同时维护 data-theme。新组件所需变量由适配层映射到旧 token。当前Web不渲染新增底栏，保留原四项侧栏与顶栏，获准效果不得改变导航形式与内容。

回迁旧项目时，不直接覆盖旧根 token 文件。先在适配层映射本组件使用的 `--qm-bg/text/muted/accent/accent-soft/glass/surface/border/highlight` 等角色，导入组件样式，提供原路由与状态。候选、返回行为、业务 hooks、事件身份和服务调用仍归各自应用。此目录没有对旧项目做任何写入。

## 材质与运动

底层实际使用 `backdrop-filter` 模糊、饱和及半透明表面，背景透过现有导航表面。SVG `feDisplacementMap` 在浏览器能组合 SVG backdrop filter 时对真实背景像素产生位移；选中透镜由实际测得的元素位置驱动，响应容器变化。高光边缘、内阴影和选中图标比例形成厚度与选择反馈；按压缩放在松开后连续恢复。

这不是跨浏览器保证的光学模拟。SVG backdrop filter 组合的支持度、渲染和性能需单独验收；不支持时保留 CSS 模糊/透射和选择透镜。完全不支持 backdrop-filter 时回落同尺寸不透明面板。读屏/键盘选中不依赖材质；高对比模式保留描边。

转场使用原生 Web Animations API，没有离场页面克隆。快速切换会取消旧动画，不叠放多个页面；背景始终由画布覆盖。减少动效时不启动页面位移，CSS选中/按压也去掉动画。组件不内置任意延迟，不等待动画完成才导航。

## 跨端规范

Android 对齐导航内容、触摸区域、选中语义、动效中断与材料降级规则；由 Compose 原生实现，不调用这些 DOM 组件。桌面连接壳加载同一 Web 内容，系统标题栏负责窗口操作；Web 不获得原生 IPC 权限。

官方依据：[backdrop-filter](https://developer.mozilla.org/en-US/docs/Web/CSS/Reference/Properties/backdrop-filter)、[Web Animations](https://developer.mozilla.org/en-US/docs/Web/API/Web_Animations_API)。自动浏览器用例见 `web/e2e`；图像质量、手机手势和桌面窗口最终以同提交产物的验收为准。
