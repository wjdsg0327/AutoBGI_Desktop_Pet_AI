package service

import (
	"errors"
	"strings"
	"time"

	"autobgi-pet/internal/profile"
	"autobgi-pet/internal/storage"
)

// todoSource 将每个连接的请求状态分开保存，避免切换连接后覆盖其他来源的结果。
type todoSource struct {
	profileID string
	baseURL   string
}

// todoAutoEntry 冻结命中时的连接代次，重连后的旧排队项不能自动发送。
type todoAutoEntry struct {
	id         string
	generation uint64
}

// todoFlight 记录当前动作所属的规则，避免同一规则在请求中重复派发。
type todoFlight struct {
	id     string
	ruleID string
}

// todoProfileLocked 只选择当前资料，离线时仍可查看该资料当天的代办。
func (s *Service) todoProfileLocked() (profile.Profile, bool) {
	for _, p := range s.config.Profiles {
		if p.ID == s.config.ActiveProfileID {
			return p, true
		}
	}
	return profile.Profile{}, false
}

// todoExpiresAt 按代办创建时的本机日期计算下一个午夜。
func todoExpiresAt(now time.Time) time.Time {
	local := now.In(time.Local)
	return time.Date(local.Year(), local.Month(), local.Day()+1, 0, 0, 0, 0, local.Location())
}

// pruneTodosLocked 只在落盘成功后移除过期记录，失败时保留原文件。
func (s *Service) pruneTodosLocked(now time.Time) error {
	if s.todosBlocked {
		return errors.New("代办记录不可用，请检查本地数据目录")
	}
	next := make([]storage.TodoRecord, 0, len(s.todos))
	for _, item := range s.todos {
		expires, err := time.Parse(time.RFC3339Nano, item.ExpiresAt)
		if err == nil && now.Before(expires) {
			next = append(next, item)
		}
	}
	if len(next) == len(s.todos) {
		return nil
	}
	if err := s.store.SaveTodos(next); err != nil {
		return err
	}
	s.todos = next
	return nil
}

// scheduleTodoTimerLocked 最多每分钟核对一次时钟，同时在最近的到期时刻及时清理。
func (s *Service) scheduleTodoTimerLocked() {
	if s.todoTimer != nil {
		s.todoTimer.Stop()
		s.todoTimer = nil
	}
	if s.closed || s.todosBlocked || len(s.todos) == 0 {
		return
	}
	delay := time.Minute
	now := time.Now()
	for _, item := range s.todos {
		expires, err := time.Parse(time.RFC3339Nano, item.ExpiresAt)
		if err == nil && expires.After(now) && expires.Sub(now) < delay {
			delay = expires.Sub(now)
		}
	}
	s.todoTimer = time.AfterFunc(delay, s.expireTodos)
}

// expireTodos 在定时器中完成跨天清理，写入失败时提醒用户并稍后再检查。
func (s *Service) expireTodos() {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	before := len(s.todos)
	err := s.pruneTodosLocked(time.Now())
	if err != nil {
		s.state.Notice = err.Error()
		s.todoNotice = err.Error()
	} else if len(s.todos) != before {
		s.todoNotice = ""
	}
	changed := len(s.todos) != before || err != nil
	s.scheduleTodoTimerLocked()
	s.mu.Unlock()
	if changed {
		s.publish()
	}
}

// createTodoLocked 将首次命中的动作与来源一起提交；相同规则和来源只保留一条。
func (s *Service) createTodoLocked(rule profile.KeywordRule, profileID, text string, now time.Time) (storage.TodoRecord, bool) {
	if rule.Todo == nil || !rule.Todo.Enabled || s.closed {
		return storage.TodoRecord{}, false
	}
	if err := s.pruneTodosLocked(now); err != nil {
		s.state.Notice = err.Error()
		s.todoNotice = err.Error()
		return storage.TodoRecord{}, false
	}
	p, ok := s.todoProfileLocked()
	if !ok || p.ID != profileID {
		return storage.TodoRecord{}, false
	}
	source := todoSource{p.ID, p.BaseURL}
	// 自动动作尚未返回时，同规则的连续日志视为同一次待办。
	if rule.Todo.AutoApprove && s.todoInFlight[source].ruleID == rule.ID {
		return storage.TodoRecord{}, false
	}
	for _, item := range s.todos {
		if item.RuleID == rule.ID && item.ProfileID == p.ID && item.BaseURL == p.BaseURL {
			// 已有手动待办保持首次快照，启用自动审批不会静默执行旧内容。
			if rule.Todo.AutoApprove && !s.autoQueuedLocked(source, item.ID) && s.todoFeedback[source].Status != "sending" {
				s.setAutoTodoFailureLocked(storage.TodoRecord{ID: item.ID, ProfileID: p.ID, BaseURL: p.BaseURL, Action: rule.Todo.Action, Target: rule.Todo.Target}, errors.New("此规则已有待办，请先手动处理或不管"))
			}
			return storage.TodoRecord{}, false
		}
	}
	if len(s.todos) >= storage.TodoLimit {
		s.todoNotice = "代办数量已达上限，请先处理或不管已有代办"
		s.state.Notice = s.todoNotice
		return storage.TodoRecord{}, false
	}
	item := storage.TodoRecord{
		ID: randomID(), RuleID: rule.ID, ProfileID: p.ID, ProfileName: p.Name, BaseURL: p.BaseURL,
		Text: text, Action: rule.Todo.Action, Target: rule.Todo.Target,
		CreatedAt: now.Format(time.RFC3339Nano), ExpiresAt: todoExpiresAt(now).Format(time.RFC3339Nano),
	}
	next := append(append([]storage.TodoRecord{}, s.todos...), item)
	if err := s.store.SaveTodos(next); err != nil {
		s.state.Notice = err.Error()
		s.todoNotice = err.Error()
		return storage.TodoRecord{}, false
	}
	s.todos = next
	s.todoNotice = ""
	s.scheduleTodoTimerLocked()
	return item, true
}

