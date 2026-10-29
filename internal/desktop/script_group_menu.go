package desktop

import (
	"errors"
	"strings"
	"unicode"

	"autobgi-pet/internal/service"
	"github.com/wailsapp/wails/v3/pkg/application"
)

// scriptGroupMenuState 只保存当前连接的配置组列表，启动门禁由两个启动菜单共享。
type scriptGroupMenuState struct {
	status, profileID, activeProfileID string
	generation, sequence, revision     uint64
	names                              []string
	loading                            bool
}

// prepare 在桌面锁内更新读取身份；返回是否重建菜单、是否发起后台读取。
func (m *scriptGroupMenuState) prepare(state service.AppState, force bool) (bool, bool) {
	// 较旧的状态广播不能回滚身份，也不能让失效名称重新可用。
	if state.Revision > 0 && state.Revision < m.revision {
		return false, false
	}
	if state.Revision > m.revision {
		m.revision = state.Revision
	}
	changed := m.profileID != state.Connection.ProfileID || m.activeProfileID != state.ActiveProfileID || m.generation != state.Connection.Generation
	if state.Connection.Status != "online" {
		update := changed || m.status != oneLongMenuUnavailable || m.loading || len(m.names) != 0
		if update {
			m.sequence++
			m.profileID, m.activeProfileID, m.generation = state.Connection.ProfileID, state.ActiveProfileID, state.Connection.Generation
			m.status, m.names, m.loading = oneLongMenuUnavailable, nil, false
		}
		return update, false
	}
	if !changed && (m.loading || (!force && m.status != "" && m.status != oneLongMenuUnavailable)) {
		return false, false
	}

	// 新连接无需等待旧读取完成；序号立即撤销旧结果，但不改变在途启动状态。
	m.profileID, m.activeProfileID, m.generation = state.Connection.ProfileID, state.ActiveProfileID, state.Connection.Generation
	m.sequence++
	m.status, m.names, m.loading = oneLongMenuLoading, nil, true
	return true, true
}

// complete 在桌面锁内提交当前读取结果，迟到响应不能覆盖已切换或离线的菜单。
func (m *scriptGroupMenuState) complete(state service.AppState, profileID, activeProfileID string, generation, sequence uint64, names []string, err error) bool {
	if sequence != m.sequence || !oneLongMenuOnlineIdentityMatches(state, profileID, activeProfileID, generation) {
		return false
	}
	m.loading = false
	if err != nil {
		m.status, m.names = oneLongMenuError, nil
	} else if len(names) == 0 {
		m.status, m.names = oneLongMenuEmpty, nil
	} else {
		m.status, m.names = oneLongMenuReady, append([]string(nil), names...)
	}
	return true
}

// addScriptGroupMenu 从内存快照构建共享原生菜单，不在 UI 线程执行网络读取。
func (d *DesktopService) addScriptGroupMenu(menu *application.Menu) {
	d.mu.Lock()
	snapshot := d.scriptGroupMenu
	names := append([]string(nil), snapshot.names...)
	pending := d.menuStartPending
	d.mu.Unlock()

	submenu := menu.AddSubmenu("启动配置组")
	// 固定提示说明点选的影响，不增加二次确认或改变原始名称。
	submenu.Add("点选将切换当前 BGI 任务").SetEnabled(false)
	if pending {
		submenu.Add("正在处理启动请求，请稍候").SetEnabled(false)
	}
	switch snapshot.status {
	case oneLongMenuReady:
		for _, name := range names {
			selectedName := name
			// 只清理显示中的控制字符；实际请求始终发送服务端原名。
			label := strings.Map(func(r rune) rune {
				if unicode.IsControl(r) {
					return ' '
				}
				return r
			}, name)
			submenu.Add(label).SetEnabled(!pending).OnClick(func(*application.Context) {
				d.startScriptGroupFromMenu(selectedName, snapshot.profileID, snapshot.activeProfileID, snapshot.generation)
			})
		}
	case oneLongMenuLoading:
		submenu.Add("正在读取配置组列表").SetEnabled(false)
	case oneLongMenuEmpty:
		submenu.Add("暂无配置组").SetEnabled(false)
	case oneLongMenuError:
		submenu.Add("读取失败，点击刷新重试").SetEnabled(false)
	default:
		submenu.Add("未连接或连接尚未完成").SetEnabled(false)
	}

	// 托盘打开不会经过桌宠右键回调，始终保留可见的显式刷新入口。
	submenu.AddSeparator()
	canRefresh := snapshot.status == oneLongMenuReady || snapshot.status == oneLongMenuEmpty || snapshot.status == oneLongMenuError
	submenu.Add("刷新配置组").SetEnabled(canRefresh && !pending).OnClick(func(*application.Context) {
		d.scheduleScriptGroupMenuRefresh(d.service.GetState(), true)
	})
}

