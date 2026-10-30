// Package bindings 只向窗口开放本版桌宠需要的操作。
package bindings

import (
	"autobgi-pet/internal/desktop"
	"autobgi-pet/internal/profile"
	"autobgi-pet/internal/service"
)

// PetAPI 限定连接和关键词规则，不暴露旧版业务服务。
type PetAPI struct {
	service *service.Service
}

// NewPetAPI 由宿主注入业务服务，前端不能自行替换服务对象。
func NewPetAPI(s *service.Service) *PetAPI { return &PetAPI{service: s} }

// GetState 返回当前连接、日志与规则状态。
func (p *PetAPI) GetState() service.AppState { return p.service.GetState() }

// SaveProfile 保存地址与 API 密钥。
func (p *PetAPI) SaveProfile(input profile.ProfileInput) (service.AppState, error) {
	return p.service.SaveProfile(input)
}

// DeleteProfile 删除指定连接。
func (p *PetAPI) DeleteProfile(id string) (service.AppState, error) {
	return p.service.DeleteProfile(id)
}

// Connect 启动指定连接的日志订阅。
func (p *PetAPI) Connect(id string) (service.AppState, error) { return p.service.Connect(id) }

// Disconnect 停止当前日志订阅。
func (p *PetAPI) Disconnect() service.AppState { return p.service.Disconnect() }

// UpdateKeywordRules 保存完整的关键词动作规则。
func (p *PetAPI) UpdateKeywordRules(rules []profile.KeywordRule) (service.AppState, error) {
	return p.service.UpdateKeywordRules(rules)
}

// ListTodoTargets 读取当前连接中可选的一条龙或配置组名称。
func (p *PetAPI) ListTodoTargets(action string) ([]string, error) {
	return p.service.ListTodoTargets(action)
}

// ProcessTodo 仅根据后端保存的代办快照执行一次固定动作。
func (p *PetAPI) ProcessTodo(id string) (service.AppState, error) {
	return p.service.ProcessTodo(id)
}

// DismissTodo 删除当前连接中的代办，不触发业务动作。
func (p *PetAPI) DismissTodo(id string) (service.AppState, error) {
	return p.service.DismissTodo(id)
}

// UpdatePickupEnabled 独立保存拾取通知开关，不覆盖关键词编辑草稿。
func (p *PetAPI) UpdatePickupEnabled(enabled bool) (service.AppState, error) {
	return p.service.UpdatePickupEnabled(enabled)
}

// DesktopAPI 限定窗口展示和拖动操作，不暴露宿主内部队列。
type DesktopAPI struct {
	desktop *desktop.DesktopService
}

// NewDesktopAPI 由宿主注入现有窗口管理对象。
func NewDesktopAPI(d *desktop.DesktopService) *DesktopAPI { return &DesktopAPI{desktop: d} }

// GetPresentation 读取无密钥的窗口展示状态。
func (d *DesktopAPI) GetPresentation() map[string]any { return d.desktop.GetPresentation() }

// SetTodoPetHover 同步角色窗口悬停，三秒计时只由宿主管理。
func (d *DesktopAPI) SetTodoPetHover(hover bool) { d.desktop.SetTodoPetHover(hover) }

// SetTodoWindowHover 保留鼠标从角色移动到代办圆圈的短暂过渡。
func (d *DesktopAPI) SetTodoWindowHover(hover bool) { d.desktop.SetTodoWindowHover(hover) }

// OpenTodoWindow 展开代办对话，关闭对话不会消费代办。
func (d *DesktopAPI) OpenTodoWindow() { d.desktop.OpenTodoWindow() }

// CloseTodoWindow 收起代办窗口，不改变代办内容。
func (d *DesktopAPI) CloseTodoWindow() { d.desktop.HideTodoWindow() }

// GetTodoWindowState 供窗口加载完成后补取当前展示模式。
func (d *DesktopAPI) GetTodoWindowState() map[string]any { return d.desktop.GetTodoWindowState() }

// StartKeywordTrigger 在桌宠首帧展示后确认当前派发轮次的动作开始。
func (d *DesktopAPI) StartKeywordTrigger(id uint64, generation uint64, attempt uint64) bool {
	return d.desktop.StartKeywordTrigger(id, generation, attempt)
}

// SetPetDocumentVisible 同步桌宠页面的可见状态，隐藏期间保留未播放完的拾取动作。
func (d *DesktopAPI) SetPetDocumentVisible(visible bool) {
	d.desktop.SetPetDocumentVisible(visible)
}

// ShowPet 显示桌宠窗口。
func (d *DesktopAPI) ShowPet() { d.desktop.ShowPet() }

// ShowPanel 打开桌宠设置。
func (d *DesktopAPI) ShowPanel() { d.desktop.ShowPanel() }

// ShowContextMenu 显示桌宠菜单。
func (d *DesktopAPI) ShowContextMenu() { d.desktop.ShowContextMenu() }

// ImportKeywordRulesFile 选择并校验分享文件，返回规则草稿而不直接保存。
func (d *DesktopAPI) ImportKeywordRulesFile() ([]profile.KeywordRule, error) {
	return d.desktop.ImportKeywordRulesFile()
}

// ExportKeywordRulesFile 将当前编辑的规则写入用户选择的分享文件。
func (d *DesktopAPI) ExportKeywordRulesFile(rules []profile.KeywordRule) (bool, error) {
	return d.desktop.ExportKeywordRulesFile(rules)
}

// PetClicked 执行桌宠点击互动。
func (d *DesktopAPI) PetClicked() { d.desktop.PetClicked() }

// BeginPetDrag 开始一次带令牌的窗口拖动。
func (d *DesktopAPI) BeginPetDrag() *desktop.PetDragStart { return d.desktop.BeginPetDrag() }

// DragPetTo 按已授权的拖动令牌移动窗口。
func (d *DesktopAPI) DragPetTo(token string, x, y int) bool { return d.desktop.DragPetTo(token, x, y) }

// PetDragged 结束拖动并保存位置。
func (d *DesktopAPI) PetDragged(token string) bool { return d.desktop.PetDragged(token) }

// CancelPetDrag 取消指定拖动会话。
func (d *DesktopAPI) CancelPetDrag(token string) bool { return d.desktop.CancelPetDrag(token) }
