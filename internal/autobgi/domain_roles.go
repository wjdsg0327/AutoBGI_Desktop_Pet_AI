package autobgi

import (
	"context"
	"net/url"
	"regexp"
	"time"
)

// queryRoleIDPattern 固定角色缓存标识的组成，拒绝路径、转义、控制字符和不完整 UID。
var queryRoleIDPattern = regexp.MustCompile(`^mys-role:([a-z][a-z0-9_]{1,31}):([1-9][0-9]{8,9})$`)

// queryRoleIdentity 只从已经固定格式的标识解析归属，不根据昵称或全局默认角色猜测。
func queryRoleIdentity(id string) (DomainRole, bool) {
	parts := queryRoleIDPattern.FindStringSubmatch(id)
	if len(parts) != 3 {
		return DomainRole{}, false
	}
	return DomainRole{Region: parts[1], UID: parts[2]}, true
}

// invalidRoleResponse 不携带远端内容，避免错误提示泄露密钥或不可信身份。
func invalidRoleResponse(message string) error {
	return &APIError{Code: "INVALID_RESPONSE", Message: message}
}

// ValidateQueryRoles 同时用于适配器与服务边界，默认标识必须是无歧义的真实列表成员。
func ValidateQueryRoles(result QueryRolesResult) error {
	if result.Scope != "query-roles" || result.Source != "mihoyo-binding-cache" || len(result.Items) > 200 {
		return invalidRoleResponse("查询角色列表范围、来源或数量无效")
	}
	if result.UpdatedAt != nil {
		if _, err := time.Parse(time.RFC3339Nano, *result.UpdatedAt); err != nil {
			return invalidRoleResponse("查询角色缓存时间无效")
		}
	}
	foundDefault := result.DefaultQueryRoleID == nil
	seen := make(map[string]bool, len(result.Items))
	for _, item := range result.Items {
		role, valid := queryRoleIdentity(item.QueryRoleID)
		if !valid || role.UID != item.UID || role.Region != item.Region || seen[item.QueryRoleID] {
			return invalidRoleResponse("查询角色标识、归属或唯一性不匹配")
		}
		seen[item.QueryRoleID] = true
		if result.DefaultQueryRoleID != nil && item.QueryRoleID == *result.DefaultQueryRoleID {
			foundDefault = true
		}
	}
	if !foundDefault {
		return invalidRoleResponse("默认查询角色不在真实列表中")
	}
	return nil
}

// unchangedQueryIdentity 必须在展示文本脱敏前调用，身份被替换时拒绝而不是伪造新绑定。
func (a *PetV1Adapter) unchangedQueryIdentity(id string, role DomainRole) error {
	if a.client.redact(id) != id || a.client.redact(role.UID) != role.UID || a.client.redact(role.Region) != role.Region {
		return invalidRoleResponse("查询角色身份与安全展示规则冲突")
	}
	return nil
}

// ListQueryRoles 只读取服务端已经存在的绑定缓存，不触发远端账号登录或新的业务采集。
func (a *PetV1Adapter) ListQueryRoles(ctx context.Context) (QueryRolesResult, error) {
	if !a.caps.QueryRoles {
		return QueryRolesResult{}, unsupported()
	}
	var result QueryRolesResult
	err := a.getCheckedDomainData(ctx, "/api/desktop-pet/v1/query-roles", nil, &result, func() error {
		if err := ValidateQueryRoles(result); err != nil {
			return err
		}
		for _, item := range result.Items {
			if err := a.unchangedQueryIdentity(item.QueryRoleID, DomainRole{UID: item.UID, Region: item.Region}); err != nil {
				return err
			}
		}
		if result.DefaultQueryRoleID != nil && a.client.redact(*result.DefaultQueryRoleID) != *result.DefaultQueryRoleID {
			return invalidRoleResponse("默认查询角色身份与安全展示规则冲突")
		}
		return nil
	})
	if err != nil {
		return QueryRolesResult{}, err
	}
	if err := ValidateQueryRoles(result); err != nil {
		return QueryRolesResult{}, err
	}
	return result, nil
}

// validateSelectedQueryRole 约束响应范围、选定标识及 currentRole 的 UID 与区域精确一致。
func validateSelectedQueryRole(scope, expectedScope string, selected *string, current *DomainRole, requested string) error {
	role, valid := queryRoleIdentity(requested)
	if !valid || scope != expectedScope || selected == nil || *selected != requested || current == nil || current.UID != role.UID || current.Region != role.Region {
		return invalidRoleResponse("角色数据范围或所选角色身份不匹配")
	}
	return nil
}

