package autobgi

import (
	"context"
	"net/http"
	"time"
)

// CloseController 只提供两个已核实的实例级关闭入口。
type CloseController interface {
	CloseBGIAndGame(context.Context) error
	CloseGame(context.Context) error
}

// closeInstance 使用独立传输副本容纳服务端停止进程的等待，不改变其他接口超时。
func closeInstance(ctx context.Context, client *Client, route string) error {
	if client == nil || client.http == nil {
		return &APIError{Code: "INVALID_ARGUMENT", Message: "关闭操作的连接不可用"}
	}
	transport, ok := client.http.Transport.(*http.Transport)
	if !ok || transport == nil {
		return &APIError{Code: "INVALID_ARGUMENT", Message: "关闭操作的连接配置无效"}
	}
	// 复制传输和客户端，保留原代理、TLS 及重定向规则并避免与共享配置竞争。
	closeTransport := transport.Clone()
	closeTransport.ResponseHeaderTimeout = 30 * time.Second
	defer closeTransport.CloseIdleConnections()
	closeClient := *client.http
	closeClient.Transport = closeTransport
	closeClient.Timeout = 35 * time.Second
	var response struct {
		Status string `json:"status"`
	}
	if err := client.requestQueryWithHTTP(ctx, &closeClient, http.MethodPost, route, nil, nil, &response); err != nil {
		return err
	}
	if response.Status != "received" {
		return &APIError{Code: "INVALID_RESPONSE", Message: "关闭请求响应格式不兼容"}
	}
	return nil
}

// CloseBGIAndGame 调用旧版连接的关闭 BetterGI 和原神接口。
func (a *LegacyAdapter) CloseBGIAndGame(ctx context.Context) error {
	return closeInstance(ctx, a.client, "/api/closeBgi")
}

// CloseGame 调用旧版连接的关闭原神接口。
func (a *LegacyAdapter) CloseGame(ctx context.Context) error {
	return closeInstance(ctx, a.client, "/api/closeYuanShen")
}

// CloseBGIAndGame 让新版桌宠连接使用相同的固定关闭接口。
func (a *PetV1Adapter) CloseBGIAndGame(ctx context.Context) error {
	return closeInstance(ctx, a.client, "/api/closeBgi")
}

// CloseGame 让新版桌宠连接使用相同的固定关闭原神接口。
func (a *PetV1Adapter) CloseGame(ctx context.Context) error {
	return closeInstance(ctx, a.client, "/api/closeYuanShen")
}
