package autobgi

import (
	"bytes"
	"context"
	"image/jpeg"
	"io"
	"mime"
	"net/http"
)

// 与 AutoBGI 自身查看远端桌面时的单张截图上限保持一致。
const maxDesktopScreenshotBytes = 20 << 20

// ReadDesktopScreenshot 仅调用当前实例的“查看桌面”接口，返回供本次详情展示的 JPEG。
func (c *Client) ReadDesktopScreenshot(ctx context.Context) ([]byte, error) {
	response, err := c.doRequest(ctx, c.http, http.MethodGet, "/api/aBgiJt", nil, nil, "image/jpeg")
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()

	// 认证失败、重定向等错误沿用现有错误映射，成功响应必须是完整图片而非部分内容。
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, decodeResponse(response, nil)
	}
	mediaType, _, err := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if response.StatusCode != http.StatusOK || err != nil || mediaType != "image/jpeg" {
		return nil, &APIError{Code: "INCOMPATIBLE_RESPONSE", Message: "服务未返回 JPEG 桌面截图"}
	}

	// 同时限制已知长度和分块传输，读取 JPEG 头和结束标记时无需展开整张桌面位图。
	if response.ContentLength > maxDesktopScreenshotBytes {
		return nil, &APIError{Code: "INVALID_RESPONSE", Message: "桌面截图超过大小限制"}
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, maxDesktopScreenshotBytes+1))
	if err != nil || len(data) > maxDesktopScreenshotBytes {
		return nil, &APIError{Code: "INVALID_RESPONSE", Message: "桌面截图读取失败或超过大小限制"}
	}
	if _, err := jpeg.DecodeConfig(bytes.NewReader(data)); err != nil || !bytes.HasSuffix(data, []byte{0xff, 0xd9}) {
		return nil, &APIError{Code: "INVALID_RESPONSE", Message: "桌面截图格式无效或数据不完整"}
	}
	return data, nil
}
