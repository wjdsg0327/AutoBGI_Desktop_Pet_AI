// Package credential 使用当前用户的系统保护保存敏感凭据。
package credential

import "errors"

// Store 不提供枚举和导出，调用方只按自身生成的引用读写。
type Store interface {
	Read(string) (string, error)
	Write(string, string) error
	Delete(string) error
}

// 固定错误不包含系统路径、凭据内容或服务端回包。
var ErrUnavailable = errors.New("密钥无法加密、读取或保存；请检查程序目录权限，或选择仅本次会话使用")
var ErrNotFound = errors.New("尚未保存 API 密钥，请输入后保存连接")
