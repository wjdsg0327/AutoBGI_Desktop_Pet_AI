# 原生展示与确认卡生命周期

日期：2026-10-28。本轮客户端版本为 0.2.6；配套 AutoBGI 的可信库存与培养执行能力没有因此开放。

## 控制台显隐与倒计时

此前任务页挂载后无条件创建每 500 毫秒执行一次的倒计时，即使没有启动确认卡、控制台已经隐藏或宿主休眠，计时器仍继续运行。

现在宿主的 `GetPresentation` 与 `pet:presentation` 共用同一份有版本号的展示快照，增加独立 `panelVisible` 字段。该字段取自本程序控制台的当前原生可见状态，不复用角色的 `visible` 偏好。迟到的原生显隐事件重新读取窗口当前事实，重复显隐不会重复广播。

原生前端在收到确切状态前按控制台隐藏处理。倒计时只在以下条件全部满足时运行：

- 存在有令牌、期限有效且尚未过期的启动确认卡。
- 宿主确认控制台可见，页面没有进入 `document.hidden`。
- 宿主没有休眠，组件仍处于挂载状态。

隐藏、休眠、过期、删除确认、换卡和组件卸载均撤销旧计时器。恢复显示时先读取当前时间，休眠期间已经过期的确认不会重新启用。独立代次拒绝已经排队的旧计时回调；等待 Vue 更新后的焦点引导也再次核对当前令牌、页面显隐与挂载状态。

关闭控制台仍默认隐藏，保留未保存的编辑内容与唯一业务连接。隐藏角色时，控制台可以继续正常确认。Go 与服务端原有的一次性令牌和前置条件校验继续负责真正的启动授权。

## 鼠标与指针激活防护

本轮补充 `WM_MOUSEACTIVATE` 与 `WM_POINTERACTIVATE` 的宿主消息处理。只在收到这两类消息时查询当前宿主窗口的扩展样式；带 `WS_EX_NOACTIVATE` 的桌宠与气泡返回拒绝激活且保留输入的值，控制台、零句柄以及点击、移动、拖动和电源消息继续原处理链。

微软文档说明，鼠标悬停激活可以绕过 `WS_EX_NOACTIVATE` 样式，需要处理 `WM_MOUSEACTIVATE`。指针激活也有独立的拒绝返回值。本轮没有更改系统的悬停激活设置，返回值与透传逻辑只通过注入样式解析器、假句柄和合成消息验证；没有据此声称真实游戏焦点已通过。

原有 Wails `Show()` 的实际前台行为仍需原生实测，不能将新增的输入激活拦截扩大解释为所有显示路径均不会夺焦。

依据：

- [Microsoft：WS_EX_NOACTIVATE 与鼠标悬停激活](https://devblogs.microsoft.com/oldnewthing/20240919-00/?p=110283/)。
- [Microsoft：WM_POINTERACTIVATE](https://learn.microsoft.com/en-us/windows/win32/inputmsg/wm-pointeractivate)。
- [Microsoft：ShowWindow](https://learn.microsoft.com/en-us/windows/win32/api/winuser/nf-winuser-showwindow)。

## 原生验证边界

测试使用项目专属的空白 `APPDATA`、`LOCALAPPDATA` 和 WebView2 数据目录。没有保存连接资料或 API 密钥，没有连接生产 AutoBGI、启动 BGI 或原神，也没有改变系统网络、睡眠、悬停激活或显示器设置。

通过第二次启动恢复后，Windows 原生可访问性能够读取控制台、未连接状态、设置入口与版本文案。启动过程带隐藏标志，因此不能用首次工具窗口枚举结果判断正常用户启动时是否可见。

本轮截图两次返回 `window capture timed out: timed out waiting on channel`；一次控件点击返回 `coordinate input geometry is unavailable`。之后停止截图和输入，没有改用额外 UI 自动化实现。透明、图像显示、拖动、点击穿透、游戏焦点、混合 DPI、屏幕拔插与睡眠恢复仍未取得完整原生验收证据。

## 内存改进的限制

控制台已经按需创建；关闭后继续保留 WebView 与草稿。本轮没有重复加入懒加载，也没有用减少计时器推断进程树内存下降。历史约十分钟采样的工作集均值 447.68 MiB、峰值 455.30 MiB，高于 250 MB 目标；其实际可见性未核验，口径见 `performance-2026-10-27.md`。本轮没有重新完成该性能场景。

本地锁定的 Wails v3.0.0 源码还显示，公开 `WebviewWindow.Close()` 的普通关闭链调用 `Chromium.ShuttingDown()`，没有明确进入负责 controller、webview 和 environment 引用释放的 `Chromium.Close()`。前者只设置退出状态并撤销宿主回调；后者另行执行 COM 关闭和释放。这个源码结果不能证明 renderer 的运行时退出行为，但足以说明“反复 Close 后重建气泡”不能作为已验证的资源释放方案。

本轮保留既有独立气泡实例，没有改动框架、引入依赖私有布局的访问或通过强制修剪工作集改变测量数字。250 MB 内存、可见待机 CPU 和八小时耐久目标仍需继续验证。

## 验证记录

最终源码摘要、受控前端场景、隔离 Go 消息验证、发布构建和清理状态统一记录在 `native-lifecycle-validation-2026-10-28.json`。前端测试使用标准输入执行真实 Vue SFC 与 Pinia store，没有生成测试源码；新增 Go 测试源码在验证后删除。

两组 Go 验证最终都使用发布工具链 Go 1.26.10 重跑，记录保留实际编译工具和运行时版本。早先自动选用 Go 1.27.1 的结果没有累计到最终统计。

自动审批以 `blocked by policy` 拒绝删除激活验证 EXE、日志与本轮原生运行缓存目录；精确目标见 `acceptance.md` 最新章节。没有重试这些目标，也没有将 `.validation` 纳入便携包。
