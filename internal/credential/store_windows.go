//go:build windows

package credential

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

// windowsStore 在程序目录保存当前 Windows 用户可解密的 DPAPI 密文。
type windowsStore struct{ directory string }

// New 使用真实可执行文件目录，不依赖启动时的工作目录。
func New() Store {
	executable, err := os.Executable()
	if err != nil {
		return windowsStore{}
	}
	return windowsStore{directory: filepath.Dir(executable)}
}

// path 用引用的哈希生成文件名，避免引用被解释为路径。
func (s windowsStore) path(ref string) (string, error) {
	if s.directory == "" || !strings.HasPrefix(ref, "AutoBGI-Pet/profile/") || len(ref) > 256 {
		return "", ErrUnavailable
	}
	sum := sha256.Sum256([]byte(ref))
	return filepath.Join(s.directory, "credential-"+hex.EncodeToString(sum[:])+".bin"), nil
}

// protect 使用引用作为附加熵，防止不同连接之间互换密文。
func protect(data []byte, ref string, decrypt bool) ([]byte, error) {
	if len(data) == 0 || len(ref) == 0 {
		return nil, ErrUnavailable
	}
	entropy := []byte(ref)
	input := windows.DataBlob{Size: uint32(len(data)), Data: &data[0]}
	extra := windows.DataBlob{Size: uint32(len(entropy)), Data: &entropy[0]}
	var output windows.DataBlob
	var err error
	if decrypt {
		err = windows.CryptUnprotectData(&input, nil, &extra, 0, nil, windows.CRYPTPROTECT_UI_FORBIDDEN, &output)
	} else {
		err = windows.CryptProtectData(&input, nil, &extra, 0, nil, windows.CRYPTPROTECT_UI_FORBIDDEN, &output)
	}
	runtime.KeepAlive(data)
	runtime.KeepAlive(entropy)
	if err != nil || output.Data == nil || output.Size == 0 {
		return nil, ErrUnavailable
	}
	defer windows.LocalFree(windows.Handle(unsafe.Pointer(output.Data)))
	return append([]byte(nil), unsafe.Slice(output.Data, int(output.Size))...), nil
}

// Write 先生成密文，再通过同目录临时文件原子替换，避免中途损坏旧密钥。
func (s windowsStore) Write(ref, key string) error {
	path, err := s.path(ref)
	if err != nil || len(key) == 0 || len(key) > 2560 {
		return ErrUnavailable
	}
	encrypted, err := protect([]byte(key), ref, false)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(s.directory, ".credential-*.tmp")
	if err != nil {
		return ErrUnavailable
	}
	temporary := f.Name()
	defer os.Remove(temporary)
	if _, err = f.Write(encrypted); err != nil {
		_ = f.Close()
		return ErrUnavailable
	}
	if err = f.Sync(); err != nil {
		_ = f.Close()
		return ErrUnavailable
	}
	if err = f.Close(); err != nil {
		return ErrUnavailable
	}
	if err = os.Rename(temporary, path); err != nil {
		return ErrUnavailable
	}
	return nil
}

// Read 限制密文体积，并仅通过当前用户 DPAPI 解密。
func (s windowsStore) Read(ref string) (string, error) {
	path, err := s.path(ref)
	if err != nil {
		return "", err
	}
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", ErrUnavailable
	}
	defer f.Close()
	encrypted, err := io.ReadAll(io.LimitReader(f, 65537))
	if err != nil || len(encrypted) == 0 || len(encrypted) > 65536 {
		return "", ErrUnavailable
	}
	plain, err := protect(encrypted, ref, true)
	if err != nil || len(plain) == 0 || len(plain) > 2560 {
		return "", ErrUnavailable
	}
	return string(plain), nil
}

// Delete 只删除该连接对应的密文文件，文件不存在时视作成功。
func (s windowsStore) Delete(ref string) error {
	path, err := s.path(ref)
	if err != nil {
		return err
	}
	if err = os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return ErrUnavailable
	}
	return nil
}
