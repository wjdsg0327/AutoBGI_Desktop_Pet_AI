package storage

import (
	"encoding/binary"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode"
)

const (
	// MaxKeywordSoundBytes 限制用户音频大小，避免选择超大文件拖慢复制和播放。
	MaxKeywordSoundBytes int64 = 10 * 1024 * 1024
	// keywordSoundStoredName 使用固定内部文件名，配置文件不暴露用户原始路径。
	keywordSoundStoredName = "keyword-sound.wav"
)

// KeywordSoundPath 返回当前用户自定义音效的内部存储路径。
func (s *Store) KeywordSoundPath() string {
	return filepath.Join(s.dataDir, keywordSoundStoredName)
}

// SaveKeywordSound 校验并复制用户选中的 WAV，成功后返回仅用于展示的原始文件名。
func (s *Store) SaveKeywordSound(source string) (string, error) {
	before, err := os.Lstat(source)
	if err != nil || !before.Mode().IsRegular() {
		return "", errors.New("请选择普通 WAV 文件，不能使用目录或符号链接")
	}
	if before.Size() <= 0 || before.Size() > MaxKeywordSoundBytes {
		return "", errors.New("自定义音效必须是 10 MiB 以内的 WAV 文件")
	}
	if !strings.EqualFold(filepath.Ext(source), ".wav") {
		return "", errors.New("自定义音效只支持 WAV 格式")
	}
	file, err := os.Open(source)
	if err != nil {
		return "", errors.New("自定义音效无法读取，请检查文件权限")
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !opened.Mode().IsRegular() || !os.SameFile(before, opened) {
		return "", errors.New("选择的自定义音效文件已经变化，请重新选择")
	}
	data, err := io.ReadAll(io.LimitReader(file, MaxKeywordSoundBytes+1))
	if err != nil || int64(len(data)) > MaxKeywordSoundBytes {
		return "", errors.New("自定义音效读取失败或超过 10 MiB")
	}
	// 读取完成后再次核对大小，避免用户在选择器关闭后替换文件内容。
	after, err := file.Stat()
	if err != nil || !after.Mode().IsRegular() || !os.SameFile(before, after) || after.Size() != before.Size() {
		return "", errors.New("选择的自定义音效文件已经变化，请重新选择")
	}
	if err := validateKeywordWAV(data); err != nil {
		return "", err
	}
	if err := atomicWrite(s.KeywordSoundPath(), data); err != nil {
		return "", errors.New("自定义音效保存失败，请检查应用数据目录权限")
	}
	name := filepath.Base(source)
	// 展示名只保留普通字符，避免控制字符污染配置和设置页。
	name = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, name)
	name = strings.TrimSpace(name)
	runes := []rune(name)
	if len(runes) > 120 {
		name = string(runes[:120])
	}
	if name == "." || name == string(filepath.Separator) || name == "" {
		name = keywordSoundStoredName
	}
	return name, nil
}

// RemoveKeywordSound 删除内部音效文件，删除不存在的文件视为成功。
func (s *Store) RemoveKeywordSound() error {
	err := os.Remove(s.KeywordSoundPath())
	if err != nil && !os.IsNotExist(err) {
		return errors.New("旧的自定义音效无法删除，请检查应用数据目录权限")
	}
	return nil
}

// ReadKeywordSound 读取当前内部音效快照，供配置保存失败时恢复原文件。
func (s *Store) ReadKeywordSound() ([]byte, bool) {
	data, err := os.ReadFile(s.KeywordSoundPath())
	if err != nil {
		return nil, false
	}
	return data, true
}

// RestoreKeywordSound 恢复更新自定义音效前的文件快照。
func (s *Store) RestoreKeywordSound(data []byte, exists bool) error {
	if !exists {
		return s.RemoveKeywordSound()
	}
	return atomicWrite(s.KeywordSoundPath(), data)
}

// ValidKeywordSound 检查内部 WAV 是否仍然存在且符合导入时的限制。
func (s *Store) ValidKeywordSound() bool {
	info, err := os.Lstat(s.KeywordSoundPath())
	if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > MaxKeywordSoundBytes {
		return false
	}
	data, err := os.ReadFile(s.KeywordSoundPath())
	return err == nil && validateKeywordWAV(data) == nil
}

// validateKeywordWAV 仅接受未压缩 PCM WAV，限制参数以保证系统播放接口可处理。
func validateKeywordWAV(data []byte) error {
	if len(data) < 44 || string(data[:4]) != "RIFF" || string(data[8:12]) != "WAVE" {
		return errors.New("自定义音效不是有效的 WAV 文件")
	}
	riffSize := binary.LittleEndian.Uint32(data[4:8])
	if riffSize < 4 || uint64(riffSize)+8 > uint64(len(data)) {
		return errors.New("自定义音效 WAV 文件长度声明无效")
	}
	riffEnd := int(uint64(riffSize) + 8)
	var (
		hasFormat bool
		hasData   bool
		channels  uint16
		rate      uint32
		blockSize uint16
		dataSize  uint64
	)
	offset := 12
	for offset < riffEnd {
		if riffEnd-offset < 8 {
			return errors.New("自定义音效 WAV 数据块损坏")
		}
		chunkSize := int(binary.LittleEndian.Uint32(data[offset+4 : offset+8]))
		chunkStart := offset + 8
		if chunkSize < 0 || chunkStart > riffEnd || chunkSize > riffEnd-chunkStart {
			return errors.New("自定义音效 WAV 数据块损坏")
		}
		switch string(data[offset : offset+4]) {
		case "fmt ":
			if chunkSize < 16 {
				return errors.New("自定义音效缺少有效的 WAV 格式信息")
			}
			audioFormat := binary.LittleEndian.Uint16(data[chunkStart : chunkStart+2])
			channels = binary.LittleEndian.Uint16(data[chunkStart+2 : chunkStart+4])
			rate = binary.LittleEndian.Uint32(data[chunkStart+4 : chunkStart+8])
			blockSize = binary.LittleEndian.Uint16(data[chunkStart+12 : chunkStart+14])
			bits := binary.LittleEndian.Uint16(data[chunkStart+14 : chunkStart+16])
			if audioFormat != 1 || channels == 0 || channels > 2 || rate < 8000 || rate > 96000 ||
				(bits != 8 && bits != 16 && bits != 24 && bits != 32) ||
				blockSize == 0 || blockSize != channels*((bits+7)/8) {
				return errors.New("自定义音效需要 8-96 kHz、单/双声道 PCM WAV")
			}
			hasFormat = true
		case "data":
			if chunkSize == 0 {
				return errors.New("自定义音效没有可播放的 WAV 音频数据")
			}
			dataSize += uint64(chunkSize)
			hasData = true
		}
		next := chunkStart + chunkSize
		if chunkSize%2 != 0 {
			next++
		}
		if next <= offset || next > riffEnd {
			return errors.New("自定义音效 WAV 数据块损坏")
		}
		offset = next
	}
	if offset != riffEnd {
		return errors.New("自定义音效 WAV 数据块损坏")
	}
	if !hasFormat || !hasData {
		return errors.New("自定义音效必须包含 PCM 格式和音频数据")
	}
	// 最长约 30 秒，既避免异常文件也让试听和触发保持短促。
	if uint64(dataSize) > uint64(rate)*uint64(blockSize)*30 {
		return errors.New("自定义音效不能超过 30 秒")
	}
	return nil
}
