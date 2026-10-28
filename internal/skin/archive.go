package skin

import (
	"archive/zip"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// makeStage 通过已固定根目录句柄创建随机暂存目录，避免根目录改名后写入别处。
func makeStage(root *os.Root) (string, error) {
	for attempt := 0; attempt < 8; attempt++ {
		var random [16]byte
		if _, err := rand.Read(random[:]); err != nil {
			return "", errStorage
		}
		stage := ".staging-" + hex.EncodeToString(random[:])
		if err := root.Mkdir(stage, 0700); err == nil {
			return stage, nil
		} else if !errors.Is(err, os.ErrExist) {
			return "", errStorage
		}
	}
	return "", errStorage
}

// fileLimit 只允许三个固定文件名，目录、替代数据流与任意附带文件都无法进入安装阶段。
func fileLimit(name string) int64 {
	switch name {
	case "manifest.json", "LICENSE.txt":
		return maxTextBytes
	case "atlas.png":
		return maxAtlasBytes
	default:
		return 0
	}
}

// readArchive 校验压缩和实际展开体积，成功证明仍属于本次导入的候选缓存。
func (v *packageValidator) readArchive(path string) (packageData, error) {
	before, err := os.Lstat(path)
	if err != nil || !before.Mode().IsRegular() || before.Size() < 1 || before.Size() > maxZIPBytes ||
		!strings.EqualFold(filepath.Ext(path), ".zip") {
		return packageData{}, errPackage
	}
	file, err := os.Open(path)
	if err != nil {
		return packageData{}, errPackage
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !os.SameFile(before, opened) || !opened.Mode().IsRegular() || opened.Size() != before.Size() {
		return packageData{}, errPackage
	}
	reader, err := zip.NewReader(file, opened.Size())
	if err != nil || len(reader.File) != 3 {
		return packageData{}, errPackage
	}
	seen := make(map[string]bool, 3)
	content := make(map[string][]byte, 3)
	var declared, actual uint64
	for _, item := range reader.File {
		name := item.Name
		key := strings.ToLower(name)
		limit := fileLimit(name)
		if limit == 0 || seen[key] || !item.Mode().IsRegular() || item.Flags&1 != 0 ||
			strings.ContainsAny(name, `/\:`) || item.UncompressedSize64 > uint64(limit) ||
			item.UncompressedSize64 > maxTotalBytes-declared {
			return packageData{}, errPackage
		}
		seen[key] = true
		declared += item.UncompressedSize64
		stream, err := item.Open()
		if err != nil {
			return packageData{}, errPackage
		}
		bytes, readErr := io.ReadAll(io.LimitReader(stream, limit+1))
		closeErr := stream.Close()
		if readErr != nil || closeErr != nil || int64(len(bytes)) > limit || uint64(len(bytes)) > maxTotalBytes-actual {
			return packageData{}, errPackage
		}
		actual += uint64(len(bytes))
		content[name] = bytes
	}
	if len(content) != 3 {
		return packageData{}, errPackage
	}
	return v.validatePackage(content["manifest.json"], content["atlas.png"], content["LICENSE.txt"])
}

// writeStageFile 只创建本次暂存目录内尚不存在的固定文件，并确保刷新完成后才允许安装。
func writeStageFile(root *os.Root, stage, name string, content []byte) error {
	file, err := root.OpenFile(stage+"/"+name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	_, writeErr := file.Write(content)
	syncErr := file.Sync()
	closeErr := file.Close()
	return errors.Join(writeErr, syncErr, closeErr)
}

// discardStage 精确清理本次生成的三个文件和空目录，绝不递归删除既有皮肤。
func discardStage(root *os.Root, stage string) {
	for _, name := range []string{"manifest.json", "atlas.png", "LICENSE.txt"} {
		_ = root.Remove(stage + "/" + name)
	}
	_ = root.Remove(stage)
}
