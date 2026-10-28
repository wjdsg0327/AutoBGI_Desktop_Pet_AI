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

// 材料事件独立于游戏执行结果，只能读取服务端已可靠接收的业务记录。
const (
	MaterialEntryReady = "talent.entry.materials-ready"
	MaterialQueueReady = "talent.queue.materials-ready"
)

// MaterialsReadyReader 不扩展任意网络绑定，也不改变旧适配器的只读能力。
type MaterialsReadyReader interface {
	ReadMaterialsReady(context.Context, *MaterialsReadyQuery) (MaterialsReadyPage, error)
}

// MaterialsReadyQuery 的三个字段必须同时提供，序号保持十进制原文。
type MaterialsReadyQuery struct {
	After            string `json:"after"`
	Generation       string `json:"generation"`
	ExecutionContext string `json:"executionContext"`
}

// MaterialReadyEvent 保留冻结的计划与账号身份，不按历史配置名更新当前队首。
type MaterialReadyEvent struct {
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
	OccurredAt       string               `json:"occurredAt"`
}

// MaterialsReadyPage 的游标与本批记录来自同一服务端采样，首次读取仅补历史。
type MaterialsReadyPage struct {
	SchemaVersion    int                  `json:"schemaVersion"`
	InstanceID       string               `json:"instanceId"`
	ExecutionContext string               `json:"executionContext"`
	Generation       string               `json:"generation"`
	Cursor           string               `json:"cursor"`
	HasMore          bool                 `json:"hasMore"`
	Truncated        bool                 `json:"truncated"`
	Events           []MaterialReadyEvent `json:"events"`
}

// MaterialSequence 拒绝负数、前导零和超过有符号六十四位的序号，避免前端精度丢失。
func MaterialSequence(value string) (uint64, bool) {
	if !queueRevisionPattern.MatchString(value) {
		return 0, false
	}
	number, err := strconv.ParseUint(value, 10, 63)
	return number, err == nil
}

// ValidateMaterialReadyEvent 同时用于网络与磁盘恢复，不能从异常字段拼出完成提示。
func ValidateMaterialReadyEvent(event MaterialReadyEvent) bool {
	sequence, sequenceOK := MaterialSequence(event.Sequence)
	revision, revisionOK := MaterialSequence(event.PlanRevision)
	occurred, timeErr := time.Parse(time.RFC3339Nano, event.OccurredAt)
	if !sequenceOK || sequence == 0 || !revisionOK || revision < 2 ||
		!queueContextPattern.MatchString(event.BusinessEventID) || !queueIdentityPattern.MatchString(event.OperationID) ||
		!queueContextPattern.MatchString(event.ExecutionContext) || !queueIdentityPattern.MatchString(event.PlanID) ||
		!queueUIDPattern.MatchString(event.Role.UID) || !queueRegionPattern.MatchString(event.Role.Region) ||
		!queueText(event.ConfigName, 128, false) || strings.TrimSpace(event.ConfigName) != event.ConfigName ||
		strings.ContainsAny(event.ConfigName, `<>:"/\|?*`) || strings.HasSuffix(event.ConfigName, ".") ||
		event.ConfigName == "." || event.ConfigName == ".." || timeErr != nil || occurred.IsZero() {
		return false
	}
	switch event.Kind {
	case MaterialEntryReady:
		return queueIdentityPattern.MatchString(event.EntryID) && event.CharacterID > 0 && int64(event.CharacterID) <= 2147483647 &&
			queueText(event.CharacterName, 64, false)
	case MaterialQueueReady:
		return event.EntryID == "" && event.CharacterID == 0 && event.CharacterName == ""
	default:
		return false
	}
}

