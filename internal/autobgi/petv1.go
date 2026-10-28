package autobgi

import (
	"context"
	"encoding/json"
	"net/http"
	"regexp"
	"strings"
	"time"
)

// PetV1Adapter 只启用服务端明确声明并经本地实现的查询、幂等命令与事件能力。
type PetV1Adapter struct {
	readOnlyBoundary
	client     *Client
	instanceID string
	presets    bool
	caps       Capabilities
}

// envelope 严格要求成功标识，避免把业务错误误判为成功。
type envelope struct {
	Code string          `json:"code"`
	Data json.RawMessage `json:"data"`
}

// getData 丢弃服务端原始错误文本，防止凭据反射到界面。
func (a *PetV1Adapter) getData(ctx context.Context, route string, dest any) error {
	var result envelope
	if err := a.client.get(ctx, route, &result); err != nil {
		return err
	}
	if result.Code != "OK" || json.Unmarshal(result.Data, dest) != nil {
		return &APIError{Code: "INCOMPATIBLE_RESPONSE", Message: "桌宠协议响应结构不兼容"}
	}
	return nil
}

// Probe 只接受明确的 1.x 实例协议，账号切换能力仍需单独实现和验收。
func (a *PetV1Adapter) Probe(ctx context.Context) (Capabilities, error) {
	var raw struct {
		APIVersion     string         `json:"apiVersion"`
		InstanceID     string         `json:"instanceId"`
		AccountScope   string         `json:"accountScope"`
		ExecutionScope string         `json:"executionScope"`
		Commands       []string       `json:"commands"`
		CommandPolicy  *CommandPolicy `json:"commandPolicy"`
		Features       struct {
			Snapshot                     bool `json:"snapshot"`
			TaskPresets                  bool `json:"taskPresets"`
			Events                       bool `json:"events"`
			Earnings                     bool `json:"earnings"`
			Backpack                     bool `json:"backpack"`
			CultivationPlans             bool `json:"cultivationPlans"`
			CultivationQueue             bool `json:"cultivationQueue"`             // 只有明确声明后才读取独立队列路径。
			CultivationQueueSkillTargets bool `json:"cultivationQueueSkillTargets"` // 新字段只在队列能力同时开启时生效。
			MaterialsReadyEvents         bool `json:"materialsReadyEvents"`         // 材料历史采用独立能力，不借旧 SSE 声明推断。
			TargetsMetEvents             bool `json:"targetsMetEvents"`             // 面板达标历史没有声明时保持关闭。
			CommandStatus                bool `json:"commandStatus"`
			CommandIdempotency           bool `json:"commandIdempotency"`
			ScanAccounts                 bool `json:"scanAccounts"`
			QueryRoles                   bool `json:"queryRoles"`
		} `json:"features"`
	}
	if err := a.getData(ctx, "/api/desktop-pet/v1/capabilities", &raw); err != nil {
		return Capabilities{}, err
	}
	if !regexp.MustCompile(`^1\.[0-9]+(?:\.[0-9]+)?$`).MatchString(raw.APIVersion) || raw.InstanceID == "" || raw.AccountScope != "instance" || !raw.Features.Snapshot {
		return Capabilities{}, &APIError{Code: "INCOMPATIBLE_PROTOCOL", Message: "服务端桌宠协议版本或账号范围不受首版支持"}
	}
	a.instanceID, a.presets = raw.InstanceID, raw.Features.TaskPresets
	id := a.client.redact(raw.InstanceID)
	caps := Capabilities{APIVersion: a.client.redact(raw.APIVersion), InstanceID: &id, SourceMode: "pet-v1", AccountScope: "instance", ExecutionScope: raw.ExecutionScope, Snapshot: true, TaskPresets: a.presets, Events: raw.Features.Events, Earnings: raw.Features.Earnings, Backpack: raw.Features.Backpack, CultivationPlans: raw.Features.CultivationPlans, ScanAccounts: raw.Features.ScanAccounts, QueryRoles: raw.Features.QueryRoles, Commands: []string{}}
	caps.CultivationQueue = raw.Features.CultivationQueue
	caps.CultivationQueueSkillTargets = raw.Features.CultivationQueue && raw.Features.CultivationQueueSkillTargets
	caps.MaterialsReadyEvents = raw.Features.MaterialsReadyEvents
	caps.TargetsMetEvents = raw.Features.TargetsMetEvents
	if raw.ExecutionScope == "instance" && raw.Features.CommandStatus && raw.Features.CommandIdempotency && raw.CommandPolicy != nil && raw.CommandPolicy.IdempotencyRetentionSeconds >= 60 && raw.CommandPolicy.ResultRetentionSeconds >= raw.CommandPolicy.IdempotencyRetentionSeconds {
		caps.CommandStatus = true
		caps.CommandIdempotency = true
		caps.CommandPolicy = raw.CommandPolicy
		for _, command := range raw.Commands {
			if command == "oneLong.start" {
				caps.Commands = append(caps.Commands, command)
			}
		}
	}
	a.caps = caps
	return caps, nil
}

