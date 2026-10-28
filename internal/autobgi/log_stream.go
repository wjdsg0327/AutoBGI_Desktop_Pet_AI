package autobgi

import (
	"context"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/coder/websocket"
)

// ListLogFiles 通过文档中的鉴权接口取得按服务端时间排序的日志文件。
func (c *Client) ListLogFiles(ctx context.Context) ([]string, error) {
	var response struct {
		Files []string `json:"files"`
	}
	if err := c.get(ctx, "/api/logFiles", &response); err != nil {
		return nil, err
	}
	if len(response.Files) > 10000 {
		return nil, &APIError{Code: "INVALID_RESPONSE", Message: "日志文件列表过大"}
	}
	for _, name := range response.Files {
		// 路径段只接受文件名，避免异常服务返回目录跳转或其他路由。
		if name == "" || len(name) > 1024 || !utf8.ValidString(name) || strings.ContainsAny(name, "/\\\x00\r\n") || name == "." || name == ".." {
			return nil, &APIError{Code: "INVALID_RESPONSE", Message: "日志文件名格式无效"}
		}
	}
	return response.Files, nil
}

// RedactLog 确保服务端意外写入的密钥不会出现在桌宠日志。
func (c *Client) RedactLog(value string) string {
	return c.redact(value)
}

// WatchLog 只订阅指定文件的新日志，连接与读取均由上层上下文取消。
func (c *Client) WatchLog(ctx context.Context, name string, connected func(), line func(string)) error {
	if name == "" || len(name) > 1024 || !utf8.ValidString(name) || strings.ContainsAny(name, "/\\\x00\r\n") || name == "." || name == ".." {
		return &APIError{Code: "INVALID_ARGUMENT", Message: "日志文件名格式无效"}
	}
	u := *c.base
	u.Path = strings.TrimRight(c.base.Path, "/") + "/ws/" + name
	u.RawPath = ""
	// 按文档该日志流公开；密钥只用于前面的鉴权 API 请求，不附在流地址中。
	conn, response, err := websocket.Dial(ctx, u.String(), &websocket.DialOptions{HTTPClient: c.http})
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if response != nil && (response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden) {
			return &APIError{Code: "AUTH_INVALID", Message: "日志流访问被拒绝，请检查服务端权限"}
		}
		return &APIError{Code: "NETWORK_ERROR", Message: "WebSocket 连接失败，请检查服务地址和网络"}
	}
	defer conn.CloseNow()
	conn.SetReadLimit(64 * 1024)
	connected()
	for {
		kind, data, err := conn.Read(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return &APIError{Code: "NETWORK_ERROR", Message: "日志连接已中断，正在重新连接"}
		}
		if kind != websocket.MessageText {
			continue
		}
		value := strings.ToValidUTF8(string(data), "�")
		if strings.TrimSpace(value) == "无法打开日志文件" {
			return &APIError{Code: "LOG_UNAVAILABLE", Message: "服务端无法打开日志文件，正在重试"}
		}
		line(c.redact(value))
	}
}
