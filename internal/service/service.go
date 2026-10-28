package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"sync"
	"time"

	"autobgi-pet/internal/autobgi"
	"autobgi-pet/internal/credential"
	"autobgi-pet/internal/platform"
	"autobgi-pet/internal/profile"
	"autobgi-pet/internal/skin"
	"autobgi-pet/internal/storage"
)

// Service 的唯一采集循环向所有窗口分发状态，窗口关闭不会复制或销毁连接。
type Service struct {
	mu                  sync.Mutex
	config              profile.Config
	store               *storage.Store
	skinStore           *skin.Manager
	credentials         credential.Store
	sessionKeys         map[string]string
	state               AppState
	emit                func(AppState)
	logTriggerEmit      func(LogTrigger)
	logTriggerSequence  uint64
	logPickupEmit       func(LogPickup)
	logPickupSequence   uint64
	cancel              context.CancelFunc
	connectionCtx       context.Context
	requestCancel       context.CancelFunc
	snapshotGate        chan struct{}
	snapshotContext     context.Context
	snapshotCancel      context.CancelFunc
	snapshotEpoch       uint64
	snapshotSequence    uint64
	snapshotObserved    uint64
	client              *autobgi.Client
	adapter             autobgi.AutoBGIAdapter
	refresh             chan struct{}
	panelVisible        bool
	paused              bool
	sleepPaused         bool
	networkOffline      bool
	closed              bool
	wg                  sync.WaitGroup
	cache               storage.SnapshotCache
	cacheDirty          bool
	cacheLastWrite      time.Time
	journal             storage.Journal
	journalBlocked      bool
	preparedGeneration  uint64
	eventStarted        bool
	eventCancel         context.CancelFunc
	eventSerial         uint64
	materialSerial      uint64 // 材料请求独立取消与排序，休眠和重同步都会使旧回包失效。
	materialCancel      context.CancelFunc
	materialCollecting  bool
	materialHistoryOnly bool
	targetSerial        uint64 // 目标历史独立排序，连接切换和恢复不能接纳旧回包。
	targetCancel        context.CancelFunc
	targetCollecting    bool
	targetHistoryOnly   bool
	notificationBlocked bool   // 宿主临时禁播事实不进入前端偏好，也不能由页面伪造来源。
	petDragGeneration   uint64 // 只随已提交的交互策略变化递增，普通快照不能中断手势。
	oneLongStartMu      sync.Mutex
	oneLongStarting     bool       // 原生菜单启动期间拒绝重复点击，旧接口没有幂等保证。
	adapterInitMu       sync.Mutex // 菜单首次读取时串行探测适配器，避免并发创建多个连接适配器。
}

