package autobgi

import (
	"context"
	"encoding/json"
	"strings"
	"time"
)

// LegacyAdapter 只映射当前仓库已经核实的只读接口和固定的一条龙启动接口。
type LegacyAdapter struct {
	readOnlyBoundary
	client  *Client
	initial *Snapshot
}

// ReadLegacySnapshot 按需读取现有状态接口，不启动额外的后台采集。
func (c *Client) ReadLegacySnapshot(ctx context.Context) (Snapshot, error) {
	return (&LegacyAdapter{client: c}).readSnapshot(ctx)
}

// Probe 以完整解析的 index 验证连接，不调用具有副作用或远程版本查询的接口。
func (a *LegacyAdapter) Probe(ctx context.Context) (Capabilities, error) {
	snapshot, err := a.readSnapshot(ctx)
	if err != nil {
		return Capabilities{}, err
	}
	a.initial = &snapshot
	return Capabilities{APIVersion: "legacy", SourceMode: "legacy", AccountScope: "instance", Snapshot: true, TaskPresets: true, Commands: []string{}}, nil
}

// GetSnapshot 复用探测结果，避免首次连接连续读取相同状态。
func (a *LegacyAdapter) GetSnapshot(ctx context.Context, query SnapshotQuery) (Snapshot, error) {
	if query.AccountID != nil {
		return Snapshot{}, unsupported()
	}
	if a.initial != nil {
		result := *a.initial
		a.initial = nil
		return result, nil
	}
	return a.readSnapshot(ctx)
}

// readSnapshot 校验必需字段，明确维持 unknown，绝不从进程或中文文本推断成功。
func (a *LegacyAdapter) readSnapshot(ctx context.Context) (Snapshot, error) {
	var raw map[string]json.RawMessage
	if err := a.client.get(ctx, "/api/index", &raw); err != nil {
		return Snapshot{}, err
	}
	var running bool
	if value, ok := raw["running"]; !ok || string(value) == "null" || json.Unmarshal(value, &running) != nil {
		return Snapshot{}, &APIError{Code: "INVALID_RESPONSE", Message: "index 缺少有效的 running 进程字段"}
	}
	values := make(map[string]*string)
	for _, key := range []string{"group", "ExpectedToEnd", "line", "scriptName", "progress", "jsProgress", "title"} {
		value, exists := raw[key]
		if !exists || string(value) == "null" {
			return Snapshot{}, &APIError{Code: "INVALID_RESPONSE", Message: "index 缺少必需的状态展示字段"}
		}
		var text string
		if json.Unmarshal(value, &text) != nil {
			return Snapshot{}, &APIError{Code: "INVALID_RESPONSE", Message: "index 状态字段类型不兼容"}
		}
		text = a.client.redact(text)
		values[key] = &text
	}
	return Snapshot{Scope: "instance", SourceMode: "legacy", FetchedAt: time.Now().Format(time.RFC3339Nano), InstanceDisplayName: values["title"], Process: ProcessStatus{Running: &running}, Task: TaskStatus{State: "unknown", GroupName: values["group"], RouteName: values["line"], ScriptName: values["scriptName"], ProgressText: values["progress"], ScriptProgressText: values["jsProgress"], ETAText: values["ExpectedToEnd"]}}, nil
}

// ListPresets 将真实名称列表规范化，不给展示名称伪造稳定服务端 ID。
func (a *LegacyAdapter) ListPresets(ctx context.Context, accountID string) ([]TaskPreset, error) {
	if accountID != "" {
		return nil, unsupported()
	}
	var response struct {
		Status string          `json:"status"`
		Data   json.RawMessage `json:"data"`
	}
	if err := a.client.get(ctx, "/api/oneLong/oneLongAllName", &response); err != nil {
		return nil, err
	}
	if response.Status != "success" || len(response.Data) == 0 {
		return nil, &APIError{Code: "INVALID_RESPONSE", Message: "一条龙列表响应格式不兼容"}
	}
	var names []string
	if json.Unmarshal(response.Data, &names) != nil {
		return nil, &APIError{Code: "INVALID_RESPONSE", Message: "一条龙列表字段类型不兼容"}
	}
	result := make([]TaskPreset, 0, len(names))
	seen := map[string]bool{}
	for _, name := range names {
		name = a.client.redact(name)
		if strings.TrimSpace(name) != "" && !seen[name] {
			result = append(result, TaskPreset{Name: name, Scope: "instance"})
			seen[name] = true
		}
	}
	return result, nil
}
