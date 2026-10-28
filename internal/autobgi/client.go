package autobgi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// NormalizeURL 接受 HTTP 和 HTTPS，拒绝携带秘密和动态参数的地址，并保留反向代理基础路径。
// 第二个参数保留旧调用兼容性，HTTP 不再依赖明文连接开关。
func NormalizeURL(raw string, _ bool) (string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u == nil || u.Host == "" || u.Opaque != "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || (u.Scheme != "http" && u.Scheme != "https") {
		return "", &APIError{Code: "INVALID_ARGUMENT", Message: "请输入不含用户名、查询参数或片段的 HTTP/HTTPS 服务地址"}
	}
	host := u.Hostname()
	if host == "" || strings.ContainsAny(host, "\\ \t\r\n") || strings.Contains(u.Path, "\\") {
		return "", &APIError{Code: "INVALID_ARGUMENT", Message: "服务地址格式无效"}
	}
	for _, part := range strings.Split(u.Path, "/") {
		if part == "." || part == ".." {
			return "", &APIError{Code: "INVALID_ARGUMENT", Message: "基础路径不能包含目录跳转"}
		}
	}
	u.Path = strings.TrimRight(u.Path, "/")
	u.RawPath = ""
	return strings.TrimRight(u.String(), "/"), nil
}

// Client 不导出 HTTP 请求方法；所有调用都来自本包内的确定路由。
type Client struct {
	base *url.URL
	key  string
	http *http.Client
}

// NewClient 强制固定来源、正常证书校验、超时及禁止重定向。
func NewClient(baseURL, key string, allowInsecure bool) (*Client, error) {
	base, err := NormalizeURL(baseURL, allowInsecure)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(key) == "" || len(key) > 4096 || strings.ContainsAny(key, "\r\n\x00") {
		return nil, &APIError{Code: "INVALID_CREDENTIAL", Message: "请输入有效的 AutoBGI API 密钥"}
	}
	u, _ := url.Parse(base)
	transport := &http.Transport{Proxy: http.ProxyFromEnvironment, DialContext: (&net.Dialer{Timeout: 3 * time.Second, KeepAlive: 30 * time.Second}).DialContext, TLSHandshakeTimeout: 3 * time.Second, ResponseHeaderTimeout: 8 * time.Second, MaxIdleConnsPerHost: 2, IdleConnTimeout: 60 * time.Second}
	return &Client{base: u, key: key, http: &http.Client{Timeout: 10 * time.Second, Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}, nil
}

// Close 释放失效 profile 的空闲连接，取消中的请求由其 context 负责。
func (c *Client) Close() { c.http.CloseIdleConnections() }

// redact 防止恶意或异常服务把密钥反射到展示字段。
func (c *Client) redact(value string) string { return strings.ReplaceAll(value, c.key, "[已隐藏]") }

// get 只允许本包定义的无查询只读路由，不把底层网络错误暴露给前端。
func (c *Client) get(ctx context.Context, route string, dest any) error {
	return c.request(ctx, http.MethodGet, route, nil, dest)
}

// request 只由适配器白名单调用，新增 POST 不能被注册为通用前端绑定。
func (c *Client) request(ctx context.Context, method, route string, body any, dest any) error {
	return c.requestQuery(ctx, method, route, nil, body, dest)
}

// requestQuery 只供已定义领域路由编码查询参数，绝不拼接不可信路径或服务来源。
func (c *Client) requestQuery(ctx context.Context, method, route string, query url.Values, body any, dest any) error {
	u := *c.base
	u.Path = strings.TrimRight(c.base.Path, "/") + route
	u.RawPath = ""
	u.RawQuery = query.Encode()
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return &APIError{Code: "INVALID_ARGUMENT", Message: "请求参数无效"}
		}
		reader = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, u.String(), reader)
	if err != nil {
		return &APIError{Code: "INVALID_ARGUMENT", Message: "无法建立请求"}
	}
	req.Header.Set("apiKey", c.key)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	response, err := c.http.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return &APIError{Code: "NETWORK_ERROR", Message: "无法连接服务，请检查地址、网络、TLS 证书或服务是否启动"}
	}
	defer response.Body.Close()
	return decodeResponse(response, dest)
}