// scheduleScriptGroupMenuRefresh 在连接变化或显式刷新时异步读取，不随每次广播重复请求。
func (d *DesktopService) scheduleScriptGroupMenuRefresh(state service.AppState, force bool) {
	if d.stopped() {
		return
	}
	d.mu.Lock()
	update, read := d.scriptGroupMenu.prepare(state, force)
	snapshot := d.scriptGroupMenu
	d.mu.Unlock()
	if update {
		d.rebuildTrayMenu()
	}
	if read {
		go d.readScriptGroupMenu(snapshot.profileID, snapshot.activeProfileID, snapshot.generation, snapshot.sequence)
	}
}

// readScriptGroupMenu 将网络响应送回 UI 队列，并在提交前核对当前连接和读取序号。
func (d *DesktopService) readScriptGroupMenu(profileID, activeProfileID string, generation, sequence uint64) {
	names, err := d.service.ReadScriptGroupsForConnection(profileID, activeProfileID, generation)
	application.InvokeAsync(func() {
		if d.stopped() {
			return
		}
		state := d.service.GetState()
		d.mu.Lock()
		update := d.scriptGroupMenu.complete(state, profileID, activeProfileID, generation, sequence, names, err)
		d.mu.Unlock()
		if update {
			d.rebuildTrayMenu()
		}
	})
}

// startScriptGroupFromMenu 将点选视为本次切换授权，仅后台派发一次，不阻塞窗口或关闭入口。
func (d *DesktopService) startScriptGroupFromMenu(name, profileID, activeProfileID string, generation uint64) {
	if strings.TrimSpace(name) == "" {
		return
	}
	state := d.service.GetState()
	if !oneLongMenuOnlineIdentityMatches(state, profileID, activeProfileID, generation) {
		d.showBubbleForState(state, "连接已变化，请重新打开菜单后选择配置组", "error")
		d.scheduleScriptGroupMenuRefresh(state, false)
		return
	}

	// 两种启动共享门禁；即使菜单尚未刷新，排队的重复点击也不能派发。
	d.mu.Lock()
	if d.menuStartPending {
		d.mu.Unlock()
		d.showMenuStartBubble("已有启动请求正在处理，请稍后查看桌宠提示", "error", profileID, activeProfileID, generation)
		return
	}
	d.menuStartPending = true
	d.mu.Unlock()
	d.rebuildTrayMenu()
	// 控制提示是完整句子，不能按运行摘要拆成窄标签列与值列。
	d.showMenuStartBubble("正在启动配置组，请稍候："+name, "manual", profileID, activeProfileID, generation)
	go func() {
		err := d.service.StartScriptGroupFromMenuForConnection(name, profileID, activeProfileID, generation)
		application.InvokeAsync(func() {
			if d.stopped() {
				return
			}
			current := d.service.GetState()
			d.mu.Lock()
			d.menuStartPending = false
			d.mu.Unlock()
			d.rebuildTrayMenu()
			// 原连接的结果不能进入新连接通知队列，未知结果也不触发重新发送。
			if !oneLongMenuIdentityMatches(current, profileID, activeProfileID, generation) {
				return
			}
			if err == nil {
				// 普通文本布局按完整名称换行，并使用对应的气泡高度估算。
				d.showMenuStartBubble("配置组启动请求已受理："+name, "manual", profileID, activeProfileID, generation)
				return
			}
			var startErr *service.OneLongStartError
			if errors.As(err, &startErr) {
				d.showMenuStartBubble(startErr.Message, "error", profileID, activeProfileID, generation)
				if startErr.Code == "GROUP_NOT_FOUND" || startErr.Code == "LIST_FAILED" {
					d.scheduleScriptGroupMenuRefresh(current, true)
				}
				return
			}
			d.showMenuStartBubble("配置组启动失败，请检查 AutoBGI 连接", "error", profileID, activeProfileID, generation)
		})
	}()
}