// GetSnapshot 验证实例身份和业务枚举，按服务端结构化状态展示但不由快照生成完成通知。
func (a *PetV1Adapter) GetSnapshot(ctx context.Context, query SnapshotQuery) (Snapshot, error) {
	if query.AccountID != nil {
		return Snapshot{}, unsupported()
	}
	var raw struct {
		InstanceID          string         `json:"instanceId"`
		InstanceDisplayName *string        `json:"instanceDisplayName"`
		AccountID           *string        `json:"accountId"`
		Scope               string         `json:"scope"`
		StateVersion        *string        `json:"stateVersion"`
		EventCursor         *string        `json:"eventCursor"`
		SourceTime          *string        `json:"sourceTime"`
		Process             *ProcessStatus `json:"process"`
		Task                *TaskStatus    `json:"task"`
	}
	if err := a.getData(ctx, "/api/desktop-pet/v1/snapshot", &raw); err != nil {
		return Snapshot{}, err
	}
	validState := map[string]bool{"unknown": true, "idle": true, "queued": true, "running": true, "succeeded": true, "failed": true, "cancelled": true}
	if raw.InstanceID != a.instanceID || raw.AccountID != nil || raw.Scope != "instance" || raw.Process == nil || raw.Task == nil || !validState[raw.Task.State] || raw.StateVersion == nil || strings.TrimSpace(*raw.StateVersion) == "" || len(*raw.StateVersion) > 512 || raw.SourceTime == nil {
		return Snapshot{}, &APIError{Code: "INCOMPATIBLE_RESPONSE", Message: "桌宠快照的实例、范围或必需字段不匹配"}
	}
	if _, err := time.Parse(time.RFC3339Nano, *raw.SourceTime); err != nil {
		return Snapshot{}, &APIError{Code: "INVALID_RESPONSE", Message: "服务端时间格式不兼容"}
	}
	for _, value := range []*string{raw.Task.ID, raw.Task.GroupName, raw.Task.RouteName, raw.Task.ScriptName, raw.Task.ProgressText, raw.Task.ScriptProgressText, raw.Task.ETAText, raw.Task.ETAAt, raw.StateVersion, raw.EventCursor} {
		if value != nil {
			*value = a.client.redact(*value)
		}
	}
	id := a.client.redact(raw.InstanceID)
	if raw.InstanceDisplayName != nil {
		name := a.client.redact(*raw.InstanceDisplayName)
		raw.InstanceDisplayName = &name
	}
	return Snapshot{ServerInstanceID: &id, InstanceDisplayName: raw.InstanceDisplayName, Scope: "instance", SourceMode: "pet-v1", StateVersion: raw.StateVersion, EventCursor: raw.EventCursor, SourceTime: raw.SourceTime, FetchedAt: time.Now().Format(time.RFC3339Nano), Process: *raw.Process, Task: *raw.Task}, nil
}

// ListPresets 只有服务端明确声明能力时读取，不探测未知路由。
func (a *PetV1Adapter) ListPresets(ctx context.Context, accountID string) ([]TaskPreset, error) {
	if !a.presets || accountID != "" {
		return nil, unsupported()
	}
	var presets []TaskPreset
	if err := a.getData(ctx, "/api/desktop-pet/v1/task-presets", &presets); err != nil {
		return nil, err
	}
	if len(presets) > 2000 {
		return nil, &APIError{Code: "INVALID_RESPONSE", Message: "配置数量超过安全限制"}
	}
	seen := map[string]bool{}
	for i := range presets {
		if presets[i].ID == "" || presets[i].Name == "" || presets[i].Scope != "instance" || seen[presets[i].ID] {
			return nil, &APIError{Code: "INVALID_RESPONSE", Message: "配置标识、名称或执行范围无效"}
		}
		seen[presets[i].ID] = true
		presets[i].ID = a.client.redact(presets[i].ID)
		presets[i].Name = a.client.redact(presets[i].Name)
		presets[i].Impact = a.client.redact(presets[i].Impact)
		presets[i].Scope = "instance"
	}
	if presets == nil {
		presets = []TaskPreset{}
	}
	return presets, nil
}

