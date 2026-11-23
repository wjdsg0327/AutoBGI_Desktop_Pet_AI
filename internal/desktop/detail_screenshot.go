package desktop

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"autobgi-pet/internal/notification"
)

// 图片地址只标识桌宠内存中的某次详情，不包含服务地址或密钥。
const screenshotPrefix = "/pet-screenshots/"

// screenshotURL 仅为有图的通知生成地址，普通台词及失败提示都没有图片资源。
func screenshotURL(entry notification.Entry) string {
	if len(entry.Screenshot) == 0 {
		return ""
	}
	return screenshotPrefix + strings.Replace(entry.ID, ":", "-", 1) + ".jpg"
}

// bubblePayload 只广播文字和预览地址，避免多个窗口复制截图数据。
func bubblePayload(entry notification.Entry) map[string]any {
	return map[string]any{"text": entry.Message, "kind": entry.Kind,
		"screenshotUrl": screenshotURL(entry), "screenshotFailed": entry.ScreenshotFailed}
}

// ServeDetailScreenshot 向气泡提供当前详情的 JPEG，旧地址、已过期的通知和旧连接均无法取图。
//
//wails:ignore
func (d *DesktopService) ServeDetailScreenshot(w http.ResponseWriter, r *http.Request) bool {
	if !strings.HasPrefix(r.URL.Path, screenshotPrefix) {
		return false
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Cross-Origin-Resource-Policy", "same-origin")
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		w.WriteHeader(http.StatusMethodNotAllowed)
		return true
	}

	// 在锁内核对截止时间和实时连接，释放锁后再将只读切片写入 WebView。
	d.mu.Lock()
	var data []byte
	if !d.stopped() && d.presentationVisibleLocked() && d.notices.center != nil &&
		r.URL.Path == screenshotURL(d.bubbleEntry) && d.prefs.Notifications.Bubble && !d.prefs.Notifications.QuietHoursEnabled {
		deadline, timed := d.notices.center.NextDeadline()
		if timed && time.Now().Before(deadline) && notificationScope(d.service.GetState()) == d.notices.scope {
			data = d.bubbleEntry.Screenshot
		}
	}
	d.mu.Unlock()
	if len(data) == 0 {
		http.NotFound(w, r)
		return true
	}
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	_, _ = w.Write(data)
	return true
}