// autoQueuedLocked 只检查当前进程中的队列，持久化记录本身不会在重启后自动执行。
func (s *Service) autoQueuedLocked(source todoSource, id string) bool {
	for _, entry := range s.todoAutoQueue[source] {
		if entry.id == id {
			return true
		}
	}
	return false
}

// removeAutoQueuedLocked 在用户丢弃或消费代办时移除其待派发标记。
func (s *Service) removeAutoQueuedLocked(source todoSource, id string) {
	queue := s.todoAutoQueue[source]
	for index, entry := range queue {
		if entry.id == id {
			queue = append(queue[:index], queue[index+1:]...)
			if len(queue) == 0 {
				delete(s.todoAutoQueue, source)
			} else {
				s.todoAutoQueue[source] = queue
			}
			return
		}
	}
}

// visibleTodosLocked 展示当前资料的记录；地址变化后的旧代办保留给用户放弃。
func (s *Service) visibleTodosLocked(now time.Time) ([]TodoItem, *TodoFeedback) {
	result := []TodoItem{}
	p, ok := s.todoProfileLocked()
	if !ok {
		return result, nil
	}
	for _, item := range s.todos {
		expires, err := time.Parse(time.RFC3339Nano, item.ExpiresAt)
		if err != nil || !now.Before(expires) || item.ProfileID != p.ID {
			continue
		}
		name := item.ProfileName
		if name == "" {
			// 兼容尚未保存来源名称的早期代办文件。
			name = p.Name
		}
		result = append(result, TodoItem{
			ID: item.ID, Text: item.Text, Action: item.Action, Target: item.Target,
			ProfileID: p.ID, ProfileName: name, BaseURL: item.BaseURL,
			CreatedAt: item.CreatedAt, ExpiresAt: item.ExpiresAt, AutoQueued: s.autoQueuedLocked(todoSource{item.ProfileID, item.BaseURL}, item.ID),
		})
	}
	if feedback, ok := s.todoFeedback[todoSource{p.ID, p.BaseURL}]; ok {
		copy := feedback
		return result, &copy
	}
	return result, nil
}

// takeTodoLocked 在同一临界区核对身份并先持久删除，重复点击无法消费同一条记录。
func (s *Service) takeTodoLocked(id string, requireOnline bool) (storage.TodoRecord, uint64, error) {
	if s.closed {
		return storage.TodoRecord{}, 0, errors.New("桌宠正在退出")
	}
	if err := s.pruneTodosLocked(time.Now()); err != nil {
		return storage.TodoRecord{}, 0, err
	}
	p, ok := s.todoProfileLocked()
	if !ok {
		return storage.TodoRecord{}, 0, errors.New("代办来源连接已不存在")
	}
	for index, item := range s.todos {
		if item.ID != id || item.ProfileID != p.ID {
			continue
		}
		generation := s.state.Connection.Generation
		// 来源地址变更后只能删除旧代办，不能把冻结动作发送到新实例。
		if requireOnline && (item.BaseURL != p.BaseURL || !s.menuConnectionMatchesLocked(p.ID, p.ID, generation) || s.client == nil) {
			return storage.TodoRecord{}, 0, errors.New("请先连接创建这条代办的 AutoBGI")
		}
		next := make([]storage.TodoRecord, 0, len(s.todos)-1)
		next = append(next, s.todos[:index]...)
		next = append(next, s.todos[index+1:]...)
		if err := s.store.SaveTodos(next); err != nil {
			return storage.TodoRecord{}, 0, err
		}
		s.todos = next
		s.removeAutoQueuedLocked(todoSource{item.ProfileID, item.BaseURL}, item.ID)
		s.todoNotice = ""
		s.scheduleTodoTimerLocked()
		return item, generation, nil
	}
	return storage.TodoRecord{}, 0, errors.New("代办已处理或已过期")
}