// validCommandID 将命令查询路径限制为 UUID，拒绝路径穿越与任意路由。
func validCommandID(id string) bool {
	return regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`).MatchString(id)
}

// validateReceipt 严格区分控制动作回执与游戏任务结果，不接受未知终态枚举。
func (a *PetV1Adapter) validateReceipt(receipt CommandReceipt, id string) (CommandReceipt, error) {
	states := map[string]bool{"accepted": true, "executing": true, "succeeded": true, "failed": true, "rejected": true, "unknown": true}
	if receipt.CommandID != id || !states[receipt.Status] {
		return CommandReceipt{}, &APIError{Code: "INVALID_RESPONSE", Message: "命令回执标识或状态无效"}
	}
	for _, timestamp := range []string{receipt.AcceptedAt, receipt.DedupeExpiresAt, receipt.ResultExpiresAt} {
		if _, err := time.Parse(time.RFC3339Nano, timestamp); err != nil {
			return CommandReceipt{}, &APIError{Code: "INVALID_RESPONSE", Message: "命令回执时间无效"}
		}
	}
	accepted, _ := time.Parse(time.RFC3339Nano, receipt.AcceptedAt)
	dedupe, _ := time.Parse(time.RFC3339Nano, receipt.DedupeExpiresAt)
	result, _ := time.Parse(time.RFC3339Nano, receipt.ResultExpiresAt)
	if dedupe.Sub(accepted) < time.Minute || result.Before(dedupe) {
		return CommandReceipt{}, &APIError{Code: "INVALID_RESPONSE", Message: "命令回执没有提供有效的幂等和结果保留窗口"}
	}
	receipt.Message = a.client.redact(receipt.Message)
	receipt.ProfileID = ""
	receipt.InstanceID = a.client.redact(a.instanceID)
	receipt.PresetName = ""
	receipt.UpdatedAt = time.Now().Format(time.RFC3339Nano)
	if receipt.TaskID != nil {
		value := a.client.redact(*receipt.TaskID)
		receipt.TaskID = &value
	}
	return receipt, nil
}

// ExecuteCommand 只提交明确的启动预设，失败与超时绝不在网络层自动重发。
func (a *PetV1Adapter) ExecuteCommand(ctx context.Context, request CommandRequest) (CommandReceipt, error) {
	if !a.caps.CommandStatus || !a.caps.CommandIdempotency || len(a.caps.Commands) == 0 {
		return CommandReceipt{}, unsupported()
	}
	if !validCommandID(request.CommandID) || request.Type != "oneLong.start" || request.AccountID != nil || request.Params.PresetID == "" || strings.TrimSpace(request.ExpectedStateVersion) == "" {
		return CommandReceipt{}, &APIError{Code: "INVALID_ARGUMENT", Message: "命令参数无效"}
	}
	var result envelope
	if err := a.client.request(ctx, http.MethodPost, "/api/desktop-pet/v1/commands", request, &result); err != nil {
		return CommandReceipt{}, err
	}
	var receipt CommandReceipt
	if result.Code != "OK" || json.Unmarshal(result.Data, &receipt) != nil {
		return CommandReceipt{}, &APIError{Code: "INVALID_RESPONSE", Message: "命令响应结构不兼容"}
	}
	return a.validateReceipt(receipt, request.CommandID)
}

// GetCommand 只查询本协议命令 UUID，不向旧接口猜测发送控制请求。
func (a *PetV1Adapter) GetCommand(ctx context.Context, id string) (CommandReceipt, error) {
	if !a.caps.CommandStatus {
		return CommandReceipt{}, unsupported()
	}
	if !validCommandID(id) {
		return CommandReceipt{}, &APIError{Code: "INVALID_ARGUMENT", Message: "命令标识无效"}
	}
	var receipt CommandReceipt
	if err := a.getData(ctx, "/api/desktop-pet/v1/commands/"+id, &receipt); err != nil {
		return CommandReceipt{}, err
	}
	return a.validateReceipt(receipt, id)
}
