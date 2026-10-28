package storage

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"unicode/utf8"
)

// uniqueJournalJSON 拒绝嵌套重复字段及大小写别名，避免后出现的值覆盖先前已经检查的状态。
func uniqueJournalJSON(data []byte) bool {
	if !utf8.Valid(data) {
		return false
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var value func(int) bool
	value = func(depth int) bool {
		if depth > 32 {
			return false
		}
		token, err := decoder.Token()
		if err != nil {
			return false
		}
		switch token {
		case json.Delim('{'):
			seen := map[string]bool{}
			for decoder.More() {
				token, err := decoder.Token()
				key, ok := token.(string)
				canonical := strings.ToLower(key)
				if err != nil || !ok || seen[canonical] || !value(depth+1) {
					return false
				}
				seen[canonical] = true
			}
			end, err := decoder.Token()
			return err == nil && end == json.Delim('}')
		case json.Delim('['):
			for decoder.More() {
				if !value(depth + 1) {
					return false
				}
			}
			end, err := decoder.Token()
			return err == nil && end == json.Delim(']')
		default:
			_, delimiter := token.(json.Delim)
			return !delimiter
		}
	}
	if !value(0) {
		return false
	}
	_, err := decoder.Token()
	return err == io.EOF
}

// notificationJSONObject 对新增持久字段使用精确键名，缺少处理状态或未知字段均不能猜测恢复。
func notificationJSONObject(data []byte, keys ...string) (map[string]json.RawMessage, bool) {
	var fields map[string]json.RawMessage
	if json.Unmarshal(data, &fields) != nil || fields == nil || len(fields) != len(keys) {
		return nil, false
	}
	for _, key := range keys {
		if _, ok := fields[key]; !ok {
			return nil, false
		}
	}
	return fields, true
}

// notificationJSONString 区分明确空字符串与 null，只有可选时间及账号允许使用 null。
func notificationJSONString(data []byte, nullable bool) bool {
	if bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		return nullable
	}
	var value string
	return json.Unmarshal(data, &value) == nil
}

// notificationJSONBool 不让 null 被 encoding/json 静默解释为 false。
func notificationJSONBool(data []byte) bool {
	data = bytes.TrimSpace(data)
	return bytes.Equal(data, []byte("true")) || bytes.Equal(data, []byte("false"))
}

// notificationJSONUint32 拒绝 null、指数和小数，计数不能被宽松解码成零或截断。
func notificationJSONUint32(data []byte) bool {
	data = bytes.TrimSpace(data)
	var value uint32
	return len(data) > 0 && data[0] >= '0' && data[0] <= '9' && json.Unmarshal(data, &value) == nil
}

// notificationJSONErrorSummary 仅允许明确 null 或完整四字段对象，版本和计数不能缺省猜测。
func notificationJSONErrorSummary(data []byte) bool {
	if bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		return true
	}
	fields, ok := notificationJSONObject(data, "revision", "unreadCount", "quietPending", "latestAt")
	if !ok || !notificationJSONString(fields["revision"], false) || !notificationJSONUint32(fields["unreadCount"]) ||
		!notificationJSONUint32(fields["quietPending"]) || !notificationJSONString(fields["latestAt"], true) {
		return false
	}
	var revision string
	if json.Unmarshal(fields["revision"], &revision) != nil {
		return false
	}
	value, valid := notificationRevision(revision)
	return valid && value != 0
}