// DismissTodo 只删除当前连接中的指定代办，不发送任何控制请求。
func (s *Service) DismissTodo(id string) (AppState, error) {
	s.mu.Lock()
	_, _, err := s.takeTodoLocked(id, false)
	s.mu.Unlock()
	if err != nil {
		return s.GetState(), err
	}
	s.publish()
	return s.GetState(), nil
}

// startTodoLocked 在同一临界区核对来源并持久删除，再异步发送一次固定动作。
func (s *Service) startTodoLocked(id string, automatic bool, expectedGeneration uint64) error {
	p, ok := s.todoProfileLocked()
	if !ok {
		return errors.New("代办来源连接已不存在")
	}
	source := todoSource{p.ID, p.BaseURL}
	// 同一连接一次只处理一条，自动审批也复用相同门禁。
	if s.todoFeedback[source].Status == "sending" {
		return errors.New("上一条代办的请求仍在处理，请先查看结果")
	}
	if automatic && !s.menuConnectionMatchesLocked(p.ID, p.ID, expectedGeneration) {
		return errors.New("连接已变化，未发送自动审批请求")
	}
	item, generation, err := s.takeTodoLocked(id, true)
	if err != nil {
		return err
	}
	if s.todoFeedback == nil {
		s.todoFeedback = make(map[todoSource]TodoFeedback)
	}
	if s.todoInFlight == nil {
		s.todoInFlight = make(map[todoSource]todoFlight)
	}
	s.todoFeedback[source] = TodoFeedback{ID: item.ID, ProfileID: item.ProfileID, BaseURL: item.BaseURL, Action: item.Action, Target: item.Target, Status: "sending", Message: "好，就由我来安排！正在提交请求……", AutoApproved: automatic}
	s.todoInFlight[source] = todoFlight{id: item.ID, ruleID: item.RuleID}
	s.wg.Add(1)
	go s.performTodo(item, generation, automatic)
	return nil
}

// setAutoTodoFailureLocked 保留未开始的代办，并说明自动审批没有发送请求。
func (s *Service) setAutoTodoFailureLocked(item storage.TodoRecord, err error) {
	if s.todoFeedback == nil {
		s.todoFeedback = make(map[todoSource]TodoFeedback)
	}
	s.todoFeedback[todoSource{item.ProfileID, item.BaseURL}] = TodoFeedback{
		ID: item.ID, ProfileID: item.ProfileID, BaseURL: item.BaseURL, Action: item.Action, Target: item.Target,
		Status: "error", Message: "自动审批未开始，代办仍可手动处理或不管：" + err.Error(), AutoApproved: true,
	}
}

// enqueueAutoTodoLocked 空闲时立即派发，忙碌时保留快照并按来源顺序排队。
func (s *Service) enqueueAutoTodoLocked(item storage.TodoRecord, generation uint64) {
	source := todoSource{item.ProfileID, item.BaseURL}
	if s.todoFeedback[source].Status == "sending" {
		if s.todoAutoQueue == nil {
			s.todoAutoQueue = make(map[todoSource][]todoAutoEntry)
		}
		s.todoAutoQueue[source] = append(s.todoAutoQueue[source], todoAutoEntry{id: item.ID, generation: generation})
		return
	}
	if err := s.startTodoLocked(item.ID, true, generation); err != nil {
		s.setAutoTodoFailureLocked(item, err)
	}
}

// releaseAutoQueueLocked 停止自动派发，未消费的记录继续留在代办中供用户决定。
func (s *Service) releaseAutoQueueLocked(source todoSource) int {
	queue := s.todoAutoQueue[source]
	delete(s.todoAutoQueue, source)
	count := 0
	for _, entry := range queue {
		for _, item := range s.todos {
			if item.ID == entry.id {
				count++
				break
			}
		}
	}
	return count
}

