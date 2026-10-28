package service

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"autobgi-pet/internal/autobgi"
	"autobgi-pet/internal/profile"
)

// MaxProfileTransferBytes 将手动资料交换限制在 64 KiB，不接受配置或历史数据库备份。
const MaxProfileTransferBytes = 64 * 1024

// profileTransferDocument 仅包含可迁移的非敏感资料，不能附带本机身份或凭据引用。
type profileTransferDocument struct {
	SchemaVersion int                    `json:"schemaVersion"`
	Profiles      []profileTransferEntry `json:"profiles"`
}

// profileTransferEntry 的权限字段只用于兼容旧导出，导入始终重新采用安全默认值。
type profileTransferEntry struct {
	Name          string `json:"name"`
	BaseURL       string `json:"baseUrl"`
	AdapterMode   string `json:"adapterMode"`
	ReadOnly      bool   `json:"readOnly"`
	AllowInsecure bool   `json:"allowInsecure"`
	AutoConnect   bool   `json:"autoConnect"`
}

// transferObject 精确匹配字段大小写并拒绝重复键，避免普通 JSON 解码的后值覆盖语义。
func transferObject(data []byte, allowed map[string]bool) (map[string]json.RawMessage, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return nil, errors.New("连接资料必须使用规定的 JSON 对象格式")
	}
	fields := make(map[string]json.RawMessage, len(allowed))
	for decoder.More() {
		token, err = decoder.Token()
		name, ok := token.(string)
		if err != nil || !ok || !allowed[name] {
			return nil, errors.New("连接资料包含不支持的字段，不能导入密钥、凭据引用或本机配置")
		}
		if _, duplicate := fields[name]; duplicate {
			return nil, errors.New("连接资料包含重复字段")
		}
		var value json.RawMessage
		if err = decoder.Decode(&value); err != nil {
			return nil, errors.New("连接资料 JSON 格式无效")
		}
		fields[name] = value
	}
	if token, err = decoder.Token(); err != nil || token != json.Delim('}') {
		return nil, errors.New("连接资料 JSON 对象未完整结束")
	}
	if decoder.Decode(new(any)) != io.EOF {
		return nil, errors.New("连接资料只能包含一份 JSON 文档")
	}
	return fields, nil
}

// parseProfileTransfer 在任何持久化前验证整批资料，不向错误消息回显文件里的敏感文本。
func parseProfileTransfer(text string) ([]profileTransferEntry, error) {
	if len(text) == 0 || len(text) > MaxProfileTransferBytes || !utf8.ValidString(text) {
		return nil, errors.New("请选择有效 UTF-8 JSON 文件，大小不能超过 64 KiB")
	}
	data := []byte(text)
	root, err := transferObject(data, map[string]bool{"schemaVersion": true, "profiles": true})
	if err != nil {
		return nil, err
	}
	var schema int
	if json.Unmarshal(root["schemaVersion"], &schema) != nil || schema != 1 {
		return nil, errors.New("不支持此连接资料版本，仅支持 schemaVersion 1")
	}
	var entries []json.RawMessage
	if json.Unmarshal(root["profiles"], &entries) != nil || len(entries) == 0 || len(entries) > 20 {
		return nil, errors.New("一次需要导入 1 至 20 个连接")
	}
	result := make([]profileTransferEntry, 0, len(entries))
	allowed := map[string]bool{"name": true, "baseUrl": true, "adapterMode": true, "readOnly": true, "allowInsecure": true, "autoConnect": true}
	for index, raw := range entries {
		fields, parseErr := transferObject(raw, allowed)
		if parseErr != nil {
			return nil, fmt.Errorf("第 %d 个连接：%s", index+1, parseErr)
		}
		// 必填文本和可选布尔值逐项解码，null 或类型混淆不能静默变成零值。
		var entry profileTransferEntry
		for name, destination := range map[string]*string{"name": &entry.Name, "baseUrl": &entry.BaseURL, "adapterMode": &entry.AdapterMode} {
			value, exists := fields[name]
			if !exists || bytes.Equal(bytes.TrimSpace(value), []byte("null")) || json.Unmarshal(value, destination) != nil {
				return nil, fmt.Errorf("第 %d 个连接的名称、地址或适配器模式缺失或格式无效", index+1)
			}
		}
		for name, destination := range map[string]*bool{"readOnly": &entry.ReadOnly, "allowInsecure": &entry.AllowInsecure, "autoConnect": &entry.AutoConnect} {
			if value, exists := fields[name]; exists {
				if bytes.Equal(bytes.TrimSpace(value), []byte("null")) || json.Unmarshal(value, destination) != nil {
					return nil, fmt.Errorf("第 %d 个连接的选项必须是布尔值", index+1)
				}
			}
		}
		if err = validateTransferredProfile(&entry); err != nil {
			return nil, fmt.Errorf("第 %d 个连接：%s", index+1, err)
		}
		result = append(result, entry)
	}
	return result, nil
}

