package service

import (
	"context"
	"strings"
	"sync"
	"time"

	"autobgi-pet/internal/autobgi"
)

// PetDetailResult 仅供本次桌宠交互使用，状态与截图可独立成功，不写入应用快照。
type PetDetailResult struct {
	Text             string
	StatusOK         bool
	JPEG             []byte
	ScreenshotFailed bool
}

// QueryPetDetail 同时读取触发时所选实例的运行详情和桌面截图，两项请求随详情会话一起取消。
func (s *Service) QueryPetDetail(clickCtx context.Context, expectedGeneration uint64) PetDetailResult {
	s.mu.Lock()
	// 异步查询启动前可能已切换实例，此时不能向新实例发送任何请求。
	if s.state.Connection.Generation != expectedGeneration {
		s.mu.Unlock()
		return PetDetailResult{Text: "连接已变化，请重新点击查看"}
	}
	if s.closed || s.paused || s.client == nil || s.connectionCtx == nil || s.connectionCtx.Err() != nil {
		s.mu.Unlock()
		return PetDetailResult{Text: "请先连接 ABGI"}
	}
	client, profileID, epoch := s.client, s.state.Connection.ProfileID, s.snapshotEpoch
	if s.snapshotContext == nil {
		s.snapshotContext, s.snapshotCancel = context.WithCancel(s.connectionCtx)
	}
	ctx, cancel := context.WithTimeout(s.snapshotContext, 10*time.Second)
	s.wg.Add(1)
	s.mu.Unlock()
	defer cancel()
	defer s.wg.Done()

	// 连续点击或窗口关闭时取消这次请求；应用断开、暂停或退出会通过父上下文一并取消。
	if clickCtx.Err() != nil {
		return PetDetailResult{}
	}
	stopWatchingClick := context.AfterFunc(clickCtx, cancel)
	defer stopWatchingClick()

	// 状态和截图并行请求，单项失败只影响对应展示，等待两者结束后再接纳结果。
	var result PetDetailResult
	var screenshot sync.WaitGroup
	screenshot.Go(func() {
		data, err := client.ReadDesktopScreenshot(ctx)
		result.JPEG, result.ScreenshotFailed = data, err != nil
	})
	snapshot, err := client.ReadLegacySnapshot(ctx)
	screenshot.Wait()
	if clickCtx.Err() != nil {
		return PetDetailResult{}
	}

	// 请求超时属于查询失败，只有连接本身失效才提示连接变化。
	s.mu.Lock()
	current := !s.paused && s.currentLocked(expectedGeneration, profileID) && s.snapshotEpoch == epoch && s.client == client && s.connectionCtx != nil && s.connectionCtx.Err() == nil && s.snapshotContext != nil && s.snapshotContext.Err() == nil
	s.mu.Unlock()
	if !current {
		return PetDetailResult{Text: "连接已变化，请重新点击查看"}
	}
	if err != nil {
		result.Text = "获取 BGI 运行情况失败，请检查 ABGI 连接"
	} else {
		result.Text, result.StatusOK = formatBGIStatus(snapshot), true
	}
	return result
}

// formatBGIStatus 保留服务端最近记录的语义，不把进程存在当作任务正在执行。
func formatBGIStatus(snapshot autobgi.Snapshot) string {
	var lines []string
	// 这些字段可能来自上一次任务，因此统一标注为最近记录。
	appendValue := func(label string, value *string) {
		if value == nil {
			return
		}
		text := strings.TrimSpace(*value)
		if text == "" || text == "[]" {
			return
		}
		lines = append(lines, label+"："+text)
	}
	appendValue("最近配置组", snapshot.Task.GroupName)
	appendValue("最近路线", snapshot.Task.RouteName)
	appendValue("最近脚本", snapshot.Task.ScriptName)
	appendValue("最近进度", snapshot.Task.ProgressText)
	// 日志中的脚本进度可能已有相同含义的前缀，避免重复显示。
	scriptProgress := snapshot.Task.ScriptProgressText
	if scriptProgress != nil {
		value := strings.TrimSpace(*scriptProgress)
		value = strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(value, "当前进度："), "当前进度:"))
		scriptProgress = &value
	}
	appendValue("最近脚本进度", scriptProgress)
	// 零时间意味着服务端尚无有效运行记录，不展示虚假的耗时或预计时间。
	if snapshot.Task.ETAText != nil {
		for _, line := range strings.Split(*snapshot.Task.ETAText, "\n") {
			line = strings.TrimSpace(line)
			if line != "" && !strings.Contains(line, "0001-01-01") && line != "【时间：】" && line != "【时长：】" && line != "【已经运行：】" && line != "【预计：】" {
				lines = append(lines, line)
			}
		}
	}
	if len(lines) == 0 {
		return "暂无运行记录"
	}
	return strings.Join(lines, "\n")
}
