// Package profile 定义不含明文凭据的本地连接资料和偏好。
package profile

// Profile 只保存系统凭据引用；密钥始终保存在操作系统或会话内存中。
type Profile struct {
	ID                 string  `json:"id"`
	Name               string  `json:"name"`
	BaseURL            string  `json:"baseUrl"`
	CredentialRef      string  `json:"credentialRef"`
	AdapterMode        string  `json:"adapterMode"`
	PreferredAccountID *string `json:"preferredAccountId"`
	ReadOnly           bool    `json:"readOnly"`
	AutoConnect        bool    `json:"autoConnect"`
	AllowInsecure      bool    `json:"allowInsecure"`
	SessionOnly        bool    `json:"sessionOnly"`
}

// ProfileView 是返回 WebView 的脱敏连接资料。
type ProfileView struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	BaseURL       string `json:"baseUrl"`
	AdapterMode   string `json:"adapterMode"`
	ReadOnly      bool   `json:"readOnly"`
	AutoConnect   bool   `json:"autoConnect"`
	AllowInsecure bool   `json:"allowInsecure"`
	HasCredential bool   `json:"hasCredential"`
	SessionOnly   bool   `json:"sessionOnly"`
}

// ProfileInput 仅在保存操作中一次性接收密钥，绝不作为状态返回。
type ProfileInput struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	BaseURL       string `json:"baseUrl"`
	APIKey        string `json:"apiKey"`
	AdapterMode   string `json:"adapterMode"`
	ReadOnly      bool   `json:"readOnly"`
	AutoConnect   bool   `json:"autoConnect"`
	AllowInsecure bool   `json:"allowInsecure"`
	SessionOnly   bool   `json:"sessionOnly"`
}

// PetPreferences 与业务连接状态分离，动画不能更改服务端状态。
type PetPreferences struct {
	SkinID           string  `json:"skinId"`
	Scale            float64 `json:"scale"`
	AlwaysOnTop      bool    `json:"alwaysOnTop"`
	ClickThrough     bool    `json:"clickThrough"`
	Visible          bool    `json:"visible"`
	RecoveryShortcut string  `json:"recoveryShortcut"`
}

// NotificationPreferences 首版仅实现气泡及勿扰，不承诺未实现的声音渠道。
type NotificationPreferences struct {
	Bubble            bool `json:"bubble"`
	QuietHoursEnabled bool `json:"quietHoursEnabled"`
	PickupEnabled     bool `json:"pickupEnabled"` // 仅在用户开启后为拾取日志播放蛋糕动作。
}

// StartupPreferences 由原生桌面服务配置登录启动，默认关闭。
type StartupPreferences struct {
	LaunchOnLogin bool `json:"launchOnLogin"`
}

// Preferences 是可安全持久化和返回前端的用户偏好。
type Preferences struct {
	Pet           PetPreferences          `json:"pet"`
	Notifications NotificationPreferences `json:"notifications"`
	Startup       StartupPreferences      `json:"startup"`
}

// KeywordRule 定义一条日志命中后的动作与台词，多个关键词之间按任一匹配处理。
type KeywordRule struct {
	ID       string   `json:"id"`
	Keywords []string `json:"keywords"`
	Action   string   `json:"action"`
	Text     string   `json:"text"`
	Enabled  bool     `json:"enabled"`
}

// Config 按版本保存资料和偏好，不保存接口原始响应或密钥。
type Config struct {
	SchemaVersion   int           `json:"schemaVersion"`
	ActiveProfileID string        `json:"activeProfileId"`
	Profiles        []Profile     `json:"profiles"`
	Preferences     Preferences   `json:"preferences"`
	KeywordRules    []KeywordRule `json:"keywordRules"`
}

// DefaultConfig 保持无凭据、无自动连接的安全初始状态。
func DefaultConfig() Config {
	return Config{SchemaVersion: 1, Profiles: []Profile{}, Preferences: Preferences{
		Pet:           PetPreferences{SkinID: "abgi-buddy", Scale: 1, AlwaysOnTop: true, Visible: true, RecoveryShortcut: "Ctrl+Shift+P"},
		Notifications: NotificationPreferences{Bubble: true},
	}}
}
