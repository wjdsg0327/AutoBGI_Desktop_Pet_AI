package desktop

import (
	"autobgi-pet/internal/notification"
	"github.com/wailsapp/wails/v3/pkg/application"
)

// layoutEntryBubble 在首次显示和工作区变化时重新计算布局，普通通知沿用原有的尺寸和摆放规则。
func (d *DesktopService) layoutEntryBubble(bubble *application.WebviewWindow, entry notification.Entry, screens []*application.Screen) {
	textHeight := bubbleHeightForKind(entry.Message, entry.Kind)
	if len(entry.Screenshot) == 0 && !entry.ScreenshotFailed {
		// 同一窗口也用于普通台词，离开截图详情时恢复紧凑尺寸。
		if width, height := bubble.Size(); width != 300 || height != textHeight {
			bubble.SetSize(300, textHeight)
		}
		d.placeBubbleOnScreens(bubble, screens)
		return
	}

	// 预览最多占 175 个逻辑像素，额外留出与详情之间的间隔；截图失败只需一行提示。
	height := textHeight + 30
	if len(entry.Screenshot) > 0 {
		height = textHeight + 185
	}
	const width = 380
	pet := d.pet.Bounds()
	_, _, screen := clampPosition(pet.X, pet.Y, pet.Width, pet.Height, monitorWorkAreas(screens))
	if screen == nil {
		// 显示器枚举刷新期间先设置内容尺寸，下一次有效的工作区快照会重新定位。
		bubble.SetSize(width, height)
		return
	}
	// 一次性设置目标屏的逻辑位置和尺寸，避免跨不同缩放比例的显示器时沿用旧屏大小。
	bubble.SetBounds(screenshotBubbleBounds(pet, screen.WorkArea, width, height))
}

// screenshotBubbleBounds 优先将详情放在桌宠上方，再尝试下方和两侧，最后限制在桌宠所属工作区内。
func screenshotBubbleBounds(pet, area application.Rect, width, height int) application.Rect {
	const gap = 4
	width, height = min(width, area.Width), min(height, area.Height)
	x, y := pet.X+(pet.Width-width)/2, pet.Y-height-gap
	if y < area.Y {
		// 上下都放不下时，横屏尽量用桌宠旁边的空间，保留完整截图和文字。
		y = pet.Y + pet.Height + gap
		if y+height > area.Y+area.Height {
			left, right := pet.X-gap-width, pet.X+pet.Width+gap
			switch {
			case right+width <= area.X+area.Width:
				x, y = right, pet.Y+(pet.Height-height)/2
			case left >= area.X:
				x, y = left, pet.Y+(pet.Height-height)/2
			case pet.Y-area.Y > area.Y+area.Height-(pet.Y+pet.Height):
				y = pet.Y - height - gap
			}
		}
	}
	// 角色可以部分拖出屏幕，详情始终整体留在有效工作区，包括负坐标副屏。
	return application.Rect{
		X: max(area.X, min(x, area.X+area.Width-width)), Y: max(area.Y, min(y, area.Y+area.Height-height)),
		Width: width, Height: height,
	}
}
