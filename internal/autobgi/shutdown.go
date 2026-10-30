package autobgi

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// ComputerShutdownController 只暴露已核实的远端电脑关机入口。
type ComputerShutdownController interface {
	ShutdownComputer(context.Context) error
}

// shutdownComputerRequest 使用固定任务名称和纯文本响应，不提供通用任务调用能力。
func shutdownComputerRequest(ctx context.Context, client *Client) error {
	if client == nil || client.http == nil {
		return &APIError{Code: "INVALID_ARGUMENT", Message: "关机操作的连接不可用"}
	}
	transport, ok := client.http.Transport.(*http.Transport)
	if !ok || transport == nil {
		return &APIError{Code: "INVALID_ARGUMENT", Message: "关机操作的连接配置无效"}
	}
	// 关闭相关软件可能等待进程退出，独立传输避免修改其他请求的超时。
	shutdownTransport := transport.Clone()
	shutdownTransport.ResponseHeaderTimeout = 50 * time.Second
	defer shutdownTransport.CloseIdleConnections()
	shutdownClient := *client.http
	shutdownClient.Transport = shutdownTransport
	shutdownClient.Timeout = 55 * time.Second
	u := *client.base
	u.Path = strings.TrimRight(client.base.Path, "/") + "/api/taskCron/AtOnceRun"
	u.RawPath = ""
	u.RawQuery = url.Values{"type": {"定时关机"}}.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), nil)
	if err != nil {
		return &APIError{Code: "INVALID_ARGUMENT", Message: "无法建立关机请求"}
	}
	req.Header.Set("apiKey", client.key)
	req.Header.Set("Accept", "text/plain")
	response, err := shutdownClient.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return &APIError{Code: "NETWORK_ERROR", Message: "关机请求连接中断"}
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return decodeResponse(response, nil)
	}
	body, readErr := io.ReadAll(io.LimitReader(response.Body, 16*1024+1))
	if readErr != nil || len(body) > 16*1024 ||
		!strings.HasPrefix(strings.ToLower(response.Header.Get("Content-Type")), "text/plain") ||
		strings.TrimSpace(string(body)) != "任务已执行" {
		return &APIError{Code: "INVALID_RESPONSE", Message: "关机请求响应格式不兼容"}
	}
	return nil
}

// ShutdownComputer 让旧版连接调用固定关机任务。
func (a *LegacyAdapter) ShutdownComputer(ctx context.Context) error {
	return shutdownComputerRequest(ctx, a.client)
}

// ShutdownComputer 让新版连接使用同一个已核实的固定任务。
func (a *PetV1Adapter) ShutdownComputer(ctx context.Context) error {
	return shutdownComputerRequest(ctx, a.client)
}
