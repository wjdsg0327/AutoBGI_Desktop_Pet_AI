package service

import (
	"errors"
	"net/http"
	"strings"

	"autobgi-pet/internal/skin"
)

// SkinCatalog 保留有效列表与独立告警，坏包不会让内置角色也从选择器中消失。
type SkinCatalog struct {
	Skins   []skin.Info `json:"skins"`
	Warning string      `json:"warning"`
}

// ListSkins 只返回描述和固定同源资源地址，不把安装目录或任意文件路径交给页面。
func (s *Service) ListSkins() SkinCatalog {
	s.mu.Lock()
	manager := s.skinStore
	s.mu.Unlock()
	if manager == nil {
		return SkinCatalog{Skins: []skin.Info{builtinSkin()}, Warning: "本地皮肤目录不可用，仍可使用内置角色"}
	}
	items, err := manager.List()
	if len(items) == 0 {
		items = []skin.Info{builtinSkin()}
	}
	warning := ""
	if err != nil {
		warning = err.Error()
	}
	return SkinCatalog{Skins: items, Warning: warning}
}

// builtinSkin 与内嵌素材协议一致，不要求用户配置目录可写。
func builtinSkin() skin.Info {
	return skin.Info{ID: "abgi-buddy", Name: "叶芽 / ABGI 小助手", Author: "AutoBGI Desktop Pet contributors", License: "CC0-1.0", ManifestURL: "/pets/default/manifest.json", BuiltIn: true}
}

// SelectSkin 只更新外观标识，原生导入对话框期间其他窗口修改的偏好不会被旧副本覆盖。
func (s *Service) SelectSkin(id string) (AppState, error) {
	s.mu.Lock()
	manager, closed := s.skinStore, s.closed
	s.mu.Unlock()
	if closed {
		return s.GetState(), errors.New("应用正在退出")
	}
	if id != "abgi-buddy" && (manager == nil || manager.Has(id) != nil) {
		return s.GetState(), errors.New("所选皮肤未安装或已损坏，请重新导入或选择内置角色")
	}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return s.GetState(), errors.New("应用正在退出")
	}
	previous := s.config.Preferences.Pet.SkinID
	s.config.Preferences.Pet.SkinID = id
	if err := s.store.Save(s.config); err != nil {
		s.config.Preferences.Pet.SkinID = previous
		s.mu.Unlock()
		return s.GetState(), err
	}
	s.mu.Unlock()
	s.publish()
	return s.GetState(), nil
}

// ImportSkinArchive 仅供原生已选择文件的入口使用，不能从 WebView 指定本地路径。
//
//wails:ignore
func (s *Service) ImportSkinArchive(path string) (skin.Info, error) {
	s.mu.Lock()
	manager, closed := s.skinStore, s.closed
	s.mu.Unlock()
	if closed || manager == nil {
		return skin.Info{}, errors.New("皮肤导入暂不可用，请检查本地应用数据目录")
	}
	return manager.Import(path)
}

// ServeSkinAsset 只接入自定义皮肤专属路径，响应内容继续受宿主同源安全策略限制。
//
//wails:ignore
func (s *Service) ServeSkinAsset(w http.ResponseWriter, r *http.Request) bool {
	s.mu.Lock()
	manager := s.skinStore
	s.mu.Unlock()
	if manager == nil {
		// 专用目录不可用时也不回落到单页应用，避免资源请求收到 HTML。
		if r.URL.Path == "/__pet/skins" || strings.HasPrefix(r.URL.Path, "/__pet/skins/") {
			http.NotFound(w, r)
			return true
		}
		return false
	}
	return manager.ServeHTTP(w, r)
}
