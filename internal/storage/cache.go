package storage

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"time"

	"autobgi-pet/internal/autobgi"
)

// 缓存只保留少量最近快照，不承担事件、日志或业务终态的历史记录职责。
const cacheMaxEntries = 20
const cacheMaxSnapshotBytes = 64 * 1024
const cacheMaxFileBytes = 1024 * 1024

// CacheEntry 以连接、来源、实例与账号四项绑定历史数据，不能跨上下文复用。
type CacheEntry struct {
	ProfileID string           `json:"profileId"`
	BaseURL   string           `json:"baseUrl"`
	Snapshot  autobgi.Snapshot `json:"snapshot"`
}

// SnapshotCache 与主配置分开保存，所有恢复的数据始终被标记为过期。
type SnapshotCache struct {
	SchemaVersion int          `json:"schemaVersion"`
	Entries       []CacheEntry `json:"entries"`
}

var errCache = errors.New("桌宠历史缓存保存失败；当前在线数据仍可使用，历史数据不会作为新采集结果")

// EmptyCache 创建安全空容器，不生成模拟快照。
func EmptyCache() SnapshotCache { return SnapshotCache{SchemaVersion: 1, Entries: []CacheEntry{}} }

// cachePath 固定使用宿主的数据目录，不接受前端任意路径。
func (s *Store) cachePath() string { return filepath.Join(s.dataDir, "snapshot-cache.json") }

// validCacheEntry 对磁盘数据做结构、上下文与尺寸校验，损坏条目不参与恢复。
func validCacheEntry(entry CacheEntry) bool {
	snapshot := entry.Snapshot
	if entry.ProfileID == "" || entry.ProfileID != snapshot.ProfileID || entry.BaseURL == "" || snapshot.Scope != "instance" || snapshot.AccountID != nil {
		return false
	}
	if snapshot.SourceMode == "legacy" {
		if snapshot.ServerInstanceID != nil || snapshot.Task.State != "unknown" {
			return false
		}
	} else if snapshot.SourceMode == "pet-v1" {
		if snapshot.ServerInstanceID == nil || *snapshot.ServerInstanceID == "" {
			return false
		}
	} else {
		return false
	}
	if _, err := time.Parse(time.RFC3339Nano, snapshot.FetchedAt); err != nil {
		return false
	}
	data, err := json.Marshal(snapshot)
	return err == nil && len(data) <= cacheMaxSnapshotBytes
}

// LoadCache 限制读取体积；格式、版本、容量或条目校验失败时整份忽略并明确告警。
func (s *Store) LoadCache() (SnapshotCache, string) {
	file, err := os.Open(s.cachePath())
	if errors.Is(err, os.ErrNotExist) {
		return EmptyCache(), ""
	}
	if err != nil {
		return EmptyCache(), "历史缓存无法读取，已安全忽略；连接功能不受影响"
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, cacheMaxFileBytes+1))
	var cache SnapshotCache
	if err != nil || len(data) > cacheMaxFileBytes || json.Unmarshal(data, &cache) != nil || cache.SchemaVersion != 1 || len(cache.Entries) > cacheMaxEntries {
		return EmptyCache(), "历史缓存损坏或超过限制，已安全忽略；不会当作在线状态显示"
	}
	for i := range cache.Entries {
		if !validCacheEntry(cache.Entries[i]) {
			return EmptyCache(), "历史缓存条目无效，已安全忽略；不会当作在线状态显示"
		}
		cache.Entries[i].Snapshot.Stale = true
	}
	if cache.Entries == nil {
		cache.Entries = []CacheEntry{}
	}
	return cache, ""
}

// SaveCache 使用与配置相同的同目录原子替换，但不混入主配置或复制旧缓存备份。
func (s *Store) SaveCache(cache SnapshotCache) error {
	if cache.SchemaVersion != 1 || len(cache.Entries) > cacheMaxEntries {
		return errCache
	}
	for _, entry := range cache.Entries {
		if !validCacheEntry(entry) || !entry.Snapshot.Stale {
			return errCache
		}
	}
	data, err := json.Marshal(cache)
	if err != nil || len(data) > cacheMaxFileBytes {
		return errCache
	}
	if atomicWrite(s.cachePath(), data) != nil {
		return errCache
	}
	return nil
}

// sameOptional 区分未知实例和明确实例，避免 nil 被当成通配符。
func sameOptional(a, b *string) bool {
	return (a == nil && b == nil) || (a != nil && b != nil && *a == *b)
}

// cloneSnapshot 防止 UI、在线快照与缓存共享指针后互相污染。
func cloneSnapshot(snapshot autobgi.Snapshot) autobgi.Snapshot {
	data, _ := json.Marshal(snapshot)
	var copy autobgi.Snapshot
	_ = json.Unmarshal(data, &copy)
	copy.Stale = true
	return copy
}

// Find 只有完整上下文匹配才恢复，调用方仍负责 PetV1 探测后的实例身份核对。
func (cache *SnapshotCache) Find(profileID, baseURL, sourceMode string, instanceID, accountID *string) *autobgi.Snapshot {
	for _, entry := range cache.Entries {
		if entry.ProfileID == profileID && entry.BaseURL == baseURL && entry.Snapshot.SourceMode == sourceMode && sameOptional(entry.Snapshot.ServerInstanceID, instanceID) && sameOptional(entry.Snapshot.AccountID, accountID) {
			result := cloneSnapshot(entry.Snapshot)
			return &result
		}
	}
	return nil
}

// RemoveProfile 在来源改变或删除资料时丢弃该连接所有身份范围的缓存。
func (cache *SnapshotCache) RemoveProfile(profileID string) bool {
	kept := make([]CacheEntry, 0, len(cache.Entries))
	for _, entry := range cache.Entries {
		if entry.ProfileID != profileID {
			kept = append(kept, entry)
		}
	}
	changed := len(kept) != len(cache.Entries)
	cache.Entries = kept
	return changed
}

// Put 只接收规范化脱敏快照；超大单条拒绝，容量不足时淘汰最旧数据。
func (cache *SnapshotCache) Put(baseURL string, snapshot autobgi.Snapshot) error {
	entry := CacheEntry{ProfileID: snapshot.ProfileID, BaseURL: baseURL, Snapshot: cloneSnapshot(snapshot)}
	if !validCacheEntry(entry) {
		return errCache
	}
	kept := make([]CacheEntry, 0, len(cache.Entries)+1)
	for _, previous := range cache.Entries {
		if previous.ProfileID == entry.ProfileID && sameOptional(previous.Snapshot.ServerInstanceID, snapshot.ServerInstanceID) && sameOptional(previous.Snapshot.AccountID, snapshot.AccountID) {
			continue
		}
		kept = append(kept, previous)
	}
	kept = append(kept, entry)
	sort.SliceStable(kept, func(i, j int) bool {
		a, _ := time.Parse(time.RFC3339Nano, kept[i].Snapshot.FetchedAt)
		b, _ := time.Parse(time.RFC3339Nano, kept[j].Snapshot.FetchedAt)
		return a.After(b)
	})
	if len(kept) > cacheMaxEntries {
		kept = kept[:cacheMaxEntries]
	}
	cache.SchemaVersion = 1
	cache.Entries = kept
	for len(cache.Entries) > 0 {
		data, _ := json.Marshal(cache)
		if len(data) <= cacheMaxFileBytes {
			return nil
		}
		cache.Entries = cache.Entries[:len(cache.Entries)-1]
	}
	return nil
}