// decodeResponse 仅接受有限 JSON；业务错误使用本地文案而不反射远端错误文本。
func decodeResponse(response *http.Response, dest any) error {
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		code, message := "SERVER_ERROR", "服务暂时不可用"
		switch response.StatusCode {
		case 401, 403:
			code, message = "AUTH_INVALID", "API 密钥无效或没有访问权限，请更新密钥后重新连接"
		case 404:
			code, message = "NOT_FOUND", "当前服务未提供此接口"
		case 429:
			code, message = "RATE_LIMITED", "服务请求过于频繁，稍后重试"
		default:
			if response.StatusCode >= 300 && response.StatusCode < 400 {
				code, message = "REDIRECT_REJECTED", "服务返回了重定向；请填写最终服务地址，密钥未向新地址转发"
			}
		}
		if response.StatusCode >= 400 && response.StatusCode != 401 && response.StatusCode != 403 {
			var result struct {
				Code string `json:"code"`
			}
			data, _ := io.ReadAll(io.LimitReader(response.Body, 16*1024))
			if json.Unmarshal(data, &result) == nil {
				messages := map[string]string{"TASK_BUSY": "当前实例的执行器正忙，请先检查 AutoBGI", "VERSION_CONFLICT": "实例状态已经变化，请重新选择并确认", "IDEMPOTENCY_CONFLICT": "命令标识与已提交参数不一致，已拒绝重复执行", "RESYNC_REQUIRED": "事件游标已过期，需要重新同步完整快照", "COMMAND_NOT_FOUND": "服务端没有找到该命令回执，请核对 AutoBGI", "INVALID_ARGUMENT": "请求参数未通过服务端校验", "UNSUPPORTED_CAPABILITY": "当前服务版本未提供此能力", "ACCOUNT_NOT_FOUND": "所选数据账户不存在", "FORBIDDEN": "此操作没有访问权限", "DATA_UNAVAILABLE": "当前领域数据暂不可用，请检查 AutoBGI 的源数据", "INVALID_SCAN_ACCOUNT": "扫描目录标识无效", "SCAN_ACCOUNT_NOT_FOUND": "扫描目录不存在，请重新读取列表", "SCAN_ACCOUNT_REQUIRED": "请先选择一个扫描目录", "INVALID_QUERY_ROLE": "查询角色标识无效", "QUERY_ROLE_NOT_FOUND": "所选查询角色不存在，请重新读取列表", "INVALID_QUERY": "查询参数无效"}
				if text, ok := messages[result.Code]; ok {
					code, message = result.Code, text
				}
				// 过期回执是保留窗口结束，不能误报服务故障或据此重发原启动请求。
				if response.StatusCode == http.StatusGone && result.Code == "COMMAND_EXPIRED" {
					code, message = "COMMAND_EXPIRED", "该命令已超过服务端保留窗口，请在 AutoBGI 核对原记录；不会自动重发"
				}
			}
		}
		return &APIError{Code: code, Message: message, HTTPStatus: response.StatusCode}
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, 2*1024*1024+1))
	if err != nil || len(body) > 2*1024*1024 {
		return &APIError{Code: "INVALID_RESPONSE", Message: "响应读取失败或超过安全大小限制"}
	}
	contentType := strings.ToLower(response.Header.Get("Content-Type"))
	if bytes.HasPrefix(bytes.TrimSpace(body), []byte("<")) || (contentType != "" && !strings.Contains(contentType, "application/json") && !strings.Contains(contentType, "+json")) {
		return &APIError{Code: "INCOMPATIBLE_RESPONSE", Message: "服务返回了网页或非 JSON 内容，请检查 API 地址和反向代理配置"}
	}
	if err := json.Unmarshal(body, dest); err != nil {
		return &APIError{Code: "INVALID_RESPONSE", Message: "服务 JSON 格式或字段类型不兼容"}
	}
	return nil
}

// NewAdapter 仅在新协议明确 404 时降级；401、HTML 和未知协议绝不静默降级。
func NewAdapter(ctx context.Context, client *Client, mode string) (AutoBGIAdapter, Capabilities, error) {
	if mode != "legacy" {
		adapter := &PetV1Adapter{client: client}
		caps, err := adapter.Probe(ctx)
		if err == nil {
			return adapter, caps, nil
		}
		apiErr, ok := err.(*APIError)
		if mode == "pet-v1" || !ok || apiErr.HTTPStatus != http.StatusNotFound {
			return nil, Capabilities{}, err
		}
	}
	adapter := &LegacyAdapter{client: client}
	caps, err := adapter.Probe(ctx)
	if err != nil {
		return nil, Capabilities{}, err
	}
	return adapter, caps, err
}
