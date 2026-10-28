package service

import (
	"encoding/json"
	"errors"
	"time"

	"autobgi-pet/internal/profile"
)

// flushCacheLocked 在线采集最多三十秒落盘一次，退出和显式失效时强制提交。
func (s *Service) flushCacheLocked(force bool) {
	if !s.cacheDirty || s.store == nil || (!force && time.Since(s.cacheLastWrite) < 30*time.Second) {
		return
	}
	s.cacheLastWrite = time.Now()
	if err := s.store.SaveCache(s.cache); err != nil {
		s.state.Notice = err.Error()
		return
	}
	s.cacheDirty = false
}

// invalidateCacheLocked 只清理目标连接的历史，不修改正在运行的其他连接快照。
func (s *Service) invalidateCacheLocked(id string) {
	if s.cache.RemoveProfile(id) {
		s.cacheDirty = true
		s.flushCacheLocked(true)
	}
}

// restoreLegacyLocked 保留旧采集时间与 stale 标记，不把缓存恢复当成连接成功。
func (s *Service) restoreLegacyLocked(p profile.Profile) {
	if p.AdapterMode == "pet-v1" {
		return
	}
	if cached := s.cache.Find(p.ID, p.BaseURL, "legacy", nil, nil); cached != nil {
		s.state.Snapshot = cached
	}
}

// ExportProfiles 仅导出明确白名单字段，不导出密钥、系统引用、内部 ID 或历史状态。
func (s *Service) ExportProfiles() (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	result := profileTransferDocument{SchemaVersion: 1, Profiles: []profileTransferEntry{}}
	for _, p := range s.config.Profiles {
		result.Profiles = append(result.Profiles, profileTransferEntry{Name: p.Name, BaseURL: p.BaseURL, AdapterMode: p.AdapterMode, ReadOnly: true, AllowInsecure: p.AllowInsecure, AutoConnect: false})
	}
	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil || len(data) > MaxProfileTransferBytes {
		return "", errors.New("连接资料导出失败")
	}
	return string(data), nil
}
