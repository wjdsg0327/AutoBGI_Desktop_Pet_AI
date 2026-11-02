// Package profile 定义不含明文凭据的本地连接资料和偏好。
package profile

// Profile 只保存系统凭据引用；密钥始终保存在操作系统或会话内存中。
type Profile struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// 昵称只用于区分连接的展示，不参与连接身份或动作目标。
	Nickname           string  `json:"nickname,omitempty"`
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
	Nickname      string `json:"nickname"`
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
	ID   string `json:"id"`
	Name string `json:"name"`
	// 空字符串清除昵称；缺失或 null 保留旧客户端已有的昵称。
	Nickname      *string `json:"nickname"`
	BaseURL       string  `json:"baseUrl"`
	APIKey        string  `json:"apiKey"`
	AdapterMode   string  `json:"adapterMode"`
	ReadOnly      bool    `json:"readOnly"`
	AutoConnect   bool    `json:"autoConnect"`
	AllowInsecure bool    `json:"allowInsecure"`
	SessionOnly   bool    `json:"sessionOnly"`
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

// 关键词音效类型兼容旧配置，并映射到内置芙宁娜 MP3 或用户自定义 WAV。
const (
	// KeywordSoundTypeNone 表示当前规则关闭音效。
	KeywordSoundTypeNone     = "none"
	DefaultKeywordSoundType  = "info"
	KeywordSoundTypeOK       = "ok"
	KeywordSoundTypeInfo     = "info"
	KeywordSoundTypeQuestion = "question"
	KeywordSoundTypeWarning  = "warning"
	KeywordSoundTypeError    = "error"
	// KeywordSoundTypeCustom 表示用户复制到应用数据目录的 WAV 音效。
	KeywordSoundTypeCustom = "custom"
)

// ValidKeywordSoundType 只接受关闭、内置语音和自定义 WAV 类型。
func ValidKeywordSoundType(value string) bool {
	switch value {
	case KeywordSoundTypeNone, KeywordSoundTypeOK, KeywordSoundTypeInfo, KeywordSoundTypeQuestion, KeywordSoundTypeWarning, KeywordSoundTypeError, KeywordSoundTypeCustom:
		return true
	default:
		return false
	}
}

// NormalizeKeywordSoundType 兼容旧配置或手工修改配置时回退到安静陪伴语音。
func NormalizeKeywordSoundType(value string) string {
	if ValidKeywordSoundType(value) {
		return value
	}
	return DefaultKeywordSoundType
}

// NotificationPreferences 保存桌宠气泡、勿扰、旧版全局音效兼容字段和拾取通知开关。
type NotificationPreferences struct {
	Bubble            bool `json:"bubble"`
	QuietHoursEnabled bool `json:"quietHoursEnabled"`
	// KeywordSoundEnabled 仅用于迁移旧配置，当前规则触发使用 KeywordRule.SoundEnabled。
	KeywordSoundEnabled bool `json:"keywordSoundEnabled"`
	// KeywordSoundType 仅用于迁移旧配置和兼容旧绑定，当前规则保存自己的音效类型。
	KeywordSoundType string `json:"keywordSoundType"`
	// KeywordSoundCustomName 仅保存用户选中音效的文件名，不保存外部绝对路径。
	KeywordSoundCustomName string `json:"keywordSoundCustomName,omitempty"`
	PickupEnabled          bool   `json:"pickupEnabled"` // 仅在用户开启后为拾取日志播放蛋糕动作。
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

// KeywordTodo 定义手动或自动处理的固定动作，目标名称不包含连接资料或密钥。
type KeywordTodo struct {
	Enabled     bool   `json:"enabled"`
	Action      string `json:"action"`
	Target      string `json:"target"`
	AutoApprove bool   `json:"autoApprove,omitempty"`
}

// KeywordRule 定义一条日志命中后的动作、台词、独立音效和可选代办。
type KeywordRule struct {
	ID       string   `json:"id"`
	Keywords []string `json:"keywords"`
	Action   string   `json:"action"`
	Text     string   `json:"text"`
	Enabled  bool     `json:"enabled"`
	// SoundEnabled 只控制当前规则，不受其他规则的音效设置影响。
	SoundEnabled bool `json:"soundEnabled,omitempty"`
	// SoundType 选择当前规则的内置 MP3 或应用中已导入的自定义 WAV 素材。
	SoundType string `json:"soundType,omitempty"`
	// SoundCustomName 仅保存当前规则展示用的自定义文件名，不保存绝对路径。
	SoundCustomName string       `json:"soundCustomName,omitempty"`
	Todo            *KeywordTodo `json:"todo,omitempty"`
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
		Notifications: NotificationPreferences{Bubble: true, KeywordSoundType: DefaultKeywordSoundType},
	}}
}
