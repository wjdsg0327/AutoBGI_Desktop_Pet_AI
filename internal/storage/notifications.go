package storage

import (
	"math"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"autobgi-pet/internal/autobgi"
)

// 通知历史分别限制来源、单来源与总量，正文同时限制字符数和编码后的字节数。
const (
	NotificationScopeLimit   = 40
	NotificationHistoryLimit = 200
	NotificationTotalLimit   = 1000
	NotificationMessageRunes = 1000
	NotificationMessageBytes = 4096
)

// NotificationEntry 只保存脱敏事实；展示尝试、用户已读和勿扰待汇总是独立状态。
type NotificationEntry struct {
	ID           string  `json:"id"`
	SourceKey    string  `json:"sourceKey"`
	Kind         string  `json:"kind"`
	Message      string  `json:"message"`
	CreatedAt    string  `json:"createdAt"`
	AttemptedAt  *string `json:"attemptedAt"`
	ReadAt       *string `json:"readAt"`
	QuietPending bool    `json:"quietPending"`
}

// NotificationErrorSummary 只保留已经裁掉正文的未读错误数量，版本来自整个日志的持久计数器。
type NotificationErrorSummary struct {
	Revision     string  `json:"revision"`
	UnreadCount  uint32  `json:"unreadCount"`
	QuietPending uint32  `json:"quietPending"`
	LatestAt     *string `json:"latestAt"`
}

// NotificationScope 按完整来源身份隔离，累计待汇总数不因裁剪未读历史而减少。
type NotificationScope struct {
	ProfileID    string                    `json:"profileId"`
	BaseURL      string                    `json:"baseUrl"`
	SourceMode   string                    `json:"sourceMode"`
	InstanceID   string                    `json:"instanceId"`
	AccountID    *string                   `json:"accountId"`
	Entries      []NotificationEntry       `json:"entries"`
	QuietPending uint32                    `json:"quietPending"`
	Truncated    bool                      `json:"truncated"`
	ErrorSummary *NotificationErrorSummary `json:"errorSummary"`
}

// notificationRevision 拒绝浮点数、前导零及溢出，版本始终以规范十进制字符串跨越前端精度边界。
func notificationRevision(value string) (uint64, bool) {
	revision, err := strconv.ParseUint(value, 10, 64)
	return revision, err == nil && strconv.FormatUint(revision, 10) == value
}

// validNotificationErrorSummary 不把未知裁剪历史猜成错误，清空后的版本墓碑仍须保持完整。
func validNotificationErrorSummary(scope NotificationScope) bool {
	summary := scope.ErrorSummary
	if summary == nil {
		return true
	}
	revision, ok := notificationRevision(summary.Revision)
	if !ok || revision == 0 || !scope.Truncated || summary.QuietPending > summary.UnreadCount {
		return false
	}
	if summary.UnreadCount == 0 {
		return summary.QuietPending == 0 && summary.LatestAt == nil
	}
	if summary.LatestAt == nil {
		return false
	}
	_, ok = notificationTime(*summary.LatestAt)
	return ok
}

// validNotificationErrorRevision 保证日志版本不落后于任何来源，来源淘汰和重建也不能重用旧版本。
func validNotificationErrorRevision(value string, scopes []NotificationScope) bool {
	revision, ok := notificationRevision(value)
	if !ok {
		return false
	}
	for _, scope := range scopes {
		if scope.ErrorSummary != nil {
			current, valid := notificationRevision(scope.ErrorSummary.Revision)
			if !valid || current == 0 || current > revision {
				return false
			}
		}
	}
	return true
}

// notificationJournalRevision 只兼容完全没有摘要的旧 Go 零值，不能替已有摘要重置全局版本。
func notificationJournalRevision(journal Journal) (string, bool) {
	if journal.NotificationErrorRevision != "" {
		return journal.NotificationErrorRevision, validNotificationErrorRevision(journal.NotificationErrorRevision, journal.NotificationScopes)
	}
	for _, scope := range journal.NotificationScopes {
		if scope.ErrorSummary != nil {
			return "", false
		}
	}
	return "0", true
}

// notificationText 拒绝身份中的控制字符和首尾空白，正文仅保留正常换行与制表符。
func notificationText(value string, limit int, multiline bool) bool {
	if value == "" || len(value) > limit || !utf8.ValidString(value) || strings.TrimSpace(value) == "" {
		return false
	}
	if !multiline && strings.TrimSpace(value) != value {
		return false
	}
	for _, char := range value {
		if unicode.IsControl(char) && (!multiline || char != '\n' && char != '\t') {
			return false
		}
	}
	return true
}

