package storage

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"
)

// TodoRecord 冻结首次命中的来源和动作，不保存密钥或原始接口响应。
type TodoRecord struct {
	ID          string `json:"id"`
	RuleID      string `json:"ruleId"`
	ProfileID   string `json:"profileId"`
	ProfileName string `json:"profileName"`
	BaseURL     string `json:"baseUrl"`
	Text        string `json:"text"`
	Action      string `json:"action"`
	Target      string `json:"target"`
	CreatedAt   string `json:"createdAt"`
	ExpiresAt   string `json:"expiresAt"`
}

// TodoLimit 防止异常日志或频繁重建规则导致本地代办无限增长。
const TodoLimit = 1000

const todoBytesLimit = 32 * 1024 * 1024

var errTodos = errors.New("代办记录无法安全读写，请检查本地数据目录")

type todoFile struct {
	Version int          `json:"version"`
	Items   []TodoRecord `json:"items"`
}

// validTodos 校验持久化内容，异常文件不能被当作可执行的代办。
func validTodos(items []TodoRecord) bool {
	if items == nil || len(items) > TodoLimit {
		return false
	}
	ids := make(map[string]bool, len(items))
	for _, item := range items {
		created, createdErr := time.Parse(time.RFC3339Nano, item.CreatedAt)
		expires, expiresErr := time.Parse(time.RFC3339Nano, item.ExpiresAt)
		if !validHex(item.ID, 32) || ids[item.ID] || item.RuleID == "" || len(item.RuleID) > 128 ||
			item.ProfileID == "" || len(item.ProfileID) > 128 || item.BaseURL == "" || len(item.BaseURL) > 2048 ||
			!utf8.ValidString(item.ProfileName) || utf8.RuneCountInString(item.ProfileName) > 80 ||
			!utf8.ValidString(item.Text) || utf8.RuneCountInString(item.Text) > 4097 ||
			!utf8.ValidString(item.Target) || utf8.RuneCountInString(item.Target) > 512 ||
			strings.ContainsAny(item.RuleID+item.ProfileID+item.BaseURL+item.Target, "\x00\r\n") ||
			createdErr != nil || expiresErr != nil || !expires.After(created) {
			return false
		}
		switch item.Action {
		case "one_long", "script_group":
			if strings.TrimSpace(item.Target) == "" {
				return false
			}
		case "close_bgi_game", "shutdown":
			if item.Target != "" {
				return false
			}
		default:
			return false
		}
		ids[item.ID] = true
	}
	return true
}

// LoadTodos 有界读取独立文件；损坏时保留原文件并禁止覆盖。
func (s *Store) LoadTodos() ([]TodoRecord, error) {
	file, err := os.Open(filepath.Join(s.dataDir, "todos.json"))
	if errors.Is(err, os.ErrNotExist) {
		return []TodoRecord{}, nil
	}
	if err != nil {
		return nil, errTodos
	}
	data, readErr := io.ReadAll(io.LimitReader(file, todoBytesLimit+1))
	closeErr := file.Close()
	var document todoFile
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if readErr != nil || closeErr != nil || len(data) > todoBytesLimit ||
		decoder.Decode(&document) != nil || document.Version != 1 || !validTodos(document.Items) ||
		decoder.Decode(new(any)) != io.EOF {
		return nil, errTodos
	}
	return document.Items, nil
}

// SaveTodos 先校验完整副本，再通过同目录原子替换提交。
func (s *Store) SaveTodos(items []TodoRecord) error {
	if items == nil {
		items = []TodoRecord{}
	}
	if !validTodos(items) {
		return errTodos
	}
	data, err := json.Marshal(todoFile{Version: 1, Items: items})
	if err != nil || len(data) > todoBytesLimit || atomicWrite(filepath.Join(s.dataDir, "todos.json"), data) != nil {
		return errTodos
	}
	return nil
}
