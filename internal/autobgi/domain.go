package autobgi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// DomainReader 是独立只读能力边界，旧协议不需要实现或探测任何领域路径。
type DomainReader interface {
	ListScanAccounts(context.Context) (ScanAccountsResult, error)
	ReadEarnings(context.Context, string) (EarningsResult, error)
	ReadBackpack(context.Context, string) (BackpackResult, error)
	ReadCultivation(context.Context, *string) (CultivationPlansResult, error)
}

// RoleDomainReader 独立扩展只读角色上下文，不要求旧服务实现，也不改变运行账户。
type RoleDomainReader interface {
	ListQueryRoles(context.Context) (QueryRolesResult, error)
	ReadRoleEarnings(context.Context, string, string) (EarningsResult, error)
	ReadRoleCultivation(context.Context, string, *string) (CultivationPlansResult, error)
}

// redactJSON 只处理 JSON 值中的字符串，避免替换字段名或把 API 密钥反射给界面。
func (c *Client) redactJSON(value any) any {
	switch typed := value.(type) {
	case string:
		return c.redact(typed)
	case []any:
		for i := range typed {
			typed[i] = c.redactJSON(typed[i])
		}
	case map[string]any:
		for key, item := range typed {
			typed[key] = c.redactJSON(item)
		}
	}
	return value
}

// getDomainData 仍沿用大小限制和固定来源请求，在返回业务 DTO 前统一脱敏所有展示文本。
func (a *PetV1Adapter) getDomainData(ctx context.Context, route string, query url.Values, dest any) error {
	return a.getCheckedDomainData(ctx, route, query, dest, nil)
}

// getCheckedDomainData 在统一脱敏前核对角色身份，身份字符串会被脱敏改变时直接拒绝响应。
func (a *PetV1Adapter) getCheckedDomainData(ctx context.Context, route string, query url.Values, dest any, checkIdentity func() error) error {
	var result envelope
	if err := a.client.requestQuery(ctx, http.MethodGet, route, query, nil, &result); err != nil {
		return err
	}
	if result.Code != "OK" {
		return &APIError{Code: "INVALID_RESPONSE", Message: "领域接口没有返回成功标识"}
	}
	if checkIdentity != nil {
		if json.Unmarshal(result.Data, dest) != nil {
			return &APIError{Code: "INVALID_RESPONSE", Message: "领域数据字段类型不兼容"}
		}
		if err := checkIdentity(); err != nil {
			return err
		}
	}
	var value any
	decoder := json.NewDecoder(strings.NewReader(string(result.Data)))
	decoder.UseNumber()
	if decoder.Decode(&value) != nil {
		return &APIError{Code: "INVALID_RESPONSE", Message: "领域数据格式无效"}
	}
	clean, err := json.Marshal(a.client.redactJSON(value))
	if err != nil || json.Unmarshal(clean, dest) != nil {
		return &APIError{Code: "INVALID_RESPONSE", Message: "领域数据字段类型不兼容"}
	}
	return nil
}

// validScanID 拒绝路径和控制字符，保留空字符串所代表的真实兼容根目录。
func validScanID(id string) bool {
	return len(id) <= 256 && id != "." && id != ".." && !strings.ContainsAny(id, "/\\\r\n\x00")
}

// ListScanAccounts 返回真实扫描目录列表，不能将此选择器用于运行账户切换。
func (a *PetV1Adapter) ListScanAccounts(ctx context.Context) (ScanAccountsResult, error) {
	if !a.caps.ScanAccounts {
		return ScanAccountsResult{}, unsupported()
	}
	var result ScanAccountsResult
	if err := a.getDomainData(ctx, "/api/desktop-pet/v1/scan-accounts", nil, &result); err != nil {
		return result, err
	}
	if result.Scope != "scanner-records" || len(result.Items) > 200 {
		return result, &APIError{Code: "INVALID_RESPONSE", Message: "扫描目录列表范围或数量无效"}
	}
	foundDefault := result.DefaultScanAccountID == nil
	for _, item := range result.Items {
		if !validScanID(item.ScanAccountID) {
			return result, &APIError{Code: "INVALID_RESPONSE", Message: "扫描目录标识无效"}
		}
		if result.DefaultScanAccountID != nil && *result.DefaultScanAccountID == item.ScanAccountID {
			foundDefault = true
		}
	}
	if !foundDefault {
		return result, &APIError{Code: "INVALID_RESPONSE", Message: "默认扫描目录不在真实列表中"}
	}
	return result, nil
}

