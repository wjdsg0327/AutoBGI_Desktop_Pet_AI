package autobgi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// 独立队列只接受生产服务发布的固定标识，不能用名称、路径或角色选择器代替。
var (
	queuePresetPattern      = regexp.MustCompile(`^preset-[0-9a-f]{32}$`)
	queueIdentityPattern    = regexp.MustCompile(`^[0-9a-f]{32}$`)
	queueContextPattern     = regexp.MustCompile(`^[0-9a-f]{64}$`)
	queueFingerprintPattern = regexp.MustCompile(`^(sha256|panel-sha256):[0-9a-f]{64}$`)
	queueUIDPattern         = regexp.MustCompile(`^[0-9]{1,32}$`)
	queueRegionPattern      = regexp.MustCompile(`^[A-Za-z0-9_-]{1,32}$`)
	queueRevisionPattern    = regexp.MustCompile(`^(0|[1-9][0-9]{0,18})$`)
)

// CultivationQueueReader 只扩展独立只读查询，不修改旧领域接口和执行身份。
type CultivationQueueReader interface {
	ReadCultivationQueue(context.Context, string) (CultivationQueueResult, error)
}

// CultivationQueueRole 仅描述计划绑定的资料归属，不代表当前游戏登录角色。
type CultivationQueueRole struct {
	UID    string `json:"uid"`
	Region string `json:"region"`
}

// CultivationQueueMaterial 保留未知库存，不能将 null 转换为零或推断培养已经完成。
type CultivationQueueMaterial struct {
	MaterialID int    `json:"materialId"`
	Name       string `json:"name"`
	Required   int64  `json:"required"`
	Owned      *int64 `json:"owned"`
	Missing    *int64 `json:"missing"`
	Excluded   bool   `json:"excluded"`
}

// CultivationQueueEntry 按持久队列顺序展示明确目标和缓存需求，当前资料缺失时保持 null。
type CultivationQueueEntry struct {
	ID                     string                        `json:"id"`
	CharacterID            int                           `json:"characterId"`
	Name                   string                        `json:"name"`
	AddedAt                string                        `json:"addedAt"`
	TargetLevel            int                           `json:"targetLevel"`
	TargetTalents          []int                         `json:"targetTalents"`
	TargetSkills           *CultivationQueueTargetSkills `json:"targetSkills,omitempty"`
	CurrentLevel           *int                          `json:"currentLevel"`
	CurrentTalents         []int                         `json:"currentTalents"`
	PanelUpdatedAt         *string                       `json:"panelUpdatedAt"`
	RequirementsAvailable  bool                          `json:"requirementsAvailable"`
	RequirementSource      string                        `json:"requirementSource"`
	RequestFingerprint     *string                       `json:"requestFingerprint"`
	RequirementsSourceTime *string                       `json:"requirementsSourceTime"`
	RequirementsFetchedAt  *string                       `json:"requirementsFetchedAt"`
	Materials              []CultivationQueueMaterial    `json:"materials"`
	Warning                string                        `json:"warning"`
}

// CultivationQueuePlan 的版本使用十进制字符串，避免 JavaScript 丢失大版本整数精度。
type CultivationQueuePlan struct {
	Revision  string                  `json:"revision"`
	Enabled   bool                    `json:"enabled"`
	Role      CultivationQueueRole    `json:"role"`
	UpdatedAt *string                 `json:"updatedAt"`
	Entries   []CultivationQueueEntry `json:"entries"`
}

// CultivationQueueResult 明确保留不可执行能力，空队列也不能被展示为培养完成。
type CultivationQueueResult struct {
	SchemaVersion             int                                  `json:"schemaVersion"`
	InstanceID                string                               `json:"instanceId"`
	Scope                     string                               `json:"scope"`
	PresetID                  string                               `json:"presetId"`
	PresetName                string                               `json:"presetName"`
	ExecutionContext          string                               `json:"executionContext"`
	Plan                      CultivationQueuePlan                 `json:"plan"`
	Status                    string                               `json:"status"`
	CurrentEntryID            *string                              `json:"currentEntryId"`
	ExecutionAvailable        bool                                 `json:"executionAvailable"`
	CompletionEventsAvailable bool                                 `json:"completionEventsAvailable"`
	InventoryVerified         bool                                 `json:"inventoryVerified"`
	Warnings                  []string                             `json:"warnings"`
	InventoryCollection       *CultivationQueueInventoryCollection `json:"inventoryCollection,omitempty"`
}

