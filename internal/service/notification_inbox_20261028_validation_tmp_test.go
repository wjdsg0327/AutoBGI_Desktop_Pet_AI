//go:build notification_inbox_validation

package service

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"autobgi-pet/internal/autobgi"
	"autobgi-pet/internal/credential"
	"autobgi-pet/internal/profile"
	"autobgi-pet/internal/storage"
)

// 隔离凭据桩禁止任何实际系统凭据读写，测试也不创建网络连接或窗口。
type inboxCredentialStub struct{}

func (inboxCredentialStub) Read(string) (string, error) { return "", credential.ErrNotFound }
func (inboxCredentialStub) Write(string, string) error  { return nil }
func (inboxCredentialStub) Delete(string) error         { return nil }

// 每个服务使用独立普通目录和空白日志，所有身份均为合成记录。
func inboxFixture(t *testing.T) (*Service, profile.Profile, string) {
	t.Helper()
	root := t.TempDir()
	store, err := storage.NewAt(filepath.Join(root, "AutoBGI-Pet"))
	if err != nil {
		t.Fatal(err)
	}
	p := profile.Profile{ID: strings.Repeat("1", 32), Name: "隔离连接", BaseURL: "http://127.0.0.1:1",
		CredentialRef: "AutoBGI-Pet/profile/" + strings.Repeat("1", 32), AdapterMode: "pet-v1", SessionOnly: true, ReadOnly: true}
	config := profile.DefaultConfig()
	config.Profiles, config.ActiveProfileID = []profile.Profile{p}, p.ID
	if err := store.Save(config); err != nil {
		t.Fatal(err)
	}
	instance := strings.Repeat("a", 32)
	s := &Service{store: store, config: config, credentials: inboxCredentialStub{}, sessionKeys: map[string]string{}, journal: storage.EmptyJournal()}
	s.state.Connection = ConnectionState{ProfileID: p.ID, Status: "online", Generation: 1}
	s.state.Capabilities = &autobgi.Capabilities{SourceMode: "pet-v1", InstanceID: &instance}
	return s, p, root
}

func inboxReadInput(s *Service, ids ...string) NotificationReadInput {
	scope, _ := s.notificationContextLocked()
	return NotificationReadInput{ProfileID: scope.ProfileID, BaseURL: scope.BaseURL, SourceMode: scope.SourceMode, InstanceID: scope.InstanceID, AccountID: scope.AccountID, IDs: ids}
}

// 本地事实落盘后才进入实时队列；实际 New 加载历史不会把正文再次投递。
func TestNotificationInboxLocalRestart(t *testing.T) {
	s, _, root := inboxFixture(t)
	s.addNoticeLocked("error", "固定错误事实")
	if len(s.journal.NotificationScopes) != 1 || len(s.state.Notifications) != 1 {
		t.Fatalf("没有同次持久记录与实时投递: %#v", s.state)
	}
	entry := s.journal.NotificationScopes[0].Entries[0]
	if entry.AttemptedAt == nil || entry.ReadAt != nil || entry.QuietPending {
		t.Fatalf("展示尝试不能等同于已读: %#v", entry)
	}
	t.Setenv("APPDATA", root)
	loaded, err := New()
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.state.Notifications) != 0 || loaded.GetState().NotificationHistory.Scopes[0].UnreadCount != 1 {
		t.Fatal("重启丢失历史或补投了旧正文")
	}
}