// New 加载非敏感配置，连接仅由宿主在启动就绪后按用户偏好发起。
func New() (*Service, error) {
	store, err := storage.New()
	if err != nil {
		return nil, err
	}
	config, notice := store.Load()
	cache, cacheNotice := store.LoadCache()
	journal, journalNotice := store.LoadJournal()
	s := &Service{store: store, credentials: credential.New(), config: config, sessionKeys: map[string]string{}, cache: cache, journal: journal, journalBlocked: journalNotice != "", materialHistoryOnly: true, targetHistoryOnly: true}
	// 本版动作与台词始终可展示，运行期间不沿用旧版气泡和勿扰偏好。
	s.config.Preferences.Notifications.Bubble = true
	s.config.Preferences.Notifications.QuietHoursEnabled = false
	s.state = AppState{Connection: ConnectionState{Status: "disconnected"}, Notifications: []Notification{}, Commands: []autobgi.CommandReceipt{}, EventStatus: "disconnected", LogStream: LogStreamState{Status: "disconnected", Logs: []LogLine{}}, Notice: strings.Join(nonempty(notice, cacheNotice, journalNotice), "；")}
	// 读取旧配置时保留其连接与窗口偏好，但不接纳损坏的关键词规则。
	rules, rulesErr := validateKeywordRules(config.KeywordRules)
	if rulesErr != nil {
		s.state.Notice = strings.Join(nonempty(s.state.Notice, "关键词规则无效，请在设置中重新保存"), "；")
		rules = []profile.KeywordRule{}
	}
	s.config.KeywordRules = rules
	// 自定义素材损坏只回退内置角色，不能阻止连接管理与恢复入口启动。
	s.skinStore, err = skin.New()
	if err != nil {
		s.state.Notice = strings.Join(nonempty(s.state.Notice, "本地皮肤目录不可用，已使用内置角色"), "；")
	}
	if config.Preferences.Pet.SkinID != "abgi-buddy" && (s.skinStore == nil || s.skinStore.Has(config.Preferences.Pet.SkinID) != nil) {
		s.config.Preferences.Pet.SkinID = "abgi-buddy"
		s.state.Notice = strings.Join(nonempty(s.state.Notice, "所选皮肤缺失或损坏，已回退内置角色"), "；")
	}
	// 忽略损坏或人工注入的凭据引用，避免读取不属于本程序的密钥文件。
	profiles := make([]profile.Profile, 0, len(config.Profiles))
	for _, p := range config.Profiles {
		if !validID(p.ID) || p.CredentialRef != "AutoBGI-Pet/profile/"+p.ID {
			s.state.Notice = "部分连接资料无效，请重新保存"
			continue
		}
		if _, err := autobgi.NormalizeURL(p.BaseURL, p.AllowInsecure); err != nil {
			s.state.Notice = "部分连接地址无效，请检查设置"
			continue
		}
		if p.AdapterMode != "auto" && p.AdapterMode != "legacy" && p.AdapterMode != "pet-v1" {
			p.AdapterMode = "auto"
		}
		profiles = append(profiles, p)
	}
	s.config.Profiles = profiles
	// 已删除资料和旧地址的提醒只能清理，不能重新绑定给同名的新来源。
	if err := s.pruneNotificationProfilesLocked(); err != nil {
		s.state.Notice = strings.Join(nonempty(s.state.Notice, "历史提醒暂无法清理，不匹配来源的记录已隐藏"), "；")
	}
	for _, p := range profiles {
		if p.ID == config.ActiveProfileID {
			s.restoreLegacyLocked(p)
		}
	}
	return s, nil
}

// nonempty 合并多个存储告警时略过空值，避免界面出现多余分隔符。
func nonempty(values ...string) []string {
	result := []string{}
	for _, value := range values {
		if value != "" {
			result = append(result, value)
		}
	}
	return result
}

// validID 将凭据引用限制到本程序随机生成的固定命名空间。
func validID(id string) bool {
	if len(id) != 32 {
		return false
	}
	_, err := hex.DecodeString(id)
	return err == nil
}

// randomID 使用密码学随机数避免 profile 和通知 ID 冲突。
func randomID() string {
	var data [16]byte
	if _, err := rand.Read(data[:]); err != nil {
		return fmt.Sprintf("%032x", time.Now().UnixNano())
	}
	return hex.EncodeToString(data[:])
}

// snapshotLocked 为每份对外快照分配单调序号，异步回包可拒绝同代次的迟到旧状态。
func (s *Service) snapshotLocked() AppState {
	s.state.Revision++
	s.state.Profiles = make([]profile.ProfileView, 0, len(s.config.Profiles))
	for _, p := range s.config.Profiles {
		key := s.sessionKeys[p.ID]
		s.state.Profiles = append(s.state.Profiles, profile.ProfileView{ID: p.ID, Name: p.Name, BaseURL: p.BaseURL, AdapterMode: p.AdapterMode, ReadOnly: p.ReadOnly, AutoConnect: p.AutoConnect, AllowInsecure: p.AllowInsecure, HasCredential: (!p.SessionOnly && p.CredentialRef != "") || key != "", SessionOnly: p.SessionOnly})
	}
	s.state.ActiveProfileID = s.config.ActiveProfileID
	s.state.Preferences = s.config.Preferences
	s.state.KeywordRules = append([]profile.KeywordRule{}, s.config.KeywordRules...)
	s.state.Commands = s.commandsLocked()
	s.state.NotificationHistory = s.notificationHistoryLocked()
	if s.state.PreparedCommand != nil {
		expires, err := time.Parse(time.RFC3339Nano, s.state.PreparedCommand.ExpiresAt)
		if err != nil || !time.Now().Before(expires) {
			s.state.PreparedCommand = nil
		}
	}
	data, _ := json.Marshal(s.state)
	var result AppState
	_ = json.Unmarshal(data, &result)
	return result
}

