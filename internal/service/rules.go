package service

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"autobgi-pet/internal/autobgi"
)

// ListPresets 查询真实配置，不缓存跨 profile 的列表，也不在读取时触发控制。
func (s *Service) ListPresets() ([]autobgi.TaskPreset, error) {
	s.mu.Lock()
	if s.state.Capabilities == nil || !s.state.Capabilities.TaskPresets {
		s.mu.Unlock()
		return nil, errors.New("当前连接未提供一条龙配置列表")
	}
	ticket, ctx, release, err := s.beginManualReadLocked(10 * time.Second)
	if err != nil {
		s.mu.Unlock()
		return nil, err
	}
	s.mu.Unlock()
	defer release()
	presets, err := ticket.adapter.ListPresets(ctx, "")
	s.mu.Lock()
	valid := s.manualReadCurrentLocked(ticket, ctx)
	s.mu.Unlock()
	if !valid {
		return nil, errors.New("读取期间连接、能力或暂停状态已变化，此次配置列表已失效")
	}
	return presets, err
}

// Ask 使用少量确定规则，无模型也能使用；模糊控制意图不推断、不执行。
func (s *Service) Ask(input string) (Answer, error) {
	query := strings.TrimSpace(input)
	if len([]rune(query)) > 500 {
		return Answer{}, errors.New("问题过长，请输入 500 字以内的简短指令")
	}
	switch {
	case query == "当前状态" || query == "现在运行到哪了" || query == "状态" || query == "运行状态":
		state := s.Refresh()
		if state.Snapshot == nil {
			return Answer{Text: "尚未取得有效状态。请先连接 AutoBGI；连接需要地址和 API 密钥。", Action: "connections", Presets: []autobgi.TaskPreset{}}, nil
		}
		label := "BGI 进程状态未知"
		if state.Snapshot.Process.Running != nil {
			if *state.Snapshot.Process.Running {
				label = "BGI 进程存在（不代表一条龙正在运行）"
			} else {
				label = "未检测到 BGI 进程（不代表任务成功）"
			}
		}
		if state.Snapshot.Stale {
			label = "以下是过期历史状态，当前连接不可用。\n" + label
		}
		message := label + "\n配置组：" + display(state.Snapshot.Task.GroupName) + "\n路线：" + display(state.Snapshot.Task.RouteName) + "\n脚本：" + display(state.Snapshot.Task.ScriptName) + "\n原始进度：" + display(state.Snapshot.Task.ProgressText) + "\n采集时间：" + state.Snapshot.FetchedAt
		return Answer{Text: message, Action: "overview", Snapshot: state.Snapshot, Presets: []autobgi.TaskPreset{}}, nil
	case query == "有哪些一条龙" || query == "一条龙列表" || query == "查看任务":
		presets, err := s.ListPresets()
		if err != nil {
			return Answer{}, err
		}
		message := "已从当前实例读取配置。启动时请在“一条龙配置”选择配置并核对确认卡；只读连接需要先明确开启控制。"
		if len(presets) == 0 {
			message = "当前实例没有可列出的一条龙配置。"
		}
		return Answer{Text: message, Action: "presets", Presets: presets}, nil
	case query == "打开 AutoBGI" || query == "打开autobgi" || query == "打开 AutoBgi":
		return Answer{Text: "请点击“打开 AutoBGI”按钮，在系统浏览器打开当前连接；不会附带 API 密钥。", Action: "open-autobgi", Presets: []autobgi.TaskPreset{}}, nil
	case strings.Contains(query, "收益"):
		return s.answerDomain("earnings")
	case strings.Contains(query, "培养") || strings.Contains(query, "背包") || strings.Contains(query, "账号"):
		if strings.Contains(query, "培养") {
			return s.answerDomain("cultivationPlans")
		}
		return s.answerDomain("backpack")
	case strings.Contains(query, "启动") || strings.Contains(query, "停止") || strings.Contains(query, "停一下") || strings.Contains(query, "关闭") || strings.Contains(query, "换个"):
		return Answer{Text: "启动一条龙请打开“一条龙配置”，选择真实配置并核对实例、地址和影响后确认。聊天不会根据模糊意图执行控制；停止、关闭与切换游戏账号请在 AutoBGI 操作。", Action: "help", Presets: []autobgi.TaskPreset{}}, nil
	default:
		return Answer{Text: "我支持：当前状态、有哪些一条龙、打开 AutoBGI、今天收益、查看培养计划。基础查询不需要模型服务。", Action: "help", Presets: []autobgi.TaskPreset{}}, nil
	}
}

// answerDomain 只把结构化事实转成简短文案，未知数量和未完成的执行能力不自行推断。
func (s *Service) answerDomain(kind string) (Answer, error) {
	result, err := s.ReadDomain("")
	if err != nil {
		return Answer{Text: err.Error(), Action: "overview", Presets: []autobgi.TaskPreset{}}, nil
	}
	message := ""
	switch kind {
	case "earnings":
		if value := result.Earnings; value != nil {
			total := "未知"
			if value.Total != nil {
				total = fmt.Sprintf("%d 摩拉", *value.Total)
			}
			message = "当前配置角色：" + value.CurrentRole.Nickname + "（" + value.CurrentRole.UID + "）\n日期：" + value.Date + "，时区：" + value.Timezone + "\n总收益：" + total + "\n数据来源：" + value.Source
		}
	case "backpack":
		if value := result.Backpack; value != nil {
			label := value.SelectedScanAccountID
			if label == "" {
				label = "兼容根目录"
			}
			message = fmt.Sprintf("扫描目录：%s\n本次扫描明确识别 %d 类物品。\n扫描时间：%s\n未识别的物品数量保持未知；扫描目录不代表当前运行账号。", label, len(value.Items), display(value.ScannedAt))
		}
	case "cultivationPlans":
		if value := result.CultivationPlans; value != nil {
			message = fmt.Sprintf("当前实例保存了 %d 项培养配置。\n这些配置不代表正在执行的队列；角色等级、天赋和材料明细可在概览的领域数据中查看。\n没有权威完成事件，材料齐全也不会被判定为培养完成。", len(value.Plans))
		}
	}
	if message == "" {
		message = result.Errors[kind]
		if message == "" {
			message = "此领域尚无可确认的数据，请在 AutoBGI 核对源记录。"
		}
	}
	return Answer{Text: message, Action: "overview", Presets: []autobgi.TaskPreset{}}, nil
}

// display 只对规则文案使用暂无数据，不制造数量或时间。
func display(value *string) string {
	if value == nil || strings.TrimSpace(*value) == "" {
		return "暂无数据"
	}
	return *value
}