// CultivationQueueInventoryCollection 只保留本次手动库存观察，不替代培养缺口或已核验库存。
type CultivationQueueInventoryCollection struct {
	SchemaVersion int                                 `json:"schemaVersion"`
	Status        string                              `json:"status"`
	RequestID     *string                             `json:"requestId"`
	ScriptVersion *string                             `json:"scriptVersion"`
	CreatedAt     *string                             `json:"createdAt"`
	ExpiresAt     *string                             `json:"expiresAt"`
	SourceTime    *string                             `json:"sourceTime"`
	FinishedAt    *string                             `json:"finishedAt"`
	Materials     []CultivationQueueInventoryMaterial `json:"materials"`
	Warning       string                              `json:"warning"`
}

// CultivationQueueInventoryMaterial 用空数量保留识别失败事实，不将未知库存默认为零。
type CultivationQueueInventoryMaterial struct {
	MaterialID int    `json:"materialId"`
	Name       string `json:"name"`
	Known      bool   `json:"known"`
	Count      *int64 `json:"count"`
	Reason     string `json:"reason"`
}

// queueInvalid 始终返回本地固定说明，错误中不回显远端 UID、名称或密钥反射内容。
func queueInvalid(message string) error {
	return &APIError{Code: "INVALID_RESPONSE", Message: message}
}

// ValidCultivationQueuePreset 供适配器和服务层共同拒绝路径或不完整的预设选择器。
func ValidCultivationQueuePreset(id string) bool { return queuePresetPattern.MatchString(id) }

// queueText 拒绝控制字符和过大展示文本，不让异常远端内容进入界面或日志。
func queueText(value string, maxRunes int, empty bool) bool {
	if !utf8.ValidString(value) || len(value) > maxRunes*4 || utf8.RuneCountInString(value) > maxRunes || !empty && strings.TrimSpace(value) == "" {
		return false
	}
	return strings.IndexFunc(value, unicode.IsControl) < 0
}

// queueTime 只校验真正提供的源时间，不使用桌宠请求时间填补 null。
func queueTime(value *string) bool {
	if value == nil {
		return true
	}
	parsed, err := time.Parse(time.RFC3339Nano, *value)
	return err == nil && !parsed.IsZero()
}

// queueTalents 同时用于明确目标与可含命座加成的实际天赋，严格要求三项。
func queueTalents(values []int, maximum int) bool {
	if len(values) != 3 {
		return false
	}
	for _, value := range values {
		if value < 1 || value > maximum {
			return false
		}
	}
	return true
}

// ValidateCultivationQueue 同时保护适配器和模拟读取器边界，不信任未验证的结构化 DTO。
func ValidateCultivationQueue(result CultivationQueueResult, instanceID, presetID string) error {
	return ValidateCultivationQueueSchema(result, instanceID, presetID, 1)
}