// ValidateMaterialsReadyPage 绑定请求代次与执行范围，并拒绝逆序、重复或不能推进的续页。
func ValidateMaterialsReadyPage(page MaterialsReadyPage, instanceID string, query *MaterialsReadyQuery) error {
	cursor, cursorOK := MaterialSequence(page.Cursor)
	if page.SchemaVersion != 1 || page.InstanceID != instanceID || !queueIdentityPattern.MatchString(page.InstanceID) ||
		!queueContextPattern.MatchString(page.ExecutionContext) || !queueIdentityPattern.MatchString(page.Generation) ||
		!cursorOK || page.Events == nil || len(page.Events) > 100 || query == nil && page.HasMore {
		return queueInvalid("材料结果的实例、范围、分页或版本无效")
	}
	var previous uint64
	if query != nil {
		var valid bool
		previous, valid = MaterialSequence(query.After)
		if !valid || page.Generation != query.Generation || page.ExecutionContext != query.ExecutionContext || page.Truncated || cursor < previous {
			return queueInvalid("材料结果的接收水位或执行范围不匹配")
		}
		if page.HasMore && len(page.Events) != 100 {
			return queueInvalid("材料结果续页没有提供完整分页")
		}
	} else {
		// 原子基线始终覆盖接收头部的最后一百条，不能把缺失页当作完整历史。
		count := min(cursor, 100)
		if uint64(len(page.Events)) != count || page.Truncated != (cursor > 100) {
			return queueInvalid("材料历史基线存在缺失记录")
		}
		previous = cursor - count
	}
	seen := map[string]bool{}
	for _, event := range page.Events {
		sequence, _ := MaterialSequence(event.Sequence)
		if !ValidateMaterialReadyEvent(event) || event.ExecutionContext != page.ExecutionContext ||
			sequence != previous+1 || sequence > cursor || seen[event.BusinessEventID] {
			return queueInvalid("材料结果的业务身份或事件顺序无效")
		}
		previous = sequence
		seen[event.BusinessEventID] = true
	}
	if query != nil && (previous != cursor || page.HasMore && len(page.Events) == 0) {
		return queueInvalid("材料结果续页没有提供连续接收水位")
	}
	return nil
}

// validateMaterialsJSON 在反序列化前要求明确字段与类型，拒绝重复字段和空值降级。
func validateMaterialsJSON(raw []byte) bool {
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
			"role", "kind", "entryId", "characterId", "characterName", "planRevision", "occurredAt")
		if !ok || !queueJSONIntegers(event, "characterId") || !queueJSONStrings(event, false, "sequence", "businessEventId",
			"operationId", "executionContext", "planId", "configName", "kind", "entryId", "characterName", "planRevision", "occurredAt") {
			return false
		}
		role, ok := queueJSONFields(event["role"], "uid", "region")
		if !ok || !queueJSONStrings(role, false, "uid", "region") {
			return false
		}
	}
	return true
}

// ReadMaterialsReady 仅访问固定只读路由，先校验再脱敏，之后再次核对身份不被替换破坏。
func (a *PetV1Adapter) ReadMaterialsReady(ctx context.Context, query *MaterialsReadyQuery) (MaterialsReadyPage, error) {
	if !a.caps.MaterialsReadyEvents {
		return MaterialsReadyPage{}, unsupported()
	}
	values := url.Values{}
	if query != nil {
		if _, ok := MaterialSequence(query.After); !ok || !queueIdentityPattern.MatchString(query.Generation) || !queueContextPattern.MatchString(query.ExecutionContext) {
			return MaterialsReadyPage{}, &APIError{Code: "INVALID_ARGUMENT", Message: "材料结果的续读参数无效"}
		}
		values.Set("after", query.After)
		values.Set("generation", query.Generation)
		values.Set("executionContext", query.ExecutionContext)
	}
	var response envelope
	if err := a.client.requestQuery(ctx, http.MethodGet, "/api/desktop-pet/v1/cultivation-material-events", values, nil, &response); err != nil {
		return MaterialsReadyPage{}, err
	}
	if response.Code != "OK" || !validateMaterialsJSON(response.Data) {
		return MaterialsReadyPage{}, queueInvalid("材料结果响应结构不兼容")
	}
	var page MaterialsReadyPage
	if json.Unmarshal(response.Data, &page) != nil {
		return MaterialsReadyPage{}, queueInvalid("材料结果无法安全解析")
	}
	if err := ValidateMaterialsReadyPage(page, a.instanceID, query); err != nil {
		return MaterialsReadyPage{}, err
	}
	// 逐字段处理原始字符串，带引号或转义字符的密钥也不会绕过脱敏。
	for _, value := range []*string{&page.InstanceID, &page.ExecutionContext, &page.Generation, &page.Cursor} {
		*value = a.client.redact(*value)
	}
	for index := range page.Events {
		event := &page.Events[index]
		for _, value := range []*string{&event.Sequence, &event.BusinessEventID, &event.OperationID, &event.ExecutionContext,
			&event.PlanID, &event.ConfigName, &event.Role.UID, &event.Role.Region, &event.Kind, &event.EntryID,
			&event.CharacterName, &event.PlanRevision, &event.OccurredAt} {
			*value = a.client.redact(*value)
		}
	}
	if err := ValidateMaterialsReadyPage(page, a.instanceID, query); err != nil {
		return MaterialsReadyPage{}, err
	}
	return page, nil
}
