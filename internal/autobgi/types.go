// Package autobgi 以白名单适配器封装 AutoBGI 协议，禁止通用网络与任意控制绑定。
package autobgi

import (
	"context"
	"encoding/json"
)

// APIError 使用固定可读文案，绝不把远端响应、URL 或凭据写入错误。
type APIError struct {
	Code       string `json:"code"`
	Message    string `json:"message"`
	HTTPStatus int    `json:"httpStatus"`
}

// Error 仅返回经过本地审核的错误信息。
func (e *APIError) Error() string { return e.Code + ": " + e.Message }

// Capabilities 分别报告服务端声明和当前客户端已实现的安全能力。
type Capabilities struct {
	APIVersion                   string         `json:"apiVersion"`
	InstanceID                   *string        `json:"instanceId"`
	SourceMode                   string         `json:"sourceMode"`
	AccountScope                 string         `json:"accountScope"`
	ExecutionScope               string         `json:"executionScope"`
	Snapshot                     bool           `json:"snapshot"`
	TaskPresets                  bool           `json:"taskPresets"`
	Accounts                     bool           `json:"accounts"`
	ScanAccounts                 bool           `json:"scanAccounts"`
	QueryRoles                   bool           `json:"queryRoles"`
	Events                       bool           `json:"events"`
	Earnings                     bool           `json:"earnings"`
	Backpack                     bool           `json:"backpack"`
	CultivationPlans             bool           `json:"cultivationPlans"`
	CultivationQueue             bool           `json:"cultivationQueue"`             // 独立队列只读能力，不升级旧计划或完成事件合同。
	CultivationQueueSkillTargets bool           `json:"cultivationQueueSkillTargets"` // 已保存技能目标必须独立协商队列 schema2。
	MaterialsReadyEvents         bool           `json:"materialsReadyEvents"`         // 仅表示可读材料条件记录，不授予游戏完成或执行能力。
	TargetsMetEvents             bool           `json:"targetsMetEvents"`             // 独立基础面板达标历史，不代表本次游戏运行完成培养。
	Commands                     []string       `json:"commands"`
	CommandStatus                bool           `json:"commandStatus"`
	CommandIdempotency           bool           `json:"commandIdempotency"`
	CommandPolicy                *CommandPolicy `json:"commandPolicy"`
}

// CommandPolicy 记录服务端幂等窗口，不为旧接口编造可重试保证。
type CommandPolicy struct {
	IdempotencyRetentionSeconds int `json:"idempotencyRetentionSeconds"`
	ResultRetentionSeconds      int `json:"resultRetentionSeconds"`
}

// ProcessStatus 的 running 只表示进程存在，不等于正在执行一条龙。
type ProcessStatus struct {
	Running *bool `json:"running"`
}

// TaskStatus 保留原始展示文本，旧版不推断任务终态和百分比。
type TaskStatus struct {
	ID                 *string `json:"id"`
	State              string  `json:"state"`
	GroupName          *string `json:"groupName"`
	RouteName          *string `json:"routeName"`
	ScriptName         *string `json:"scriptName"`
	ProgressText       *string `json:"progressText"`
	ScriptProgressText *string `json:"scriptProgressText"`
	ETAText            *string `json:"etaText"`
	ETAAt              *string `json:"etaAt"`
}

// Snapshot 是规范化脱敏快照；未知身份、账号和时间保持 null。
type Snapshot struct {
	ProfileID           string        `json:"profileId"`
	ServerInstanceID    *string       `json:"serverInstanceId"`
	InstanceDisplayName *string       `json:"instanceDisplayName"`
	AccountID           *string       `json:"accountId"`
	AccountLabel        *string       `json:"accountLabel"`
	Scope               string        `json:"scope"`
	SourceMode          string        `json:"sourceMode"`
	StateVersion        *string       `json:"stateVersion"`
	EventCursor         *string       `json:"eventCursor"`
	SourceTime          *string       `json:"sourceTime"`
	FetchedAt           string        `json:"fetchedAt"`
	Stale               bool          `json:"stale"`
	Process             ProcessStatus `json:"process"`
	Task                TaskStatus    `json:"task"`
}

// TaskPreset 只列出真实配置，控制时还需单独准备并消费一次性确认。
type TaskPreset struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Scope  string `json:"scope"`
	Impact string `json:"impact"`
}

// SnapshotQuery 与 Account 保留明确的身份边界，实例协议不能伪造运行账户。
type SnapshotQuery struct {
	AccountID *string `json:"accountId"`
}
type Account struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}
type CommandRequest struct {
	CommandID            string        `json:"commandId"`
	Type                 string        `json:"type"`
	AccountID            *string       `json:"accountId"`
	Params               CommandParams `json:"params"`
	ExpectedStateVersion string        `json:"expectedStateVersion"`
}

// CommandParams 只接受已核实的一条龙配置标识，拒绝任意命令参数。
type CommandParams struct {
	PresetID string `json:"presetId"`
}

// CommandReceipt 同时携带冻结的本地上下文，未知结果不能被当作任务完成。
type CommandReceipt struct {
	CommandID       string  `json:"commandId"`
	Status          string  `json:"status"`
	TaskID          *string `json:"taskId"`
	AcceptedAt      string  `json:"acceptedAt"`
	DedupeExpiresAt string  `json:"dedupeExpiresAt"`
	ResultExpiresAt string  `json:"resultExpiresAt"`
	Message         string  `json:"message"`
	ProfileID       string  `json:"profileId"`
	InstanceID      string  `json:"instanceId"`
	PresetName      string  `json:"presetName"`
	UpdatedAt       string  `json:"updatedAt"`
}
type EventQuery struct {
	AccountID *string `json:"accountId"`
	Cursor    *string `json:"cursor"`
}
type Event struct {
	SchemaVersion int             `json:"schemaVersion"`
	EventID       string          `json:"eventId"`
	InstanceID    string          `json:"instanceId"`
	AccountID     *string         `json:"accountId"`
	TaskID        *string         `json:"taskId"`
	Type          string          `json:"type"`
	OccurredAt    string          `json:"occurredAt"`
	Data          json.RawMessage `json:"data"`
}

// AutoBGIAdapter 将旧接口与建议的新协议隔离，未支持的方法明确拒绝。
type AutoBGIAdapter interface {
	Probe(context.Context) (Capabilities, error)
	GetSnapshot(context.Context, SnapshotQuery) (Snapshot, error)
	ListAccounts(context.Context) ([]Account, error)
	ListPresets(context.Context, string) ([]TaskPreset, error)
	ExecuteCommand(context.Context, CommandRequest) (CommandReceipt, error)
	GetCommand(context.Context, string) (CommandReceipt, error)
	Subscribe(context.Context, EventQuery, func(Event)) error
}

// unsupported 是本地能力拒绝，不会触发任何网络或副作用。
func unsupported() error {
	return &APIError{Code: "UNSUPPORTED_CAPABILITY", Message: "当前客户端或服务版本未提供此能力"}
}

// readOnlyBoundary 为旧协议提供明确拒绝实现，避免探测未知控制或订阅路由。
type readOnlyBoundary struct{}

func (readOnlyBoundary) ListAccounts(context.Context) ([]Account, error) { return nil, unsupported() }
func (readOnlyBoundary) ExecuteCommand(context.Context, CommandRequest) (CommandReceipt, error) {
	return CommandReceipt{}, unsupported()
}
func (readOnlyBoundary) GetCommand(context.Context, string) (CommandReceipt, error) {
	return CommandReceipt{}, unsupported()
}
func (readOnlyBoundary) Subscribe(context.Context, EventQuery, func(Event)) error {
	return unsupported()
}
