// Package desktop 实现独立原生窗口、恢复入口和显示器工作区约束。
package desktop

import "github.com/wailsapp/wails/v3/pkg/application"

// clampToDesktop 逐屏寻找最近的有效位置，避免把显示器间隙当成可放置桌宠的工作区。
func clampToDesktop(x, y, width, height int, screens []*application.Screen) (int, int, bool) {
	if len(screens) == 0 {
		return 0, 0, false
	}
	bestX, bestY := x, y
	bestDistance := int64(-1)
	for _, screen := range screens {
		area := screen.WorkArea
		// 完整窗口可以在相邻屏之间过渡，但至少留出 40 逻辑像素的可交互区域。
		reachableWidth, reachableHeight := min(width, 40), min(height, 40)
		cx := max(area.X-width+reachableWidth, min(x, area.X+area.Width-reachableWidth))
		cy := max(area.Y, min(y, area.Y+area.Height-reachableHeight))
		dx, dy := int64(cx)-int64(x), int64(cy)-int64(y)
		distance := dx*dx + dy*dy
		if bestDistance < 0 || distance < bestDistance {
			bestX, bestY, bestDistance = cx, cy, distance
		}
	}
	return bestX, bestY, true
}

// clampPosition 按每个工作区单独判断，避免把两屏之间的不可见空隙当成可用区域。
func clampPosition(x, y, width, height int, screens []*application.Screen) (int, int, *application.Screen) {
	var chosen *application.Screen
	best := 0
	for _, screen := range screens {
		area := screen.WorkArea
		overlap := max(0, min(x+width, area.X+area.Width)-max(x, area.X)) * max(0, min(y+height, area.Y+area.Height)-max(y, area.Y))
		if overlap > best {
			chosen, best = screen, overlap
		}
	}
	if chosen == nil {
		for _, screen := range screens {
			if screen.IsPrimary {
				chosen = screen
				break
			}
		}
		if chosen == nil && len(screens) > 0 {
			chosen = screens[0]
		}
	}
	if chosen == nil {
		return x, y, nil
	}
	a := chosen.WorkArea
	return max(a.X, min(x, a.X+max(0, a.Width-width))), max(a.Y, min(y, a.Y+max(0, a.Height-height))), chosen
}
