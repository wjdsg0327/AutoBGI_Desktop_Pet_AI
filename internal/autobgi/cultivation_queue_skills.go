package autobgi

import (
	"bytes"
	"encoding/json"
)

// CultivationQueueTargetSkills 只描述已保存的元素和 A/E/Q 实际技能标识，不携带临时确认凭证。
type CultivationQueueTargetSkills struct {
	Element  string  `json:"element"`
	SkillIDs []int64 `json:"skillIds"`
}

// CultivationQueueSchemaVersion 只有队列和技能目标能力同时声明时才允许显式请求新版。
func CultivationQueueSchemaVersion(caps Capabilities) int {
	if caps.CultivationQueue && caps.CultivationQueueSkillTargets {
		return 2
	}
	return 1
}

// validateQueueTargetSkills 对未知绑定保留空值，对实际标识仅执行固定合同校验。
func validateQueueTargetSkills(value *CultivationQueueTargetSkills) bool {
	if value == nil {
		return true
	}
	switch value.Element {
	case "pyro", "hydro", "anemo", "electro", "dendro", "cryo", "geo":
	default:
		return false
	}
	if len(value.SkillIDs) != 3 {
		return false
	}
	seen := map[int64]bool{}
	for _, id := range value.SkillIDs {
		if id < 1 || id > 9007199254740991 || seen[id] {
			return false
		}
		seen[id] = true
	}
	return true
}

// validateQueueTargetSkillsJSON 要求显式 null 或完整精确对象，拒绝缺省、部分、别名和额外证明字段。
func validateQueueTargetSkillsJSON(raw []byte) bool {
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return true
	}
	fields, ok := queueJSONFields(raw, "element", "skillIds")
	if !ok || !queueJSONStrings(fields, false, "element") {
		return false
	}
	ids, ok := queueJSONArray(fields["skillIds"])
	if !ok || len(ids) != 3 {
		return false
	}
	// 逐项解码整数，不能让 null、浮点或指数写法被默认值掩盖。
	for _, rawID := range ids {
		if !queueJSONIntegers(map[string]json.RawMessage{"id": rawID}, "id") {
			return false
		}
	}
	var parsed CultivationQueueTargetSkills
	return json.Unmarshal(raw, &parsed) == nil && validateQueueTargetSkills(&parsed)
}

// MarshalJSON 在 schema2 为每项保留显式空绑定，同时使 schema1 维持旧字段集合。
func (result CultivationQueueResult) MarshalJSON() ([]byte, error) {
	type plain CultivationQueueResult
	if result.SchemaVersion != 2 {
		return json.Marshal(plain(result))
	}
	// 外层同名字段覆盖嵌入字段，仅调整新版绑定的空值编码，不修改原始切片或版本。
	type skillEntry struct {
		CultivationQueueEntry
		TargetSkills *CultivationQueueTargetSkills `json:"targetSkills"`
	}
	type skillPlan struct {
		CultivationQueuePlan
		Entries []skillEntry `json:"entries"`
	}
	var entries []skillEntry
	if result.Plan.Entries != nil {
		entries = make([]skillEntry, len(result.Plan.Entries))
		for index, entry := range result.Plan.Entries {
			entries[index] = skillEntry{CultivationQueueEntry: entry, TargetSkills: entry.TargetSkills}
		}
	}
	return json.Marshal(struct {
		plain
		Plan skillPlan `json:"plan"`
	}{plain: plain(result), Plan: skillPlan{CultivationQueuePlan: result.Plan, Entries: entries}})
}
