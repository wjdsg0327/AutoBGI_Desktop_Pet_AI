package skin

import (
	"bytes"
	"crypto/sha256"
	"image/png"
)

// pngValidationCacheLimit 只保留少量成功证明，不随请求数或导入数量持续增长。
const pngValidationCacheLimit = 32

// pngValidation 保存完整内容摘要及已验证尺寸，不持有压缩字节或解码图像。
type pngValidation struct {
	digest        [sha256.Size]byte
	width, height int
}

// pngValidationCache 使用固定容量先进先出替换，缓存值可复制以隔离失败操作。
type pngValidationCache struct {
	entries [pngValidationCacheLimit]pngValidation
	size    int
	next    int
}

// packageValidator 在管理器锁内验证一个候选操作，成功后才提交缓存变化。
type packageValidator struct {
	pngs    pngValidationCache
	decodes *uint64
}

// validationCandidate 不共享其他管理器的验证证明，失败也不会驱逐已有成功项。
func (m *Manager) validationCandidate() packageValidator {
	return packageValidator{pngs: m.pngs, decodes: &m.pngDecodes}
}

// validatePNG 每次计算完整字节摘要，命中仍核对本次描述文件要求的网格尺寸。
func (v *packageValidator) validatePNG(atlas []byte, width, height int) (pngValidation, bool, error) {
	digest := sha256.Sum256(atlas)
	for index := 0; index < v.pngs.size; index++ {
		known := v.pngs.entries[index]
		if known.digest == digest {
			if known.width != width || known.height != height {
				return pngValidation{}, false, errPackage
			}
			return known, true, nil
		}
	}
	// 未命中的内容保留原有静态格式、尺寸和完整解码检查，损坏内容不形成证明。
	if !staticPNG(atlas) {
		return pngValidation{}, false, errPackage
	}
	config, err := png.DecodeConfig(bytes.NewReader(atlas))
	if err != nil || config.Width != width || config.Height != height {
		return pngValidation{}, false, errPackage
	}
	// 计数只在真实整图解码之前递增，用于核对重复请求和并发的分配边界。
	if v.decodes != nil {
		(*v.decodes)++
	}
	decoded, err := png.Decode(bytes.NewReader(atlas))
	if err != nil || decoded.Bounds().Dx() != width || decoded.Bounds().Dy() != height {
		return pngValidation{}, false, errPackage
	}
	return pngValidation{digest: digest, width: width, height: height}, false, nil
}

// rememberPNG 只在完整皮肤包验证成功后调用，固定数组不会保留解码器分配的图像。
func (v *packageValidator) rememberPNG(value pngValidation) {
	v.pngs.entries[v.pngs.next] = value
	v.pngs.next = (v.pngs.next + 1) % pngValidationCacheLimit
	if v.pngs.size < pngValidationCacheLimit {
		v.pngs.size++
	}
}