// publish 在锁外调用宿主事件回调，避免窗口事件回调造成死锁。
func (s *Service) publish() {
	s.mu.Lock()
	value := s.snapshotLocked()
	emit := s.emit
	s.mu.Unlock()
	if emit != nil {
		emit(value)
	}
}

// GetState 仅返回脱敏展示数据，凭据没有任何读取绑定。
func (s *Service) GetState() AppState { s.mu.Lock(); defer s.mu.Unlock(); return s.snapshotLocked() }

// SetEmitter 只供 Go 宿主接线，不允许被 WebView 调用。
//
//wails:ignore
func (s *Service) SetEmitter(emit func(AppState)) { s.mu.Lock(); s.emit = emit; s.mu.Unlock() }

// SetLogTriggerEmitter 逐次向原生宿主投递命中事件，避免状态快照合并丢失动作。
//
//wails:ignore
func (s *Service) SetLogTriggerEmitter(emit func(LogTrigger)) {
	s.mu.Lock()
	s.logTriggerEmit = emit
	s.mu.Unlock()
}

// SetLogPickupEmitter 逐次向桌面宿主投递拾取事件，不依赖可能合并的状态快照。
//
//wails:ignore
func (s *Service) SetLogPickupEmitter(emit func(LogPickup)) {
	s.mu.Lock()
	s.logPickupEmit = emit
	s.mu.Unlock()
}

// SetPanelVisible 只调整现有采集器频率，不创建第二条连接。
//
//wails:ignore
func (s *Service) SetPanelVisible(visible bool) {
	s.mu.Lock()
	s.panelVisible = visible
	s.mu.Unlock()
	s.Refresh()
}

// ActiveURL 只供原生浏览器使用；不附带密钥，也不接受任意外部 URL。
//
//wails:ignore
func (s *Service) ActiveURL() (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, p := range s.config.Profiles {
		if p.ID == s.config.ActiveProfileID {
			return p.BaseURL, nil
		}
	}
	return "", errors.New("请先选择一个连接")
}

// AutoConnect 在桌面和托盘就绪后启动保存的唯一活动连接。
//
//wails:ignore
func (s *Service) AutoConnect() {
	s.mu.Lock()
	id := ""
	for _, p := range s.config.Profiles {
		if p.ID == s.config.ActiveProfileID && p.AutoConnect && !p.SessionOnly {
			id = p.ID
			break
		}
	}
	s.mu.Unlock()
	if id != "" {
		_, _ = s.Connect(id)
	}
}

