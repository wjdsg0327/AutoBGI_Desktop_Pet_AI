//go:build windows

package desktop

import (
	"fmt"
	"strings"
	"sync"
	"unsafe"

	"autobgi-pet/internal/profile"

	"golang.org/x/sys/windows"
)

var (
	// winmmDLL 负责播放内嵌 MP3 和用户选择的 WAV。
	winmmDLL = windows.NewLazySystemDLL("winmm.dll")
	// playSound 保留给用户自定义 WAV 音效。
	playSound = winmmDLL.NewProc("PlaySoundW")
	// mciSendStringW 播放内嵌 MP3，Windows 自带的 MCI mpegvideo 驱动负责解码。
	mciSendString = winmmDLL.NewProc("mciSendStringW")
)

var (
	builtinPlaybackMu sync.Mutex
	builtinAlias      string
	builtinAliasID    uint64
	// soundRequestMu 串行化停止与开始请求，避免并发试听和关键词触发交叉。
	soundRequestMu sync.Mutex
)

// keywordSoundPlaybackSupported 表示 Windows 原生 MP3/WAV 播放链路可用。
func keywordSoundPlaybackSupported() bool { return true }

// playKeywordSound 播放内嵌 MP3 或用户自定义 WAV，失败时静默返回。
func playKeywordSound(soundType string, customPath ...string) {
	soundRequestMu.Lock()
	defer soundRequestMu.Unlock()
	playKeywordSoundLocked(soundType, customPath...)
}

// playKeywordSoundLocked 在请求锁内切换当前音效，避免两个入口同时抢占播放设备。
func playKeywordSoundLocked(soundType string, customPath ...string) {
	// 新动作开始前停止上一个音效，避免连续命中时多个声音叠加。
	stopKeywordSoundLocked()
	if soundType == profile.KeywordSoundTypeCustom {
		if len(customPath) == 0 || customPath[0] == "" {
			return
		}
		path, err := windows.UTF16PtrFromString(customPath[0])
		if err != nil {
			return
		}
		const (
			sndAsync     = uintptr(0x0001)
			sndNodefault = uintptr(0x0002)
			sndFilename  = uintptr(0x00020000)
		)
		// PlaySoundW 的第二个参数是模块句柄，第三个参数才是播放标志。
		_, _, _ = playSound.Call(uintptr(unsafe.Pointer(path)), 0, sndAsync|sndNodefault|sndFilename)
		return
	}
	path, err := builtinSoundPath(soundType)
	if err != nil {
		return
	}
	builtinPlaybackMu.Lock()
	builtinAliasID++
	alias := fmt.Sprintf("autobgi_sound_%d", builtinAliasID)
	builtinAlias = alias
	builtinPlaybackMu.Unlock()
	go playBuiltinMP3(alias, path)
}

// playBuiltinMP3 使用独立协程等待播放完成，再关闭 MCI 媒体别名。
func playBuiltinMP3(alias, path string) {
	escapedPath := strings.ReplaceAll(path, `"`, `\"`)
	// 打开媒体时持有轮次锁，避免旧播放在停止后又迟到打开文件。
	builtinPlaybackMu.Lock()
	if builtinAlias != alias {
		builtinPlaybackMu.Unlock()
		return
	}
	if mciCommand(fmt.Sprintf(`open "%s" type mpegvideo alias %s`, escapedPath, alias)) != 0 {
		builtinPlaybackMu.Unlock()
		clearBuiltinAlias(alias)
		return
	}
	if builtinAlias != alias {
		_ = mciCommand("close " + alias)
		builtinPlaybackMu.Unlock()
		return
	}
	builtinPlaybackMu.Unlock()
	_ = mciCommand("play " + alias + " wait")
	_ = mciCommand("close " + alias)
	clearBuiltinAlias(alias)
}

// mciCommand 调用 Windows MCI 字符串接口，返回零表示命令成功。
func mciCommand(command string) uint32 {
	value, err := windows.UTF16PtrFromString(command)
	if err != nil {
		return 1
	}
	result, _, _ := mciSendString.Call(uintptr(unsafe.Pointer(value)), 0, 0, 0)
	return uint32(result)
}

// clearBuiltinAlias 只清除仍属于当前播放轮次的别名。
func clearBuiltinAlias(alias string) {
	builtinPlaybackMu.Lock()
	if builtinAlias == alias {
		builtinAlias = ""
	}
	builtinPlaybackMu.Unlock()
}

// stopKeywordSound 停止异步 WAV 和内嵌 MP3，避免多个音效叠加或文件被占用。
func stopKeywordSound() {
	soundRequestMu.Lock()
	defer soundRequestMu.Unlock()
	stopKeywordSoundLocked()
}

// stopKeywordSoundLocked 在请求锁内停止当前音效。
func stopKeywordSoundLocked() {
	_, _, _ = playSound.Call(0, 0, 0)
	builtinPlaybackMu.Lock()
	alias := builtinAlias
	builtinAlias = ""
	builtinPlaybackMu.Unlock()
	if alias != "" {
		_ = mciCommand("stop " + alias)
		_ = mciCommand("close " + alias)
	}
}
