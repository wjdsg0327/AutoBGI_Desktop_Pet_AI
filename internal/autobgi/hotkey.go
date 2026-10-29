package autobgi

import (
	"context"
	"net/http"
	"net/url"
	"strings"
)

// HotKeyController 只提供已核实的 BGI 快捷键读取和按键路由。
type HotKeyController interface {
	ReadBGIHotKeys(context.Context) (map[string]string, error)
	PressBGIHotKey(context.Context, string) error
}

// readBGIHotKeys 读取当前服务端配置，并拒绝无效结构和不可安全显示的按键。
func readBGIHotKeys(ctx context.Context, client *Client) (map[string]string, error) {
	if client == nil {
		return nil, &APIError{Code: "INVALID_ARGUMENT", Message: "快捷键连接不可用"}
	}
	var response struct {
		Status string            `json:"status"`
		Data   map[string]string `json:"data"`
	}
	if err := client.get(ctx, "/api/HotKeyConfig/HotKeyQuery", &response); err != nil {
		return nil, err
	}
	if response.Status != "success" || response.Data == nil {
		return nil, &APIError{Code: "INVALID_RESPONSE", Message: "快捷键列表响应格式不兼容"}
	}
	for name, key := range response.Data {
		// 按键既用于菜单显示也用于请求，异常内容不能被修改后继续发送。
		// 短密钥可能与 F1 等合法按键重合，不能因此拒绝正常配置。
		if len(name) > 256 || len(key) > 256 || strings.ContainsAny(name+key, "\r\n\x00") || (len(client.key) >= 8 && strings.Contains(key, client.key)) {
			return nil, &APIError{Code: "INVALID_RESPONSE", Message: "快捷键列表包含无效内容"}
		}
	}
	return response.Data, nil
}

// pressBGIHotKey 仅发送一次查询参数请求；成功表示服务端已响应，不保证实际按键成功。
func pressBGIHotKey(ctx context.Context, client *Client, key string) error {
	if client == nil || strings.TrimSpace(key) == "" || len(key) > 256 || strings.ContainsAny(key, "\r\n\x00") {
		return &APIError{Code: "INVALID_ARGUMENT", Message: "快捷键按键无效"}
	}
	var response string
	query := url.Values{"key": []string{key}}
	if err := client.requestQuery(ctx, http.MethodPost, "/api/HotKeyConfig/PressKey", query, nil, &response); err != nil {
		return err
	}
	if response != "按键成功" {
		return &APIError{Code: "INVALID_RESPONSE", Message: "快捷键请求响应格式不兼容"}
	}
	return nil
}

// ReadBGIHotKeys 读取旧版连接的 BGI 系统快捷键。
func (a *LegacyAdapter) ReadBGIHotKeys(ctx context.Context) (map[string]string, error) {
	return readBGIHotKeys(ctx, a.client)
}

// PressBGIHotKey 使用旧版连接发送 BGI 系统快捷键。
func (a *LegacyAdapter) PressBGIHotKey(ctx context.Context, key string) error {
	return pressBGIHotKey(ctx, a.client, key)
}

// ReadBGIHotKeys 通过桌宠协议连接读取同一组 BGI 系统快捷键。
func (a *PetV1Adapter) ReadBGIHotKeys(ctx context.Context) (map[string]string, error) {
	return readBGIHotKeys(ctx, a.client)
}

// PressBGIHotKey 通过桌宠协议连接发送同一固定按键接口。
func (a *PetV1Adapter) PressBGIHotKey(ctx context.Context, key string) error {
	return pressBGIHotKey(ctx, a.client, key)
}
