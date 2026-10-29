package desktop

import (
	"strings"
	"time"
	"unicode"

	"autobgi-pet/internal/autobgi"
	"autobgi-pet/internal/service"
)

// petClickSummary 用四行有限文本报告当前连接、配置、路线及真正的采集时间。
func petClickSummary(state service.AppState) string {
	label := "尚未连接，请打开连接设置"
	switch state.Connection.Status {
	case "online":
		label = "连接正常"
	case "connecting":
		label = "正在连接，等待更新"
	case "backoff":
		label = "连接已断开，等待重连"
	case "auth_failed":
		label = "密钥验证失败，请检查连接"
	case "incompatible":
		label = "协议不兼容，请检查连接"
	case "paused":
		label = "同步已暂停"
	case "disconnected":
		label = "连接已断开"
	}
	snapshot := currentClickSnapshot(state)
	if snapshot == nil {
		return label + "\n配置：未知\n路线：未知\n上次更新于：暂无可靠记录"
	}
	if state.Connection.Status == "online" && snapshot.Stale {
		label += "（历史数据）"
	} else if state.Connection.Status != "online" {
		label += "（历史数据）"
	}
	updated := "时间不可用"
	if fetched, err := time.Parse(time.RFC3339Nano, snapshot.FetchedAt); err == nil && !fetched.IsZero() {
		updated = fetched.Local().Format("2006-01-02 15:04:05")
	}
	return label + "\n配置：" + clickSummaryName(snapshot.Task.GroupName) +
		"\n路线：" + clickSummaryName(snapshot.Task.RouteName) + "\n上次更新于：" + updated
}

// currentClickSnapshot 只使用当前保存资料与已核验来源，不把重连前的实例当作当前身份。
func currentClickSnapshot(state service.AppState) *autobgi.Snapshot {
	if state.ActiveProfileID == "" || state.Connection.ProfileID != state.ActiveProfileID {
		return nil
	}
	found := false
	for _, p := range state.Profiles {
		if p.ID == state.ActiveProfileID {
			found = true
			break
		}
	}
	snapshot := state.Snapshot
	if !found || snapshot == nil || snapshot.ProfileID != state.ActiveProfileID ||
		snapshot.Scope != "instance" || snapshot.AccountID != nil {
		return nil
	}
	caps := state.Capabilities
	switch snapshot.SourceMode {
	case "pet-v1":
		if caps != nil && caps.SourceMode == "pet-v1" && caps.InstanceID != nil && *caps.InstanceID != "" &&
			snapshot.ServerInstanceID != nil && *snapshot.ServerInstanceID == *caps.InstanceID {
			return snapshot
		}
	case "legacy":
		if snapshot.ServerInstanceID == nil && (caps == nil || caps.SourceMode == "legacy") {
			return snapshot
		}
	}
	return nil
}

// clickSummaryName 清理换行和控制字符，按字符截断以保持气泡四行布局和有效中文。
func clickSummaryName(value *string) string {
	if value == nil {
		return "未知"
	}
	plain := strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || unicode.IsSpace(r) || unicode.Is(unicode.Cf, r) {
			return ' '
		}
		return r
	}, *value)
	plain = strings.Join(strings.Fields(plain), " ")
	if plain == "" {
		return "未知"
	}
	runes := []rune(plain)
	if len(runes) > 18 {
		return string(runes[:17]) + "…"
	}
	return plain
}
