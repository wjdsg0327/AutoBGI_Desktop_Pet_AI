// assetgen 用确定性的几何绘制生成原创桌宠精灵和托盘图标，不依赖外部角色素材。
package main

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"math"
	"os"
	"path/filepath"
)

var ink = color.RGBA{35, 66, 57, 255}
var mint = color.RGBA{163, 207, 171, 255}
var cream = color.RGBA{250, 244, 220, 255}
var orange = color.RGBA{235, 145, 77, 255}

// 椭圆使用二倍分辨率采样，生成的 PNG 具有透明边缘且缩放时不会出现黑底。
func oval(dst *image.RGBA, x, y, rx, ry float64, c color.RGBA) {
	for py := int(y - ry); py <= int(y+ry); py++ {
		for px := int(x - rx); px <= int(x+rx); px++ {
			if math.Pow((float64(px)-x)/rx, 2)+math.Pow((float64(py)-y)/ry, 2) <= 1 {
				dst.SetRGBA(px, py, c)
			}
		}
	}
}

// 线段由圆刷连接，避免引入字体和图像处理工具的额外许可证。
func line(dst *image.RGBA, x, y, x2, y2, width float64, c color.RGBA) {
	n := int(math.Hypot(x2-x, y2-y)) + 1
	for i := 0; i <= n; i++ {
		t := float64(i) / float64(n)
		oval(dst, x+(x2-x)*t, y+(y2-y)*t, width/2, width/2, c)
	}
}

// 每个动作仅改变角色姿态；这些状态绝不反向修改服务端任务状态。
func frame(action string, f int) *image.RGBA {
	im := image.NewRGBA(image.Rect(0, 0, 384, 416))
	bob := math.Sin(float64(f)*math.Pi/2) * 4
	if action == "sleep" {
		bob = 8
	}
	oval(im, 192, 380, 108, 12, color.RGBA{40, 70, 55, 32})
	oval(im, 145, 357+bob, 34, 18, ink)
	oval(im, 239, 357+bob, 34, 18, ink)
	oval(im, 192, 271+bob, 100, 91, ink)
	oval(im, 192, 269+bob, 94, 84, mint)
	oval(im, 192, 296+bob, 59, 54, cream)
	line(im, 192, 92+bob, 206, 55+bob, 9, ink)
	oval(im, 224, 48+bob, 30, 14, mint)
	oval(im, 108, 132+bob, 32, 44, ink)
	oval(im, 276, 132+bob, 32, 44, ink)
	oval(im, 108, 130+bob, 24, 35, mint)
	oval(im, 276, 130+bob, 24, 35, mint)
	oval(im, 192, 171+bob, 103, 91, ink)
	oval(im, 192, 168+bob, 96, 84, cream)
	oval(im, 137, 192+bob, 19, 10, color.RGBA{240, 177, 133, 255})
	oval(im, 247, 192+bob, 19, 10, color.RGBA{240, 177, 133, 255})
	if action == "sleep" || (action == "idle" && f == 3) {
		line(im, 145, 167+bob, 162, 170+bob, 7, ink)
		line(im, 223, 170+bob, 240, 167+bob, 7, ink)
	} else {
		oval(im, 154, 167+bob, 8, 12, ink)
		oval(im, 231, 167+bob, 8, 12, ink)
		oval(im, 156, 163+bob, 2, 3, cream)
		oval(im, 233, 163+bob, 2, 3, cream)
	}
	line(im, 184, 192+bob, 192, 198+bob, 5, ink)
	line(im, 192, 198+bob, 201, 192+bob, 5, ink)
	line(im, 119, 259+bob, 105, 297+bob, 25, ink)
	line(im, 119, 259+bob, 105, 292+bob, 17, mint)
	if action == "wave" || action == "success" {
		line(im, 265, 254+bob, 291+float64(f%2)*14, 204+bob, 25, ink)
		line(im, 265, 254+bob, 291+float64(f%2)*14, 204+bob, 17, mint)
	} else {
		line(im, 265, 257+bob, 278, 294+bob, 25, ink)
		line(im, 265, 257+bob, 278, 290+bob, 17, mint)
	}
	line(im, 141, 245+bob, 242, 322+bob, 10, orange)
	oval(im, 242, 319+bob, 35, 29, ink)
	oval(im, 242, 317+bob, 29, 23, orange)
	line(im, 224, 313+bob, 260, 313+bob, 5, cream)
	if action == "working" {
		line(im, 148, 300+bob, 222, 300+bob, 15, ink)
		line(im, 148, 300+bob, 222, 300+bob, 8, orange)
	}
	if action == "warning" {
		oval(im, 306, 95, 20, 20, orange)
		line(im, 306, 84, 306, 97, 5, ink)
		oval(im, 306, 106, 2, 2, ink)
	}
	if action == "success" {
		for i := 0; i < 3; i++ {
			x := 60 + float64(i)*132
			line(im, x, 70+float64(f)*5, x, 90+float64(f)*5, 4, orange)
			line(im, x-9, 80+float64(f)*5, x+9, 80+float64(f)*5, 4, orange)
		}
	}
	// 四像素平均采样获得可复现的抗锯齿，不从网络获取任何图片。
	out := image.NewRGBA(image.Rect(0, 0, 192, 208))
	for y := 0; y < 208; y++ {
		for x := 0; x < 192; x++ {
			var r, g, b, a uint32
			for dy := 0; dy < 2; dy++ {
				for dx := 0; dx < 2; dx++ {
					p := im.RGBAAt(x*2+dx, y*2+dy)
					r += uint32(p.R)
					g += uint32(p.G)
					b += uint32(p.B)
					a += uint32(p.A)
				}
			}
			out.SetRGBA(x, y, color.RGBA{uint8(r / 4), uint8(g / 4), uint8(b / 4), uint8(a / 4)})
		}
	}
	return out
}