// ValidateCultivationQueueSchema 绑定请求时协商的版本，不能通过改写版本接受另一份协议。
func ValidateCultivationQueueSchema(result CultivationQueueResult, instanceID, presetID string, expectedSchema int) error {
	if expectedSchema != 1 && expectedSchema != 2 || result.SchemaVersion != expectedSchema ||
		!queueIdentityPattern.MatchString(result.InstanceID) || result.InstanceID != instanceID ||
		result.Scope != "instance-config-queue" || !ValidCultivationQueuePreset(presetID) || result.PresetID != presetID ||
		!queueContextPattern.MatchString(result.ExecutionContext) || !queueText(result.PresetName, 128, false) {
		return queueInvalid("培养队列的实例、预设或执行范围不匹配")
	}
	if result.ExecutionAvailable || result.CompletionEventsAvailable || result.InventoryVerified ||
		result.Plan.Entries == nil || len(result.Plan.Entries) > 32 || result.Warnings == nil || len(result.Warnings) > 32 ||
		!validateQueueInventoryCollection(result.InventoryCollection) {
		return queueInvalid("培养队列能力或数量不符合只读合同")
	}
	for _, warning := range result.Warnings {
		if !queueText(warning, 1024, true) {
			return queueInvalid("培养队列提示文本无效")
		}
	}
	plan := result.Plan
	revision, err := strconv.ParseUint(plan.Revision, 10, 63)
	if err != nil || !queueRevisionPattern.MatchString(plan.Revision) || !queueTime(plan.UpdatedAt) {
		return queueInvalid("培养队列版本或更新时间无效")
	}
	emptyRole := plan.Role == (CultivationQueueRole{})
	if !emptyRole && (!queueUIDPattern.MatchString(plan.Role.UID) || !queueRegionPattern.MatchString(plan.Role.Region)) ||
		emptyRole && (plan.Enabled || len(plan.Entries) > 0) {
		return queueInvalid("培养队列绑定身份不完整")
	}
	if revision == 0 && (plan.Enabled || !emptyRole || len(plan.Entries) != 0 || plan.UpdatedAt != nil) ||
		revision > 0 && plan.UpdatedAt == nil {
		return queueInvalid("培养队列版本与持久状态不一致")
	}
	expectedStatus := "blocked"
	if len(plan.Entries) == 0 {
		expectedStatus = "empty"
		if result.CurrentEntryID != nil {
			return queueInvalid("空培养队列不能声明当前角色")
		}
	} else {
		if !plan.Enabled {
			expectedStatus = "disabled"
		}
		if result.CurrentEntryID == nil || *result.CurrentEntryID != plan.Entries[0].ID {
			return queueInvalid("培养队列当前角色与首项不一致")
		}
	}
	if result.Status != expectedStatus {
		return queueInvalid("培养队列状态与已保存计划不一致")
	}
	ids, characters := map[string]bool{}, map[int]bool{}
	for _, entry := range plan.Entries {
		// 旧版不能携带技能字段；新版的空值明确表示未绑定，不推断面板或任务结论。
		if expectedSchema == 1 && entry.TargetSkills != nil || expectedSchema == 2 && !validateQueueTargetSkills(entry.TargetSkills) {
			return queueInvalid("培养队列技能目标不符合协商版本")
		}
		added, timeErr := time.Parse(time.RFC3339Nano, entry.AddedAt)
		if !queueIdentityPattern.MatchString(entry.ID) || ids[entry.ID] || entry.CharacterID < 1 || int64(entry.CharacterID) > 9007199254740991 ||
			characters[entry.CharacterID] || !queueText(entry.Name, 64, false) || timeErr != nil || added.IsZero() ||
			entry.TargetLevel < 1 || entry.TargetLevel > 100 || !queueTalents(entry.TargetTalents, 10) ||
			!queueText(entry.Warning, 1024, true) || entry.Materials == nil || len(entry.Materials) > 128 {
			return queueInvalid("培养队列条目身份、目标、时间或数量无效")
		}
		ids[entry.ID], characters[entry.CharacterID] = true, true
		if plan.UpdatedAt != nil {
			updated, _ := time.Parse(time.RFC3339Nano, *plan.UpdatedAt)
			if added.After(updated) {
				return queueInvalid("培养队列加入时间晚于计划更新时间")
			}
		}
		if (entry.CurrentLevel == nil) != (entry.CurrentTalents == nil) || entry.CurrentLevel != nil &&
			(*entry.CurrentLevel < 1 || *entry.CurrentLevel > 100 || !queueTalents(entry.CurrentTalents, 15)) ||
			!queueTime(entry.PanelUpdatedAt) || !queueTime(entry.RequirementsSourceTime) || !queueTime(entry.RequirementsFetchedAt) {
			return queueInvalid("培养队列当前面板或来源时间无效")
		}
		if entry.RequestFingerprint != nil && !queueFingerprintPattern.MatchString(*entry.RequestFingerprint) {
			return queueInvalid("培养队列需求指纹无效")
		}
		if !entry.RequirementsAvailable {
			if entry.RequirementSource != "" || len(entry.Materials) != 0 || entry.RequirementsSourceTime != nil || entry.RequirementsFetchedAt != nil {
				return queueInvalid("未知培养需求不能包含材料或伪造来源")
			}
		} else {
			if entry.CurrentLevel == nil || entry.RequestFingerprint == nil {
				return queueInvalid("可用培养需求缺少完整面板或请求指纹")
			}
			switch entry.RequirementSource {
			case "official-calculator-cache":
				if !strings.HasPrefix(*entry.RequestFingerprint, "sha256:") {
					return queueInvalid("官方需求缓存的请求指纹不匹配")
				}
			case "panel-already-met":
				if !strings.HasPrefix(*entry.RequestFingerprint, "panel-sha256:") || len(entry.Materials) != 0 ||
					entry.RequirementsSourceTime != nil || entry.RequirementsFetchedAt != nil || *entry.CurrentLevel < entry.TargetLevel {
					return queueInvalid("面板达标结论的来源或需求无效")
				}
				for index, target := range entry.TargetTalents {
					if entry.CurrentTalents[index] < target {
						return queueInvalid("面板实际天赋尚未达到明确目标")
					}
				}
			default:
				return queueInvalid("培养需求来源不属于只读缓存合同")
			}
		}
		if entry.RequirementsSourceTime != nil && entry.RequirementsFetchedAt != nil {
			source, _ := time.Parse(time.RFC3339Nano, *entry.RequirementsSourceTime)
			fetched, _ := time.Parse(time.RFC3339Nano, *entry.RequirementsFetchedAt)
			if source.After(fetched) {
				return queueInvalid("培养需求源时间晚于获取时间")
			}
		}
		materials := map[int]bool{}
		hasPositive := false
		for _, material := range entry.Materials {
			if material.MaterialID < 1 || int64(material.MaterialID) > 9007199254740991 || materials[material.MaterialID] ||
				!queueText(material.Name, 128, false) || material.Required < 0 || material.Required > 9007199254740991 ||
				material.Owned != nil || material.Missing != nil || material.Excluded != (material.MaterialID == 202 || material.MaterialID == 104319) {
				return queueInvalid("培养材料身份、数量或未知库存合同无效")
			}
			materials[material.MaterialID] = true
			hasPositive = hasPositive || material.Required > 0
		}
		if entry.RequirementsAvailable {
			needsUpgrade := *entry.CurrentLevel < entry.TargetLevel
			for index, target := range entry.TargetTalents {
				needsUpgrade = needsUpgrade || entry.CurrentTalents[index] < target
			}
			if needsUpgrade && !hasPositive || !needsUpgrade && hasPositive {
				return queueInvalid("培养材料需求与当前面板目标不一致")
			}
		}
	}
	return nil
}