// 真实配置写入跨越重启后，勿扰未读只生成一个原子摘要，已读项不计入摘要。
func TestNotificationInboxQuietRestart(t *testing.T) {
	s, _, root := inboxFixture(t)
	prefs := s.config.Preferences
	prefs.Notifications.QuietHoursEnabled = true
	if _, err := s.UpdatePreferences(prefs); err != nil {
		t.Fatal(err)
	}
	s.addNoticeLocked("error", "第一条")
	s.addNoticeLocked("connection", "第二条")
	if len(s.state.Notifications) != 0 || s.journal.NotificationScopes[0].QuietPending != 2 {
		t.Fatal("勿扰期间有正文投递或计数缺失")
	}
	id := s.journal.NotificationScopes[0].Entries[0].ID
	if _, err := s.MarkNotificationsRead(inboxReadInput(s, id)); err != nil {
		t.Fatal(err)
	}
	if s.journal.NotificationScopes[0].QuietPending != 1 {
		t.Fatal("已读未减少待摘要数量")
	}
	t.Setenv("APPDATA", root)
	loaded, err := New()
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.state.Notifications) != 0 || loaded.journal.NotificationScopes[0].QuietPending != 1 {
		t.Fatal("重启没有保留待汇总状态")
	}
	loaded.state.Connection, loaded.state.Capabilities = s.state.Connection, s.state.Capabilities
	prefs.Notifications.QuietHoursEnabled = false
	if _, err := loaded.UpdatePreferences(prefs); err != nil {
		t.Fatal(err)
	}
	if len(loaded.state.Notifications) != 1 || !strings.Contains(loaded.state.Notifications[0].Message, "1 条未读") || loaded.journal.NotificationScopes[0].QuietPending != 0 {
		t.Fatal("摘要和清计数未同时提交")
	}
	if err := loaded.reconcileNotificationPolicyLocked(); err != nil || len(loaded.state.Notifications) != 1 {
		t.Fatal("摘要重复投递")
	}
	again, err := New()
	if err != nil || len(again.state.Notifications) != 0 || again.journal.NotificationScopes[0].QuietPending != 0 {
		t.Fatal("摘要重启后重放")
	}
}

// 整批来源和标识验证失败时，任何提醒都不能被部分标成已读。
func TestNotificationInboxReadIsolation(t *testing.T) {
	s, _, _ := inboxFixture(t)
	s.addNoticeLocked("info", "正文")
	id := s.journal.NotificationScopes[0].Entries[0].ID
	valid := inboxReadInput(s, id)
	otherAccount := "伪造账号"
	for _, mutate := range []func(*NotificationReadInput){
		func(v *NotificationReadInput) { v.ProfileID = "其他资料" },
		func(v *NotificationReadInput) { v.BaseURL = "http://127.0.0.1:2" },
		func(v *NotificationReadInput) { v.InstanceID = strings.Repeat("b", 32) },
		func(v *NotificationReadInput) { v.SourceMode = "legacy" },
		func(v *NotificationReadInput) { v.AccountID = &otherAccount },
		func(v *NotificationReadInput) { v.IDs = []string{id, strings.Repeat("f", 32)} },
		func(v *NotificationReadInput) { v.IDs = []string{id, id} },
	} {
		input := valid
		mutate(&input)
		if _, err := s.MarkNotificationsRead(input); err == nil || s.journal.NotificationScopes[0].Entries[0].ReadAt != nil {
			t.Fatal("跨来源或坏批次修改了已读")
		}
	}
	if _, err := s.MarkNotificationsRead(valid); err != nil {
		t.Fatal(err)
	}
	before := *s.journal.NotificationScopes[0].Entries[0].ReadAt
	if _, err := s.MarkNotificationsRead(valid); err != nil || *s.journal.NotificationScopes[0].Entries[0].ReadAt != before {
		t.Fatal("重复已读不是幂等")
	}
}

// 禁播仅清当前范围的摘要资格，其他实例未读仍保留，恢复不补弹当前正文。
func TestNotificationInboxBlockedScope(t *testing.T) {
	s, _, _ := inboxFixture(t)
	s.config.Preferences.Notifications.QuietHoursEnabled = true
	s.addNoticeLocked("error", "实例A")
	other := strings.Repeat("b", 32)
	s.state.Capabilities.InstanceID = &other
	s.addNoticeLocked("error", "实例B")
	s.SetNotificationDeliveryBlocked(true)
	if s.journal.NotificationScopes[0].QuietPending != 1 || s.journal.NotificationScopes[1].QuietPending != 0 {
		t.Fatal("禁播跨范围清空了摘要")
	}
	s.SetNotificationDeliveryBlocked(false)
	s.config.Preferences.Notifications.QuietHoursEnabled = false
	if err := s.reconcileNotificationPolicyLocked(); err != nil || len(s.state.Notifications) != 0 {
		t.Fatal("被禁播的当前范围产生补弹")
	}
	s.state.Capabilities.InstanceID = &s.journal.NotificationScopes[0].InstanceID
	if err := s.reconcileNotificationPolicyLocked(); err != nil || len(s.state.Notifications) != 1 {
		t.Fatal("原实例的独立摘要无法恢复")
	}
}

