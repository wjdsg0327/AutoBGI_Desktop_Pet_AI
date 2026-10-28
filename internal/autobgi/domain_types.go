package autobgi

// DomainRole 表示只读数据归属的游戏角色，不是可切换执行账户。
type DomainRole struct {
	UID      string `json:"uid"`
	Region   string `json:"region"`
	Nickname string `json:"nickname"`
}

// QueryRole 来自米游社绑定缓存，标识仅用于领域查询，不参与实例任务或命令账户。
type QueryRole struct {
	QueryRoleID string `json:"queryRoleId"`
	UID         string `json:"uid"`
	Region      string `json:"region"`
	Nickname    string `json:"nickname"`
}

// QueryRolesResult 的默认值必须对应真实列表，缓存时间不代表当前 Cookie 已获授权。
type QueryRolesResult struct {
	Scope              string      `json:"scope"`
	Source             string      `json:"source"`
	UpdatedAt          *string     `json:"updatedAt"`
	DefaultQueryRoleID *string     `json:"defaultQueryRoleId"`
	Items              []QueryRole `json:"items"`
	Warnings           []string    `json:"warnings"`
}

// ScanAccount 的 ID 来自背包扫描记录目录，空字符串明确表示兼容根目录。
type ScanAccount struct {
	ScanAccountID string `json:"scanAccountId"`
	Label         string `json:"label"`
}

// ScanAccountsResult 将扫描目录选择器与实例执行身份分开。
type ScanAccountsResult struct {
	Scope                string        `json:"scope"`
	Items                []ScanAccount `json:"items"`
	DefaultScanAccountID *string       `json:"defaultScanAccountId"`
}

// BackpackItem 的稳定 ID 只用于列表展示，不冒充游戏官方材料 ID。
type BackpackItem struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Category string `json:"category"`
	Count    int64  `json:"count"`
}

// BackpackResult 只代表一次扫描识别出的物品，缺席条目不能自动推断为零。
type BackpackResult struct {
	Scope                 string         `json:"scope"`
	SelectedScanAccountID string         `json:"selectedScanAccountId"`
	ScannedAt             *string        `json:"scannedAt"`
	Timezone              string         `json:"timezone"`
	Version               *string        `json:"version"`
	Available             bool           `json:"available"`
	Completeness          string         `json:"completeness"`
	Items                 []BackpackItem `json:"items"`
	Warnings              []string       `json:"warnings"`
}

// EarningsResult 复用摩拉收益总账；没有记录时接口返回不可用，不能制造零收益。
type EarningsResult struct {
	Scope       string     `json:"scope"`
	QueryRoleID *string    `json:"queryRoleId"`
	CurrentRole DomainRole `json:"currentRole"`
	Date        string     `json:"date"`
	Timezone    string     `json:"timezone"`
	ResetTime   *string    `json:"resetTime"`
	Available   bool       `json:"available"`
	Unit        string     `json:"unit"`
	Total       *int64     `json:"total"`
	Elite       *int64     `json:"elite"`
	Small       *int64     `json:"small"`
	Chest       *int64     `json:"chest"`
	Source      string     `json:"source"`
	UpdatedAt   *string    `json:"updatedAt"`
	Warnings    []string   `json:"warnings"`
}

// CultivationMaterial 仅在扫描明确识别数量时填写已有和缺口，未知材料保留 null。
type CultivationMaterial struct {
	ID       int    `json:"id"`
	Name     string `json:"name"`
	Icon     string `json:"icon,omitempty"`
	Required int64  `json:"required"`
	Owned    *int64 `json:"owned"`
	Missing  *int64 `json:"missing"`
}

// CultivationCharacter 读取已有面板和精确匹配的培养需求缓存，不触发网络更新。
type CultivationCharacter struct {
	Name                  string                `json:"name"`
	Available             bool                  `json:"available"`
	Level                 *int                  `json:"level"`
	Talents               *[3]int               `json:"talents"`
	PanelUpdatedAt        *string               `json:"panelUpdatedAt"`
	TargetLevel           *int                  `json:"targetLevel"`
	TargetTalents         *[3]int               `json:"targetTalents"`
	TargetSource          string                `json:"targetSource"`
	RequirementsAvailable bool                  `json:"requirementsAvailable"`
	Materials             []CultivationMaterial `json:"materials"`
	Warning               string                `json:"warning"`
}

// CultivationPlan 是正式配置计划的只读视图，不将 ID 顺序冒充运行中的 FIFO 队列。
type CultivationPlan struct {
	ID            string                 `json:"id"`
	OneLong       string                 `json:"oneLong"`
	RankedGroup   string                 `json:"rankedGroup"`
	IsRanked      bool                   `json:"isRanked"`
	IsTalent      bool                   `json:"isTalent"`
	DefaultTalent string                 `json:"defaultTalent"`
	Status        string                 `json:"status"`
	UpdatedAt     *string                `json:"updatedAt"`
	Characters    []CultivationCharacter `json:"characters"`
}

// CultivationPlansResult 明确报告队列和完成事件缺失，避免库存齐全被显示成执行成功。
type CultivationPlansResult struct {
	Scope                     string            `json:"scope"`
	QueryRoleID               *string           `json:"queryRoleId"`
	CurrentRole               *DomainRole       `json:"currentRole"`
	SelectedScanAccountID     *string           `json:"selectedScanAccountId"`
	ScannedAt                 *string           `json:"scannedAt"`
	QueueAvailable            bool              `json:"queueAvailable"`
	CompletionEventsAvailable bool              `json:"completionEventsAvailable"`
	Plans                     []CultivationPlan `json:"plans"`
	Warnings                  []string          `json:"warnings"`
}
