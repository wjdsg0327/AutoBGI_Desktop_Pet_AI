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
func (s *Service) createTodoLocked(rule profile.KeywordRule, profileID, text string, now time.Time) {
	if rule.Todo == nil || !rule.Todo.Enabled || s.closed {
		return
	}
	if err := s.pruneTodosLocked(now); err != nil {
		s.state.Notice = err.Error()
		s.todoNotice = err.Error()
		return
	}
	p, ok := s.todoProfileLocked()
	if !ok || p.ID != profileID {
		return
	}
	for _, item := range s.todos {
		if item.RuleID == rule.ID && item.ProfileID == p.ID && item.BaseURL == p.BaseURL {
			return
		}
	}
	if len(s.todos) >= storage.TodoLimit {
		s.todoNotice = "代办数量已达上限，请先处理或不管已有代办"
		s.state.Notice = s.todoNotice
		return
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
		return
	}
	s.todos = next
	s.todoNotice = ""
	s.scheduleTodoTimerLocked()
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
			CreatedAt: item.CreatedAt, ExpiresAt: item.ExpiresAt,
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

// ProcessTodo 先删除代办并保留内存快照，再异步发送一次固定动作。
func (s *Service) ProcessTodo(id string) (AppState, error) {
	s.mu.Lock()
	// 同一连接一次只处理一条，确保每次请求的最终结果都能留在对话中。
	if p, ok := s.todoProfileLocked(); ok && s.todoFeedback[todoSource{p.ID, p.BaseURL}].Status == "sending" {
		s.mu.Unlock()
		return s.GetState(), errors.New("上一条代办的请求仍在处理，请先查看结果")
	}
	item, generation, err := s.takeTodoLocked(id, true)
	if err != nil {
		s.mu.Unlock()
		return s.GetState(), err
	}
	if s.todoFeedback == nil {
		s.todoFeedback = make(map[todoSource]TodoFeedback)
	}
	source := todoSource{item.ProfileID, item.BaseURL}
	s.todoFeedback[source] = TodoFeedback{ID: item.ID, ProfileID: item.ProfileID, BaseURL: item.BaseURL, Action: item.Action, Target: item.Target, Status: "sending", Message: "好，就由我来安排！正在提交请求……"}
	s.wg.Add(1)
	s.mu.Unlock()
	s.publish()
	go s.performTodo(item, generation)
	return s.GetState(), nil
}

// performTodo 不接收前端传入的动作参数，网络失败或结果未知均不自动重发。
func (s *Service) performTodo(item storage.TodoRecord, generation uint64) {
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
		s.todoFeedback[todoSource{item.ProfileID, item.BaseURL}] = TodoFeedback{
			ID: item.ID, ProfileID: item.ProfileID, BaseURL: item.BaseURL,
			Action: item.Action, Target: item.Target, Status: status, Message: message,
		}
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