// validNotificationJSON 仅允许整个新增字段缺失的旧版迁移，已存在的通知结构必须完整且类型明确。
func validNotificationJSON(data []byte) bool {
	var root map[string]json.RawMessage
	if json.Unmarshal(data, &root) != nil || root == nil {
		return false
	}
	for key := range root {
		switch key {
		case "schemaVersion", "commands", "scopes", "materialScopes", "targetScopes", "notificationScopes", "notificationErrorRevision":
		default:
			return false
		}
	}
	// 只有整个目标字段缺失才按旧版迁移；已存在的目标正文不能靠零值绕过严格结构检查。
	if targets, exists := root["targetScopes"]; exists && !validTargetScopesJSON(targets) {
		return false
	}
	if rawRevision, exists := root["notificationErrorRevision"]; exists {
		if !notificationJSONString(rawRevision, false) {
			return false
		}
		var revision string
		if json.Unmarshal(rawRevision, &revision) != nil {
			return false
		}
		if _, valid := notificationRevision(revision); !valid {
			return false
		}
	}
	// 旧命令缺少终态提示标记时保持未处理，明确提供的 null 不能伪装成 false。
	if rawCommands, ok := root["commands"]; ok {
		var commands []map[string]json.RawMessage
		if json.Unmarshal(rawCommands, &commands) != nil {
			return false
		}
		for _, command := range commands {
			for key := range command {
				if strings.EqualFold(key, "notificationHandled") && key != "notificationHandled" {
					return false
				}
			}
			if handled, exists := command["notificationHandled"]; exists && !notificationJSONBool(handled) {
				return false
			}
		}
	}
	raw, exists := root["notificationScopes"]
	if !exists {
		return true
	}
	var scopes []json.RawMessage
	if json.Unmarshal(raw, &scopes) != nil || scopes == nil {
		return false
	}
	for _, rawScope := range scopes {
		keys := []string{"profileId", "baseUrl", "sourceMode", "instanceId", "accountId", "entries", "quietPending", "truncated"}
		var rawFields map[string]json.RawMessage
		if json.Unmarshal(rawScope, &rawFields) != nil {
			return false
		}
		// 旧版没有摘要字段时保留未知边界；已经存在的摘要必须完整校验。
		if summary, exists := rawFields["errorSummary"]; exists {
			if !notificationJSONErrorSummary(summary) {
				return false
			}
			keys = append(keys, "errorSummary")
		}
		scope, ok := notificationJSONObject(rawScope, keys...)
		if !ok {
			return false
		}
		for _, key := range []string{"profileId", "baseUrl", "sourceMode", "instanceId"} {
			if !notificationJSONString(scope[key], false) {
				return false
			}
		}
		if !notificationJSONString(scope["accountId"], true) || !notificationJSONBool(scope["truncated"]) {
			return false
		}
		if !notificationJSONUint32(scope["quietPending"]) {
			return false
		}
		var entries []json.RawMessage
		if json.Unmarshal(scope["entries"], &entries) != nil || entries == nil {
			return false
		}
		for _, rawEntry := range entries {
			entry, ok := notificationJSONObject(rawEntry, "id", "sourceKey", "kind", "message", "createdAt", "attemptedAt", "readAt", "quietPending")
			if !ok || !notificationJSONBool(entry["quietPending"]) {
				return false
			}
			for _, key := range []string{"id", "sourceKey", "kind", "message", "createdAt"} {
				if !notificationJSONString(entry[key], false) {
					return false
				}
			}
			if !notificationJSONString(entry["attemptedAt"], true) || !notificationJSONString(entry["readAt"], true) {
				return false
			}
		}
	}
	return true
}

// decodeJournal 在强类型解码之前检查重复键与新增字段结构，迁移不改写磁盘原文件。
func decodeJournal(data []byte) (Journal, bool) {
	if !uniqueJournalJSON(data) || !validNotificationJSON(data) {
		return Journal{}, false
	}
	var journal Journal
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&journal) != nil {
		return Journal{}, false
	}
	// 只有磁盘中完全缺少计数器的旧日志才迁移为零，显式空值已由结构检查拒绝。
	if journal.NotificationErrorRevision == "" {
		journal.NotificationErrorRevision = "0"
	}
	if !validJournal(journal) {
		return Journal{}, false
	}
	if journal.NotificationScopes == nil {
		journal.NotificationScopes = []NotificationScope{}
	}
	if journal.TargetScopes == nil {
		journal.TargetScopes = []TargetScope{}
	}
	return journal, true
}