// 主程序同时生成发布素材和前端公开副本，保持二者协议完全相同。
func main() {
	actions := []string{"idle", "wave", "working", "success", "warning", "offline", "sleep", "dragging"}
	atlas := image.NewRGBA(image.Rect(0, 0, 192*4, 208*8))
	defs := map[string]any{}
	for row, action := range actions {
		frames := []int{}
		for f := 0; f < 4; f++ {
			draw.Draw(atlas, image.Rect(f*192, row*208, (f+1)*192, (row+1)*208), frame(action, f), image.Point{}, draw.Src)
			frames = append(frames, row*4+f)
		}
		defs[action] = map[string]any{"frames": frames, "fps": 8, "loop": action != "wave" && action != "success"}
	}
	manifest := map[string]any{"schemaVersion": 1, "id": "abgi-buddy", "name": "叶芽 / ABGI 小助手", "author": "AutoBGI Desktop Pet contributors", "license": "CC0-1.0", "atlas": "atlas.png", "frameWidth": 192, "frameHeight": 208, "columns": 4, "rows": 8, "scale": 1, "anchor": map[string]float64{"x": 0.5, "y": 1}, "actions": defs, "fallbackAction": "idle"}
	data, _ := json.MarshalIndent(manifest, "", "  ")
	var pngData bytes.Buffer
	if err := png.Encode(&pngData, atlas); err != nil {
		panic(err)
	}
	for _, root := range []string{"assets/pets/default", "frontend/public/pets/default"} {
		if err := os.MkdirAll(root, 0755); err != nil {
			panic(err)
		}
		if err := os.WriteFile(filepath.Join(root, "manifest.json"), data, 0644); err != nil {
			panic(err)
		}
		if err := os.WriteFile(filepath.Join(root, "atlas.png"), pngData.Bytes(), 0644); err != nil {
			panic(err)
		}
	}
	// ICO 内嵌 PNG，供 Windows 托盘和可执行文件使用，不复用游戏图标。
	icon := image.NewRGBA(image.Rect(0, 0, 64, 64))
	src := frame("idle", 0)
	for y := 0; y < 64; y++ {
		for x := 0; x < 64; x++ {
			icon.Set(x, y, src.At(x*3, y*3+8))
		}
	}
	var raw bytes.Buffer
	if err := png.Encode(&raw, icon); err != nil {
		panic(err)
	}
	var ico bytes.Buffer
	for _, v := range []uint16{0, 1, 1} {
		_ = binary.Write(&ico, binary.LittleEndian, v)
	}
	ico.Write([]byte{64, 64, 0, 0})
	for _, v := range []uint16{1, 32} {
		_ = binary.Write(&ico, binary.LittleEndian, v)
	}
	_ = binary.Write(&ico, binary.LittleEndian, uint32(raw.Len()))
	_ = binary.Write(&ico, binary.LittleEndian, uint32(22))
	ico.Write(raw.Bytes())
	if err := os.WriteFile("assets/icon.ico", ico.Bytes(), 0644); err != nil {
		panic(err)
	}
}