// 模拟普通文件被目录阻塞；第一次失败后不自动重试、不改已提交事实。
func TestNotificationInboxPersistenceFailure(t *testing.T) {
	s, _, root := inboxFixture(t)
	s.config.Preferences.Notifications.QuietHoursEnabled = true
	s.addNoticeLocked("error", "待汇总")
	path := filepath.Join(root, "AutoBGI-Pet", "command-event-journal.json")
	if err := os.Rename(path, path+".held"); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	before := cloneJournal(s.journal)
	s.config.Preferences.Notifications.QuietHoursEnabled = false
	if err := s.reconcileNotificationPolicyLocked(); err == nil || !s.journalBlocked || !reflect.DeepEqual(before, s.journal) || len(s.state.Notifications) != 0 {
		t.Fatal("保存失败仍清计数或投递")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(path+".held", path); err != nil {
		t.Fatal(err)
	}
	if err := s.reconcileNotificationPolicyLocked(); err == nil {
		t.Fatal("存储恢复后不应越过同会话阻断自动重试")
	}
	s.addNoticeLocked("info", "不应记录")
	if _, err := s.MarkNotificationsRead(inboxReadInput(s, before.NotificationScopes[0].Entries[0].ID)); err == nil || !reflect.DeepEqual(before, s.journal) {
		t.Fatal("失败阻断后仍允许写入")
	}
}

// 命令终态、事件游标、旧尝试标记和通知正文必须通过同一次真实 journal 保存。
func TestNotificationInboxSSETransaction(t *testing.T) {
	s, p, root := inboxFixture(t)
	instance := *s.state.Capabilities.InstanceID
	receipt := autobgi.CommandReceipt{CommandID: "11111111-1111-4111-8111-111111111111", Status: "accepted", ProfileID: p.ID, InstanceID: instance, PresetName: "隔离配置", UpdatedAt: time.Now().Format(time.RFC3339Nano)}
	s.journal.Commands = []storage.CommandEntry{{BaseURL: p.BaseURL, Receipt: receipt}}
	s.journal.Scopes = []storage.EventScope{{ProfileID: p.ID, BaseURL: p.BaseURL, InstanceID: instance, Cursor: "epoch:1", Seen: []string{}, Attempted: []string{}}}
	receipt.Status = "failed"
	body, _ := json.Marshal(receipt)
	event := autobgi.Event{InstanceID: instance, EventID: "epoch:2", Type: "command.updated", OccurredAt: time.Now().Format(time.RFC3339Nano), Data: body}
	if changed, err := s.acceptEventLocked(p, event); err != nil || !changed {
		t.Fatal(err)
	}
	loaded, warning := s.store.LoadJournal()
	if warning != "" || loaded.Scopes[0].Cursor != "epoch:2" || len(loaded.Scopes[0].Attempted) != 1 || !loaded.Commands[0].NotificationHandled || len(loaded.NotificationScopes[0].Entries) != 1 || len(s.state.Notifications) != 1 {
		t.Fatal("事件事务缺失字段")
	}
	if changed, err := s.acceptEventLocked(p, event); err != nil || changed || len(s.state.Notifications) != 1 {
		t.Fatal("重复事件再次提醒")
	}
	// 同一状态的新传输序号也只推进游标，不创建第二个事实。
	event.EventID = "epoch:3"
	if _, err := s.acceptEventLocked(p, event); err != nil || len(s.state.Notifications) != 1 {
		t.Fatal("同一终态重新入箱")
	}
	_ = root
}

// 墓碑随命令事实保留，正文裁剪和旧日志迁移均不能重新弹出已处理终态。
func TestNotificationInboxCommandTombstone(t *testing.T) {
	for _, migrated := range []bool{false, true} {
		t.Run(map[bool]string{false: "正文裁剪", true: "旧Attempted迁移"}[migrated], func(t *testing.T) {
			s, p, root := inboxFixture(t)
			instance := *s.state.Capabilities.InstanceID
			receipt := autobgi.CommandReceipt{CommandID: "11111111-1111-4111-8111-111111111111", Status: "succeeded", ProfileID: p.ID, InstanceID: instance, PresetName: "原配置", UpdatedAt: time.Now().Add(-time.Hour).Format(time.RFC3339Nano)}
			s.journal.Commands = []storage.CommandEntry{{BaseURL: p.BaseURL, Receipt: receipt, NotificationHandled: !migrated}}
			if migrated {
				s.journal.Scopes = []storage.EventScope{{ProfileID: p.ID, BaseURL: p.BaseURL, InstanceID: instance, Cursor: "epoch:1", Seen: []string{}, Attempted: []string{receipt.CommandID + ":succeeded"}}}
				// 写入没有新字段的旧版 JSON，再通过真实解码路径迁移。
				data, _ := json.Marshal(s.journal)
				var legacy map[string]any
				_ = json.Unmarshal(data, &legacy)
				delete(legacy, "notificationScopes")
				delete(legacy["commands"].([]any)[0].(map[string]any), "notificationHandled")
				data, _ = json.Marshal(legacy)
				journalPath := filepath.Join(root, "AutoBGI-Pet", "command-event-journal.json")
				if err := os.WriteFile(journalPath, data, 0600); err != nil {
					t.Fatal(err)
				}
				var warning string
				s.journal, warning = s.store.LoadJournal()
				if warning != "" || s.journal.Commands[0].NotificationHandled {
					t.Fatal("旧日志字段缺失迁移失败")
				}
			} else {
				// 通过真实容量整理淘汰已读正文，命令事实中的墓碑继续保留。
				identity, _ := s.notificationContextLocked()
				at := time.Now().Format(time.RFC3339Nano)
				next := cloneJournal(s.journal)
				_, err := storage.PutNotification(&next, identity, storage.NotificationEntry{ID: randomID(), SourceKey: "command:" + receipt.CommandID + ":succeeded", Kind: "command", Message: "已读正文", CreatedAt: at, ReadAt: &at})
				if err != nil {
					t.Fatal(err)
				}
				for i := 0; i < storage.NotificationHistoryLimit; i++ {
					if _, err := storage.PutNotification(&next, identity, storage.NotificationEntry{ID: randomID(), SourceKey: "local:" + randomID(), Kind: "info", Message: "其他未读", CreatedAt: at}); err != nil {
						t.Fatal(err)
					}
				}
				if len(next.NotificationScopes[0].Entries) != storage.NotificationHistoryLimit {
					t.Fatal("未实际发生容量裁剪")
				}
				for _, entry := range next.NotificationScopes[0].Entries {
					if strings.HasPrefix(entry.SourceKey, "command:") {
						t.Fatal("已读命令正文未被裁剪")
					}
				}
				if err := s.commitJournalLocked(next); err != nil {
					t.Fatal(err)
				}
			}
			next := cloneJournal(s.journal)
			notice, err := s.stageCommandNotificationLocked(&next, p, receipt, time.Now().Format(time.RFC3339Nano), true)
			if err != nil || notice != nil || len(next.NotificationScopes) != map[bool]int{false: 1, true: 0}[migrated] || !next.Commands[0].NotificationHandled {
				t.Fatalf("重新创建了处理过的终态: %#v %v", notice, err)
			}
		})
	}
}

// 已有终态但没有通知证据时只补历史，不以手动查询时间冒充新事件。
func TestNotificationInboxHistoricalCommand(t *testing.T) {
	s, p, _ := inboxFixture(t)
	receipt := autobgi.CommandReceipt{CommandID: "11111111-1111-4111-8111-111111111111", Status: "succeeded", ProfileID: p.ID, InstanceID: *s.state.Capabilities.InstanceID, UpdatedAt: time.Now().Add(-time.Hour).Format(time.RFC3339Nano)}
	s.journal.Commands = []storage.CommandEntry{{BaseURL: p.BaseURL, Receipt: receipt}}
	next := cloneJournal(s.journal)
	notice, err := s.stageCommandNotificationLocked(&next, p, receipt, time.Now().Format(time.RFC3339Nano), true)
	if err != nil || notice != nil || len(next.NotificationScopes[0].Entries) != 1 || next.NotificationScopes[0].Entries[0].AttemptedAt != nil {
		t.Fatal("历史查询错误地标记或投递为新提醒")
	}
}

// 来源缺失不伪造Legacy，真实旧版保持空实例；修改地址和删除资料只清对应范围。
func TestNotificationInboxSourceLifecycle(t *testing.T) {
	s, p, _ := inboxFixture(t)
	s.state.Capabilities = nil
	s.addNoticeLocked("error", "探测前错误")
	if s.journal.NotificationScopes[0].SourceMode != "unverified" || s.journal.NotificationScopes[0].InstanceID != "" {
		t.Fatal("探测前错误伪装为已核验来源")
	}
	s.state.Capabilities = &autobgi.Capabilities{SourceMode: "legacy"}
	s.addNoticeLocked("info", "旧版事实")
	if s.journal.NotificationScopes[1].SourceMode != "legacy" || s.journal.NotificationScopes[1].InstanceID != "" {
		t.Fatal("Legacy伪造了实例")
	}
	s.config.Profiles[0].BaseURL = "http://127.0.0.1:2"
	if len(s.GetState().NotificationHistory.Scopes) != 0 {
		t.Fatal("改地址仍显示旧来源")
	}
	if err := s.pruneNotificationProfilesLocked(); err != nil || len(s.journal.NotificationScopes) != 0 {
		t.Fatal("旧地址范围未清理")
	}
	s.config.Profiles[0].BaseURL = p.BaseURL
	s.addNoticeLocked("info", "删除前事实")
	if _, err := s.DeleteProfile(p.ID); err != nil || len(s.journal.NotificationScopes) != 0 || s.GetState().NotificationHistory != nil {
		t.Fatal("删除资料遗留可见通知")
	}
}

// 真实材料分页的接收水位与提醒正文同次保存，历史补偿不投递到实时队列。
func TestNotificationInboxMaterialTransaction(t *testing.T) {
	for _, historyOnly := range []bool{false, true} {
		t.Run(map[bool]string{false: "实时材料", true: "历史材料"}[historyOnly], func(t *testing.T) {
			s, p, _ := inboxFixture(t)
			event := autobgi.MaterialReadyEvent{Sequence: "1", BusinessEventID: strings.Repeat("b", 64), OperationID: strings.Repeat("c", 32), ExecutionContext: strings.Repeat("d", 64),
				PlanID: strings.Repeat("e", 32), ConfigName: "合成配置", Role: autobgi.CultivationQueueRole{UID: "123456789", Region: "cn_gf01"}, Kind: autobgi.MaterialEntryReady,
				EntryID: strings.Repeat("f", 32), CharacterID: 10000002, CharacterName: "合成角色", PlanRevision: "2", OccurredAt: time.Now().Format(time.RFC3339Nano)}
			page := autobgi.MaterialsReadyPage{SchemaVersion: 1, InstanceID: *s.state.Capabilities.InstanceID, ExecutionContext: event.ExecutionContext, Generation: strings.Repeat("1", 32), Cursor: "1", Events: []autobgi.MaterialReadyEvent{event}}
			if err := s.acceptMaterialsReadyLocked(p, page, historyOnly, time.Now()); err != nil {
				t.Fatal(err)
			}
			loaded, warning := s.store.LoadJournal()
			if warning != "" || loaded.MaterialScopes[0].Cursor != "1" || len(loaded.NotificationScopes[0].Entries) != 1 || len(s.state.Notifications) != map[bool]int{false: 1, true: 0}[historyOnly] {
				t.Fatal("材料事务不完整或历史被重播")
			}
		})
	}
}

// 标记已读、取快照与本地通知并行访问同一服务，所有持久写仍串行且无状态竞态。
func TestNotificationInboxConcurrent(t *testing.T) {
	s, _, _ := inboxFixture(t)
	s.addNoticeLocked("error", "并发已读目标")
	input := inboxReadInput(s, s.journal.NotificationScopes[0].Entries[0].ID)
	var work sync.WaitGroup
	for i := 0; i < 12; i++ {
		work.Add(1)
		go func() {
			defer work.Done()
			for j := 0; j < 5; j++ {
				if _, err := s.MarkNotificationsRead(input); err != nil {
					t.Error(err)
				}
				_ = s.GetState()
				s.mu.Lock()
				s.addNoticeLocked("progress", "并发进度事实")
				s.mu.Unlock()
			}
		}()
	}
	work.Wait()
	if len(s.journal.NotificationScopes[0].Entries) != 61 || s.journal.NotificationScopes[0].Entries[0].ReadAt == nil || len(s.state.Notifications) > 20 {
		t.Fatal("并发回写丢失或队列越界")
	}
}
