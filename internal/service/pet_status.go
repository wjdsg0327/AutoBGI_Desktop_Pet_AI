package service

import (
	"context"
	"errors"
	"strings"
	"time"

	"autobgi-pet/internal/autobgi"
)

// QueryBGIStatus 按需读取当前连接的状态，拒绝旧连接的迟到结果。
func (s *Service) QueryBGIStatus() (string, error) {
	s.mu.Lock()
	if s.closed || s.paused || s.client == nil || s.connectionCtx == nil {
		s.mu.Unlock()
		return "", errors.New("请先连接 ABGI")
	}
	client, generation, profileID, epoch := s.client, s.state.Connection.Generation, s.state.Connection.ProfileID, s.snapshotEpoch
	if s.snapshotContext == nil {
		s.snapshotContext, s.snapshotCancel = context.WithCancel(s.connectionCtx)
	}
	ctx, cancel := context.WithTimeout(s.snapshotContext, 10*time.Second)
	s.wg.Add(1)
	s.mu.Unlock()
	defer cancel()
	defer s.wg.Done()

	snapshot, err := client.ReadLegacySnapshot(ctx)
	// 请求超时属于查询失败，只有连接本身失效才提示连接变化。
	s.mu.Lock()
	current := !s.closed && !s.paused && s.currentLocked(generation, profileID) && s.snapshotEpoch == epoch && s.client == client && s.connectionCtx != nil && s.connectionCtx.Err() == nil && s.snapshotContext != nil && s.snapshotContext.Err() == nil
	s.mu.Unlock()
	if !current {
		return "", errors.New("连接已变化，请重新点击查看")
	}
	if err != nil {
		return "", errors.New("获取 BGI 运行情况失败，请检查 ABGI 连接")
	}
	return formatBGIStatus(snapshot), nil
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
