package desktop

import (
	"errors"

	"autobgi-pet/internal/profile"
)

// PreviewKeywordSound 试听指定的内置 MP3 或自定义 WAV，不修改已保存的音效类型。
func (d *DesktopService) PreviewKeywordSound(soundType string) error {
	if !profile.ValidKeywordSoundType(soundType) {
		return errors.New("关键词音效类型无效")
	}
	if soundType == profile.KeywordSoundTypeNone {
		return errors.New("当前规则未启用音效")
	}
	if d.stopped() {
		return errors.New("桌面宿主正在退出")
	}
	if !keywordSoundPlaybackSupported() {
		return errors.New("当前平台不支持关键词音效播放")
	}
	if soundType == profile.KeywordSoundTypeCustom && !d.service.KeywordSoundAvailable() {
		return errors.New("自定义音效文件不存在或已损坏，请重新选择")
	}
	playKeywordSound(soundType, d.service.KeywordSoundPath())
	return nil
}