// validateQueueInventoryCollection 严格区分请求、完整观察与过期历史，不以客户端当前时间续期。
func validateQueueInventoryCollection(value *CultivationQueueInventoryCollection) bool {
	if value == nil {
		return true
	}
	if value.SchemaVersion != 1 || value.Materials == nil || len(value.Materials) > 4096 ||
		!queueText(value.Warning, 512, true) || strings.TrimSpace(value.Warning) != value.Warning {
		return false
	}
	// 时间长度与服务端一致，避免超长小数秒被解析器截断后看似合法。
	for _, stamp := range []*string{value.CreatedAt, value.ExpiresAt, value.SourceTime, value.FinishedAt} {
		if stamp != nil && len(*stamp) > 64 {
			return false
		}
	}
	if value.Status == "unavailable" {
		return value.RequestID == nil && value.ScriptVersion == nil && value.CreatedAt == nil && value.ExpiresAt == nil &&
			value.SourceTime == nil && value.FinishedAt == nil && len(value.Materials) == 0
	}
	if value.Status != "pending" && value.Status != "observed" && value.Status != "expired" ||
		value.RequestID == nil || !queueContextPattern.MatchString(*value.RequestID) ||
		value.ScriptVersion == nil || *value.ScriptVersion != "1.0.0" ||
		value.CreatedAt == nil || value.ExpiresAt == nil || !queueTime(value.CreatedAt) || !queueTime(value.ExpiresAt) ||
		!queueTime(value.SourceTime) || !queueTime(value.FinishedAt) || (value.SourceTime == nil) != (value.FinishedAt == nil) {
		return false
	}
	created, _ := time.Parse(time.RFC3339Nano, *value.CreatedAt)
	expires, _ := time.Parse(time.RFC3339Nano, *value.ExpiresAt)
	if expires.Sub(created) != 15*time.Minute {
		return false
	}
	if value.SourceTime == nil {
		return value.Status != "observed" && len(value.Materials) == 0
	}
	if value.Status == "pending" || len(value.Materials) == 0 {
		return false
	}
	source, _ := time.Parse(time.RFC3339Nano, *value.SourceTime)
	finished, _ := time.Parse(time.RFC3339Nano, *value.FinishedAt)
	if source.Before(created) || finished.Before(source) || finished.After(expires) {
		return false
	}
	ids, names := map[int]bool{}, map[string]bool{}
	for _, material := range value.Materials {
		if material.MaterialID <= 0 || int64(material.MaterialID) > 9007199254740991 ||
			!queueText(material.Name, 128, false) || strings.TrimSpace(material.Name) != material.Name ||
			ids[material.MaterialID] || names[material.Name] ||
			material.Known && (material.Count == nil || *material.Count < 0 || *material.Count > 9007199254740991 || material.Reason != "") ||
			!material.Known && (material.Count != nil || material.Reason != "not-returned" && material.Reason != "recognition-failed") {
			return false
		}
		ids[material.MaterialID], names[material.Name] = true, true
	}
	return true
}

