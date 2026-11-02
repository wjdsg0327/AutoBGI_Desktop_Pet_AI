package desktop

import (
	"embed"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"sync"

	"autobgi-pet/internal/profile"
)

//go:embed assets/sounds/*.mp3
var builtinSoundFS embed.FS

var (
	// builtinSoundFiles 将原有配置值映射到用户提供的内置 MP3。
	builtinSoundFiles = map[string]string{
		profile.KeywordSoundTypeInfo:     "assets/sounds/calm.mp3",
		profile.KeywordSoundTypeQuestion: "assets/sounds/happy.mp3",
		profile.KeywordSoundTypeWarning:  "assets/sounds/wave.mp3",
		profile.KeywordSoundTypeError:    "assets/sounds/sad.mp3",
		profile.KeywordSoundTypeOK:       "assets/sounds/surprised.mp3",
	}
	builtinSoundMu    sync.Mutex
	builtinSoundPaths = map[string]string{}
)

// builtinSoundPath 将内嵌 MP3 提取到临时目录，供 Windows 原生 MCI 播放。
func builtinSoundPath(soundType string) (string, error) {
	resource, ok := builtinSoundFiles[soundType]
	if !ok {
		return "", errors.New("内置关键词音效类型无效")
	}
	builtinSoundMu.Lock()
	defer builtinSoundMu.Unlock()
	if path := builtinSoundPaths[soundType]; path != "" {
		if info, err := os.Stat(path); err == nil && info.Mode().IsRegular() {
			return path, nil
		}
	}
	data, err := builtinSoundFS.ReadFile(resource)
	if err != nil {
		return "", errors.New("内置关键词音效资源缺失")
	}
	// 每个进程使用独立目录，避免多个桌宠实例互相删除正在播放的 MP3。
	root := filepath.Join(os.TempDir(), "AutoBGI-Pet", "builtin-sounds", strconv.Itoa(os.Getpid()))
	if err := os.MkdirAll(root, 0700); err != nil {
		return "", errors.New("内置关键词音效目录不可用")
	}
	path := filepath.Join(root, soundType+".mp3")
	if err := os.WriteFile(path, data, 0600); err != nil {
		return "", errors.New("内置关键词音效无法准备播放")
	}
	builtinSoundPaths[soundType] = path
	return path, nil
}

// cleanupBuiltinSoundAssets 退出时清理本进程提取的 MP3，避免临时目录长期积累。
func cleanupBuiltinSoundAssets() {
	builtinSoundMu.Lock()
	paths := make([]string, 0, len(builtinSoundPaths))
	for _, path := range builtinSoundPaths {
		paths = append(paths, path)
	}
	builtinSoundPaths = map[string]string{}
	builtinSoundMu.Unlock()
	for _, path := range paths {
		_ = os.Remove(path)
	}
	if len(paths) > 0 {
		_ = os.Remove(filepath.Dir(paths[0]))
	}
}
