# Wails 本地源码

本目录基于 Wails v3.0.0，保留本项目依赖的源码和上游 MIT 许可证。`go.mod` 使用此目录，以便在 Windows 窗口层修复桌宠显示时的焦点问题。

本地修改位于 `pkg/application/webview_window_windows.go`：无边框窗口初始化时不激活窗口；显示带有 `WS_EX_NOACTIVATE` 的窗口时使用 `SW_SHOWNOACTIVATE`。其他窗口继续使用原来的显示方式，WebView 的可见状态仍由 Wails 管理。升级 Wails 时需要重新检查并合并这两处修改。