// RedactCultivationQueue 在服务边界也复制并脱敏全部远端字符串，身份碰撞时固定拒绝而非改绑。
func (c *Client) RedactCultivationQueue(result CultivationQueueResult, instanceID, presetID string) (CultivationQueueResult, error) {
	return c.RedactCultivationQueueSchema(result, instanceID, presetID, 1)
}

// RedactCultivationQueueSchema 复制与脱敏均保留协商版本，新版空绑定仍输出显式 null。
func (c *Client) RedactCultivationQueueSchema(result CultivationQueueResult, instanceID, presetID string, expectedSchema int) (CultivationQueueResult, error) {
	if c == nil || c.key == "" {
		return CultivationQueueResult{}, queueInvalid("培养队列缺少当前连接的安全展示上下文")
	}
	if err := ValidateCultivationQueueSchema(result, instanceID, presetID, expectedSchema); err != nil {
		return CultivationQueueResult{}, err
	}
	data, err := json.Marshal(result)
	if err != nil {
		return CultivationQueueResult{}, queueInvalid("培养队列无法安全复制")
	}
	var value any
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if decoder.Decode(&value) != nil {
		return CultivationQueueResult{}, queueInvalid("培养队列无法安全复制")
	}
	data, err = json.Marshal(c.redactJSON(value))
	if err != nil {
		return CultivationQueueResult{}, queueInvalid("培养队列无法安全脱敏")
	}
	clean, err := decodeCultivationQueueJSON(data, expectedSchema)
	if err != nil {
		return CultivationQueueResult{}, queueInvalid("培养队列无法安全脱敏")
	}
	if err := ValidateCultivationQueueSchema(clean, instanceID, presetID, expectedSchema); err != nil {
		return CultivationQueueResult{}, err
	}
	return clean, nil
}

