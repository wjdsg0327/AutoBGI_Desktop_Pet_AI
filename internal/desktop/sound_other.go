//go:build !windows

package desktop

// playKeywordSound 在非 Windows 平台保持空实现，确保桌宠仍可跨平台编译。
func playKeywordSound(_ string, _ ...string) {}

// stopKeywordSound 在非 Windows 平台保持空实现。
func stopKeywordSound() {}

// keywordSoundPlaybackSupported 明确告知前端当前平台没有原生音效实现。
func keywordSoundPlaybackSupported() bool { return false }