// SaveProfile 仅一次性接收密钥；明确的会话模式绝不写入明文文件。
func (s *Service) SaveProfile(input profile.ProfileInput) (AppState, error) {
	base, err := autobgi.NormalizeURL(input.BaseURL, input.AllowInsecure)
	if err != nil {
		return s.GetState(), err
	}
	name := strings.TrimSpace(input.Name)
	if name == "" || len([]rune(name)) > 80 {
		return s.GetState(), errors.New("连接名称需要 1 至 80 个字符")
	}
	if input.AdapterMode == "" {
		input.AdapterMode = "auto"
	}
	if input.AdapterMode != "auto" && input.AdapterMode != "legacy" && input.AdapterMode != "pet-v1" {
		return s.GetState(), errors.New("未知适配器模式")
	}
	if strings.ContainsAny(input.APIKey, "\r\n\x00") || len(input.APIKey) > 2560 {
		return s.GetState(), errors.New("API 密钥格式无效或长度超过限制")
	}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return s.GetState(), errors.New("应用正在退出")
	}
	index := -1
	for i, p := range s.config.Profiles {
		if p.ID == input.ID {
			index = i
			break
		}
	}
	if input.ID != "" && index < 0 {
		s.mu.Unlock()
		return s.GetState(), errors.New("连接不存在，请重新选择")
	}
	if index < 0 && len(s.config.Profiles) >= 20 {
		s.mu.Unlock()
		return s.GetState(), errors.New("最多保存 20 个连接")
	}
	if input.ID == "" {
		input.ID = randomID()
	}
	ref := "AutoBGI-Pet/profile/" + input.ID
	key := input.APIKey
	if key == "" {
		if index >= 0 && s.config.Profiles[index].BaseURL != base {
			s.mu.Unlock()
			return s.GetState(), errors.New("修改服务地址后请重新输入 API 密钥，避免把旧实例凭据发送到新地址")
		}
		if existing, ok := s.sessionKeys[input.ID]; ok {
			key = existing
		} else if index >= 0 && s.config.Profiles[index].SessionOnly {
			s.mu.Unlock()
			return s.GetState(), credential.ErrNotFound
		} else {
			key, err = s.credentials.Read(ref)
		}
	}
	if key == "" || err != nil {
		s.mu.Unlock()
		return s.GetState(), credential.ErrNotFound
	}
	previousKey, previousExists := "", false
	if !input.SessionOnly {
		// 先在内存保存旧密钥，JSON 提交失败时补偿恢复；磁盘只保存密文。
		previousKey, previousExists, err = s.previousCredentialLocked(ref)
		if err != nil {
			s.mu.Unlock()
			return s.GetState(), err
		}
		if err = s.credentials.Write(ref, key); err != nil {
			s.mu.Unlock()
			return s.GetState(), err
		}
	}
	p := profile.Profile{ID: input.ID, Name: name, BaseURL: base, CredentialRef: ref, AdapterMode: input.AdapterMode, ReadOnly: input.ReadOnly, AutoConnect: input.AutoConnect && !input.SessionOnly, AllowInsecure: input.AllowInsecure, SessionOnly: input.SessionOnly}
	old := s.config
	s.config.Profiles = append([]profile.Profile(nil), s.config.Profiles...)
	if index < 0 {
		s.config.Profiles = append(s.config.Profiles, p)
	} else {
		s.config.Profiles[index] = p
	}
	// 保存未激活的资料不切换现有连接；首次保存才选作默认连接。
	if s.config.ActiveProfileID == "" {
		s.config.ActiveProfileID = input.ID
	}
	if err = s.store.Save(s.config); err != nil {
		s.config = old
		if !input.SessionOnly {
			s.restoreCredentialLocked(input.ID, ref, previousKey, previousExists)
		}
		s.mu.Unlock()
		s.publish()
		return s.GetState(), err
	}
	if s.state.Connection.ProfileID == input.ID {
		s.stopLocked()
		s.state.Connection.Status = "disconnected"
	}
	// 地址、凭据或模式变更后不能继续展示旧来源缓存或保留旧确认令牌。
	s.invalidateCacheLocked(input.ID)
	// 配置和提醒是两个原子文件；资料提交后清理失败保留告警，不谎称已经回滚资料。
	notificationCleanupErr := s.pruneNotificationProfilesLocked()
	if s.state.Snapshot != nil && s.state.Snapshot.ProfileID == input.ID {
		s.state.Snapshot = nil
	}
	if s.state.PreparedCommand != nil && s.state.PreparedCommand.ProfileID == input.ID {
		s.state.PreparedCommand = nil
	}
	// 会话标记先提交成功，再删除旧密文；JSON 失败时旧凭据完全不变。
	credentialNotice := ""
	if input.SessionOnly {
		if err = s.credentials.Delete(ref); err != nil {
			credentialNotice = "仅本次会话使用密钥；程序目录中的旧密钥文件暂无法清理，但不会自动读取，请检查目录权限"
		}
		s.sessionKeys[input.ID] = key
	} else {
		delete(s.sessionKeys, input.ID)
	}
	s.state.Notice = credentialNotice
	if notificationCleanupErr != nil {
		s.state.Notice = strings.Join(nonempty(s.state.Notice, "连接已保存，旧地址提醒暂无法清理；不匹配的历史已隐藏"), "；")
	}
	s.mu.Unlock()
	s.publish()
	return s.GetState(), nil
}

