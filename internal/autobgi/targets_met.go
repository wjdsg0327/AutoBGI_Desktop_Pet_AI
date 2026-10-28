package autobgi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// 目标达标事件独立于游戏执行结果，只能读取服务端已可靠接收的业务记录。
const (
	TargetEntryMet = "talent.entry.targets-met"
	TargetQueueMet = "talent.queue.targets-met"
)

// TargetsMetReader 不扩展任意网络绑定，也不改变旧适配器的只读能力。
type TargetsMetReader interface {
	ReadTargetsMet(context.Context, *TargetsMetQuery) (TargetsMetPage, error)
}

// TargetsMetQuery 的三个字段必须同时提供，序号保持十进制原文。
type TargetsMetQuery struct {
	After            string `json:"after"`
	Generation       string `json:"generation"`
	ExecutionContext string `json:"executionContext"`
}

// TargetMetEvent 保留冻结的计划与账号身份，不按历史配置名更新当前队首。
type TargetMetEvent struct {
	Sequence         string               `json:"sequence"`
	BusinessEventID  string               `json:"businessEventId"`
	OperationID      string               `json:"operationId"`
	ExecutionContext string               `json:"executionContext"`
	PlanID           string               `json:"planId"`
	ConfigName       string               `json:"configName"`
	Role             CultivationQueueRole `json:"role"`
	Kind             string               `json:"kind"`
	EntryID          string               `json:"entryId"`
	CharacterID      int                  `json:"characterId"`
	CharacterName    string               `json:"characterName"`
	PlanRevision     string               `json:"planRevision"`
	ObservedAt       string               `json:"observedAt"` // ObservedAt 是可信基础面板实际源时间，不是本次游戏培养完成时间。
	OccurredAt       string               `json:"occurredAt"` // OccurredAt 是服务端队列事务提交时间。
}

// TargetsMetPage 的游标与本批记录来自同一服务端采样，首次读取仅补历史。
type TargetsMetPage struct {
	SchemaVersion    int              `json:"schemaVersion"`
	InstanceID       string           `json:"instanceId"`
	ExecutionContext string           `json:"executionContext"`
	Generation       string           `json:"generation"`
	Cursor           string           `json:"cursor"`
	HasMore          bool             `json:"hasMore"`
	Truncated        bool             `json:"truncated"`
	Events           []TargetMetEvent `json:"events"`
}

// TargetSequence 拒绝负数、前导零和超过有符号六十四位的序号，避免前端精度丢失。
func TargetSequence(value string) (uint64, bool) {
	if !queueRevisionPattern.MatchString(value) {
		return 0, false
	}
	number, err := strconv.ParseUint(value, 10, 63)
	return number, err == nil
}

// ValidateTargetMetEvent 同时用于网络与磁盘恢复，不能从异常字段拼出完成提示。
func ValidateTargetMetEvent(event TargetMetEvent) bool {
	sequence, sequenceOK := TargetSequence(event.Sequence)
	revision, revisionOK := TargetSequence(event.PlanRevision)
	occurred, timeErr := time.Parse(time.RFC3339Nano, event.OccurredAt)
	observed, observedErr := time.Parse(time.RFC3339Nano, event.ObservedAt)
	if !sequenceOK || sequence == 0 || !revisionOK || revision < 2 ||
		!queueContextPattern.MatchString(event.BusinessEventID) || !queueIdentityPattern.MatchString(event.OperationID) ||
		!queueContextPattern.MatchString(event.ExecutionContext) || !queueIdentityPattern.MatchString(event.PlanID) ||
		!queueUIDPattern.MatchString(event.Role.UID) || !queueRegionPattern.MatchString(event.Role.Region) ||
		!queueText(event.ConfigName, 128, false) || strings.TrimSpace(event.ConfigName) != event.ConfigName ||
		strings.ContainsAny(event.ConfigName, `<>:"/\|?*`) || strings.HasSuffix(event.ConfigName, ".") ||
		event.ConfigName == "." || event.ConfigName == ".." || len(event.OccurredAt) > 64 || len(event.ObservedAt) > 64 ||
		timeErr != nil || occurred.IsZero() || occurred.Year() < 1 ||
		observedErr != nil || observed.IsZero() || observed.Year() < 1 || observed.After(occurred) {
		return false
	}
	switch event.Kind {
	case TargetEntryMet:
		return queueIdentityPattern.MatchString(event.EntryID) && event.CharacterID > 0 && int64(event.CharacterID) <= 2147483647 &&
			queueText(event.CharacterName, 64, false)
	case TargetQueueMet:
		return event.EntryID == "" && event.CharacterID == 0 && event.CharacterName == ""
	default:
		return false
	}
}

