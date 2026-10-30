package desktop

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"unicode/utf8"

	"autobgi-pet/internal/profile"
	"autobgi-pet/internal/service"
)

// 分享文件只包含规则内容，不包含连接资料、密钥或本地规则标识。
const maxKeywordTransferBytes = 512 * 1024

var keywordTransferGate sync.Mutex

type keywordTransferRule struct {
	Keywords *[]string            `json:"keywords"`
	Action   *string              `json:"action"`
	Text     *string              `json:"text"`
	Enabled  *bool                `json:"enabled"`
	Todo     *profile.KeywordTodo `json:"todo,omitempty"`
}

type keywordTransferDocument struct {
	Version int                   `json:"version"`
	Rules   []keywordTransferRule `json:"rules"`
}

// ImportKeywordRulesFile 只读取用户选择的文件，返回经过校验的规则，不修改现有配置。
func (d *DesktopService) ImportKeywordRulesFile() ([]profile.KeywordRule, error) {
	if d.app == nil || d.stopped() {
		return nil, errors.New("桌面宿主尚未就绪")
	}
	if !keywordTransferGate.TryLock() {
		return nil, errors.New("请先完成已打开的规则文件对话框")
	}
	defer keywordTransferGate.Unlock()
	dialog := d.app.Dialog.OpenFile().SetTitle("导入关键词规则").
		AddFilter("关键词规则 JSON", "*.json").CanChooseFiles(true).CanChooseDirectories(false).AllowsOtherFileTypes(false)
	d.mu.Lock()
	panel := d.panel
	d.mu.Unlock()
	if panel != nil {
		dialog.AttachToWindow(panel)
	}
	path, err := dialog.PromptForSingleSelection()
	if err != nil {
		return nil, errors.New("无法打开规则文件选择器")
	}
	if path == "" || d.stopped() {
		return nil, nil
	}
	data, err := readKeywordTransfer(path)
	if err != nil {
		return nil, err
	}
	return decodeKeywordTransfer(data)
}

// decodeKeywordTransfer 先检查完整文档，再统一执行与保存规则相同的业务校验。
func decodeKeywordTransfer(data []byte) ([]profile.KeywordRule, error) {
	if !utf8.Valid(data) {
		return nil, errors.New("规则文件不是有效的 UTF-8 文本")
	}
	var document keywordTransferDocument
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&document); err != nil || (document.Version != 1 && document.Version != 2) || document.Rules == nil {
		return nil, errors.New("规则文件格式无效或版本不受支持")
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return nil, errors.New("规则文件包含多余内容")
	}
	if len(document.Rules) > 40 {
		return nil, errors.New("最多导入 40 条关键词规则")
	}
	rules := make([]profile.KeywordRule, 0, len(document.Rules))
	for _, item := range document.Rules {
		if item.Keywords == nil || item.Action == nil || item.Text == nil || item.Enabled == nil {
			return nil, errors.New("规则文件缺少必要字段")
		}
		if document.Version == 1 && item.Todo != nil {
			return nil, errors.New("旧版规则文件包含不支持的代办字段")
		}
		// 编辑界面使用逗号分隔关键词，拒绝无法无损呈现的单个关键词。
		for _, keyword := range *item.Keywords {
			if strings.ContainsAny(keyword, ",，") {
				return nil, errors.New("单个关键词不能包含逗号")
			}
		}
		rules = append(rules, profile.KeywordRule{Keywords: *item.Keywords, Action: *item.Action, Text: *item.Text, Enabled: *item.Enabled, Todo: item.Todo})
	}
	return service.ValidateKeywordRules(rules)
}

// ExportKeywordRulesFile 先校验草稿，再通过原生对话框选择导出位置。
func (d *DesktopService) ExportKeywordRulesFile(input []profile.KeywordRule) (bool, error) {
	if d.app == nil || d.stopped() {
		return false, errors.New("桌面宿主尚未就绪")
	}
	rules, err := service.ValidateKeywordRules(input)
	if err != nil {
		return false, err
	}
	// 第二版带可选代办设置，第一版仍可导入并默认关闭代办。
	document := keywordTransferDocument{Version: 2, Rules: make([]keywordTransferRule, 0, len(rules))}
	for _, rule := range rules {
		document.Rules = append(document.Rules, keywordTransferRule{Keywords: &rule.Keywords, Action: &rule.Action, Text: &rule.Text, Enabled: &rule.Enabled, Todo: rule.Todo})
	}
	data, err := json.MarshalIndent(document, "", "  ")
	if err != nil || len(data) > maxKeywordTransferBytes {
		return false, errors.New("规则内容无法导出或文件超过大小限制")
	}
	if !keywordTransferGate.TryLock() {
		return false, errors.New("请先完成已打开的规则文件对话框")
	}
	defer keywordTransferGate.Unlock()
	dialog := d.app.Dialog.SaveFile().SetFilename("AutoBGI-Pet-keyword-rules.json").
		SetMessage("导出当前编辑的规则；确认保存会覆盖所选文件。").
		AddFilter("关键词规则 JSON", "*.json").AllowsOtherFileTypes(false).CanCreateDirectories(true)
	d.mu.Lock()
	panel := d.panel
	d.mu.Unlock()
	if panel != nil {
		dialog.AttachToWindow(panel)
	}
	path, err := dialog.PromptForSingleSelection()
	if err != nil {
		return false, errors.New("无法打开规则保存对话框")
	}
	if path == "" || d.stopped() {
		return false, nil
	}
	if err := writeKeywordTransfer(path, data); err != nil {
		return false, err
	}
	return true, nil
}

// 读取时核对路径和文件句柄，限制文件大小并拒绝目录或链接。
func readKeywordTransfer(path string) ([]byte, error) {
	before, err := os.Lstat(path)
	if err != nil || !before.Mode().IsRegular() || before.Size() > maxKeywordTransferBytes {
		return nil, errors.New("请选择不超过 512 KiB 的普通规则文件")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, errors.New("规则文件无法读取")
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !opened.Mode().IsRegular() || !os.SameFile(before, opened) {
		return nil, errors.New("规则文件已经变化，请重新选择")
	}
	data, err := io.ReadAll(io.LimitReader(file, maxKeywordTransferBytes+1))
	if err != nil || len(data) > maxKeywordTransferBytes {
		return nil, errors.New("规则文件读取失败或超过 512 KiB")
	}
	return data, nil
}

// 导出先写同目录临时文件，写入完成后再替换目标，避免留下不完整的分享文件。
func writeKeywordTransfer(path string, data []byte) error {
	failure := errors.New("规则文件未保存，请检查目标文件和目录权限")
	if existing, err := os.Lstat(path); err == nil {
		if !existing.Mode().IsRegular() {
			return failure
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return failure
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".pet-rules-export-*")
	if err != nil {
		return failure
	}
	name := file.Name()
	defer os.Remove(name)
	if _, err = file.Write(data); err != nil {
		file.Close()
		return failure
	}
	if err = file.Sync(); err != nil {
		file.Close()
		return failure
	}
	if err = file.Close(); err != nil {
		return failure
	}
	if err = os.Rename(name, path); err != nil {
		return failure
	}
	return nil
}
