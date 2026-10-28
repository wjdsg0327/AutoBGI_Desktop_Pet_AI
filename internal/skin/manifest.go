// Package skin 管理经过严格校验的本地精灵皮肤，不提供脚本、远程资源或任意文件访问。
package skin

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"math"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	builtInID       = "abgi-buddy"
	assetPrefix     = "/__pet/skins/"
	maxPackages     = 64
	maxRootEntries  = 1024
	maxZIPBytes     = 8 << 20
	maxTotalBytes   = 12 << 20
	maxAtlasBytes   = 10 << 20
	maxTextBytes    = 64 << 10
	maxPixels       = 8_000_000
	maxDimension    = 8192
	maxPNGChunks    = 4096
	maxActionCount  = 32
	maxActionFrames = 512
)

var (
	idPattern     = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)
	actionPattern = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,31}$`)
	errPackage    = errors.New("皮肤包格式或资源不符合安全限制")
	errStorage    = errors.New("本地皮肤目录不可用，请检查当前用户目录权限")
	errMissing    = errors.New("该皮肤资源已损坏或不可用，请选择内置角色")
)

// Info 仅向界面返回展示元数据与固定同源地址，不泄露安装目录或用户选择的文件路径。
type Info struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Author      string `json:"author"`
	License     string `json:"license"`
	ManifestURL string `json:"manifestUrl"`
	BuiltIn     bool   `json:"builtIn"`
}

// manifest 使用明确字段和指针区分缺省项，禁止把缺失的循环布尔值误当成有效动作。
type manifest struct {
	SchemaVersion  int               `json:"schemaVersion"`
	ID             string            `json:"id"`
	Name           string            `json:"name"`
	Author         string            `json:"author"`
	License        string            `json:"license"`
	Atlas          string            `json:"atlas"`
	FrameWidth     int               `json:"frameWidth"`
	FrameHeight    int               `json:"frameHeight"`
	Columns        int               `json:"columns"`
	Rows           int               `json:"rows"`
	Scale          *float64          `json:"scale"`
	Anchor         *anchor           `json:"anchor"`
	Actions        map[string]action `json:"actions"`
	FallbackAction string            `json:"fallbackAction"`
}

// anchor 的坐标始终限定在当前帧内，避免外部皮肤将绘制原点移出合理范围。
type anchor struct {
	X *float64 `json:"x"`
	Y *float64 `json:"y"`
}

// action 只描述帧序列与播放速度，不含可执行表达式或脚本入口。
type action struct {
	Frames []int   `json:"frames"`
	FPS    float64 `json:"fps"`
	Loop   *bool   `json:"loop"`
}

// packageData 将全部校验结果保留在有界内存中，成功前不修改正式皮肤目录。
type packageData struct {
	info     Info
	manifest []byte
	atlas    []byte
	license  []byte
}

// validID 同时排除 Windows 设备名和内置保留标识，保持跨平台目录语义一致。
func validID(id string) bool {
	if id == builtInID || len(id) == 0 || len(id) > 64 || !idPattern.MatchString(id) {
		return false
	}
	switch id {
	case "con", "prn", "aux", "nul":
		return false
	}
	return !(len(id) == 4 && (strings.HasPrefix(id, "com") || strings.HasPrefix(id, "lpt")) && id[3] >= '0' && id[3] <= '9')
}

// validText 验证有界纯文本元数据，控制字符不能进入皮肤名称、作者或许可标签。
func validText(text string, limit int) bool {
	if !utf8.ValidString(text) || strings.TrimSpace(text) == "" || utf8.RuneCountInString(text) > limit {
		return false
	}
	for _, value := range text {
		if unicode.IsControl(value) {
			return false
		}
	}
	return true
}

// finiteRange 拒绝非数值与无穷值，导入参数不会绕过前端的有限绘制边界。
func finiteRange(value, minimum, maximum float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0) && value >= minimum && value <= maximum
}

// uniqueJSONValue 拒绝重复键与异常嵌套，避免不同解析器对同一描述文件产生不同解释。
func uniqueJSONValue(decoder *json.Decoder, depth int) bool {
	if depth > 8 {
		return false
	}
	token, err := decoder.Token()
	if err != nil {
		return false
	}
	delim, compound := token.(json.Delim)
	if !compound {
		return true
	}
	if delim == '{' {
		seen := make(map[string]bool)
		for decoder.More() {
			key, err := decoder.Token()
			name, text := key.(string)
			if err != nil || !text || seen[name] {
				return false
			}
			seen[name] = true
			if !uniqueJSONValue(decoder, depth+1) {
				return false
			}
		}
		end, err := decoder.Token()
		return err == nil && end == json.Delim('}')
	}
	if delim == '[' {
		for decoder.More() {
			if !uniqueJSONValue(decoder, depth+1) {
				return false
			}
		}
		end, err := decoder.Token()
		return err == nil && end == json.Delim(']')
	}
	return false
}

// allowedJSONFields 补足标准解码器的大小写宽松规则，只接受协议声明的精确字段名。
func allowedJSONFields(raw []byte, allowed map[string]bool) (map[string]json.RawMessage, bool) {
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil || fields == nil {
		return nil, false
	}
	for name := range fields {
		if !allowed[name] {
			return nil, false
		}
	}
	return fields, true
}

// exactManifestFields 校验嵌套动作与锚点的字段白名单，显式 null 也不冒充可省略默认值。
func exactManifestFields(raw []byte) bool {
	fields, valid := allowedJSONFields(raw, map[string]bool{
		"schemaVersion": true, "id": true, "name": true, "author": true, "license": true,
		"atlas": true, "frameWidth": true, "frameHeight": true, "columns": true, "rows": true,
		"scale": true, "anchor": true, "actions": true, "fallbackAction": true,
	})
	if !valid {
		return false
	}
	for _, name := range []string{"scale", "anchor", "fallbackAction"} {
		if value, exists := fields[name]; exists && bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return false
		}
	}
	if value, exists := fields["anchor"]; exists {
		if _, valid := allowedJSONFields(value, map[string]bool{"x": true, "y": true}); !valid {
			return false
		}
	}
	var actions map[string]json.RawMessage
	if json.Unmarshal(fields["actions"], &actions) != nil || actions == nil || len(actions) > maxActionCount {
		return false
	}
	for _, value := range actions {
		if _, valid := allowedJSONFields(value, map[string]bool{"frames": true, "fps": true, "loop": true}); !valid {
			return false
		}
	}
	return true
}

// validatePackage 每次严格解析描述文件，仅复用同一完整 PNG 内容的成功解码证明。
func (v *packageValidator) validatePackage(rawManifest, atlas, license []byte) (packageData, error) {
	if len(rawManifest) == 0 || len(rawManifest) > maxTextBytes || !utf8.Valid(rawManifest) ||
		len(atlas) == 0 || len(atlas) > maxAtlasBytes || len(license) == 0 || len(license) > maxTextBytes ||
		!utf8.Valid(license) || bytes.IndexByte(license, 0) >= 0 || strings.TrimSpace(string(license)) == "" {
		return packageData{}, errPackage
	}
	keyDecoder := json.NewDecoder(bytes.NewReader(rawManifest))
	keyDecoder.UseNumber()
	if !uniqueJSONValue(keyDecoder, 0) || !exactManifestFields(rawManifest) {
		return packageData{}, errPackage
	}
	var data manifest
	decoder := json.NewDecoder(bytes.NewReader(rawManifest))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&data) != nil {
		return packageData{}, errPackage
	}
	var trailing any
	if decoder.Decode(&trailing) != io.EOF || data.SchemaVersion != 1 || !validID(data.ID) ||
		!validText(data.Name, 128) || !validText(data.Author, 128) || !validText(data.License, 256) || data.Atlas != "atlas.png" {
		return packageData{}, errPackage
	}
	// 先限定因子再相乘，恶意超大整数不能通过乘法溢出绕过像素上限。
	if data.FrameWidth < 1 || data.FrameWidth > maxDimension || data.FrameHeight < 1 || data.FrameHeight > maxDimension ||
		data.Columns < 1 || data.Columns > maxDimension || data.Rows < 1 || data.Rows > maxDimension {
		return packageData{}, errPackage
	}
	width := int64(data.FrameWidth) * int64(data.Columns)
	height := int64(data.FrameHeight) * int64(data.Rows)
	if width > maxDimension || height > maxDimension || width*height > maxPixels {
		return packageData{}, errPackage
	}
	if data.Scale == nil {
		value := 1.0
		data.Scale = &value
	}
	if !finiteRange(*data.Scale, 0.25, 2) {
		return packageData{}, errPackage
	}
	if data.Anchor == nil {
		x, y := 0.5, 1.0
		data.Anchor = &anchor{X: &x, Y: &y}
	}
	if data.Anchor.X == nil || data.Anchor.Y == nil || !finiteRange(*data.Anchor.X, 0, 1) || !finiteRange(*data.Anchor.Y, 0, 1) {
		return packageData{}, errPackage
	}
	if len(data.Actions) == 0 || len(data.Actions) > maxActionCount {
		return packageData{}, errPackage
	}
	totalFrames := data.Columns * data.Rows
	for name, animation := range data.Actions {
		if !actionPattern.MatchString(name) || len(animation.Frames) == 0 || len(animation.Frames) > maxActionFrames ||
			animation.Loop == nil || !finiteRange(animation.FPS, 1, 30) {
			return packageData{}, errPackage
		}
		for _, frame := range animation.Frames {
			if frame < 0 || frame >= totalFrames {
				return packageData{}, errPackage
			}
		}
	}
	idle, exists := data.Actions["idle"]
	if !exists || !*idle.Loop || data.FallbackAction != "" && data.FallbackAction != "idle" {
		return packageData{}, errPackage
	}
	data.FallbackAction = "idle"
	proof, cached, err := v.validatePNG(atlas, int(width), int(height))
	if err != nil {
		return packageData{}, errPackage
	}
	normalized, err := json.Marshal(data)
	if err != nil || len(normalized) > maxTextBytes {
		return packageData{}, errPackage
	}
	if !cached {
		v.rememberPNG(proof)
	}
	return packageData{
		info: Info{ID: data.ID, Name: data.Name, Author: data.Author, License: data.License,
			ManifestURL: assetPrefix + data.ID + "/manifest.json"},
		manifest: normalized, atlas: atlas, license: license,
	}, nil
}

// staticPNG 拒绝 APNG 与尾随数据，浏览器不会绕过精灵帧协议播放隐藏的多帧图像。
func staticPNG(data []byte) bool {
	if len(data) < 8 || !bytes.Equal(data[:8], []byte{137, 80, 78, 71, 13, 10, 26, 10}) {
		return false
	}
	offset := 8
	for chunks := 0; chunks < maxPNGChunks && offset+12 <= len(data); chunks++ {
		size := uint64(binary.BigEndian.Uint32(data[offset : offset+4]))
		end := uint64(offset) + 12 + size
		if end > uint64(len(data)) {
			return false
		}
		kind := string(data[offset+4 : offset+8])
		if kind == "acTL" || kind == "fcTL" || kind == "fdAT" {
			return false
		}
		if kind == "IEND" {
			return size == 0 && end == uint64(len(data))
		}
		offset = int(end)
	}
	return false
}
