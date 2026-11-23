package profile

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode"
)

// NormalizeRecoveryShortcut 将按键录入结果规范化为 Wails 全局快捷键格式。
func NormalizeRecoveryShortcut(value string) (string, error) {
	parts := strings.Split(strings.TrimSpace(value), "+")
	if len(parts) < 2 {
		return "", errors.New("快捷键至少需要一个修饰键和一个主键")
	}
	seen := make(map[string]bool, len(parts)-1)
	modifiers := make([]string, 0, len(parts)-1)
	for _, raw := range parts[:len(parts)-1] {
		name := strings.ToLower(strings.TrimSpace(raw))
		var canonical string
		switch name {
		case "ctrl", "control", "cmdorctrl":
			canonical = "Ctrl"
		case "alt", "option", "optionoralt":
			canonical = "Alt"
		case "shift":
			canonical = "Shift"
		case "win", "super":
			// Wails 解析器使用 Super 表示 Windows 键，界面层可显示为 Win。
			canonical = "Super"
		default:
			return "", fmt.Errorf("不支持的快捷键修饰键：%s", raw)
		}
		if seen[canonical] {
			return "", fmt.Errorf("快捷键修饰键重复：%s", canonical)
		}
		seen[canonical] = true
		modifiers = append(modifiers, canonical)
	}
	rawKey := strings.TrimSpace(parts[len(parts)-1])
	if rawKey == "" {
		return "", errors.New("快捷键主键不能为空")
	}
	key := normalizeShortcutKey(rawKey)
	if key == "" {
		return "", fmt.Errorf("不支持的快捷键主键：%s", rawKey)
	}
	order := []string{"Ctrl", "Alt", "Shift", "Super"}
	result := make([]string, 0, len(modifiers)+1)
	for _, name := range order {
		if seen[name] {
			result = append(result, name)
		}
	}
	result = append(result, key)
	return strings.Join(result, "+"), nil
}

// normalizeShortcutKey 只允许 Wails 能识别的 ASCII 主键和功能键。
func normalizeShortcutKey(value string) string {
	lower := strings.ToLower(strings.TrimSpace(value))
	named := map[string]string{
		"backspace": "Backspace", "tab": "Tab", "return": "Return", "enter": "Enter",
		"escape": "Escape", "esc": "Escape", "left": "Left", "right": "Right",
		"up": "Up", "down": "Down", "space": "Space", "delete": "Delete",
		"home": "Home", "end": "End", "pageup": "Page Up", "page up": "Page Up",
		"pagedown": "Page Down", "page down": "Page Down", "forward delete": "Forward Delete",
		"numlock": "NumLock", "plus": "plus",
	}
	if key, ok := named[lower]; ok {
		return key
	}
	if strings.HasPrefix(lower, "f") {
		if number, err := strconv.Atoi(strings.TrimPrefix(lower, "f")); err == nil && number >= 1 && number <= 35 {
			return "F" + strconv.Itoa(number)
		}
	}
	if len([]rune(lower)) != 1 {
		return ""
	}
	r := []rune(lower)[0]
	if unicode.IsControl(r) || r > unicode.MaxASCII || unicode.IsSpace(r) {
		return ""
	}
	return strings.ToUpper(string(r))
}
