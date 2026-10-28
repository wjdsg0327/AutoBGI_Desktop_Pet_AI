package service

import "autobgi-pet/internal/autobgi"

// CultivationQueueDisplayEntry 使用具象字段生成 Wails 类型，双指针仅区分旧版省略与新版显式空绑定。
type CultivationQueueDisplayEntry struct {
	ID                     string                                 `json:"id"`
	CharacterID            int                                    `json:"characterId"`
	Name                   string                                 `json:"name"`
	AddedAt                string                                 `json:"addedAt"`
	TargetLevel            int                                    `json:"targetLevel"`
	TargetTalents          []int                                  `json:"targetTalents"`
	TargetSkills           **autobgi.CultivationQueueTargetSkills `json:"targetSkills,omitempty"`
	CurrentLevel           *int                                   `json:"currentLevel"`
	CurrentTalents         []int                                  `json:"currentTalents"`
	PanelUpdatedAt         *string                                `json:"panelUpdatedAt"`
	RequirementsAvailable  bool                                   `json:"requirementsAvailable"`
	RequirementSource      string                                 `json:"requirementSource"`
	RequestFingerprint     *string                                `json:"requestFingerprint"`
	RequirementsSourceTime *string                                `json:"requirementsSourceTime"`
	RequirementsFetchedAt  *string                                `json:"requirementsFetchedAt"`
	Materials              []autobgi.CultivationQueueMaterial     `json:"materials"`
	Warning                string                                 `json:"warning"`
}

// CultivationQueueDisplayPlan 保留已验证队列顺序和十进制版本，不引入新业务状态。
type CultivationQueueDisplayPlan struct {
	Revision  string                         `json:"revision"`
	Enabled   bool                           `json:"enabled"`
	Role      autobgi.CultivationQueueRole   `json:"role"`
	UpdatedAt *string                        `json:"updatedAt"`
	Entries   []CultivationQueueDisplayEntry `json:"entries"`
}

// CultivationQueueDisplayResult 没有自定义序列化方法，前端类型始终来自正式具象 Go 合同。
type CultivationQueueDisplayResult struct {
	SchemaVersion             int                                          `json:"schemaVersion"`
	InstanceID                string                                       `json:"instanceId"`
	Scope                     string                                       `json:"scope"`
	PresetID                  string                                       `json:"presetId"`
	PresetName                string                                       `json:"presetName"`
	ExecutionContext          string                                       `json:"executionContext"`
	Plan                      CultivationQueueDisplayPlan                  `json:"plan"`
	Status                    string                                       `json:"status"`
	CurrentEntryID            *string                                      `json:"currentEntryId"`
	ExecutionAvailable        bool                                         `json:"executionAvailable"`
	CompletionEventsAvailable bool                                         `json:"completionEventsAvailable"`
	InventoryVerified         bool                                         `json:"inventoryVerified"`
	Warnings                  []string                                     `json:"warnings"`
	InventoryCollection       *autobgi.CultivationQueueInventoryCollection `json:"inventoryCollection,omitempty"`
}

// cultivationQueueDisplay 只投影已经校验和脱敏的结果，不改写协议版本或重新解释技能身份。
func cultivationQueueDisplay(result autobgi.CultivationQueueResult) *CultivationQueueDisplayResult {
	entries := make([]CultivationQueueDisplayEntry, len(result.Plan.Entries))
	for index, entry := range result.Plan.Entries {
		var binding **autobgi.CultivationQueueTargetSkills
		if result.SchemaVersion == 2 {
			target := entry.TargetSkills
			binding = &target
		}
		entries[index] = CultivationQueueDisplayEntry{
			ID: entry.ID, CharacterID: entry.CharacterID, Name: entry.Name, AddedAt: entry.AddedAt,
			TargetLevel: entry.TargetLevel, TargetTalents: entry.TargetTalents, TargetSkills: binding,
			CurrentLevel: entry.CurrentLevel, CurrentTalents: entry.CurrentTalents, PanelUpdatedAt: entry.PanelUpdatedAt,
			RequirementsAvailable: entry.RequirementsAvailable, RequirementSource: entry.RequirementSource,
			RequestFingerprint: entry.RequestFingerprint, RequirementsSourceTime: entry.RequirementsSourceTime,
			RequirementsFetchedAt: entry.RequirementsFetchedAt, Materials: entry.Materials, Warning: entry.Warning,
		}
	}
	return &CultivationQueueDisplayResult{
		SchemaVersion: result.SchemaVersion, InstanceID: result.InstanceID, Scope: result.Scope,
		PresetID: result.PresetID, PresetName: result.PresetName, ExecutionContext: result.ExecutionContext,
		Plan:   CultivationQueueDisplayPlan{Revision: result.Plan.Revision, Enabled: result.Plan.Enabled, Role: result.Plan.Role, UpdatedAt: result.Plan.UpdatedAt, Entries: entries},
		Status: result.Status, CurrentEntryID: result.CurrentEntryID, ExecutionAvailable: result.ExecutionAvailable,
		CompletionEventsAvailable: result.CompletionEventsAvailable, InventoryVerified: result.InventoryVerified,
		Warnings: result.Warnings, InventoryCollection: result.InventoryCollection,
	}
}
