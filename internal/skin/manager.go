package skin

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
)

// Manager 将全部本地读写串行化；调用方只通过白名单宿主绑定使用本类型。
type Manager struct {
	mu         sync.Mutex
	root       string
	pngs       pngValidationCache
	pngDecodes uint64
}

// builtInInfo 始终提供随程序打包的内置角色，磁盘导入失败不会移除该恢复入口。
func builtInInfo() Info {
	return Info{
		ID: builtInID, Name: "叶芽 / ABGI 小助手", Author: "AutoBGI Desktop Pet contributors",
		License: "CC0-1.0", ManifestURL: "/pets/default/manifest.json", BuiltIn: true,
	}
}

// New 将皮肤安装在当前用户配置目录，不要求管理员权限，也不读取 AutoBGI 配置。
func New() (*Manager, error) {
	directory, err := os.UserConfigDir()
	if err != nil {
		return nil, errStorage
	}
	return NewAt(filepath.Join(directory, "AutoBGI-Pet", "skins"))
}

// NewAt 供宿主和隔离测试指定皮肤根目录，本函数不会注册为前端绑定。
func NewAt(root string) (*Manager, error) {
	if strings.TrimSpace(root) == "" {
		return nil, errStorage
	}
	absolute, err := filepath.Abs(root)
	if err != nil || !safeExistingParents(absolute) || os.MkdirAll(absolute, 0700) != nil {
		return nil, errStorage
	}
	manager := &Manager{root: filepath.Clean(absolute)}
	safeRoot, err := manager.openRoot()
	if err != nil {
		return nil, errStorage
	}
	if safeRoot.Close() != nil {
		return nil, errStorage
	}
	return manager, nil
}

// safeExistingParents 在创建目录之前拒绝已存在的父级别名，失败初始化也不在联接目标中创建目录。
func safeExistingParents(path string) bool {
	for current := filepath.Clean(path); ; current = filepath.Dir(current) {
		info, err := os.Lstat(current)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return false
		}
		if err == nil && (!info.IsDir() || info.Mode()&os.ModeSymlink != 0) {
			return false
		}
		if parent := filepath.Dir(current); parent == current {
			return true
		}
	}
}

// openRoot 拒绝根目录及父级别名，后续 os.Root 句柄同时阻止运行时路径逃逸。
func (m *Manager) openRoot() (*os.Root, error) {
	info, err := os.Lstat(m.root)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, errStorage
	}
	resolved, err := filepath.EvalSymlinks(m.root)
	if err != nil {
		return nil, errStorage
	}
	same := filepath.Clean(resolved) == m.root
	if runtime.GOOS == "windows" {
		same = strings.EqualFold(filepath.Clean(resolved), m.root)
	}
	if !same {
		return nil, errStorage
	}
	root, err := os.OpenRoot(m.root)
	if err != nil {
		return nil, errStorage
	}
	opened, err := root.Stat(".")
	if err != nil || !os.SameFile(info, opened) {
		_ = root.Close()
		return nil, errStorage
	}
	return root, nil
}

// rootEntries 对磁盘目录也施加上限，异常目录不会让列表扫描无限分配内存。
func rootEntries(root *os.Root) ([]os.DirEntry, error) {
	directory, err := root.Open(".")
	if err != nil {
		return nil, errStorage
	}
	defer directory.Close()
	entries, err := directory.ReadDir(maxRootEntries + 1)
	if err != nil && err != io.EOF || len(entries) > maxRootEntries {
		return nil, errStorage
	}
	sort.Slice(entries, func(left, right int) bool { return entries[left].Name() < entries[right].Name() })
	return entries, nil
}

// readInstalledFile 对每次读取重新核对文件类型、身份和实际长度，符号链接不会被当作资源。
func readInstalledFile(root *os.Root, name string) ([]byte, error) {
	limit := fileLimit(name)
	before, err := root.Lstat(name)
	if err != nil || limit == 0 || !before.Mode().IsRegular() || before.Size() < 1 || before.Size() > limit {
		return nil, errMissing
	}
	file, err := root.Open(name)
	if err != nil {
		return nil, errMissing
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !opened.Mode().IsRegular() || !os.SameFile(before, opened) || before.Size() != opened.Size() {
		return nil, errMissing
	}
	content, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil || int64(len(content)) > limit {
		return nil, errMissing
	}
	return content, nil
}

// loadInstalled 只读取固定三个文件，命中完整内容证明前仍核对路径、文件类型及全部实际字节。
func (v *packageValidator) loadInstalled(root *os.Root, id string) (packageData, error) {
	if !validID(id) {
		return packageData{}, errMissing
	}
	info, err := root.Lstat(id)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return packageData{}, errMissing
	}
	directory, err := root.OpenRoot(id)
	if err != nil {
		return packageData{}, errMissing
	}
	defer directory.Close()
	files, err := rootEntries(directory)
	if err != nil || len(files) != 3 {
		return packageData{}, errMissing
	}
	for _, entry := range files {
		if fileLimit(entry.Name()) == 0 || !entry.Type().IsRegular() {
			return packageData{}, errMissing
		}
	}
	rawManifest, manifestErr := readInstalledFile(directory, "manifest.json")
	atlas, atlasErr := readInstalledFile(directory, "atlas.png")
	license, licenseErr := readInstalledFile(directory, "LICENSE.txt")
	if errors.Join(manifestErr, atlasErr, licenseErr) != nil {
		return packageData{}, errMissing
	}
	data, err := v.validatePackage(rawManifest, atlas, license)
	if err != nil || data.info.ID != id {
		return packageData{}, errMissing
	}
	return data, nil
}

