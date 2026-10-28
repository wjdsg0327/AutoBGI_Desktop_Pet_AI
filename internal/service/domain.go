package service

import (
	"errors"
	"time"

	"autobgi-pet/internal/autobgi"
)

// DomainSummary 的领域块各自保留源时间和未知值；顶层身份用于拒绝切换连接后的迟到结果。
type DomainSummary struct {
	ProfileID        string                          `json:"profileId"`
	Generation       uint64                          `json:"generation"`
	InstanceID       string                          `json:"instanceId"`
	FetchedAt        string                          `json:"fetchedAt"`
	SourceTime       *string                         `json:"sourceTime"`
	Stale            bool                            `json:"stale"`
	QueryRoles       *autobgi.QueryRolesResult       `json:"queryRoles"`
	QueryRoleID      *string                         `json:"queryRoleId"`
	ScanAccounts     *autobgi.ScanAccountsResult     `json:"scanAccounts"`
	Earnings         *autobgi.EarningsResult         `json:"earnings"`
	Backpack         *autobgi.BackpackResult         `json:"backpack"`
	CultivationPlans *autobgi.CultivationPlansResult `json:"cultivationPlans"`
	Errors           map[string]string               `json:"errors"`
}

// ReadDomain 保留旧单参绑定；新服务使用明确默认查询角色，旧服务保持原有实例读取语义。
func (s *Service) ReadDomain(scanAccountID string) (DomainSummary, error) {
	return s.readDomain(scanAccountID, "")
}

// ReadRoleDomain 的角色与扫描目录仅存在于这次手动查询，不修改快照、全局账号或连接资料。
func (s *Service) ReadRoleDomain(scanAccountID, queryRoleID string) (DomainSummary, error) {
	return s.readDomain(scanAccountID, queryRoleID)
}

