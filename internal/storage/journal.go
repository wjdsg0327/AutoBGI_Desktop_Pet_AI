package storage

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"time"

	"autobgi-pet/internal/autobgi"
)

// CommandEntry 将回执绑定到固定来源，换地址后的同名连接不能查询旧来源命令。
type CommandEntry struct {
	BaseURL             string                 `json:"baseUrl"`
	Receipt             autobgi.CommandReceipt `json:"receipt"`
	NotificationHandled bool                   `json:"notificationHandled"` // 终态事实已处理，正文裁剪后不再生成重复提醒。
}

// EventScope 将游标和去重记录一起保存，通知展示必须在原子提交之后进行。
type EventScope struct {
	ProfileID  string   `json:"profileId"`
	BaseURL    string   `json:"baseUrl"`
	InstanceID string   `json:"instanceId"`
	Cursor     string   `json:"cursor"`
	Seen       []string `json:"seen"`
	Attempted  []string `json:"attempted"`
}

// Journal 仅保存已脱敏命令和事件元数据，绝不保存密钥或服务端原始事件内容。
type Journal struct {
	SchemaVersion             int                 `json:"schemaVersion"`
	Commands                  []CommandEntry      `json:"commands"`
	Scopes                    []EventScope        `json:"scopes"`
	MaterialScopes            []MaterialScope     `json:"materialScopes"`            // 与命令共用原子提交，保存安全材料事实及展示处理标记。
	TargetScopes              []TargetScope       `json:"targetScopes,omitempty"`    // 目标达标采用独立游标与正文，旧日志缺少时迁移为空。
	NotificationScopes        []NotificationScope `json:"notificationScopes"`        // 通知收件、已读与勿扰摘要和游标共同原子提交。
	NotificationErrorRevision string              `json:"notificationErrorRevision"` // 来源删除后也保留版本，旧页面不能误确认重建后的新错误。
}

var errJournal = errors.New("命令与事件记录无法安全保存；控制操作已停止，请检查配置目录权限")

// EmptyJournal 在没有历史记录时返回明确空集合，不伪造任何执行结果。
func EmptyJournal() Journal {
	return Journal{SchemaVersion: 1, Commands: []CommandEntry{}, Scopes: []EventScope{}, NotificationScopes: []NotificationScope{}, NotificationErrorRevision: "0"}
}

// journalPath 固定使用宿主的数据目录，前端不能指定任意磁盘路径。
func (s *Store) journalPath() string {
	return filepath.Join(s.dataDir, "command-event-journal.json")
}

// validJournal 为磁盘恢复施加容量和身份约束，损坏记录不能成为新的执行依据。
func validJournal(j Journal) bool {
	if j.SchemaVersion != 1 || len(j.Commands) > 200 || len(j.Scopes) > 40 ||
		!validMaterialScopes(j.MaterialScopes) || !validTargetScopes(j.TargetScopes) || !validNotificationScopes(j.NotificationScopes, true) ||
		!validNotificationErrorRevision(j.NotificationErrorRevision, j.NotificationScopes) {
		return false
	}
	for _, entry := range j.Commands {
		r := entry.Receipt
		if entry.BaseURL == "" || r.ProfileID == "" || r.InstanceID == "" || r.CommandID == "" || len(r.CommandID) > 64 {
			return false
		}
		switch r.Status {
		case "sending", "accepted", "executing", "succeeded", "failed", "rejected", "unknown":
		default:
			return false
		}
		if entry.NotificationHandled && r.Status != "succeeded" && r.Status != "failed" && r.Status != "rejected" {
			return false
		}
		if _, err := time.Parse(time.RFC3339Nano, r.UpdatedAt); err != nil {
			return false
		}
	}
	for _, scope := range j.Scopes {
		if scope.ProfileID == "" || scope.BaseURL == "" || scope.InstanceID == "" || len(scope.Cursor) > 512 || len(scope.Seen) > 256 || len(scope.Attempted) > 256 {
			return false
		}
		for _, ids := range [][]string{scope.Seen, scope.Attempted} {
			for _, id := range ids {
				if id == "" || len(id) > 512 {
					return false
				}
			}
		}
	}
	return true
}

// LoadJournal 将上次退出时尚未确认的发送状态恢复为 unknown，重启绝不自动重发。
func (s *Store) LoadJournal() (Journal, string) {
	file, err := os.Open(s.journalPath())
	if errors.Is(err, os.ErrNotExist) {
		return EmptyJournal(), ""
	}
	if err != nil {
		return EmptyJournal(), errJournal.Error()
	}
	data, err := io.ReadAll(io.LimitReader(file, journalByteLimit+1))
	// Windows 不允许替换仍由本进程占用的文件，恢复状态落盘前必须先关闭读取句柄。
	closeErr := file.Close()
	journal, valid := decodeJournal(data)
	if err != nil || closeErr != nil || len(data) > journalByteLimit || !valid {
		return EmptyJournal(), "命令与事件记录损坏，已安全忽略；请先在 AutoBGI 核对历史命令，桌宠不会自动重发"
	}
	changed := false
	for i := range journal.Commands {
		switch journal.Commands[i].Receipt.Status {
		case "sending", "accepted", "executing":
			journal.Commands[i].Receipt.Status = "unknown"
			journal.Commands[i].Receipt.Message = "应用重启后结果尚未核实，请查询原命令回执；不会自动重发"
			changed = true
		}
	}
	if changed && s.SaveJournal(journal) != nil {
		return journal, errJournal.Error()
	}
	return journal, ""
}

// SaveJournal 将命令结果、游标、去重和通知尝试标记通过单次原子替换一起提交。
func (s *Store) SaveJournal(journal Journal) error {
	// 旧版调用方的零值切片按空历史保存，已存在但损坏的磁盘字段不会经过此迁移。
	if journal.NotificationScopes == nil {
		journal.NotificationScopes = []NotificationScope{}
	}
	revision, valid := notificationJournalRevision(journal)
	if !valid {
		return errJournal
	}
	journal.NotificationErrorRevision = revision
	if !validJournal(journal) {
		return errJournal
	}
	// 损坏恢复返回的空历史不能覆盖原证据，即使其他调用方没有继续保留阻断状态。
	if file, err := os.Open(s.journalPath()); err == nil {
		data, readErr := io.ReadAll(io.LimitReader(file, journalByteLimit+1))
		closeErr := file.Close()
		previous, valid := decodeJournal(data)
		if readErr != nil || closeErr != nil || len(data) > journalByteLimit || !valid {
			return errJournal
		}
		// 旧调用方持有的快照不能回退磁盘版本，同版本的正文已读和勿扰更新仍可正常提交。
		currentRevision, _ := notificationRevision(journal.NotificationErrorRevision)
		previousRevision, _ := notificationRevision(previous.NotificationErrorRevision)
		if currentRevision < previousRevision {
			return errJournal
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return errJournal
	}
	data, err := json.Marshal(journal)
	if err != nil || len(data) > journalByteLimit || atomicWrite(s.journalPath(), data) != nil {
		return errJournal
	}
	return nil
}
