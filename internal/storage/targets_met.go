package storage

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"

	"autobgi-pet/internal/autobgi"
)

// 目标达标记录有独立容量边界；新鲜去重记录不足时宁可停止接收，也不冒险重复提醒。
const (
	TargetScopeLimit   = 10
	TargetHistoryLimit = 100
	TargetSeenLimit    = 4096
)

// TargetSeen 将跨传输代次不变的业务身份与原始事实摘要一起保存。
type TargetSeen struct {
	BusinessEventID     string `json:"businessEventId"`
	Fingerprint         string `json:"fingerprint"`
	OccurredAt          string `json:"occurredAt"`
	PresentationHandled bool   `json:"presentationHandled"`
}

// TargetScope 按连接、地址、实例和执行范围隔离水位与历史，改名不能重新绑定计划。
type TargetScope struct {
	ProfileID        string                   `json:"profileId"`
	BaseURL          string                   `json:"baseUrl"`
	InstanceID       string                   `json:"instanceId"`
	ExecutionContext string                   `json:"executionContext"`
	Generation       string                   `json:"generation"`
	Cursor           string                   `json:"cursor"`
	Truncated        bool                     `json:"truncated"`
	History          []autobgi.TargetMetEvent `json:"history"`
	Seen             []TargetSeen             `json:"seen"`
}

// TargetFingerprint 排除可在新代次重新分配的传输序号，其余冻结事实必须完全一致。
func TargetFingerprint(event autobgi.TargetMetEvent) string {
	event.Sequence = ""
	data, _ := json.Marshal(event)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// validTargetScopes 校验有界历史与业务去重元数据，恢复不会产生任何气泡。
func validTargetScopes(scopes []TargetScope) bool {
	if len(scopes) > TargetScopeLimit {
		return false
	}
	keys := map[string]bool{}
	for _, scope := range scopes {
		_, cursorOK := autobgi.TargetSequence(scope.Cursor)
		key := scope.ProfileID + "\x00" + scope.BaseURL + "\x00" + scope.InstanceID + "\x00" + scope.ExecutionContext
		normalizedURL, urlErr := autobgi.NormalizeURL(scope.BaseURL, true)
		if scope.ProfileID == "" || scope.BaseURL == "" || !validHex(scope.InstanceID, 32) || !validHex(scope.ExecutionContext, 64) ||
			!validHex(scope.Generation, 32) || !cursorOK || len(scope.History) > TargetHistoryLimit ||
			len(scope.Seen) > TargetSeenLimit || scope.History == nil || scope.Seen == nil || keys[key] ||
			urlErr != nil || normalizedURL != scope.BaseURL {
			return false
		}
		keys[key] = true
		ids := map[string]bool{}
		seen := map[string]TargetSeen{}
		for _, item := range scope.Seen {
			occurred, err := time.Parse(time.RFC3339Nano, item.OccurredAt)
			if !validHex(item.BusinessEventID, 64) || !validHex(item.Fingerprint, 64) || ids[item.BusinessEventID] ||
				err != nil || occurred.IsZero() || occurred.Year() < 1 || !item.PresentationHandled {
				return false
			}
			ids[item.BusinessEventID] = true
			seen[item.BusinessEventID] = item
		}
		historyIDs := map[string]bool{}
		for _, event := range scope.History {
			if !autobgi.ValidateTargetMetEvent(event) || event.ExecutionContext != scope.ExecutionContext || historyIDs[event.BusinessEventID] {
				return false
			}
			if item, ok := seen[event.BusinessEventID]; ok && (item.Fingerprint != TargetFingerprint(event) || item.OccurredAt != event.OccurredAt) {
				return false
			}
			historyIDs[event.BusinessEventID] = true
		}
	}
	return true
}
