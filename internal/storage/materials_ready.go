package storage

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"

	"autobgi-pet/internal/autobgi"
)

// 材料记录有独立容量边界；新鲜去重记录不足时宁可停止接收，也不冒险重复提醒。
const (
	MaterialScopeLimit   = 10
	MaterialHistoryLimit = 100
	MaterialSeenLimit    = 4096
	journalByteLimit     = 16 * 1024 * 1024
)

// MaterialSeen 将跨传输代次不变的业务身份与原始事实摘要一起保存。
type MaterialSeen struct {
	BusinessEventID     string `json:"businessEventId"`
	Fingerprint         string `json:"fingerprint"`
	OccurredAt          string `json:"occurredAt"`
	PresentationHandled bool   `json:"presentationHandled"`
}

// MaterialScope 按连接、地址、实例和执行范围隔离水位与历史，改名不能重新绑定计划。
type MaterialScope struct {
	ProfileID        string                       `json:"profileId"`
	BaseURL          string                       `json:"baseUrl"`
	InstanceID       string                       `json:"instanceId"`
	ExecutionContext string                       `json:"executionContext"`
	Generation       string                       `json:"generation"`
	Cursor           string                       `json:"cursor"`
	Truncated        bool                         `json:"truncated"`
	History          []autobgi.MaterialReadyEvent `json:"history"`
	Seen             []MaterialSeen               `json:"seen"`
}

// MaterialFingerprint 排除可在新代次重新分配的传输序号，其余冻结事实必须完全一致。
func MaterialFingerprint(event autobgi.MaterialReadyEvent) string {
	event.Sequence = ""
	data, _ := json.Marshal(event)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// validHex 只接受规范小写身份，不让磁盘恢复引入别名或异常键。
func validHex(value string, length int) bool {
	if len(value) != length {
		return false
	}
	for _, char := range value {
		if char < '0' || char > '9' && char < 'a' || char > 'f' {
			return false
		}
	}
	return true
}

// validMaterialScopes 校验有界历史与业务去重元数据，恢复不会产生任何气泡。
func validMaterialScopes(scopes []MaterialScope) bool {
	if len(scopes) > MaterialScopeLimit {
		return false
	}
	keys := map[string]bool{}
	for _, scope := range scopes {
		_, cursorOK := autobgi.MaterialSequence(scope.Cursor)
		key := scope.ProfileID + "\x00" + scope.BaseURL + "\x00" + scope.InstanceID + "\x00" + scope.ExecutionContext
		if scope.ProfileID == "" || scope.BaseURL == "" || !validHex(scope.InstanceID, 32) || !validHex(scope.ExecutionContext, 64) ||
			!validHex(scope.Generation, 32) || !cursorOK || len(scope.History) > MaterialHistoryLimit ||
			len(scope.Seen) > MaterialSeenLimit || keys[key] {
			return false
		}
		keys[key] = true
		ids := map[string]bool{}
		seen := map[string]MaterialSeen{}
		for _, item := range scope.Seen {
			occurred, err := time.Parse(time.RFC3339Nano, item.OccurredAt)
			if !validHex(item.BusinessEventID, 64) || !validHex(item.Fingerprint, 64) || ids[item.BusinessEventID] ||
				err != nil || occurred.IsZero() || !item.PresentationHandled {
				return false
			}
			ids[item.BusinessEventID] = true
			seen[item.BusinessEventID] = item
		}
		historyIDs := map[string]bool{}
		for _, event := range scope.History {
			if !autobgi.ValidateMaterialReadyEvent(event) || event.ExecutionContext != scope.ExecutionContext || historyIDs[event.BusinessEventID] {
				return false
			}
			if item, ok := seen[event.BusinessEventID]; ok && (item.Fingerprint != MaterialFingerprint(event) || item.OccurredAt != event.OccurredAt) {
				return false
			}
			historyIDs[event.BusinessEventID] = true
		}
	}
	return true
}