// ReadEarnings 留空日期时由服务端确定当天，显式日期仍严格核对，未知值保持 null。
func (a *PetV1Adapter) ReadEarnings(ctx context.Context, date string) (EarningsResult, error) {
	if !a.caps.Earnings {
		return EarningsResult{}, unsupported()
	}
	query := url.Values{}
	if date != "" {
		if _, err := time.Parse(time.DateOnly, date); err != nil {
			return EarningsResult{}, &APIError{Code: "INVALID_ARGUMENT", Message: "收益日期无效"}
		}
		query.Set("date", date)
	}
	var result EarningsResult
	if err := a.getDomainData(ctx, "/api/desktop-pet/v1/earnings", query, &result); err != nil {
		return result, err
	}
	// 默认日期可以与客户端不同，但必须是有效日历日期，不能将未知日期当成今天。
	_, dateErr := time.Parse(time.DateOnly, result.Date)
	if result.Scope != "instance-configured-role" || result.QueryRoleID != nil || result.Unit != "mora" || dateErr != nil || (date != "" && result.Date != date) || result.Timezone == "" {
		return result, &APIError{Code: "INVALID_RESPONSE", Message: "收益数据范围、日期或单位不兼容"}
	}
	return result, nil
}

// ReadBackpack 必须显式发送扫描目录，即使兼容根目录也保留 scanAccountId= 参数。
func (a *PetV1Adapter) ReadBackpack(ctx context.Context, scanAccountID string) (BackpackResult, error) {
	if !a.caps.Backpack {
		return BackpackResult{}, unsupported()
	}
	if !validScanID(scanAccountID) {
		return BackpackResult{}, &APIError{Code: "INVALID_ARGUMENT", Message: "扫描目录标识无效"}
	}
	var result BackpackResult
	if err := a.getDomainData(ctx, "/api/desktop-pet/v1/backpack", url.Values{"scanAccountId": {scanAccountID}}, &result); err != nil {
		return result, err
	}
	if result.Scope != "scanner-records" || result.SelectedScanAccountID != scanAccountID || result.Completeness != "recognized-items-only" || len(result.Items) > 20000 {
		return result, &APIError{Code: "INVALID_RESPONSE", Message: "背包数据的扫描目录或完整性说明不匹配"}
	}
	for _, item := range result.Items {
		if item.Count < 0 || item.ID == "" {
			return result, &APIError{Code: "INVALID_RESPONSE", Message: "背包物品数量或标识无效"}
		}
	}
	return result, nil
}

// ReadCultivation 读取持久计划和需求缓存，不计算运行队列、任务完成或切换运行账户。
func (a *PetV1Adapter) ReadCultivation(ctx context.Context, scanAccountID *string) (CultivationPlansResult, error) {
	if !a.caps.CultivationPlans {
		return CultivationPlansResult{}, unsupported()
	}
	query := url.Values{}
	if scanAccountID != nil {
		if !validScanID(*scanAccountID) {
			return CultivationPlansResult{}, &APIError{Code: "INVALID_ARGUMENT", Message: "扫描目录标识无效"}
		}
		query.Set("scanAccountId", *scanAccountID)
	}
	var result CultivationPlansResult
	if err := a.getDomainData(ctx, "/api/desktop-pet/v1/cultivation-plans", query, &result); err != nil {
		return result, err
	}
	if result.Scope != "instance" || result.QueryRoleID != nil || result.QueueAvailable || result.CompletionEventsAvailable || len(result.Plans) > 2000 {
		return result, &APIError{Code: "INVALID_RESPONSE", Message: "培养计划范围或队列语义不兼容"}
	}
	if scanAccountID != nil && (result.SelectedScanAccountID == nil || *result.SelectedScanAccountID != *scanAccountID) {
		return result, &APIError{Code: "INVALID_RESPONSE", Message: "培养计划扫描目录不匹配"}
	}
	return result, nil
}