// validateTransferredProfile 沿用固定来源地址规则，并拒绝控制字符、超长文本和非法端口。
func validateTransferredProfile(entry *profileTransferEntry) error {
	entry.Name = strings.TrimSpace(entry.Name)
	if entry.Name == "" || utf8.RuneCountInString(entry.Name) > 80 || strings.IndexFunc(entry.Name, unicode.IsControl) >= 0 {
		return errors.New("连接名称需要 1 至 80 个字符，且不能包含控制字符")
	}
	if entry.AdapterMode != "auto" && entry.AdapterMode != "legacy" && entry.AdapterMode != "pet-v1" {
		return errors.New("适配器模式只能是 auto、legacy 或 pet-v1")
	}
	if len(entry.BaseURL) > 2048 || strings.IndexFunc(entry.BaseURL, unicode.IsControl) >= 0 {
		return errors.New("服务地址超过长度限制或包含控制字符")
	}
	parsed, err := url.Parse(strings.TrimSpace(entry.BaseURL))
	if err != nil || parsed == nil || strings.IndexFunc(parsed.Path, unicode.IsControl) >= 0 || strings.HasSuffix(parsed.Host, ":") {
		return errors.New("服务地址格式无效")
	}
	if port := parsed.Port(); port != "" {
		value, portErr := strconv.Atoi(port)
		if portErr != nil || value < 1 || value > 65535 {
			return errors.New("服务端口需要在 1 至 65535 之间")
		}
	}
	base, err := autobgi.NormalizeURL(entry.BaseURL, entry.AllowInsecure)
	if err != nil {
		return err
	}
	entry.BaseURL = base
	return nil
}

// ImportProfiles 仅追加全批校验通过的连接，不访问凭据、网络或现有活动连接。
func (s *Service) ImportProfiles(text string) (AppState, error) {
	entries, err := parseProfileTransfer(text)
	if err != nil {
		return s.GetState(), err
	}
	s.mu.Lock()
	if s.closed || s.store == nil {
		s.mu.Unlock()
		return s.GetState(), errors.New("应用正在退出或配置存储尚未就绪")
	}
	if len(s.config.Profiles)+len(entries) > 20 {
		s.mu.Unlock()
		return s.GetState(), errors.New("导入后连接总数不能超过 20，请先删除不需要的连接")
	}
	// 在独立副本中生成本机身份；提交失败时内存、当前连接和原配置均保持原值。
	next := s.config
	next.Profiles = append([]profile.Profile(nil), s.config.Profiles...)
	used := make(map[string]bool, len(next.Profiles)+len(entries))
	for _, existing := range next.Profiles {
		used[existing.ID] = true
	}
	for _, entry := range entries {
		var id string
		for attempt := 0; attempt < 8; attempt++ {
			var random [16]byte
			if _, err = rand.Read(random[:]); err != nil {
				s.mu.Unlock()
				return s.GetState(), errors.New("无法生成连接身份，未导入任何连接")
			}
			id = hex.EncodeToString(random[:])
			if !used[id] {
				break
			}
			id = ""
		}
		if id == "" {
			s.mu.Unlock()
			return s.GetState(), errors.New("无法分配唯一连接身份，未导入任何连接")
		}
		used[id] = true
		// 会话标记使无密钥资料重启后仍可展示，且不会把新引用误报为已有系统凭据。
		next.Profiles = append(next.Profiles, profile.Profile{ID: id, Name: entry.Name, BaseURL: entry.BaseURL, CredentialRef: "AutoBGI-Pet/profile/" + id, AdapterMode: entry.AdapterMode, ReadOnly: true, AutoConnect: false, AllowInsecure: entry.AllowInsecure, SessionOnly: true})
	}
	if err = s.store.Save(next); err != nil {
		s.mu.Unlock()
		return s.GetState(), err
	}
	s.config = next
	s.mu.Unlock()
	s.publish()
	return s.GetState(), nil
}