// validNotificationIdentity 不允许探测前错误冒充旧版来源，当前实例协议不接受账号范围。
func validNotificationIdentity(scope NotificationScope) bool {
	if !notificationText(scope.ProfileID, 128, false) || !notificationText(scope.BaseURL, 2048, false) || scope.AccountID != nil {
		return false
	}
	base, err := autobgi.NormalizeURL(scope.BaseURL, true)
	if err != nil || base != scope.BaseURL {
		return false
	}
	switch scope.SourceMode {
	case "unverified", "legacy":
		return scope.InstanceID == ""
	case "pet-v1":
		return notificationText(scope.InstanceID, 128, false)
	default:
		return false
	}
}

// notificationTime 只接受有效且非零的完整时间，避免无效标记被解释为已读或已展示。
func notificationTime(value string) (time.Time, bool) {
	if len(value) > 64 {
		return time.Time{}, false
	}
	parsed, err := time.Parse(time.RFC3339Nano, value)
	return parsed, err == nil && !parsed.IsZero() && parsed.Year() >= 1 && parsed.Year() <= 9999
}

// validNotificationEntry 将首次事实与后续处理标记分开校验，勿扰待汇总不能同时已经读过或尝试展示。
func validNotificationEntry(entry NotificationEntry) bool {
	if !validHex(entry.ID, 32) || !notificationText(entry.SourceKey, 192, false) ||
		!notificationText(entry.Message, NotificationMessageBytes, true) || utf8.RuneCountInString(entry.Message) > NotificationMessageRunes {
		return false
	}
	switch entry.Kind {
	case "info", "connection", "progress", "error", "command", "materials", "targets", "summary":
	default:
		return false
	}
	_, ok := notificationTime(entry.CreatedAt)
	if !ok || entry.QuietPending && (entry.AttemptedAt != nil || entry.ReadAt != nil) {
		return false
	}
	for _, marker := range []*string{entry.AttemptedAt, entry.ReadAt} {
		if marker != nil {
			// 服务端事实时间与本地处理时间允许有时钟偏差，不能用先后关系阻断合法已读。
			if _, valid := notificationTime(*marker); !valid {
				return false
			}
		}
	}
	return true
}

// NotificationScopeIndex 使用全部来源字段比较，空账号与明确账号不能意外共用历史。
func NotificationScopeIndex(scopes []NotificationScope, identity NotificationScope) int {
	for index, scope := range scopes {
		if scope.ProfileID == identity.ProfileID && scope.BaseURL == identity.BaseURL &&
			scope.SourceMode == identity.SourceMode && scope.InstanceID == identity.InstanceID &&
			sameOptional(scope.AccountID, identity.AccountID) {
			return index
		}
	}
	return -1
}

// validNotificationScopes 的宽松容量模式只供纯裁剪使用，磁盘恢复与保存始终执行全部上限。
func validNotificationScopes(scopes []NotificationScope, capacity bool) bool {
	total := 0
	if capacity && len(scopes) > NotificationScopeLimit {
		return false
	}
	for index, scope := range scopes {
		if !validNotificationIdentity(scope) || scope.Entries == nil || NotificationScopeIndex(scopes[:index], scope) >= 0 ||
			!validNotificationErrorSummary(scope) || capacity && len(scope.Entries) > NotificationHistoryLimit {
			return false
		}
		total += len(scope.Entries)
		ids, sources := map[string]bool{}, map[string]bool{}
		var pending uint64
		if scope.ErrorSummary != nil {
			pending = uint64(scope.ErrorSummary.QuietPending)
		}
		for _, entry := range scope.Entries {
			if !validNotificationEntry(entry) || ids[entry.ID] || sources[entry.SourceKey] {
				return false
			}
			ids[entry.ID], sources[entry.SourceKey] = true, true
			if entry.QuietPending {
				pending++
			}
		}
		if pending > uint64(scope.QuietPending) || !scope.Truncated && pending != uint64(scope.QuietPending) {
			return false
		}
	}
	return !capacity || total <= NotificationTotalLimit
}

