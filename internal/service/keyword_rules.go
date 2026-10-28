package service

import (
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"autobgi-pet/internal/profile"
)

// validateKeywordRules 同时检查保存输入与旧配置，避免无效规则进入匹配循环。
func validateKeywordRules(rules []profile.KeywordRule) ([]profile.KeywordRule, error) {
	if len(rules) > 40 {
		return nil, errors.New("最多保存 40 条关键词规则")
	}
	result := make([]profile.KeywordRule, 0, len(rules))
	ids := make(map[string]bool, len(rules))
	for i, input := range rules {
		rule := input
		rule.ID = strings.TrimSpace(rule.ID)
		if rule.ID == "" {
			rule.ID = randomID()
		}
		if len(rule.ID) > 128 || strings.ContainsAny(rule.ID, "\x00\r\n") || ids[rule.ID] {
			return nil, fmt.Errorf("第 %d 条规则的标识无效或重复", i+1)
		}
		ids[rule.ID] = true
		switch rule.Action {
		case "idle", "happy", "wave", "surprised", "sad":
		default:
			return nil, fmt.Errorf("第 %d 条规则的动作无效", i+1)
		}
		// 设置页使用单行台词；拒绝换行，避免原生气泡高度不足时静默截断内容。
		if !utf8.ValidString(rule.Text) || utf8.RuneCountInString(rule.Text) > 300 || strings.ContainsAny(rule.Text, "\x00\r\n") {
			return nil, fmt.Errorf("第 %d 条规则的台词须为单行，且不能超过 300 个字符", i+1)
		}
		if len(rule.Keywords) > 20 {
			return nil, fmt.Errorf("第 %d 条规则最多填写 20 个关键词", i+1)
		}
		// 同一规则重复关键词不改变行为，保存时按首次输入顺序去重。
		keywords := make([]string, 0, len(rule.Keywords))
		seen := make(map[string]bool, len(rule.Keywords))
		for _, keyword := range rule.Keywords {
			keyword = strings.TrimSpace(keyword)
			if keyword == "" {
				continue
			}
			if !utf8.ValidString(keyword) || utf8.RuneCountInString(keyword) > 80 || strings.ContainsAny(keyword, "\x00\r\n") {
				return nil, fmt.Errorf("第 %d 条规则的关键词不能超过 80 个字符", i+1)
			}
			folded := strings.ToLower(keyword)
			if !seen[folded] {
				seen[folded] = true
				keywords = append(keywords, keyword)
			}
		}
		if len(keywords) == 0 {
			return nil, fmt.Errorf("第 %d 条规则至少填写一个关键词", i+1)
		}
		rule.Keywords = keywords
		result = append(result, rule)
	}
	return result, nil
}

// ValidateKeywordRules 供分享文件复用保存规则的完整校验，不修改已保存的配置。
func ValidateKeywordRules(rules []profile.KeywordRule) ([]profile.KeywordRule, error) {
	return validateKeywordRules(rules)
}

// UpdateKeywordRules 将完整规则列表原子落盘，失败时继续使用原列表。
func (s *Service) UpdateKeywordRules(input []profile.KeywordRule) (AppState, error) {
	rules, err := validateKeywordRules(input)
	if err != nil {
		return s.GetState(), err
	}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return s.GetState(), errors.New("应用正在退出")
	}
	previous := s.config.KeywordRules
	s.config.KeywordRules = rules
	if err := s.store.Save(s.config); err != nil {
		s.config.KeywordRules = previous
		s.mu.Unlock()
		return s.GetState(), err
	}
	s.mu.Unlock()
	s.publish()
	return s.GetState(), nil
}

// matchKeyword 按规则顺序查找首个命中项，避免一行日志反复触发多个动作。
func matchKeyword(rules []profile.KeywordRule, line string) (*profile.KeywordRule, string) {
	folded := strings.ToLower(line)
	for i := range rules {
		if !rules[i].Enabled {
			continue
		}
		for _, keyword := range rules[i].Keywords {
			if strings.Contains(folded, strings.ToLower(keyword)) {
				return &rules[i], keyword
			}
		}
	}
	return nil, ""
}
