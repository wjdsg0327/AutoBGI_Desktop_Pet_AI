package desktop

import (
	"errors"
	"strings"
	"unicode"

	"autobgi-pet/internal/service"
	"github.com/wailsapp/wails/v3/pkg/application"
)

// hotKeyNames 按固定顺序列出服务端支持的六个系统快捷键。
var hotKeyNames = []string{
	"启动停止BetterGI",
	"停止当前脚本/ 独立任务",
	"暂停当前脚本/ 独立任务",
	"游戏截图",
	"日志与状态窗口展示开关",
	"启动停止一条龙",
}

// hotKeyMenuState 仅保存当前连接的菜单快照和在途请求状态。
type hotKeyMenuState struct {
	status, profileID, activeProfileID string
	generation, sequence, revision     uint64
	keys                               map[string]string
	loading, pending                   bool
}

// addHotKeyMenu 根据快照生成托盘和桌宠共用的快捷键子菜单。
func (d *DesktopService) addHotKeyMenu(menu *application.Menu) {
	d.mu.Lock()
	snapshot := d.hotKeyMenu
	keys := make(map[string]string, len(snapshot.keys))
	for name, key := range snapshot.keys {
		keys[name] = key
	}
	d.mu.Unlock()

	submenu := menu.AddSubmenu("BGI 快捷键")
	if snapshot.status != oneLongMenuReady {
		hint := "未连接或连接尚未完成"
		if snapshot.status == oneLongMenuLoading {
			hint = "正在读取 BGI 快捷键"
		} else if snapshot.status == oneLongMenuError {
			// 托盘打开菜单不经过桌宠右键入口，因此同时提供显式重试。
			submenu.Add("读取失败，点击重试").OnClick(func(*application.Context) {
				d.scheduleHotKeyMenuRefresh(d.service.GetState(), true)
			})
			return
		}
		submenu.Add(hint).SetEnabled(false)
		return
	}
	if snapshot.pending {
		submenu.Add("正在发送按键，请稍候").SetEnabled(false)
	}
	for _, name := range hotKeyNames {
		key := keys[name]
		labelKey := strings.Map(func(r rune) rune {
			if unicode.IsControl(r) {
				return ' '
			}
			return r
		}, key)
		if strings.TrimSpace(key) == "" {
			labelKey = "未配置"
		}
		item := submenu.Add(name + "（" + labelKey + "）")
		selectedName, selectedKey := name, key
		item.SetEnabled(strings.TrimSpace(key) != "" && !snapshot.pending).OnClick(func(*application.Context) {
			d.pressHotKeyFromMenu(selectedName, selectedKey, snapshot.profileID, snapshot.activeProfileID, snapshot.generation)
		})
	}
	// 托盘和桌宠均可主动刷新，便于读取 BGI 刚修改的配置。
	submenu.AddSeparator()
	submenu.Add("刷新快捷键").SetEnabled(!snapshot.pending).OnClick(func(*application.Context) {
		d.scheduleHotKeyMenuRefresh(d.service.GetState(), true)
	})
}

// scheduleHotKeyMenuRefresh 异步读取当前在线连接，连接变化时立即撤销旧菜单。
func (d *DesktopService) scheduleHotKeyMenuRefresh(state service.AppState, force bool) {
	if d.stopped() {
		return
	}
	d.mu.Lock()
	m := &d.hotKeyMenu
	if state.Revision > 0 && state.Revision < m.revision {
		d.mu.Unlock()
		return
	}
	if state.Revision > m.revision {
		m.revision = state.Revision
	}
	changed := m.profileID != state.Connection.ProfileID || m.activeProfileID != state.ActiveProfileID || m.generation != state.Connection.Generation
	if state.Connection.Status != "online" {
		update := changed || m.status != oneLongMenuUnavailable || m.loading || len(m.keys) != 0
		if update {
			m.sequence++
			m.profileID, m.activeProfileID, m.generation = state.Connection.ProfileID, state.ActiveProfileID, state.Connection.Generation
			m.status, m.keys, m.loading = oneLongMenuUnavailable, nil, false
		}
		d.mu.Unlock()
		if update {
			d.rebuildTrayMenu()
		}
		return
	}
	if !changed && (m.loading || (!force && m.status != "" && m.status != oneLongMenuUnavailable)) {
		d.mu.Unlock()
		return
	}
	// 新连接无需等待旧连接的读取结束；序号使旧回包失效。
	m.profileID, m.activeProfileID, m.generation = state.Connection.ProfileID, state.ActiveProfileID, state.Connection.Generation
	m.sequence++
	sequence := m.sequence
	m.status, m.keys, m.loading = oneLongMenuLoading, nil, true
	profileID, activeProfileID, generation := m.profileID, m.activeProfileID, m.generation
	d.mu.Unlock()
	d.rebuildTrayMenu()
	go d.readHotKeyMenu(profileID, activeProfileID, generation, sequence)
}

// readHotKeyMenu 只提交仍属于当前连接和读取序号的结果。
func (d *DesktopService) readHotKeyMenu(profileID, activeProfileID string, generation, sequence uint64) {
	keys, err := d.service.ReadBGIHotKeysForConnection(profileID, activeProfileID, generation)
	application.InvokeAsync(func() {
		if d.stopped() {
			return
		}
		state := d.service.GetState()
		d.mu.Lock()
		m := &d.hotKeyMenu
		if sequence != m.sequence || !oneLongMenuOnlineIdentityMatches(state, profileID, activeProfileID, generation) {
			d.mu.Unlock()
			return
		}
		m.loading = false
		if err != nil {
			m.status, m.keys = oneLongMenuError, nil
		} else {
			m.status, m.keys = oneLongMenuReady, keys
		}
		d.mu.Unlock()
		d.rebuildTrayMenu()
	})
}

// pressHotKeyFromMenu 冻结菜单身份并在后台单次提交，旧连接结果不影响新连接。
func (d *DesktopService) pressHotKeyFromMenu(name, key, profileID, activeProfileID string, generation uint64) {
	state := d.service.GetState()
	if !oneLongMenuOnlineIdentityMatches(state, profileID, activeProfileID, generation) {
		d.showBubbleForState(state, "连接已变化，请重新打开菜单", "error")
		d.scheduleHotKeyMenuRefresh(state, false)
		return
	}
	d.mu.Lock()
	if d.hotKeyMenu.pending {
		d.mu.Unlock()
		d.showBubble("已有按键请求正在处理，请稍候", "error")
		return
	}
	d.hotKeyMenu.pending = true
	d.mu.Unlock()
	d.rebuildTrayMenu()
	go func() {
		err := d.service.PressBGIHotKeyForConnection(name, key, profileID, activeProfileID, generation)
		application.InvokeAsync(func() {
			if d.stopped() {
				return
			}
			current := d.service.GetState()
			d.mu.Lock()
			d.hotKeyMenu.pending = false
			d.mu.Unlock()
			d.rebuildTrayMenu()
			if !oneLongMenuIdentityMatches(current, profileID, activeProfileID, generation) {
				return
			}
			if err == nil {
				d.showBubble("按键请求已发送："+name, "status")
				return
			}
			var menuErr *service.OneLongStartError
			if errors.As(err, &menuErr) {
				d.showBubble(menuErr.Message, "error")
				if menuErr.Code == "HOTKEY_CHANGED" || menuErr.Code == "LIST_FAILED" {
					d.scheduleHotKeyMenuRefresh(current, true)
				}
				return
			}
			d.showBubble("快捷键请求失败，请检查连接", "error")
		})
	}()
}
