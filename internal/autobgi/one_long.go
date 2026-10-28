package autobgi

import (
	"context"
	"net/http"
	"strings"
)

// OneLongStarter 只允许调用已经核实的一条龙启动路由，不提供任意 HTTP 能力。
type OneLongStarter interface {
	StartOneLong(context.Context, string) error
}

// startOneLongRequest 按旧版接口合同发送 JSON 根字符串，并校验成功标识。
func startOneLongRequest(ctx context.Context, client *Client, name string) error {
	if client == nil || strings.TrimSpace(name) == "" {
		return &APIError{Code: "INVALID_ARGUMENT", Message: "一条龙名称不能为空"}
	}
	var response struct {
		Status string `json:"status"`
	}
	// request 会通过 JSON 编码器把 name 序列化为根字符串，而不是对象。
	if err := client.request(ctx, http.MethodPost, "/api/oneLong/startOneLong", name, &response); err != nil {
		return err
	}
	switch response.Status {
	case "success":
		return nil
	case "error":
		return &APIError{Code: "ONE_DRAGON_START_FAILED", Message: "一条龙启动失败"}
	default:
		return &APIError{Code: "INVALID_RESPONSE", Message: "一条龙启动响应格式不兼容"}
	}
}

// StartOneLong 调用文档确认的旧版一条龙启动接口。
func (a *LegacyAdapter) StartOneLong(ctx context.Context, name string) error {
	return startOneLongRequest(ctx, a.client, name)
}

// StartOneLong 让新版协议连接也能使用同一个已核实的旧版启动入口。
func (a *PetV1Adapter) StartOneLong(ctx context.Context, name string) error {
	return startOneLongRequest(ctx, a.client, name)
}