// readDomain 共用唯一业务连接并在结束时复核身份，不向状态广播保存角色列表或查询选择。
func (s *Service) readDomain(scanAccountID, queryRoleID string) (DomainSummary, error) {
	s.mu.Lock()
	if s.closed || s.paused || s.state.Connection.Status != "online" || s.adapter == nil || s.connectionCtx == nil || s.state.Capabilities == nil || s.state.Capabilities.InstanceID == nil {
		s.mu.Unlock()
		return DomainSummary{}, errors.New("请先连接提供领域数据的当前实例")
	}
	reader, ok := s.adapter.(autobgi.DomainReader)
	if !ok {
		s.mu.Unlock()
		return DomainSummary{}, errors.New("当前适配器不支持结构化领域数据，请使用 AutoBGI 网页")
	}
	caps := cloneManualCapabilities(*s.state.Capabilities)
	if !caps.QueryRoles && queryRoleID != "" {
		s.mu.Unlock()
		return DomainSummary{}, &autobgi.APIError{Code: "UNSUPPORTED_CAPABILITY", Message: "服务端未声明只读查询角色能力"}
	}
	roleReader, supportsRoles := s.adapter.(autobgi.RoleDomainReader)
	ticket, ctx, release, err := s.beginManualReadLocked(35 * time.Second)
	if err != nil {
		s.mu.Unlock()
		return DomainSummary{}, err
	}
	p, gen, instanceID := ticket.profile, ticket.generation, *caps.InstanceID
	s.mu.Unlock()
	defer release()
	result := DomainSummary{ProfileID: p.ID, Generation: gen, InstanceID: instanceID, Errors: map[string]string{}}
	var roleError error
	if caps.QueryRoles {
		if !supportsRoles {
			roleError = errors.New("当前适配器不支持服务端声明的只读查询角色")
		} else {
			roles, err := roleReader.ListQueryRoles(ctx)
			if err == nil {
				err = autobgi.ValidateQueryRoles(roles)
			}
			if err != nil {
				roleError = err
			} else {
				result.QueryRoles = &roles
				result.QueryRoleID, roleError = selectQueryRole(roles, queryRoleID)
			}
		}
		if roleError != nil {
			// 列表失败或显式角色不存在时，绝不退回服务端当前默认角色读取角色领域块。
			result.Errors["queryRoles"] = roleError.Error()
		}
	}
	// 前一块等待期间若发生暂停或超时，后续领域不得继续发起查询。
	if ctx.Err() != nil {
		return DomainSummary{}, errors.New("领域读取已取消或超时，请重新刷新")
	}
	var selected *string
	if caps.ScanAccounts {
		accounts, err := reader.ListScanAccounts(ctx)
		if err != nil {
			result.Errors["scanAccounts"] = err.Error()
		} else {
			result.ScanAccounts = &accounts
			// 空选择优先匹配真实根目录；不存在根目录时使用服务端明确给出的默认目录。
			for _, account := range accounts.Items {
				if account.ScanAccountID == scanAccountID {
					id := scanAccountID
					selected = &id
					break
				}
			}
			if selected == nil && scanAccountID == "" && accounts.DefaultScanAccountID != nil {
				id := *accounts.DefaultScanAccountID
				selected = &id
			}
			if selected == nil && scanAccountID != "" {
				result.Errors["backpack"] = "所选扫描目录已不存在，请重新选择"
			}
		}
	} else {
		result.Errors["scanAccounts"] = "服务端未声明扫描目录列表能力"
	}
	if ctx.Err() != nil {
		return DomainSummary{}, errors.New("领域读取已取消或超时，请重新刷新")
	}
	if caps.Earnings {
		// 当天归属由服务端时区决定，不能用桌宠电脑的本地日期替代服务器日期。
		var earnings autobgi.EarningsResult
		var err error
		if roleError != nil {
			err = errors.New("查询角色不可用，未读取该角色收益")
		} else if caps.QueryRoles {
			earnings, err = roleReader.ReadRoleEarnings(ctx, *result.QueryRoleID, "")
			if err == nil {
				err = autobgi.ValidateRoleEarnings(earnings, *result.QueryRoleID, "")
			}
		} else {
			earnings, err = reader.ReadEarnings(ctx, "")
		}
		if err != nil {
			result.Errors["earnings"] = err.Error()
		} else {
			result.Earnings = &earnings
		}
	} else {
		result.Errors["earnings"] = "服务端未声明结构化收益能力"
	}
	if ctx.Err() != nil {
		return DomainSummary{}, errors.New("领域读取已取消或超时，请重新刷新")
	}
	if caps.Backpack && selected != nil {
		backpack, err := reader.ReadBackpack(ctx, *selected)
		if err != nil {
			result.Errors["backpack"] = err.Error()
		} else {
			result.Backpack = &backpack
		}
	} else if _, exists := result.Errors["backpack"]; !exists {
		if !caps.Backpack {
			result.Errors["backpack"] = "服务端未声明背包数据能力"
		} else {
			result.Errors["backpack"] = "当前没有可明确选择的扫描记录"
		}
	}
	if ctx.Err() != nil {
		return DomainSummary{}, errors.New("领域读取已取消或超时，请重新刷新")
	}
	if caps.CultivationPlans {
		var plans autobgi.CultivationPlansResult
		var err error
		if roleError != nil {
			err = errors.New("查询角色不可用，未读取该角色培养数据")
		} else if caps.QueryRoles {
			plans, err = roleReader.ReadRoleCultivation(ctx, *result.QueryRoleID, selected)
			if err == nil {
				err = autobgi.ValidateRoleCultivation(plans, *result.QueryRoleID, selected)
			}
		} else {
			plans, err = reader.ReadCultivation(ctx, selected)
		}
		if err != nil {
			result.Errors["cultivationPlans"] = err.Error()
		} else {
			result.CultivationPlans = &plans
		}
	} else {
		result.Errors["cultivationPlans"] = "服务端未声明培养计划能力"
	}
	result.FetchedAt = time.Now().Format(time.RFC3339Nano)
	// 多个领域没有统一源时刻，顶层 sourceTime 保持未知，各块使用自己的源时间字段。
	s.mu.Lock()
	current := s.manualReadCurrentLocked(ticket, ctx)
	s.mu.Unlock()
	if !current {
		result.Stale = true
		return DomainSummary{}, errors.New("领域读取期间连接或实例已变化，请重新刷新")
	}
	return result, nil
}

// selectQueryRole 只接受已校验列表的明确默认值或成员，不使用昵称、列表顺序或全局账号兜底。
func selectQueryRole(roles autobgi.QueryRolesResult, requested string) (*string, error) {
	if requested == "" {
		if roles.DefaultQueryRoleID == nil {
			return nil, errors.New("服务端没有明确默认查询角色，请选择角色后重试")
		}
		requested = *roles.DefaultQueryRoleID
	}
	for _, role := range roles.Items {
		if role.QueryRoleID == requested {
			selected := requested
			return &selected, nil
		}
	}
	return nil, errors.New("所选查询角色已不在真实列表中，请重新选择")
}
