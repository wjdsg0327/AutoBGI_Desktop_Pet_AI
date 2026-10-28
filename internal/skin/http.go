package skin

import (
	"net/http"
	"strconv"
	"strings"
)

// ServeHTTP 只拦截固定皮肤命名空间；命名空间内失败必须终止，避免落入前端首页回退。
func (m *Manager) ServeHTTP(w http.ResponseWriter, r *http.Request) bool {
	if r.URL.Path != strings.TrimSuffix(assetPrefix, "/") && !strings.HasPrefix(r.URL.Path, assetPrefix) {
		return false
	}
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; sandbox")
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "仅支持读取皮肤资源", http.StatusMethodNotAllowed)
		return true
	}
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, assetPrefix), "/")
	if len(parts) != 2 || !validID(parts[0]) || fileLimit(parts[1]) == 0 {
		http.NotFound(w, r)
		return true
	}
	m.mu.Lock()
	root, err := m.openRoot()
	var data packageData
	if err == nil {
		data, err = m.loadInstalled(root, parts[0])
		_ = root.Close()
	}
	m.mu.Unlock()
	if err != nil {
		http.NotFound(w, r)
		return true
	}
	var content []byte
	switch parts[1] {
	case "manifest.json":
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		content = data.manifest
	case "atlas.png":
		w.Header().Set("Content-Type", "image/png")
		content = data.atlas
	case "LICENSE.txt":
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		content = data.license
	}
	w.Header().Set("Content-Length", strconv.Itoa(len(content)))
	// Wails 的空响应默认使用 501，HEAD 必须显式提交成功状态，不能依赖 net/http 的默认值。
	w.WriteHeader(http.StatusOK)
	if r.Method == http.MethodGet {
		_, _ = w.Write(content)
	}
	return true
}