// ValidateRoleEarnings 拒绝以默认角色或另一 UID 的数据回答当前角色请求。
func ValidateRoleEarnings(result EarningsResult, queryRoleID, date string) error {
	if err := validateSelectedQueryRole(result.Scope, "query-role", result.QueryRoleID, &result.CurrentRole, queryRoleID); err != nil {
		return err
	}
	_, dateErr := time.Parse(time.DateOnly, result.Date)
	if result.Unit != "mora" || result.Timezone == "" || dateErr != nil || (date != "" && result.Date != date) {
		return invalidRoleResponse("角色收益日期、时区或单位不兼容")
	}
	return nil
}

// ReadRoleEarnings 显式提交查询角色，日期留空时仍由服务端判断其业务当天。
func (a *PetV1Adapter) ReadRoleEarnings(ctx context.Context, queryRoleID, date string) (EarningsResult, error) {
	if !a.caps.QueryRoles || !a.caps.Earnings {
		return EarningsResult{}, unsupported()
	}
	if _, valid := queryRoleIdentity(queryRoleID); !valid {
		return EarningsResult{}, &APIError{Code: "INVALID_QUERY_ROLE", Message: "查询角色标识无效"}
	}
	query := url.Values{"queryRoleId": {queryRoleID}}
	if date != "" {
		if _, err := time.Parse(time.DateOnly, date); err != nil {
			return EarningsResult{}, &APIError{Code: "INVALID_ARGUMENT", Message: "收益日期无效"}
		}
		query.Set("date", date)
	}
	var result EarningsResult
	err := a.getCheckedDomainData(ctx, "/api/desktop-pet/v1/earnings", query, &result, func() error {
		if err := ValidateRoleEarnings(result, queryRoleID, date); err != nil {
			return err
		}
		return a.unchangedQueryIdentity(*result.QueryRoleID, result.CurrentRole)
	})
	if err != nil {
		return EarningsResult{}, err
	}
	if err := ValidateRoleEarnings(result, queryRoleID, date); err != nil {
		return EarningsResult{}, err
	}
	return result, nil
}

// ValidateRoleCultivation 保留实例计划范围；当前协议没有可靠角色扫描绑定，库存与缺口始终未知。
func ValidateRoleCultivation(result CultivationPlansResult, queryRoleID string, scanAccountID *string) error {
	if err := validateSelectedQueryRole(result.Scope, "instance-plans-query-role", result.QueryRoleID, result.CurrentRole, queryRoleID); err != nil {
		return err
	}
	if result.QueueAvailable || result.CompletionEventsAvailable || len(result.Plans) > 2000 {
		return invalidRoleResponse("角色培养计划范围或队列语义不兼容")
	}
	if scanAccountID != nil {
		if result.SelectedScanAccountID == nil || *result.SelectedScanAccountID != *scanAccountID {
			return invalidRoleResponse("角色培养计划扫描目录不匹配")
		}
	} else if result.SelectedScanAccountID != nil {
		return invalidRoleResponse("未选择扫描目录却返回了库存绑定")
	}
	for _, plan := range result.Plans {
		for _, character := range plan.Characters {
			for _, material := range character.Materials {
				if material.Owned != nil || material.Missing != nil {
					return invalidRoleResponse("没有可靠角色扫描绑定，角色培养材料不能包含库存或缺口")
				}
			}
		}
	}
	return nil
}

// ReadRoleCultivation 只查询该角色面板与全实例计划，不保存选择、不影响执行账号或任务队列。
func (a *PetV1Adapter) ReadRoleCultivation(ctx context.Context, queryRoleID string, scanAccountID *string) (CultivationPlansResult, error) {
	if !a.caps.QueryRoles || !a.caps.CultivationPlans {
		return CultivationPlansResult{}, unsupported()
	}
	if _, valid := queryRoleIdentity(queryRoleID); !valid {
		return CultivationPlansResult{}, &APIError{Code: "INVALID_QUERY_ROLE", Message: "查询角色标识无效"}
	}
	query := url.Values{"queryRoleId": {queryRoleID}}
	if scanAccountID != nil {
		if !validScanID(*scanAccountID) {
			return CultivationPlansResult{}, &APIError{Code: "INVALID_ARGUMENT", Message: "扫描目录标识无效"}
		}
		query.Set("scanAccountId", *scanAccountID)
	}
	var result CultivationPlansResult
	err := a.getCheckedDomainData(ctx, "/api/desktop-pet/v1/cultivation-plans", query, &result, func() error {
		if err := ValidateRoleCultivation(result, queryRoleID, scanAccountID); err != nil {
			return err
		}
		return a.unchangedQueryIdentity(*result.QueryRoleID, *result.CurrentRole)
	})
	if err != nil {
		return CultivationPlansResult{}, err
	}
	if err := ValidateRoleCultivation(result, queryRoleID, scanAccountID); err != nil {
		return CultivationPlansResult{}, err
	}
	return result, nil
}
