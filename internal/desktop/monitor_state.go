package desktop

import (
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// monitorTracker 按值保存运行期屏幕锚点，由宿主锁保护，不读取位置文件或调用原生窗口。
type monitorTracker struct {
	monitorID string
	workArea  application.Rect
	relativeX int
	relativeY int
}

// monitorWorkAreas 忽略枚举中的临时空项和无效工作区，保留宿主提供的稳定屏幕顺序。
func monitorWorkAreas(screens []*application.Screen) []*application.Screen {
	usable := make([]*application.Screen, 0, len(screens))
	for _, screen := range screens {
		if screen != nil && screen.ID != "" && screen.WorkArea.Width > 0 && screen.WorkArea.Height > 0 {
			usable = append(usable, screen)
		}
	}
	return usable
}

// monitorPresent 按标识判断屏幕是否仍在，不使用名称、坐标或列表序号冒充屏幕身份。
func monitorPresent(id string, screens []*application.Screen) bool {
	for _, screen := range screens {
		if screen != nil && screen.ID == id {
			return true
		}
	}
	return false
}

// monitorLayoutSignature 保留无效工作区与缩放位值并忽略枚举顺序，防止拔屏或仅 DPI 改变被吞掉。
func monitorLayoutSignature(screens []*application.Screen) string {
	parts := make([]string, 0, len(screens))
	for _, screen := range screens {
		if screen != nil {
			parts = append(parts, fmt.Sprintf("%q:%v:%t:%08x", screen.ID, screen.WorkArea, screen.IsPrimary, math.Float32bits(screen.ScaleFactor)))
		}
	}
	sort.Strings(parts)
	return strings.Join(parts, ";")
}

// pending 在任何移动消息消费前识别原屏移除、工作区失效或同标识几何变化。
func (m *monitorTracker) pending(screens []*application.Screen) bool {
	if m.monitorID == "" {
		return false
	}
	for _, screen := range screens {
		if screen != nil && screen.ID == m.monitorID {
			// 临时失效仍属于原屏，等待可用枚举后再完成迁移。
			return screen.WorkArea.Width < 1 || screen.WorkArea.Height < 1 ||
				m.workArea.Width > 0 && m.workArea.Height > 0 && screen.WorkArea != m.workArea
		}
	}
	return true
}

// remember 仅接纳布局未变时的真实移动，返回是否可以保存；早到消息不能抹掉待迁移锚点。
func (m *monitorTracker) remember(x, y, width, height int, screens []*application.Screen) bool {
	if m.pending(screens) {
		return false
	}
	return m.adopt(x, y, width, height, screens)
}

// adopt 仅供已确认的显式拖动替换启动恢复，保留正常拖动允许的部分可见边缘。
func (m *monitorTracker) adopt(x, y, width, height int, screens []*application.Screen) bool {
	usable := monitorWorkAreas(screens)
	if width < 1 || height < 1 || len(usable) == 0 {
		return false
	}
	_, _, screen := clampPosition(x, y, width, height, usable)
	if screen == nil {
		return false
	}
	// 普通拖动允许边缘只留可交互区域，不能在记录时擅自将其夹回全窗可见位置。
	m.monitorID, m.workArea = screen.ID, screen.WorkArea
	m.relativeX, m.relativeY = x-screen.WorkArea.X, y-screen.WorkArea.Y
	return true
}

// anchor 用于重置和启动恢复，先替换旧事实再移动原生窗口，防止回调套用旧屏幕偏移。
func (m *monitorTracker) anchor(x, y, width, height int, target *application.Screen) (int, int, bool) {
	usable := monitorWorkAreas([]*application.Screen{target})
	if width < 1 || height < 1 || len(usable) == 0 {
		return x, y, false
	}
	cx, cy, screen := clampPosition(x, y, width, height, usable)
	m.monitorID, m.workArea = screen.ID, screen.WorkArea
	m.relativeX, m.relativeY = cx-screen.WorkArea.X, cy-screen.WorkArea.Y
	return cx, cy, true
}

// correct 在同屏布局变化时投影旧逻辑偏移，原屏移除才回主屏；普通移动沿用逐屏夹取规则。
func (m *monitorTracker) correct(x, y, width, height int, screens []*application.Screen) (int, int, bool) {
	usable := monitorWorkAreas(screens)
	if width < 1 || height < 1 || len(usable) == 0 {
		return x, y, false
	}
	if m.monitorID != "" && monitorPresent(m.monitorID, screens) && !monitorPresent(m.monitorID, usable) {
		// 标识仍在但工作区暂不可用时等待下次枚举，不能把几何读取失败解释为移屏。
		return x, y, false
	}
	if m.monitorID != "" && !monitorPresent(m.monitorID, screens) {
		// 主屏暂未标注时采用首个有效工作区，仍不依据旧坐标选择其他副屏。
		target := usable[0]
		for _, screen := range usable {
			if screen.IsPrimary {
				target = screen
				break
			}
		}
		usable = []*application.Screen{target}
	} else if m.workArea.Width > 0 && m.workArea.Height > 0 {
		// 同一标识的工作区变更必须留在原屏，旧绝对坐标即使重叠别屏也不能更换归属。
		for _, screen := range usable {
			if screen.ID == m.monitorID && screen.WorkArea != m.workArea {
				x, y = screen.WorkArea.X+m.relativeX, screen.WorkArea.Y+m.relativeY
				usable = []*application.Screen{screen}
				break
			}
		}
	}
	cx, cy, screen := clampPosition(x, y, width, height, usable)
	if screen == nil {
		return x, y, false
	}
	// 以实际夹取结果建立新锚点，下一次纠正保持幂等，尺寸变化也不会累积漂移。
	m.monitorID, m.workArea = screen.ID, screen.WorkArea
	m.relativeX, m.relativeY = cx-screen.WorkArea.X, cy-screen.WorkArea.Y
	return cx, cy, true
}