// DeleteProfile 在内存备份旧凭据，持久化移除失败时补偿恢复，避免遗失原密钥。
func (s *Service) DeleteProfile(id string) (AppState, error) {
	s.mu.Lock()
	index := -1
	for i, p := range s.config.Profiles {
		if p.ID == id {
			index = i
			break
		}
	}
	if index < 0 {
		s.mu.Unlock()
		return s.GetState(), errors.New("连接不存在")
	}
	p := s.config.Profiles[index]
	ref := p.CredentialRef
	previousKey, previousExists := "", false
	if !p.SessionOnly {
		var err error
		previousKey, previousExists, err = s.previousCredentialLocked(ref)
		if err != nil {
			s.mu.Unlock()
			return s.GetState(), err
		}
		if err := s.credentials.Delete(ref); err != nil {
			s.mu.Unlock()
			return s.GetState(), err
		}
	}
	old := s.config
	s.config.Profiles = append([]profile.Profile(nil), s.config.Profiles...)
	s.config.Profiles = append(s.config.Profiles[:index], s.config.Profiles[index+1:]...)
	if s.config.ActiveProfileID == id {
		s.config.ActiveProfileID = ""
	}
	if err := s.store.Save(s.config); err != nil {
		s.config = old
		if !p.SessionOnly {
			s.restoreCredentialLocked(id, ref, previousKey, previousExists)
		}
		s.mu.Unlock()
		s.publish()
		return s.GetState(), err
	}
	if s.state.Connection.ProfileID == id {
		s.stopLocked()
		s.state.Connection = ConnectionState{Status: "disconnected", Generation: s.state.Connection.Generation}
		s.state.Snapshot = nil
		s.state.Capabilities = nil
	}
	delete(s.sessionKeys, id)
	s.invalidateCacheLocked(id)
	s.removeJournalProfileLocked(id)
	if p.SessionOnly {
		if err := s.credentials.Delete(ref); err != nil {
			s.state.Notice = "连接已删除；程序目录中的旧密钥文件暂无法清理，请检查目录权限"
		}
	}
	s.mu.Unlock()
	s.publish()
	return s.GetState(), nil
}

// previousCredentialLocked 仅快照本程序引用，无法确认旧凭据时不冒险覆盖或删除。
func (s *Service) previousCredentialLocked(ref string) (string, bool, error) {
	key, err := s.credentials.Read(ref)
	if errors.Is(err, credential.ErrNotFound) {
		return "", false, nil
	}
	if err != nil {
		return "", false, credential.ErrUnavailable
	}
	return key, true, nil
}

// restoreCredentialLocked 在两个存储无法原子提交时补偿；系统恢复失败仍把原钥保留在会话内存。
func (s *Service) restoreCredentialLocked(id, ref, key string, exists bool) {
	var err error
	if exists {
		err = s.credentials.Write(ref, key)
	} else {
		err = s.credentials.Delete(ref)
	}
	if err != nil {
		if exists {
			if _, sessionExists := s.sessionKeys[id]; !sessionExists {
				s.sessionKeys[id] = key
			}
		}
		s.state.Notice = "配置保存失败，旧密钥文件也无法恢复；原密钥仅保留在当前会话，请检查程序目录权限并重新保存连接"
	}
}