// cloneNotificationScopes 切断切片和时间指针别名，纯处理失败不能改变调用方已提交状态。
func cloneNotificationScopes(scopes []NotificationScope) []NotificationScope {
	result := make([]NotificationScope, len(scopes))
	for index, scope := range scopes {
		result[index] = scope
		if scope.AccountID != nil {
			account := *scope.AccountID
			result[index].AccountID = &account
		}
		if scope.ErrorSummary != nil {
			summary := *scope.ErrorSummary
			result[index].ErrorSummary = &summary
			if summary.LatestAt != nil {
				at := *summary.LatestAt
				summary.LatestAt = &at
			}
		}
		result[index].Entries = make([]NotificationEntry, len(scope.Entries))
		for entryIndex, entry := range scope.Entries {
			result[index].Entries[entryIndex] = entry
			if entry.AttemptedAt != nil {
				at := *entry.AttemptedAt
				result[index].Entries[entryIndex].AttemptedAt = &at
			}
			if entry.ReadAt != nil {
				at := *entry.ReadAt
				result[index].Entries[entryIndex].ReadAt = &at
			}
		}
	}
	return result
}

// notificationVictim 跨来源优先选最旧已读项，只有没有已读项时才裁剪最旧未读项。
func notificationVictim(scopes []NotificationScope, onlyScope int) (int, int) {
	bestScope, bestEntry := -1, -1
	var bestTime time.Time
	bestRead := false
	for scopeIndex, scope := range scopes {
		if onlyScope >= 0 && scopeIndex != onlyScope {
			continue
		}
		for entryIndex, entry := range scope.Entries {
			created, _ := notificationTime(entry.CreatedAt)
			read := entry.ReadAt != nil
			if bestScope < 0 || read && !bestRead || read == bestRead && created.Before(bestTime) {
				bestScope, bestEntry, bestTime, bestRead = scopeIndex, entryIndex, created, read
			}
		}
	}
	return bestScope, bestEntry
}

// trimNotificationEntry 在裁掉未读错误前保留可确认摘要，任何计数溢出都交给调用方整体回滚。
func trimNotificationEntry(scopes []NotificationScope, scopeIndex, entryIndex int, revision *uint64) error {
	scope := &scopes[scopeIndex]
	entry := scope.Entries[entryIndex]
	if entry.ReadAt == nil && entry.Kind == "error" {
		summary := scope.ErrorSummary
		if *revision == math.MaxUint64 || summary != nil &&
			(summary.UnreadCount == math.MaxUint32 || entry.QuietPending && summary.QuietPending == math.MaxUint32) {
			return errJournal
		}
		if summary == nil {
			summary = &NotificationErrorSummary{}
			scope.ErrorSummary = summary
		}
		*revision++
		summary.Revision = strconv.FormatUint(*revision, 10)
		summary.UnreadCount++
		if entry.QuietPending {
			summary.QuietPending++
		}
		// 服务器事实可能乱序到达，只保留被裁掉错误的最晚事实时间。
		created, _ := notificationTime(entry.CreatedAt)
		latest := time.Time{}
		if summary.LatestAt != nil {
			latest, _ = notificationTime(*summary.LatestAt)
		}
		if summary.LatestAt == nil || created.After(latest) {
			at := entry.CreatedAt
			summary.LatestAt = &at
		}
	}
	if entry.ReadAt == nil {
		scope.Truncated = true
	}
	scope.Entries = append(scope.Entries[:entryIndex], scope.Entries[entryIndex+1:]...)
	return nil
}

// notificationScopeVictim 仅淘汰无待汇总、无未读正文和未确认摘要的来源。
func notificationScopeVictim(scopes []NotificationScope) int {
	best := -1
	var oldest time.Time
	for index, scope := range scopes {
		if scope.QuietPending != 0 || scope.ErrorSummary != nil && scope.ErrorSummary.UnreadCount != 0 {
			continue
		}
		eligible := true
		var latest time.Time
		for _, entry := range scope.Entries {
			if entry.ReadAt == nil {
				eligible = false
				break
			}
			created, _ := notificationTime(entry.CreatedAt)
			if created.After(latest) {
				latest = created
			}
		}
		if eligible && (best < 0 || latest.Before(oldest)) {
			best, oldest = index, latest
		}
	}
	return best
}

