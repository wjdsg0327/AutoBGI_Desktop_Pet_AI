package service

import (
	"context"
	"errors"
	"strings"
	"time"

	"autobgi-pet/internal/autobgi"
)

// ReadScriptGroupsForConnection 只读取菜单绑定的在线连接，不接纳切换或暂停后的旧列表。
//
//wails:ignore
func (s *Service) ReadScriptGroupsForConnection(profileID, activeProfileID string, generation uint64) ([]string, error) {
	s.mu.Lock()
	current := s.menuConnectionMatchesLocked(profileID, activeProfileID, generation)
	s.mu.Unlock()
	if !current {
		return nil, errors.New("连接已变化，请重新打开菜单")
	}
	if err := s.ensureMenuAdapter(); err != nil {
		return nil, err
	}

	// 探测结束后再次冻结来源，防止同名配置组被新连接的适配器替代。
	s.mu.Lock()
	if !s.menuConnectionMatchesLocked(profileID, activeProfileID, generation) {
		s.mu.Unlock()
		return nil, errors.New("连接已变化，请重新打开菜单")
	}
	ticket, ctx, release, err := s.beginManualReadLocked(10 * time.Second)
	if err != nil {
		s.mu.Unlock()
		return nil, err
	}
	controller, ok := ticket.adapter.(autobgi.ScriptGroupController)
	s.mu.Unlock()
	defer release()
	if !ok {
		return nil, errors.New("当前连接未提供配置组接口")
	}

	// 读取完成仍需核对代次、暂停周期和能力，即使底层忽略取消也不能返回旧名单。
	names, err := controller.ListScriptGroups(ctx)
	s.mu.Lock()
	current = s.manualReadCurrentLocked(ticket, ctx) && s.menuConnectionMatchesLocked(profileID, activeProfileID, generation)
	s.mu.Unlock()
	if !current {
		return nil, errors.New("读取期间连接已变化或请求超时，请重新打开菜单")
	}
	return names, err
}

// StartScriptGroupFromMenuForConnection 将点选视为切换授权，只向冻结连接发送一次单组启动请求。
//
//wails:ignore
func (s *Service) StartScriptGroupFromMenuForConnection(name, profileID, activeProfileID string, generation uint64) error {
	if strings.TrimSpace(name) == "" {
		return oneLongMenuError("INVALID_ARGUMENT", "配置组名称不能为空")
	}
	s.mu.Lock()
	current := s.menuConnectionMatchesLocked(profileID, activeProfileID, generation)
	s.mu.Unlock()
	if !current {
		return oneLongMenuError("CONNECTION_CHANGED", "连接已变化，请重新打开菜单后启动")
	}
	// 与一条龙共享在途门禁，但不阻止用户使用已有的停止和关闭菜单。
	if !s.setMenuStarting(true) {
		return oneLongMenuError("TASK_BUSY", "已有启动请求正在处理，请稍后查看桌宠提示")
	}
	defer s.setMenuStarting(false)
	// 把可能的首次适配器探测计入总预算，不能在探测结束后重新获得完整等待期限。
	deadline := time.Now().Add(5*time.Minute + 20*time.Second)
	if err := s.ensureMenuAdapter(); err != nil {
		return oneLongMenuError("CONNECTION_UNAVAILABLE", "当前连接不可用，请重新连接")
	}

	// 服务端同步等待启动就绪；总期限覆盖短列表校验和五分钟启动等待。
	s.mu.Lock()
	if !s.menuConnectionMatchesLocked(profileID, activeProfileID, generation) {
		s.mu.Unlock()
		return oneLongMenuError("CONNECTION_CHANGED", "连接已变化，请重新打开菜单后启动")
	}
	remaining := time.Until(deadline)
	if remaining <= 0 {
		s.mu.Unlock()
		return oneLongMenuError("CONNECTION_UNAVAILABLE", "连接准备已超时，请重新打开菜单")
	}
	ticket, ctx, release, err := s.beginManualReadLocked(remaining)
	if err != nil {
		s.mu.Unlock()
		return oneLongMenuError("CONNECTION_UNAVAILABLE", "当前连接不可用，请重新连接")
	}
	controller, ok := ticket.adapter.(autobgi.ScriptGroupController)
	s.mu.Unlock()
	defer release()
	if !ok {
		return oneLongMenuError("UNSUPPORTED_CAPABILITY", "当前连接未提供配置组启动能力")
	}

	// 原始名称精确匹配最新列表，不裁剪空格或改写路径，避免误启动另一份配置。
	listCtx, cancelList := context.WithTimeout(ctx, 10*time.Second)
	names, listErr := controller.ListScriptGroups(listCtx)
	listContextErr := listCtx.Err()
	cancelList()
	s.mu.Lock()
	current = s.manualReadCurrentLocked(ticket, ctx) && s.menuConnectionMatchesLocked(profileID, activeProfileID, generation)
	s.mu.Unlock()
	if !current {
		return oneLongMenuError("CONNECTION_CHANGED", "连接已变化，请重新打开菜单后启动")
	}
	if listErr != nil || listContextErr != nil {
		return oneLongMenuError("LIST_FAILED", "无法读取配置组列表，请重新打开菜单")
	}
	found := false
	for _, candidate := range names {
		if candidate == name {
			found = true
			break
		}
	}
	if !found {
		return oneLongMenuError("GROUP_NOT_FOUND", "所选配置组已失效，请重新打开菜单")
	}

	// 用户明确授权切换运行中的 BGI；不按进程状态拒绝，也不受仅查询模式阻挡。
	s.mu.Lock()
	current = s.manualReadCurrentLocked(ticket, ctx) && s.menuConnectionMatchesLocked(profileID, activeProfileID, generation)
	s.mu.Unlock()
	if !current {
		return oneLongMenuError("CONNECTION_CHANGED", "连接已变化，请重新打开菜单后启动")
	}
	if err := controller.StartScriptGroups(ctx, []string{name}); err != nil {
		return oneLongRequestUnknown(err)
	}

	// 与来源检查同锁唤醒原连接采集，避免旧回包在切换间隙刷新新连接。
	s.mu.Lock()
	current = s.manualReadCurrentLocked(ticket, ctx) && s.menuConnectionMatchesLocked(profileID, activeProfileID, generation)
	if current && s.refresh != nil {
		select {
		case s.refresh <- struct{}{}:
		default:
		}
	}
	s.mu.Unlock()
	if !current {
		return &OneLongStartError{Code: "RESULT_UNKNOWN", Message: "连接已变化，启动请求结果未知，请在 AutoBGI 核对；不会自动重发", Unknown: true}
	}
	return nil
}