// ReadCultivationQueue 只访问明确能力下的 GET 路径，不拉面板、扫描背包或修改任何配置。
func (a *PetV1Adapter) ReadCultivationQueue(ctx context.Context, presetID string) (CultivationQueueResult, error) {
	if !a.caps.CultivationQueue {
		return CultivationQueueResult{}, unsupported()
	}
	if !ValidCultivationQueuePreset(presetID) {
		return CultivationQueueResult{}, &APIError{Code: "INVALID_ARGUMENT", Message: "培养队列预设标识无效"}
	}
	// 能力只影响本次显式协商，旧服务的请求查询参数完全保持原样。
	expectedSchema := CultivationQueueSchemaVersion(a.caps)
	query := url.Values{"presetId": {presetID}}
	if expectedSchema == 2 {
		query.Set("schemaVersion", "2")
	}
	var response envelope
	if err := a.client.requestQuery(ctx, http.MethodGet, "/api/desktop-pet/v1/cultivation-queue",
		query, nil, &response); err != nil {
		// 此独立路由的冲突和未找到使用固定合同，不让服务端异常正文进入错误。
		if apiErr, ok := err.(*APIError); ok {
			if apiErr.HTTPStatus == http.StatusConflict {
				return CultivationQueueResult{}, &APIError{Code: "CONTEXT_CHANGED", Message: "培养队列执行目录或预设已变化，请重新读取", HTTPStatus: http.StatusConflict}
			}
			if apiErr.HTTPStatus == http.StatusNotFound {
				return CultivationQueueResult{}, &APIError{Code: "PRESET_NOT_FOUND", Message: "所选一条龙预设已不存在，请重新读取列表", HTTPStatus: http.StatusNotFound}
			}
		}
		return CultivationQueueResult{}, err
	}
	if response.Code != "OK" {
		return CultivationQueueResult{}, queueInvalid("培养队列响应结构不兼容")
	}
	result, err := decodeCultivationQueueJSON(response.Data, expectedSchema)
	if err != nil {
		return CultivationQueueResult{}, err
	}
	return a.client.RedactCultivationQueueSchema(result, a.instanceID, presetID, expectedSchema)
}

// UnmarshalJSON 要求真实布尔值和显式 null 字段，不能用 Go 零值掩盖缺失的协议字段。
func (result *CultivationQueueResult) UnmarshalJSON(data []byte) error {
	parsed, err := decodeCultivationQueueJSON(data, 1)
	if err == nil {
		*result = parsed
	}
	return err
}

