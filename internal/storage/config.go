// Package storage 使用有界 JSON、备份和同目录原子替换持久化非敏感配置。
package storage

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"

	"autobgi-pet/internal/profile"
)

// Store 由宿主确定配置与其他数据的路径。
type Store struct {
	path    string
	dataDir string
}

var errConfig = errors.New("桌宠配置读取或保存失败，请检查程序所在目录的读写权限")

// New 将配置保存在可执行文件所在目录。
func New() (*Store, error) {
	executable, err := os.Executable()
	if err != nil {
		return nil, errConfig
	}
	store, err := NewAt(filepath.Dir(executable))
	if err != nil {
		return nil, err
	}
	// 历史缓存与事件记录维持原位置，不随连接配置迁移。
	if directory, configErr := os.UserConfigDir(); configErr == nil {
		store.dataDir = filepath.Join(directory, "AutoBGI-Pet")
		if err := os.MkdirAll(store.dataDir, 0700); err != nil {
			return nil, errors.New("桌宠历史数据目录无法访问，请检查当前用户应用数据目录权限")
		}
	} else {
		return nil, errors.New("桌宠历史数据目录无法访问，请检查当前用户应用数据目录权限")
	}
	return store, nil
}

// NewAt 仅供 Go 宿主和隔离测试使用，未注册为前端绑定。
func NewAt(directory string) (*Store, error) {
	if err := os.MkdirAll(directory, 0700); err != nil {
		return nil, errConfig
	}
	return &Store{path: filepath.Join(directory, "config.json"), dataDir: directory}, nil
}

// decode 限制文件体积和 schemaVersion，防止配置损坏产生无限读取。
func decode(path string) (profile.Config, error) {
	file, err := os.Open(path)
	if err != nil {
		return profile.Config{}, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, 1024*1024+1))
	if err != nil || len(data) > 1024*1024 {
		return profile.Config{}, errConfig
	}
	var result profile.Config
	if json.Unmarshal(data, &result) != nil || result.SchemaVersion != 1 || len(result.Profiles) > 20 {
		return profile.Config{}, errConfig
	}
	// 旧配置或缺失字段不能隐式开启控制，只有显式 readOnly:false 才保留用户授权。
	var flags struct {
		Profiles []struct {
			ReadOnly *bool `json:"readOnly"`
		} `json:"profiles"`
	}
	if json.Unmarshal(data, &flags) != nil || len(flags.Profiles) != len(result.Profiles) {
		return profile.Config{}, errConfig
	}
	for i := range result.Profiles {
		if flags.Profiles[i].ReadOnly == nil {
			result.Profiles[i].ReadOnly = true
		}
	}
	if result.Profiles == nil {
		result.Profiles = []profile.Profile{}
	}
	if result.Preferences.Pet.Scale < 0.6 || result.Preferences.Pet.Scale > 2 {
		result.Preferences.Pet.Scale = 1
	}
	if result.Preferences.Pet.SkinID == "" {
		result.Preferences.Pet.SkinID = "abgi-buddy"
	}
	if result.Preferences.Pet.RecoveryShortcut == "" {
		result.Preferences.Pet.RecoveryShortcut = "Ctrl+Shift+P"
	}
	return result, nil
}

// Load 配置损坏时尝试备份并给出告警，绝不把损坏内容当有效连接使用。
func (s *Store) Load() (profile.Config, string) {
	result, err := decode(s.path)
	if err == nil {
		return result, ""
	}
	if errors.Is(err, os.ErrNotExist) {
		// 优先恢复程序目录里的备份，避免旧配置覆盖现有数据。
		if backup, backupErr := decode(s.path + ".bak"); backupErr == nil {
			return backup, "配置文件缺失，已从程序目录的有效备份恢复；请检查连接设置"
		} else if !errors.Is(backupErr, os.ErrNotExist) {
			return profile.DefaultConfig(), "配置文件缺失且备份不可用；原备份保留，请检查连接设置"
		}
		return profile.DefaultConfig(), ""
	}
	if backup, backupErr := decode(s.path + ".bak"); backupErr == nil {
		return backup, "配置损坏，已从上次有效备份恢复；请检查连接设置"
	}
	return profile.DefaultConfig(), "配置损坏且没有可用备份，已恢复安全默认设置；原文件保留，保存后才替换"
}

// Save 先备份有效旧版本，再用完整落盘的临时文件替换，失败不破坏旧配置。
func (s *Store) Save(config profile.Config) error {
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil || len(data) > 1024*1024 {
		return errConfig
	}
	if _, err := decode(s.path); err == nil {
		previous, readErr := os.ReadFile(s.path)
		if readErr != nil || atomicWrite(s.path+".bak", previous) != nil {
			return errConfig
		}
	}
	if atomicWrite(s.path, data) != nil {
		return errConfig
	}
	return nil
}

// atomicWrite 的临时文件始终与目标同目录，避免跨卷替换和半写入文件。
func atomicWrite(path string, data []byte) error {
	file, err := os.CreateTemp(filepath.Dir(path), ".pet-config-*")
	if err != nil {
		return err
	}
	name := file.Name()
	defer os.Remove(name)
	if err := file.Chmod(0600); err != nil {
		file.Close()
		return err
	}
	if _, err := file.Write(data); err != nil {
		file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return replace(name, path)
}