// loadInstalled 仅在包身份也通过后提交候选证明，失败不污染管理器已有缓存。
func (m *Manager) loadInstalled(root *os.Root, id string) (packageData, error) {
	candidate := m.validationCandidate()
	data, err := candidate.loadInstalled(root, id)
	if err == nil {
		m.pngs = candidate.pngs
	}
	return data, err
}

// List 在损坏目录存在时仍返回内置及其余有效皮肤，并用简短提示说明忽略范围。
func (m *Manager) List() ([]Info, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := []Info{builtInInfo()}
	root, err := m.openRoot()
	if err != nil {
		return result, errStorage
	}
	defer root.Close()
	entries, err := rootEntries(root)
	if err != nil {
		return result, errStorage
	}
	invalid := false
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".staging-") {
			continue
		}
		if !validID(entry.Name()) || !entry.IsDir() || len(result) > maxPackages {
			invalid = true
			continue
		}
		data, err := m.loadInstalled(root, entry.Name())
		if err != nil {
			invalid = true
			continue
		}
		result = append(result, data.info)
	}
	if invalid {
		return result, errors.New("部分本地皮肤损坏或超出数量限制，已忽略")
	}
	return result, nil
}

// Has 不仅判断名称存在，还验证实际安装内容；内置角色始终可以选择。
func (m *Manager) Has(id string) error {
	if id == builtInID {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	root, err := m.openRoot()
	if err != nil {
		return errStorage
	}
	defer root.Close()
	_, err = m.loadInstalled(root, id)
	return err
}

// Import 仅接收宿主文件对话框已经选择的 ZIP；完整验证后在同目录执行原子改名。
func (m *Manager) Import(path string) (Info, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	candidate := m.validationCandidate()
	data, err := candidate.readArchive(path)
	if err != nil {
		return Info{}, err
	}
	root, err := m.openRoot()
	if err != nil {
		return Info{}, errStorage
	}
	defer root.Close()
	entries, err := rootEntries(root)
	if err != nil {
		return Info{}, errStorage
	}
	count := 0
	for _, entry := range entries {
		if validID(entry.Name()) && entry.IsDir() {
			count++
		}
	}
	if count >= maxPackages {
		return Info{}, errors.New("本地最多保存 64 套导入皮肤")
	}
	if _, err := root.Lstat(data.info.ID); !errors.Is(err, os.ErrNotExist) {
		return Info{}, errors.New("该皮肤 ID 已存在，请使用不同 ID，不能覆盖已安装皮肤")
	}
	stage, err := makeStage(root)
	if err != nil {
		return Info{}, errStorage
	}
	defer discardStage(root, stage)
	for _, item := range []struct {
		name string
		body []byte
	}{{"manifest.json", data.manifest}, {"atlas.png", data.atlas}, {"LICENSE.txt", data.license}} {
		if err := writeStageFile(root, stage, item.name, item.body); err != nil {
			return Info{}, errStorage
		}
	}
	// 暂存内容也走运行时的真实校验，确认写入完整后才让正式目录对列表可见。
	stageRoot, err := root.OpenRoot(stage)
	if err != nil {
		return Info{}, errStorage
	}
	var stagedManifest, stagedAtlas, stagedLicense []byte
	stagedManifest, err = readInstalledFile(stageRoot, "manifest.json")
	if err == nil {
		stagedAtlas, err = readInstalledFile(stageRoot, "atlas.png")
	}
	if err == nil {
		stagedLicense, err = readInstalledFile(stageRoot, "LICENSE.txt")
	}
	closeErr := stageRoot.Close()
	if err != nil || closeErr != nil {
		return Info{}, errStorage
	}
	if _, err := candidate.validatePackage(stagedManifest, stagedAtlas, stagedLicense); err != nil {
		return Info{}, errStorage
	}
	if _, err := root.Lstat(data.info.ID); !errors.Is(err, os.ErrNotExist) {
		return Info{}, errors.New("该皮肤 ID 已存在，请使用不同 ID，不能覆盖已安装皮肤")
	}
	if err := root.Rename(stage, data.info.ID); err != nil {
		return Info{}, errStorage
	}
	m.pngs = candidate.pngs
	return data.info, nil
}
