package desktop

import (
	"crypto/rand"
	"encoding/hex"
	"math"
)

// PetDragStart 只向角色窗口返回一次手势令牌和逻辑原点，不暴露原生句柄或磁盘位置。
type PetDragStart struct {
	Token string `json:"token"`
	X     int    `json:"x"`
	Y     int    `json:"y"`
}

// petDragEnvironment 按值固定手势开始时的布局、实际尺寸和交互偏好代次。
type petDragEnvironment struct {
	layout     string
	width      int
	height     int
	scale      float64
	generation uint64
}

// valid 拒绝空布局和无效尺寸；缩放值必须保持有限，不能用 NaN 绕过环境比较。
func (e petDragEnvironment) valid() bool {
	return e.layout != "" && e.width > 0 && e.height > 0 &&
		e.scale > 0 && !math.IsNaN(e.scale) && !math.IsInf(e.scale, 0)
}

// petDragState 仅保存一个当前手势，调用方使用宿主锁，不在此处调用窗口或存储。
type petDragState struct {
	token       string
	environment petDragEnvironment
	revision    uint64
}

// invalidate 不生成替代令牌；任何恢复操作都必须重新开始完整手势。
func (s *petDragState) invalidate() {
	s.token = ""
	s.environment = petDragEnvironment{}
	s.revision++
}

// begin 无论成功与否先撤销旧手势，随机源或枚举失败不能留下旧令牌继续移动。
func (s *petDragState) begin(token string, environment petDragEnvironment) bool {
	s.invalidate()
	if token == "" || !environment.valid() {
		return false
	}
	s.token, s.environment = token, environment
	return true
}

// accepts 先匹配令牌再处理环境，迟到的旧手势不能借无效环境撤销新手势。
func (s *petDragState) accepts(token string, environment petDragEnvironment) bool {
	if token == "" || token != s.token {
		return false
	}
	if !environment.valid() || environment != s.environment {
		s.invalidate()
		return false
	}
	return true
}

// observe 让主动收到的布局或偏好变化永久撤销旧手势，随后恢复原值也不会复活。
func (s *petDragState) observe(environment petDragEnvironment) {
	if s.token != "" && (!environment.valid() || environment != s.environment) {
		s.invalidate()
	}
}

// finish 只消费仍然匹配的手势一次，持久化失败也不能再次消费同一结束请求。
func (s *petDragState) finish(token string, environment petDragEnvironment) bool {
	if !s.accepts(token, environment) {
		return false
	}
	s.invalidate()
	return true
}

// cancel 只撤销指定令牌，不因前一个手势的迟到取消影响新的拖动。
func (s *petDragState) cancel(token string) bool {
	if token == "" || token != s.token {
		return false
	}
	s.invalidate()
	return true
}

// newPetDragToken 使用系统随机源生成不可预测的独立令牌，失败时由宿主保持无会话状态。
func newPetDragToken() (string, error) {
	var token [32]byte
	if _, err := rand.Read(token[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(token[:]), nil
}