// ValidateTargetsMetPage 绑定请求代次与执行范围，并拒绝逆序、重复或不能推进的续页。
func ValidateTargetsMetPage(page TargetsMetPage, instanceID string, query *TargetsMetQuery) error {
	cursor, cursorOK := TargetSequence(page.Cursor)
	if page.SchemaVersion != 1 || page.InstanceID != instanceID || !queueIdentityPattern.MatchString(page.InstanceID) ||
		!queueContextPattern.MatchString(page.ExecutionContext) || !queueIdentityPattern.MatchString(page.Generation) ||
		!cursorOK || page.Events == nil || len(page.Events) > 100 || query == nil && page.HasMore {
		return queueInvalid("目标达标结果的实例、范围、分页或版本无效")
	}
	var previous uint64
	if query != nil {
		var valid bool
		previous, valid = TargetSequence(query.After)
		if !valid || page.Generation != query.Generation || page.ExecutionContext != query.ExecutionContext || page.Truncated || cursor < previous {
			return queueInvalid("目标达标结果的接收水位或执行范围不匹配")
		}
		if page.HasMore && len(page.Events) != 100 {
			return queueInvalid("目标达标结果续页没有提供完整分页")
		}
	} else {
		// 原子基线始终覆盖接收头部的最后一百条，不能把缺失页当作完整历史。
		count := min(cursor, 100)
		if uint64(len(page.Events)) != count || page.Truncated != (cursor > 100) {
			return queueInvalid("目标达标历史基线存在缺失记录")
		}
		previous = cursor - count
	}
	seen := map[string]bool{}
	for _, event := range page.Events {
		sequence, _ := TargetSequence(event.Sequence)
		if !ValidateTargetMetEvent(event) || event.ExecutionContext != page.ExecutionContext ||
			sequence != previous+1 || sequence > cursor || seen[event.BusinessEventID] {
			return queueInvalid("目标达标结果的业务身份或事件顺序无效")
		}
		previous = sequence
		seen[event.BusinessEventID] = true
	}
	if query != nil && (previous != cursor || page.HasMore && len(page.Events) == 0) {
		return queueInvalid("目标达标结果续页没有提供连续接收水位")
	}
	return nil
}

// validateTargetsJSON 在反序列化前要求明确字段与类型，拒绝重复字段和空值降级。
func validateTargetsJSON(raw []byte) bool {
	if !queueJSONUnique(raw) {
		return false
	}
	top, ok := queueJSONFields(raw, "schemaVersion", "instanceId", "executionContext", "generation", "cursor", "hasMore", "truncated", "events")
	if !ok || !queueJSONIntegers(top, "schemaVersion") || !queueJSONStrings(top, false, "instanceId", "executionContext", "generation", "cursor") ||
		!queueJSONBooleans(top, "hasMore", "truncated") {
		return false
	}
	events, ok := queueJSONArray(top["events"])
	if !ok || len(events) > 100 {
		return false
	}
	for _, rawEvent := range events {
		event, ok := queueJSONFields(rawEvent, "sequence", "businessEventId", "operationId", "executionContext", "planId", "configName",
			"role", "kind", "entryId", "characterId", "characterName", "planRevision", "observedAt", "occurredAt")
		if !ok || !queueJSONIntegers(event, "characterId") || !queueJSONStrings(event, false, "sequence", "businessEventId",
			"operationId", "executionContext", "planId", "configName", "kind", "entryId", "characterName", "planRevision", "observedAt", "occurredAt") {
			return false
		}
		role, ok := queueJSONFields(event["role"], "uid", "region")
		if !ok || !queueJSONStrings(role, false, "uid", "region") {
			return false
		}
	}
	return true
}

// ReadTargetsMet 仅访问固定只读路由，先校验再脱敏，之后再次核对身份不被替换破坏。
func (a *PetV1Adapter) ReadTargetsMet(ctx context.Context, query *TargetsMetQuery) (TargetsMetPage, error) {
	if !a.caps.TargetsMetEvents {
		return TargetsMetPage{}, unsupported()
	}
	values := url.Values{}
	if query != nil {
		if _, ok := TargetSequence(query.After); !ok || !queueIdentityPattern.MatchString(query.Generation) || !queueContextPattern.MatchString(query.ExecutionContext) {
			return TargetsMetPage{}, &APIError{Code: "INVALID_ARGUMENT", Message: "目标达标结果的续读参数无效"}
		}
		values.Set("after", query.After)
		values.Set("generation", query.Generation)
		values.Set("executionContext", query.ExecutionContext)
	}
	var response envelope
	if err := a.client.requestQuery(ctx, http.MethodGet, "/api/desktop-pet/v1/cultivation-target-events", values, nil, &response); err != nil {
		return TargetsMetPage{}, err
	}
	if response.Code != "OK" || !validateTargetsJSON(response.Data) {
		return TargetsMetPage{}, queueInvalid("目标达标结果响应结构不兼容")
	}
	var page TargetsMetPage
	if json.Unmarshal(response.Data, &page) != nil {
		return TargetsMetPage{}, queueInvalid("目标达标结果无法安全解析")
	}
	if err := ValidateTargetsMetPage(page, a.instanceID, query); err != nil {
		return TargetsMetPage{}, err
	}
	// 逐字段处理原始字符串，带引号或转义字符的密钥也不会绕过脱敏。
	for _, value := range []*string{&page.InstanceID, &page.ExecutionContext, &page.Generation, &page.Cursor} {
		*value = a.client.redact(*value)
	}
	for index := range page.Events {
		event := &page.Events[index]
		for _, value := range []*string{&event.Sequence, &event.BusinessEventID, &event.OperationID, &event.ExecutionContext,
			&event.PlanID, &event.ConfigName, &event.Role.UID, &event.Role.Region, &event.Kind, &event.EntryID,
			&event.CharacterName, &event.PlanRevision, &event.ObservedAt, &event.OccurredAt} {
			*value = a.client.redact(*value)
		}
	}
	if err := ValidateTargetsMetPage(page, a.instanceID, query); err != nil {
		return TargetsMetPage{}, err
	}
	return page, nil
}