// stopLocked 取消旧代请求并递增 generation；延迟返回的旧数据无法写入新连接。
func (s *Service) stopLocked() {
	s.invalidateMaterialsLocked(true)
	s.invalidateTargetsLocked(true)
	s.flushCacheLocked(true)
	s.invalidateSnapshotsLocked()
	s.state.PreparedCommand = nil
	s.state.Notifications = []Notification{}
	s.state.EventStatus = "disconnected"
	s.state.LogStream.Status = "disconnected"
	s.state.LogStream.LastActivityAt = ""
	s.state.LogStream.Error = ""
	s.state.LogStream.LastTrigger = nil
	s.eventStarted = false
	s.eventSerial++
	if s.eventCancel != nil {
		s.eventCancel()
		s.eventCancel = nil
	}
	if s.cancel != nil {
		s.cancel()
		s.cancel = nil
	}
	s.connectionCtx = nil
	if s.requestCancel != nil {
		s.requestCancel()
		s.requestCancel = nil
	}
	if s.client != nil {
		s.client.Close()
		s.client = nil
	}
	s.adapter = nil
	s.refresh = nil
	s.state.Connection.Generation++
	if s.state.Snapshot != nil {
		s.state.Snapshot.Stale = true
	}
}

// Connect 只激活显式选择的 profile，不扫描地址或尝试默认密钥。
func (s *Service) Connect(id string) (AppState, error) {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return s.GetState(), errors.New("应用正在退出")
	}
	var selected *profile.Profile
	for _, p := range s.config.Profiles {
		if p.ID == id {
			copy := p
			selected = &copy
			break
		}
	}
	if selected == nil {
		s.mu.Unlock()
		return s.GetState(), errors.New("请先保存并选择连接")
	}
	key, session := s.sessionKeys[id]
	var err error
	if !session && !selected.SessionOnly {
		key, err = s.credentials.Read(selected.CredentialRef)
	}
	if err != nil || key == "" {
		s.mu.Unlock()
		return s.GetState(), credential.ErrNotFound
	}
	client, err := autobgi.NewClient(selected.BaseURL, key, selected.AllowInsecure)
	if err != nil {
		s.mu.Unlock()
		return s.GetState(), err
	}
	s.stopLocked()
	gen := s.state.Connection.Generation
	if s.state.Snapshot != nil && s.state.Snapshot.ProfileID != id {
		s.state.Snapshot = nil
	}
	s.state.Capabilities = nil
	s.state.Notifications = []Notification{}
	s.config.ActiveProfileID = id
	if err = s.store.Save(s.config); err != nil {
		s.state.Notice = "连接已激活，但无法保存默认连接；请检查配置目录权限"
	}
	s.state.Connection = ConnectionState{Status: "connecting", ProfileID: id, Generation: gen}
	// 新版只监听日志，不启动旧的业务状态采集或事件订阅。
	s.state.Snapshot = nil
	s.state.LogStream = LogStreamState{Status: "connecting", Logs: []LogLine{}}
	ctx, cancel := context.WithCancel(context.Background())
	trigger := make(chan struct{}, 1)
	s.cancel = cancel
	s.connectionCtx = ctx
	s.client = client
	s.refresh = trigger
	// 新连接按自身地址重新计算网络暂停，本机回环地址不依赖外部网卡。
	s.reconcilePauseLocked()
	s.wg.Add(1)
	s.mu.Unlock()
	s.publish()
	go s.logLoop(ctx, *selected, client, trigger, gen)
	return s.GetState(), nil
}

// Disconnect 仅停止网络，保留带过期标识的快照和已保存凭据。
func (s *Service) Disconnect() AppState {
	s.mu.Lock()
	s.stopLocked()
	s.state.Connection.Status = "disconnected"
	s.state.Connection.ErrorCode = ""
	s.state.Connection.ErrorMessage = ""
	s.state.Connection.RetryAt = nil
	s.mu.Unlock()
	s.publish()
	return s.GetState()
}