// decodeCultivationQueueJSON 为两个版本保留独立精确字段集合，调用者必须传入冻结的请求版本。
func decodeCultivationQueueJSON(data []byte, expectedSchema int) (CultivationQueueResult, error) {
	result := CultivationQueueResult{}
	if expectedSchema != 1 && expectedSchema != 2 {
		return result, queueInvalid("培养队列协商版本无效")
	}
	if !queueJSONUnique(data) {
		return result, queueInvalid("培养队列 JSON 包含重复字段或非法嵌套")
	}
	// 新增观察对象可以缺省或为 null；其余原有字段仍须精确匹配，兼容旧服务端不会放宽键检查。
	names := []string{"schemaVersion", "instanceId", "scope", "presetId", "presetName", "executionContext",
		"plan", "status", "currentEntryId", "executionAvailable", "completionEventsAvailable", "inventoryVerified", "warnings"}
	var fields map[string]json.RawMessage
	if json.Unmarshal(data, &fields) == nil {
		if _, present := fields["inventoryCollection"]; present {
			names = append(names, "inventoryCollection")
		}
	}
	top, ok := queueJSONFields(data, names...)
	if !ok || !queueJSONBooleans(top, "executionAvailable", "completionEventsAvailable", "inventoryVerified") ||
		!queueJSONStrings(top, false, "instanceId", "scope", "presetId", "presetName", "executionContext", "status") ||
		!queueJSONStrings(top, true, "currentEntryId") || !queueJSONIntegers(top, "schemaVersion") {
		return result, queueInvalid("培养队列缺少必需字段或真实能力布尔值")
	}
	var actualSchema int
	if json.Unmarshal(top["schemaVersion"], &actualSchema) != nil || actualSchema != expectedSchema {
		return result, queueInvalid("培养队列响应版本与请求协商不一致")
	}
	if raw, present := top["inventoryCollection"]; present && !bytes.Equal(bytes.TrimSpace(raw), []byte("null")) &&
		!validateQueueInventoryJSON(raw) {
		return result, queueInvalid("独立库存观察字段无效")
	}
	plan, ok := queueJSONFields(top["plan"], "revision", "enabled", "role", "updatedAt", "entries")
	if !ok || !queueJSONBooleans(plan, "enabled") || !queueJSONStrings(plan, false, "revision") || !queueJSONStrings(plan, true, "updatedAt") {
		return result, queueInvalid("培养队列计划字段不完整")
	}
	role, ok := queueJSONFields(plan["role"], "uid", "region")
	if !ok || !queueJSONStrings(role, false, "uid", "region") {
		return result, queueInvalid("培养队列角色字段不完整")
	}
	entries, ok := queueJSONArray(plan["entries"])
	if !ok || len(entries) > 32 {
		return result, queueInvalid("培养队列条目数组无效")
	}
	for _, raw := range entries {
		entryNames := []string{"id", "characterId", "name", "addedAt", "targetLevel", "targetTalents", "currentLevel",
			"currentTalents", "panelUpdatedAt", "requirementsAvailable", "requirementSource", "requestFingerprint",
			"requirementsSourceTime", "requirementsFetchedAt", "materials", "warning"}
		if expectedSchema == 2 {
			entryNames = append(entryNames, "targetSkills")
		}
		entry, ok := queueJSONFields(raw, entryNames...)
		if !ok || !queueJSONBooleans(entry, "requirementsAvailable") ||
			!queueJSONStrings(entry, false, "id", "name", "addedAt", "requirementSource", "warning") ||
			!queueJSONStrings(entry, true, "panelUpdatedAt", "requestFingerprint", "requirementsSourceTime", "requirementsFetchedAt") ||
			!queueJSONIntegers(entry, "characterId", "targetLevel") {
			return result, queueInvalid("培养队列条目字段不完整")
		}
		if expectedSchema == 2 && !validateQueueTargetSkillsJSON(entry["targetSkills"]) {
			return result, queueInvalid("培养队列技能目标字段无效")
		}
		materials, ok := queueJSONArray(entry["materials"])
		if !ok || len(materials) > 128 {
			return result, queueInvalid("培养队列材料数组无效")
		}
		for _, rawMaterial := range materials {
			material, ok := queueJSONFields(rawMaterial, "materialId", "name", "required", "owned", "missing", "excluded")
			if !ok || !queueJSONBooleans(material, "excluded") || !queueJSONStrings(material, false, "name") ||
				!queueJSONIntegers(material, "materialId", "required") || !bytes.Equal(bytes.TrimSpace(material["owned"]), []byte("null")) ||
				!bytes.Equal(bytes.TrimSpace(material["missing"]), []byte("null")) {
				return result, queueInvalid("培养队列材料字段或未知库存无效")
			}
		}
	}
	type plain CultivationQueueResult
	var parsed plain
	if err := json.Unmarshal(data, &parsed); err != nil {
		return result, queueInvalid("培养队列字段类型无效")
	}
	return CultivationQueueResult(parsed), nil
}

