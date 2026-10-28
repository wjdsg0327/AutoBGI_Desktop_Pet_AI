package desktop

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync"

	"autobgi-pet/internal/service"
)

// profileTransferDialogGate 防止多个窗口同时打开资料文件对话框，避免重复导入或目标混淆。
var profileTransferDialogGate sync.Mutex

// ProfileImportResult 只在实际完成整批导入后返回新增数量和脱敏状态，取消选择时返回空值。
type ProfileImportResult struct {
	Count int              `json:"count"`
	State service.AppState `json:"state"`
}

// SaveProfilesFile 仅写入用户在原生保存对话框中明确选定的目标，前端不能提供文件路径。
func (d *DesktopService) SaveProfilesFile() (bool, error) {
	if d.app == nil || d.stopped() {
		return false, errors.New("桌面宿主尚未就绪")
	}
	if !profileTransferDialogGate.TryLock() {
		return false, errors.New("请先完成已经打开的连接资料文件对话框")
	}
	defer profileTransferDialogGate.Unlock()
	dialog := d.app.Dialog.SaveFile().
		SetFilename("AutoBGI-Pet-connections.json").
		SetMessage("导出不含密钥的连接资料；选择已有文件并确认保存将覆盖该文件。").
		SetButtonText("保存连接资料").
		AddFilter("连接资料 JSON", "*.json").
		AllowsOtherFileTypes(false).
		CanCreateDirectories(true)
	d.mu.Lock()
	panel := d.panel
	d.mu.Unlock()
	if panel != nil {
		dialog.AttachToWindow(panel)
	}
	path, err := dialog.PromptForSingleSelection()
	if err != nil {
		return false, errors.New("无法打开连接资料保存对话框")
	}
	if path == "" || d.stopped() {
		return false, nil
	}
	text, err := d.service.ExportProfiles()
	if err != nil {
		return false, err
	}
	if err = saveProfileTransferFile(path, []byte(text)); err != nil {
		return false, err
	}
	return true, nil
}

// ImportProfilesFile 只读取用户手动选择的普通文件，不接受 WebView 任意路径或自动连接指令。
func (d *DesktopService) ImportProfilesFile() (*ProfileImportResult, error) {
	if d.app == nil || d.stopped() {
		return nil, errors.New("桌面宿主尚未就绪")
	}
	if !profileTransferDialogGate.TryLock() {
		return nil, errors.New("请先完成已经打开的连接资料文件对话框")
	}
	defer profileTransferDialogGate.Unlock()
	dialog := d.app.Dialog.OpenFile().
		SetTitle("导入 AutoBGI 连接资料").
		SetMessage("选择不含密钥的连接资料 JSON；导入仅添加资料，之后需编辑并重新填写密钥。").
		SetButtonText("导入连接资料").
		AddFilter("连接资料 JSON（最大 64 KiB）", "*.json").
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
		return nil, errors.New("无法打开连接资料选择对话框")
	}
	if path == "" || d.stopped() {
		return nil, nil
	}
	data, err := readProfileTransferFile(path)
	if err != nil {
		return nil, err
	}
	state, err := d.service.ImportProfiles(string(data))
	if err != nil {
		return nil, err
	}
	// 服务已经逐字段校验整份文档，这里仅提取本次成功追加的数量供界面反馈。
	var document struct {
		Profiles []json.RawMessage `json:"profiles"`
	}
	_ = json.Unmarshal(data, &document)
	return &ProfileImportResult{Count: len(document.Profiles), State: state}, nil
}

// readProfileTransferFile 同时检查路径和已打开句柄，拒绝链接、目录、超限文件及读取期替换。
func readProfileTransferFile(path string) ([]byte, error) {
	before, err := os.Lstat(path)
	if err != nil || !before.Mode().IsRegular() {
		return nil, errors.New("请选择普通 JSON 文件，不能使用目录或符号链接")
	}
	if before.Size() > service.MaxProfileTransferBytes {
		return nil, errors.New("连接资料文件不能超过 64 KiB")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, errors.New("连接资料文件无法读取，请检查文件权限")
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !opened.Mode().IsRegular() || !os.SameFile(before, opened) {
		return nil, errors.New("选择的连接资料文件已经变化，请重新选择")
	}
	data, err := io.ReadAll(io.LimitReader(file, service.MaxProfileTransferBytes+1))
	if err != nil || len(data) > service.MaxProfileTransferBytes {
		return nil, errors.New("连接资料读取失败或超过 64 KiB")
	}
	return data, nil
}

// saveProfileTransferFile 先完整写入同目录临时文件再原子替换，失败保留原有导出文件。
func saveProfileTransferFile(path string, data []byte) error {
	errSave := errors.New("连接资料未保存，请检查目标文件类型和目录写入权限")
	if len(data) == 0 || len(data) > service.MaxProfileTransferBytes {
		return errSave
	}
	if existing, err := os.Lstat(path); err == nil {
		if !existing.Mode().IsRegular() {
			return errSave
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return errSave
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".pet-profile-export-*")
	if err != nil {
		return errSave
	}
	name := file.Name()
	defer os.Remove(name)
	if err = file.Chmod(0600); err != nil {
		file.Close()
		return errSave
	}
	if _, err = file.Write(data); err != nil {
		file.Close()
		return errSave
	}
	if err = file.Sync(); err != nil {
		file.Close()
		return errSave
	}
	if err = file.Close(); err != nil {
		return errSave
	}
	if err = os.Rename(name, path); err != nil {
		return errSave
	}
	return nil
}