// Refresh 合并手动刷新，采集循环自行限流，鉴权失败不会被自动唤醒。
func (s *Service) Refresh() AppState {
	s.mu.Lock()
	trigger := s.refresh
	status := s.state.Connection.Status
	s.mu.Unlock()
	if trigger != nil && status != "auth_failed" && status != "incompatible" {
		select {
		case trigger <- struct{}{}:
		default:
		}
	}
	return s.GetState()
}

// UpdatePreferences 校验所有原生选项；未实现的开机启动明确拒绝而非假保存。
func (s *Service) UpdatePreferences(prefs profile.Preferences) (AppState, error) {
	// 恢复快捷键仅允许三个固定组合，防止前端传入任意系统按键串。
	if prefs.Pet.RecoveryShortcut != "Ctrl+Shift+P" && prefs.Pet.RecoveryShortcut != "Ctrl+Alt+P" && prefs.Pet.RecoveryShortcut != "Ctrl+Shift+Y" {
		return s.GetState(), errors.New("请选择支持的恢复快捷键：Ctrl+Shift+P、Ctrl+Alt+P 或 Ctrl+Shift+Y")
	}
	if math.IsNaN(prefs.Pet.Scale) || math.IsInf(prefs.Pet.Scale, 0) || prefs.Pet.Scale < 0.6 || prefs.Pet.Scale > 2 {
		return s.GetState(), errors.New("桌宠缩放范围为 0.6 至 2")
	}
	if prefs.Pet.SkinID != "abgi-buddy" && (s.skinStore == nil || s.skinStore.Has(prefs.Pet.SkinID) != nil) {
		return s.GetState(), errors.New("所选皮肤未安装或已损坏，请重新导入或选择内置角色")
	}
	s.mu.Lock()
	old := s.config.Preferences
	if old.Startup.LaunchOnLogin != prefs.Startup.LaunchOnLogin {
		if err := platform.SetLaunchOnLogin(prefs.Startup.LaunchOnLogin); err != nil {
			s.mu.Unlock()
			return s.GetState(), err
		}
	}
	s.config.Preferences = prefs
	if err := s.store.Save(s.config); err != nil {
		s.config.Preferences = old
		_ = platform.SetLaunchOnLogin(old.Startup.LaunchOnLogin)
		s.mu.Unlock()
		return s.GetState(), err
	}
	// 在解除配置锁前记录每次已保存的变化，异步广播跳过中间快照也不会漏掉先禁用再恢复。
	if petDragPolicyChanged(old, prefs) {
		s.petDragGeneration++
	}
	// 勿扰摘要与计数在 journal 内一次提交；配置已提交后失败保留待汇总状态供当前范围恢复。
	notificationErr := s.reconcileNotificationPolicyLocked()
	if notificationErr != nil {
		s.state.Notice = "偏好已保存，但提醒状态暂无法保存；旧提醒和待汇总计数保持不变"
	}
	s.mu.Unlock()
	s.publish()
	return s.GetState(), notificationErr
}

// Pause 保持原生电源调用兼容，只更新休眠原因，不解除仍存在的断网暂停。
//
//wails:ignore
func (s *Service) Pause(paused bool) {
	s.mu.Lock()
	if s.closed || s.sleepPaused == paused {
		s.mu.Unlock()
		return
	}
	s.sleepPaused = paused
	s.reconcilePauseLocked()
	if err := s.reconcileNotificationPolicyLocked(); err != nil {
		s.state.Notice = "提醒状态无法保存，已停止新提醒投递，请检查本地目录权限"
	}
	s.mu.Unlock()
	s.Refresh()
	s.publish()
}

// Shutdown 等待唯一采集器和在途列表请求结束，再移除内存凭据引用。
//
//wails:ignore
func (s *Service) Shutdown() {
	s.mu.Lock()
	s.closed = true
	s.stopLocked()
	s.emit = nil
	s.logTriggerEmit = nil
	s.logPickupEmit = nil
	s.mu.Unlock()
	s.wg.Wait()
	s.mu.Lock()
	clear(s.sessionKeys)
	s.flushCacheLocked(true)
	s.mu.Unlock()
}
