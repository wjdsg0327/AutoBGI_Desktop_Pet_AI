package desktop

import (
	"errors"
	"sync"

	"autobgi-pet/internal/skin"
)

// skinImportDialogGate 避免多个窗口重复打开皮肤选择器并安装同一份文件。
var skinImportDialogGate sync.Mutex

// ImportSkin 只接受原生文件选择器选中的 ZIP，不开放任意文件读取绑定。
func (d *DesktopService) ImportSkin() (*skin.Info, error) {
	if d.app == nil || d.stopped() {
		return nil, errors.New("桌面宿主尚未就绪")
	}
	if !skinImportDialogGate.TryLock() {
		return nil, errors.New("请先完成已经打开的皮肤文件对话框")
	}
	defer skinImportDialogGate.Unlock()
	dialog := d.app.Dialog.OpenFile().SetTitle("导入桌宠皮肤 ZIP").
		AddFilter("桌宠皮肤包", "*.zip").CanChooseFiles(true).CanChooseDirectories(false).
		AllowsOtherFileTypes(false)
	d.mu.Lock()
	panel := d.panel
	d.mu.Unlock()
	if panel != nil {
		dialog.AttachToWindow(panel)
	}
	path, err := dialog.PromptForSingleSelection()
	if err != nil {
		return nil, errors.New("无法打开皮肤文件选择器，请重试")
	}
	if path == "" || d.stopped() {
		return nil, nil
	}
	info, err := d.service.ImportSkinArchive(path)
	if err != nil {
		return nil, err
	}
	return &info, nil
}