// continueAutoQueueLocked 上一条受理后才处理下一条；失败或换连接时不继续自动派发。
func (s *Service) continueAutoQueueLocked(source todoSource, failed bool) {
	if len(s.todoAutoQueue[source]) == 0 {
		return
	}
	p, ok := s.todoProfileLocked()
	if failed || !ok || p.ID != source.profileID || p.BaseURL != source.baseURL ||
		!s.menuConnectionMatchesLocked(source.profileID, source.profileID, s.todoAutoQueue[source][0].generation) {
		if count := s.releaseAutoQueueLocked(source); count > 0 {
			feedback := s.todoFeedback[source]
			feedback.Message += " 后续自动代办已保留，请手动处理或不管。"
			s.todoFeedback[source] = feedback
		}
		return
	}
	// 排队可能跨过本机午夜，先清理过期记录，再跳过已失效的队列项。
	if err := s.pruneTodosLocked(time.Now()); err != nil {
		for _, item := range s.todos {
			if item.ID == s.todoAutoQueue[source][0].id {
				s.releaseAutoQueueLocked(source)
				s.setAutoTodoFailureLocked(item, err)
				return
			}
		}
		s.releaseAutoQueueLocked(source)
		s.todoNotice = err.Error()
		return
	}
	s.scheduleTodoTimerLocked()
	for len(s.todoAutoQueue[source]) > 0 {
		entry := s.todoAutoQueue[source][0]
		// 已被用户丢弃的排队项跳过，不能重新创建或发送。
		var item storage.TodoRecord
		for _, candidate := range s.todos {
			if candidate.ID == entry.id {
				item = candidate
				break
			}
		}
		if item.ID == "" {
			s.removeAutoQueuedLocked(source, entry.id)
			continue
		}
		if err := s.startTodoLocked(entry.id, true, entry.generation); err != nil {
			s.releaseAutoQueueLocked(source)
			s.setAutoTodoFailureLocked(item, err)
		}
		return
	}
}

// ProcessTodo 按用户明确点击消费指定代办，不接收动作或目标参数。
func (s *Service) ProcessTodo(id string) (AppState, error) {
	s.mu.Lock()
	err := s.startTodoLocked(id, false, 0)
	s.mu.Unlock()
	if err != nil {
		return s.GetState(), err
	}
	s.publish()
	return s.GetState(), nil
}

// performTodo 不接收前端传入的动作参数，网络失败或结果未知均不自动重发。
func (s *Service) performTodo(item storage.TodoRecord, generation uint64, automatic bool) {
	defer s.wg.Done()
	var err error
	switch item.Action {
	case "one_long":
		err = s.startOneLongForConnection(item.Target, item.ProfileID, item.ProfileID, generation, true)
	case "script_group":
		err = s.StartScriptGroupFromMenuForConnection(item.Target, item.ProfileID, item.ProfileID, generation)
	case "close_bgi_game":
		err = s.CloseFromMenuForConnection("bgi_and_game", item.ProfileID, item.ProfileID, generation)
	case "shutdown":
		err = s.shutdownFromTodoForConnection(item.ProfileID, generation)
	default:
		err = errors.New("代办动作无效")
	}
	status, message := "sent", "请求已受理，实际运行情况请在 AutoBGI 核对。"
	if item.Action == "shutdown" {
		message = "关机请求已提交；该任务会尝试关闭相关软件并安排 60 秒倒计时，请查看 AutoBGI 日志确认。"
	}
	if err != nil {
		status, message = "error", err.Error()
		var known *OneLongStartError
		if errors.As(err, &known) {
			message = known.Message
		}
	}
	s.mu.Lock()
	if !s.closed {
		source := todoSource{item.ProfileID, item.BaseURL}
		delete(s.todoInFlight, source)
		s.todoFeedback[source] = TodoFeedback{
			ID: item.ID, ProfileID: item.ProfileID, BaseURL: item.BaseURL,
			Action: item.Action, Target: item.Target, Status: status, Message: message, AutoApproved: automatic,
		}
		s.continueAutoQueueLocked(source, err != nil)
	}
	closed := s.closed
	s.mu.Unlock()
	if !closed {
		s.publish()
	}
}

// ListTodoTargets 只读取当前连接的真实名称，并在响应后重新核对来源。
func (s *Service) ListTodoTargets(action string) ([]string, error) {
	s.mu.Lock()
	profileID, activeID, generation := s.state.Connection.ProfileID, s.state.ActiveProfileID, s.state.Connection.Generation
	current := s.menuConnectionMatchesLocked(profileID, activeID, generation)
	s.mu.Unlock()
	if !current {
		return nil, errors.New("请先连接 AutoBGI 后读取配置名称")
	}
	var result []string
	switch action {
	case "one_long":
		presets, err := s.ListPresets()
		if err != nil {
			return nil, err
		}
		for _, preset := range presets {
			if strings.TrimSpace(preset.Name) != "" {
				result = append(result, preset.Name)
			}
		}
	case "script_group":
		names, err := s.ReadScriptGroupsForConnection(profileID, activeID, generation)
		if err != nil {
			return nil, err
		}
		result = append(result, names...)
	default:
		return nil, errors.New("此代办动作不需要配置名称")
	}
	s.mu.Lock()
	current = s.menuConnectionMatchesLocked(profileID, activeID, generation)
	s.mu.Unlock()
	if !current {
		return nil, errors.New("读取期间连接已变化，请重新读取")
	}
	if result == nil {
		result = []string{}
	}
	return result, nil
}
