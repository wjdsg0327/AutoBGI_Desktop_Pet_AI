package desktop

import (
	"errors"
	"sync"

	"autobgi-pet/internal/service"
)

// keywordSoundDialogGate 防止多个窗口同时替换用户音效，避免选中文件互相覆盖。
var keywordSoundDialogGate sync.Mutex

// SelectKeywordCustomSound 通过原生文件选择器导入合法 WAV，并切换到自定义音效。
func (d *DesktopService) SelectKeywordCustomSound() (service.AppState, error) {
	if d.app == nil || d.stopped() {
		return d.service.GetState(), errors.New("桌面宿主尚未就绪")
	}
	if !keywordSoundDialogGate.TryLock() {
		return d.service.GetState(), errors.New("请先完成已经打开的音效文件对话框")
	}
	defer keywordSoundDialogGate.Unlock()
	dialog := d.app.Dialog.OpenFile().
		SetTitle("选择关键词自定义音效").
		SetMessage("仅支持 10 MiB 以内、最长 30 秒的 PCM WAV 文件。").
		SetButtonText("使用此音效").
		AddFilter("PCM WAV 音效", "*.wav").
		CanChooseFiles(true).
		CanChooseDirectories(false).
		AllowsOtherFileTypes(false)
	d.mu.Lock()
	panel := d.panel
	d.mu.Unlock()
	if panel != nil {
		dialog.AttachToWindow(panel)
	}
	path, err := dialog.PromptForSingleSelection()
	if err != nil {
		return d.service.GetState(), errors.New("无法打开自定义音效选择器，请重试")
	}
	if path == "" || d.stopped() {
		return d.service.GetState(), nil
	}
	// 先停止可能仍在播放的旧 WAV，减少 Windows 删除或替换内部文件失败的机会。
	stopKeywordSound()
	return d.service.UpdateKeywordCustomSound(path)
}

// ClearKeywordCustomSound 删除已保存的自定义音效并回退到安静陪伴语音。
func (d *DesktopService) ClearKeywordCustomSound() (service.AppState, error) {
	if d.stopped() {
		return d.service.GetState(), errors.New("桌面宿主正在退出")
	}
	// 清除前停止异步播放，确保旧文件可以被可靠删除。
	stopKeywordSound()
	return d.service.ClearKeywordCustomSound()
}
