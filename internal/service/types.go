// Package service 只暴露确定性的桌宠业务绑定，网络和凭据留在 Go 层。
package service

import (
	"autobgi-pet/internal/autobgi"
	"autobgi-pet/internal/profile"
)

// ConnectionState 显式区分鉴权失败与可重试网络故障。
type ConnectionState struct {
	Status       string  `json:"status"`
	ProfileID    string  `json:"profileId"`
	Generation   uint64  `json:"generation"`
	ErrorCode    string  `json:"errorCode"`
	ErrorMessage string  `json:"errorMessage"`
	RetryAt      *string `json:"retryAt"`
}

// Notification 只接收本地事实提示，不把旧版日志伪造为成功事件。
type Notification struct {
	ID        string `json:"id"`
	Kind      string `json:"kind"`
	Message   string `json:"message"`
	CreatedAt string `json:"createdAt"`
}

// LogLine 仅保存在当前会话中，时间代表桌宠收到该行的时刻。
type LogLine struct {
	Time string `json:"time"`
	Text string `json:"text"`
}

// LogTrigger 是一条规则命中后的独立事件，编号在应用运行期间递增。
type LogTrigger struct {
	ID         uint64 `json:"id"`
	Generation uint64 `json:"generation"`
	Action     string `json:"action"`
	Text       string `json:"text"`
	Keyword    string `json:"keyword"`
}

// LogPickup 是一条拾取日志中的独立物品事件。
type LogPickup struct {
	ID         uint64 `json:"id"`
	Generation uint64 `json:"generation"`
	Name       string `json:"name"`
}

// LogStreamState 记录日志连接状态和最近收到的有限数量的行。
type LogStreamState struct {
	Status         string      `json:"status"`
	FileName       string      `json:"fileName"`
	Logs           []LogLine   `json:"logs"`
	Error          string      `json:"error"`
	LastTrigger    *LogTrigger `json:"lastTrigger"`
	LastActivityAt string      `json:"lastActivityAt"` // 仅在线时记录最后一次收到日志或建立连接的时间。
	// DroppedTriggers 记录本次连接因桌宠展示队列已满而未播放的命中。
	DroppedTriggers uint64 `json:"droppedTriggers"`
}

// TodoItem 仅向当前连接的窗口展示已脱敏的代办快照。
type TodoItem struct {
	ID          string `json:"id"`
	Text        string `json:"text"`
	Action      string `json:"action"`
	Target      string `json:"target"`
	ProfileID   string `json:"profileId"`
	ProfileName string `json:"profileName"`
	BaseURL     string `json:"baseUrl"`
	CreatedAt   string `json:"createdAt"`
	ExpiresAt   string `json:"expiresAt"`
}

// TodoFeedback 描述单次处理请求，受理不等于远端任务已经完成。
type TodoFeedback struct {
	ID        string `json:"id"`
	ProfileID string `json:"profileId"`
	BaseURL   string `json:"baseUrl"`
	Action    string `json:"action"`
	Target    string `json:"target"`
	Status    string `json:"status"`
	Message   string `json:"message"`
}

// AppState 是所有窗口共享的唯一状态，内含的连接资料已经脱敏。
type AppState struct {
	Revision            uint64                   `json:"revision"`
	Profiles            []profile.ProfileView    `json:"profiles"`
	ActiveProfileID     string                   `json:"activeProfileId"`
	Connection          ConnectionState          `json:"connection"`
	Snapshot            *autobgi.Snapshot        `json:"snapshot"`
	Capabilities        *autobgi.Capabilities    `json:"capabilities"`
	Preferences         profile.Preferences      `json:"preferences"`
	Notifications       []Notification           `json:"notifications"`
	NotificationHistory *NotificationHistory     `json:"notificationHistory"` // 持久历史与本进程实时投递分开，恢复不重播。
	Notice              string                   `json:"notice"`
	PreparedCommand     *CommandConfirmation     `json:"preparedCommand"`
	Commands            []autobgi.CommandReceipt `json:"commands"`
	EventStatus         string                   `json:"eventStatus"`
	MaterialsReady      *MaterialsReadyState     `json:"materialsReady"` // 独立材料历史只读展示，不代表游戏升级或任务成功。
	TargetsMet          *TargetsMetState         `json:"targetsMet"`     // 独立基础面板达标历史，保持原始观察与提交时间。
	KeywordRules        []profile.KeywordRule    `json:"keywordRules"`
	LogStream           LogStreamState           `json:"logStream"`
	Todos               []TodoItem               `json:"todos"`
	TodoFeedback        *TodoFeedback            `json:"todoFeedback"`
	TodoNotice          string                   `json:"todoNotice"`
}

// CommandConfirmation 冻结确认对象；令牌只能消费一次，前端不能通过布尔值伪造确认。
type CommandConfirmation struct {
	Token                string  `json:"token"`
	ProfileID            string  `json:"profileId"`
	ProfileName          string  `json:"profileName"`
	BaseURL              string  `json:"baseUrl"`
	InstanceID           string  `json:"instanceId"`
	AccountID            *string `json:"accountId"`
	Scope                string  `json:"scope"`
	CommandID            string  `json:"commandId"`
	Type                 string  `json:"type"`
	PresetID             string  `json:"presetId"`
	PresetName           string  `json:"presetName"`
	Impact               string  `json:"impact"`
	ExpectedStateVersion string  `json:"expectedStateVersion"`
	ExpiresAt            string  `json:"expiresAt"`
}

// Answer 只返回本地规则生成的纯文本和有限面板动作，不接受任意 URL。
type Answer struct {
	Text     string               `json:"text"`
	Action   string               `json:"action"`
	Snapshot *autobgi.Snapshot    `json:"snapshot"`
	Presets  []autobgi.TaskPreset `json:"presets"`
}
