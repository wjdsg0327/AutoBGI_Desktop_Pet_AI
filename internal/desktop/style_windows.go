//go:build windows

package desktop

// windowExNoActivate 与 Win32 的 WS_EX_NOACTIVATE 对齐，样式查询与窗口创建使用同一标记。
const windowExNoActivate = 0x08000000

// petExStyle 显式保留透明组合、工具窗与不激活样式，不能只使用 CSS 禁止点击。
func petExStyle(top bool) int {
	style := windowExNoActivate | 0x00000080 | 0x00200000
	if top {
		style |= 0x00000008
	}
	return style
}