// validateQueueInventoryJSON 先核对完整字段及真实类型，避免 null 或缺失字段被补成可用观察。
func validateQueueInventoryJSON(raw []byte) bool {
	fields, ok := queueJSONFields(raw, "schemaVersion", "status", "requestId", "scriptVersion", "createdAt", "expiresAt",
		"sourceTime", "finishedAt", "materials", "warning")
	if !ok || !queueJSONIntegers(fields, "schemaVersion") || !queueJSONStrings(fields, false, "status", "warning") ||
		!queueJSONStrings(fields, true, "requestId", "scriptVersion", "createdAt", "expiresAt", "sourceTime", "finishedAt") {
		return false
	}
	materials, ok := queueJSONArray(fields["materials"])
	if !ok || len(materials) > 4096 {
		return false
	}
	for _, rawMaterial := range materials {
		material, ok := queueJSONFields(rawMaterial, "materialId", "name", "known", "count", "reason")
		if !ok || !queueJSONIntegers(material, "materialId") || !queueJSONStrings(material, false, "name", "reason") ||
			!queueJSONBooleans(material, "known") ||
			!bytes.Equal(bytes.TrimSpace(material["count"]), []byte("null")) && !queueJSONIntegers(material, "count") {
			return false
		}
	}
	var value CultivationQueueInventoryCollection
	return json.Unmarshal(raw, &value) == nil && validateQueueInventoryCollection(&value)
}

// queueJSONFields 要求对象字段集合精确匹配，拒绝大小写和 Unicode 字段别名。
func queueJSONFields(raw []byte, names ...string) (map[string]json.RawMessage, bool) {
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil || fields == nil || len(fields) != len(names) {
		return nil, false
	}
	for _, name := range names {
		if _, present := fields[name]; !present {
			return nil, false
		}
	}
	return fields, true
}

// queueJSONBooleans 明确拒绝遗漏、null、字符串或数值形式的布尔字段。
func queueJSONBooleans(fields map[string]json.RawMessage, names ...string) bool {
	for _, name := range names {
		raw := bytes.TrimSpace(fields[name])
		if !bytes.Equal(raw, []byte("true")) && !bytes.Equal(raw, []byte("false")) {
			return false
		}
	}
	return true
}

// queueJSONStrings 区分真正字符串与显式未知值，拒绝把 null 解码成空字符串。
func queueJSONStrings(fields map[string]json.RawMessage, nullable bool, names ...string) bool {
	for _, name := range names {
		raw := bytes.TrimSpace(fields[name])
		if nullable && bytes.Equal(raw, []byte("null")) {
			continue
		}
		if len(raw) == 0 || raw[0] != '"' {
			return false
		}
	}
	return true
}

// queueJSONIntegers 拒绝 null、浮点或指数形式的整数，不能静默转成零数量。
func queueJSONIntegers(fields map[string]json.RawMessage, names ...string) bool {
	for _, name := range names {
		raw := bytes.TrimSpace(fields[name])
		var value int64
		if len(raw) == 0 || bytes.Equal(raw, []byte("null")) || json.Unmarshal(raw, &value) != nil {
			return false
		}
	}
	return true
}

// queueJSONArray 将真实空数组与缺失或 null 分开，保留协议中的未知值语义。
func queueJSONArray(raw []byte) ([]json.RawMessage, bool) {
	raw = bytes.TrimSpace(raw)
	var items []json.RawMessage
	valid := len(raw) > 0 && raw[0] == '[' && json.Unmarshal(raw, &items) == nil
	return items, valid
}

// queueJSONUnique 在解码对象前检查重复键、有效 UTF-8 和深度，避免恶意正文产生不同解释。
func queueJSONUnique(data []byte) bool {
	if len(data) == 0 || len(data) > 2<<20 || !utf8.Valid(data) {
		return false
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var value func(int) bool
	value = func(depth int) bool {
		if depth > 12 {
			return false
		}
		token, err := decoder.Token()
		if err != nil {
			return false
		}
		delimiter, container := token.(json.Delim)
		if !container {
			return true
		}
		switch delimiter {
		case '{':
			seen := map[string]bool{}
			for decoder.More() {
				token, err := decoder.Token()
				key, valid := token.(string)
				if err != nil || !valid || seen[key] || !value(depth+1) {
					return false
				}
				seen[key] = true
			}
			end, err := decoder.Token()
			return err == nil && end == json.Delim('}')
		case '[':
			for decoder.More() {
				if !value(depth + 1) {
					return false
				}
			}
			end, err := decoder.Token()
			return err == nil && end == json.Delim(']')
		}
		return false
	}
	if !value(0) {
		return false
	}
	_, err := decoder.Token()
	return err == io.EOF
}
