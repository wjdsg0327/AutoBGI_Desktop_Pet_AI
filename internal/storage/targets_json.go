package storage

import "encoding/json"

// validTargetScopesJSON 单独限制目标历史格式，旧材料日志不会被解释为新事件。
func validTargetScopesJSON(data []byte) bool {
	var scopes []json.RawMessage
	if json.Unmarshal(data, &scopes) != nil || scopes == nil || len(scopes) > TargetScopeLimit {
		return false
	}
	for _, rawScope := range scopes {
		scope, ok := notificationJSONObject(rawScope, "profileId", "baseUrl", "instanceId", "executionContext",
			"generation", "cursor", "truncated", "history", "seen")
		if !ok || !notificationJSONBool(scope["truncated"]) {
			return false
		}
		for _, key := range []string{"profileId", "baseUrl", "instanceId", "executionContext", "generation", "cursor"} {
			if !notificationJSONString(scope[key], false) {
				return false
			}
		}
		var history, seen []json.RawMessage
		if json.Unmarshal(scope["history"], &history) != nil || history == nil || len(history) > TargetHistoryLimit ||
			json.Unmarshal(scope["seen"], &seen) != nil || seen == nil || len(seen) > TargetSeenLimit {
			return false
		}
		for _, event := range history {
			if !validTargetEventJSON(event) {
				return false
			}
		}
		for _, rawSeen := range seen {
			item, ok := notificationJSONObject(rawSeen, "businessEventId", "fingerprint", "occurredAt", "presentationHandled")
			if !ok || !notificationJSONBool(item["presentationHandled"]) {
				return false
			}
			for _, key := range []string{"businessEventId", "fingerprint", "occurredAt"} {
				if !notificationJSONString(item[key], false) {
					return false
				}
			}
		}
	}
	return true
}

// validTargetEventJSON 要求观察时间与提交时间分别存在，不能用 null 或省略字段降级。
func validTargetEventJSON(data []byte) bool {
	event, ok := notificationJSONObject(data, "sequence", "businessEventId", "operationId", "executionContext",
		"planId", "configName", "role", "kind", "entryId", "characterId", "characterName", "planRevision", "observedAt", "occurredAt")
	if !ok || !notificationJSONUint32(event["characterId"]) {
		return false
	}
	for _, key := range []string{"sequence", "businessEventId", "operationId", "executionContext", "planId",
		"configName", "kind", "entryId", "characterName", "planRevision", "observedAt", "occurredAt"} {
		if !notificationJSONString(event[key], false) {
			return false
		}
	}
	role, ok := notificationJSONObject(event["role"], "uid", "region")
	return ok && notificationJSONString(role["uid"], false) && notificationJSONString(role["region"], false)
}