// TrimNotificationScopes 同时返回有界副本与全局版本，失败时二者均不能提交。
func TrimNotificationScopes(scopes []NotificationScope, revision string) ([]NotificationScope, string, error) {
	if !validNotificationScopes(scopes, false) || !validNotificationErrorRevision(revision, scopes) {
		return nil, revision, errJournal
	}
	current, _ := notificationRevision(revision)
	next := cloneNotificationScopes(scopes)
	total := 0
	for index := range next {
		for len(next[index].Entries) > NotificationHistoryLimit {
			scopeIndex, entryIndex := notificationVictim(next, index)
			if trimNotificationEntry(next, scopeIndex, entryIndex, &current) != nil {
				return nil, revision, errJournal
			}
		}
		total += len(next[index].Entries)
	}
	for total > NotificationTotalLimit {
		scopeIndex, entryIndex := notificationVictim(next, -1)
		if trimNotificationEntry(next, scopeIndex, entryIndex, &current) != nil {
			return nil, revision, errJournal
		}
		total--
	}
	for len(next) > NotificationScopeLimit {
		index := notificationScopeVictim(next)
		if index < 0 {
			return nil, revision, errJournal
		}
		next = append(next[:index], next[index+1:]...)
	}
	if !validNotificationScopes(next, true) {
		return nil, revision, errJournal
	}
	return next, strconv.FormatUint(current, 10), nil
}

// PutNotification 只在事实合法且整理成功后修改日志；重复来源保留首次身份、时间和处理状态。
func PutNotification(journal *Journal, identity NotificationScope, entry NotificationEntry) (bool, error) {
	if journal == nil || journal.SchemaVersion != 1 || !validNotificationIdentity(identity) ||
		!validNotificationEntry(entry) || !validNotificationScopes(journal.NotificationScopes, true) {
		return false, errJournal
	}
	revision, valid := notificationJournalRevision(*journal)
	if !valid {
		return false, errJournal
	}
	index := NotificationScopeIndex(journal.NotificationScopes, identity)
	if index >= 0 {
		for _, previous := range journal.NotificationScopes[index].Entries {
			if previous.SourceKey == entry.SourceKey {
				if previous.Kind != entry.Kind || previous.Message != entry.Message {
					return false, errJournal
				}
				return false, nil
			}
			if previous.ID == entry.ID {
				return false, errJournal
			}
		}
	}
	next := cloneNotificationScopes(journal.NotificationScopes)
	if index < 0 {
		identity.Entries, identity.QuietPending, identity.Truncated = []NotificationEntry{}, 0, false
		identity.ErrorSummary = nil
		next = append(next, identity)
		index = len(next) - 1
	}
	if entry.QuietPending {
		if next[index].QuietPending == math.MaxUint32 {
			return false, errJournal
		}
		next[index].QuietPending++
	}
	next[index].Entries = append(next[index].Entries, entry)
	trimmed, revision, err := TrimNotificationScopes(next, revision)
	if err != nil {
		return false, err
	}
	journal.NotificationScopes = trimmed
	journal.NotificationErrorRevision = revision
	return true, nil
}

// AcknowledgeNotificationErrors 只确认用户看到的精确版本，整次成功后才替换摘要和勿扰计数。
func AcknowledgeNotificationErrors(journal *Journal, identity NotificationScope, expectedRevision string) error {
	if journal == nil || journal.SchemaVersion != 1 || !validNotificationIdentity(identity) ||
		!validNotificationScopes(journal.NotificationScopes, true) {
		return errJournal
	}
	revision, valid := notificationJournalRevision(*journal)
	expected, expectedValid := notificationRevision(expectedRevision)
	if !valid || !expectedValid || expected == 0 {
		return errJournal
	}
	current, _ := notificationRevision(revision)
	index := NotificationScopeIndex(journal.NotificationScopes, identity)
	if index < 0 || current == math.MaxUint64 {
		return errJournal
	}
	previous := journal.NotificationScopes[index].ErrorSummary
	if previous == nil || previous.UnreadCount == 0 || previous.Revision != expectedRevision ||
		previous.QuietPending > journal.NotificationScopes[index].QuietPending {
		return errJournal
	}
	next := cloneNotificationScopes(journal.NotificationScopes)
	scope := &next[index]
	scope.QuietPending -= previous.QuietPending
	scope.ErrorSummary.Revision = strconv.FormatUint(current+1, 10)
	scope.ErrorSummary.UnreadCount, scope.ErrorSummary.QuietPending, scope.ErrorSummary.LatestAt = 0, 0, nil
	revision = strconv.FormatUint(current+1, 10)
	if !validNotificationScopes(next, true) || !validNotificationErrorRevision(revision, next) {
		return errJournal
	}
	journal.NotificationScopes, journal.NotificationErrorRevision = next, revision
	return nil
}
