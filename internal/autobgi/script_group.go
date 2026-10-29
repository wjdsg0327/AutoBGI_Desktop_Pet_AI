package autobgi

import (
	"context"
	"net/http"
	"strings"
	"time"
)

// ScriptGroupController 仅暴露已核实的配置组列表和启动接口，不提供通用 HTTP 控制能力。
type ScriptGroupController interface {
	ListScriptGroups(context.Context) ([]string, error)
	StartScriptGroups(context.Context, []string) error
}

// listScriptGroupsRequest 读取裸数组，并保留服务端原始名称和目录分隔符。
func listScriptGroupsRequest(ctx context.Context, client *Client) ([]string, error) {
	if client == nil || client.http == nil {
		return nil, &APIError{Code: "INVALID_ARGUMENT", Message: "配置组列表的连接不可用"}
	}
	var names []string
	if err := client.get(ctx, "/api/scriptGroup/listGroups", &names); err != nil {
		return nil, err
	}
	// null 和空数组均转换为空列表；仅用去空白判断有效性，不改写合法原名。
	result := make([]string, 0, len(names))
	seen := make(map[string]struct{}, len(names))
	for _, name := range names {
		if strings.TrimSpace(name) == "" {
			continue
		}
		if _, exists := seen[name]; exists {
			continue
		}
		seen[name] = struct{}{}
		result = append(result, name)
	}
	return result, nil
}

// scriptGroupStartClient 为同步启动单独延长等待，保留代理、TLS 和禁止重定向策略。
func scriptGroupStartClient(client *Client) (*http.Client, error) {
	if client == nil || client.http == nil {
		return nil, &APIError{Code: "INVALID_ARGUMENT", Message: "配置组启动的连接不可用"}
	}
	transport, ok := client.http.Transport.(*http.Transport)
	if !ok || transport == nil {
		return nil, &APIError{Code: "INVALID_ARGUMENT", Message: "配置组启动的连接配置无效"}
	}
	// 副本独享连接池和超时，避免长请求修改共享查询客户端而引入竞争。
	startTransport := transport.Clone()
	startTransport.ResponseHeaderTimeout = 5 * time.Minute
	startClient := *client.http
	startClient.Transport = startTransport
	startClient.Timeout = 5 * time.Minute
	return &startClient, nil
}

// startScriptGroupsRequest 只发送一次根字符串数组，业务失败不能由 HTTP 200 误判成功。
func startScriptGroupsRequest(ctx context.Context, client *Client, names []string) error {
	if len(names) == 0 {
		return &APIError{Code: "INVALID_ARGUMENT", Message: "至少需要一个配置组名称"}
	}
	for _, name := range names {
		if strings.TrimSpace(name) == "" {
			return &APIError{Code: "INVALID_ARGUMENT", Message: "配置组名称不能为空"}
		}
	}
	startClient, err := scriptGroupStartClient(client)
	if err != nil {
		return err
	}
	defer startClient.CloseIdleConnections()
	var response struct {
		Status string `json:"status"`
	}
	// 单组同样编码成数组；固定路径复用现有来源校验、凭据和响应大小限制。
	if err := client.requestQueryWithHTTP(ctx, startClient, http.MethodPost, "/api/startGroups", nil, names, &response); err != nil {
		return err
	}
	switch response.Status {
	case "success":
		return nil
	case "error":
		return &APIError{Code: "SCRIPT_GROUP_START_FAILED", Message: "配置组启动失败，请在 AutoBGI 核对运行状态"}
	default:
		return &APIError{Code: "INVALID_RESPONSE", Message: "配置组启动响应格式不兼容"}
	}
}

// ListScriptGroups 读取旧版服务返回的全部配置组相对名称。
func (a *LegacyAdapter) ListScriptGroups(ctx context.Context) ([]string, error) {
	return listScriptGroupsRequest(ctx, a.client)
}

// StartScriptGroups 调用旧版固定配置组启动接口，不自动重发。
func (a *LegacyAdapter) StartScriptGroups(ctx context.Context, names []string) error {
	return startScriptGroupsRequest(ctx, a.client, names)
}

// ListScriptGroups 使新版连接也使用同一份真实配置组目录。
func (a *PetV1Adapter) ListScriptGroups(ctx context.Context) ([]string, error) {
	return listScriptGroupsRequest(ctx, a.client)
}

// StartScriptGroups 使新版连接使用同一个已核实的配置组启动入口。
func (a *PetV1Adapter) StartScriptGroups(ctx context.Context, names []string) error {
	return startScriptGroupsRequest(ctx, a.client, names)
}
