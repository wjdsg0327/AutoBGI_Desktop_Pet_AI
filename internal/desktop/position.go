package desktop

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// savedPosition 保存逻辑坐标与显示器相对位置；不混入网络配置或凭据。
type savedPosition struct {
	Version   int    `json:"version"`
	Monitor   string `json:"monitor"`
	X         int    `json:"x"`
	Y         int    `json:"y"`
	RelativeX int    `json:"relativeX"`
	RelativeY int    `json:"relativeY"`
}

// positionPath 只使用系统当前用户目录，不接收前端路径。
func positionPath() string {
	root, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(root, "AutoBGI-Pet", "position.json")
}

// loadPosition 有界读取并拒绝未知版本，损坏时由宿主重置到可见工作区。
func loadPosition() (savedPosition, bool) {
	info, err := os.Stat(positionPath())
	if err != nil || info.Size() > 4096 {
		return savedPosition{}, false
	}
	data, err := os.ReadFile(positionPath())
	var p savedPosition
	if err != nil || json.Unmarshal(data, &p) != nil || p.Version != 1 {
		return savedPosition{}, false
	}
	return p, true
}

// savePosition 先完整落盘再原子替换，不能先删除旧位置文件。
func savePosition(p savedPosition) {
	path := positionPath()
	if path == "" {
		return
	}
	data, err := json.Marshal(p)
	if err != nil {
		return
	}
	if os.MkdirAll(filepath.Dir(path), 0700) != nil {
		return
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".position-*")
	if err != nil {
		return
	}
	defer os.Remove(file.Name())
	if _, err = file.Write(data); err != nil {
		file.Close()
		return
	}
	if file.Sync() != nil {
		file.Close()
		return
	}
	if file.Close() != nil {
		return
	}
	_ = replacePosition(file.Name(), path)
}
