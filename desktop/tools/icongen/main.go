// icongen 生成本项目的应用图标: 一个访问域名分成两条线路落到不同目标。
//
// 图形全部用有符号距离场 (SDF) 画, 因此每个尺寸都是按该分辨率重新光栅化的, 不是把大图缩下来 ——
// 16×16 的托盘图标不会糊成一团。抗锯齿直接由距离换算成覆盖率, 不需要超采样。
//
// 用法 (在 desktop 目录下):
//
//	go run ./tools/icongen
package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"path/filepath"
)

// 画布坐标一律用 0..1 的归一化值, 与具体像素尺寸无关。
const (
	bgInset  = 0.035 // 圆角矩形四周留白
	bgRadius = 0.225 // 圆角半径, 与 Windows / macOS 的图标观感对齐
	stroke   = 0.055 // 线条半径
	nodeR    = 0.100 // 端点圆半径
	hubX     = 0.215 // 入口节点
	forkX    = 0.485 // 分叉点
	leafX    = 0.785 // 两个落点
	leafUpY  = 0.245
	leafDnY  = 0.755
	midY     = 0.500
)

var (
	colBG1    = rgba{0.094, 0.137, 0.235, 1} // 背景渐变: 上
	colBG2    = rgba{0.035, 0.058, 0.110, 1} // 背景渐变: 下
	colTrunk  = rgba{0.918, 0.945, 0.984, 1} // 入口与主干: 近白
	colCF     = rgba{0.965, 0.510, 0.122, 1} // Cloudflare 橙
	colDirect = rgba{0.298, 0.761, 1.000, 1} // 另一条线路: 天蓝
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "生成图标失败:", err)
		os.Exit(1)
	}
}

func run() error {
	// Wails 拿 appicon.png 做 macOS 图标与窗口图标, Windows 的 exe 与安装包认 icon.ico
	if err := writePNG(filepath.Join("build", "appicon.png"), render(1024)); err != nil {
		return err
	}
	// 16..128 走 DIB, 256 走 PNG: NSIS 与老 Windows 对小尺寸的 PNG 压缩条目并不总是买账
	ico := []*image.NRGBA{render(16), render(32), render(48), render(64), render(128), render(256)}
	if err := writeICO(filepath.Join("build", "windows", "icon.ico"), ico); err != nil {
		return err
	}
	// 前端 dev / 浏览器标签页用的那份, 顺手保持一致
	favicon := []*image.NRGBA{render(16), render(32), render(48)}
	return writeICO(filepath.Join("..", "web", "app", "favicon.ico"), favicon)
}

// render 按给定边长画一张图标。
func render(size int) *image.NRGBA {
	c := newCanvas(size)

	// 背景: 圆角矩形 + 竖向渐变
	c.fill(
		sdfRoundRect(0.5, 0.5, 0.5-bgInset, 0.5-bgInset, bgRadius),
		func(_, y float64) rgba { return mix(colBG1, colBG2, smoothstep(0.05, 0.95, y)) },
	)

	// 线条先画, 端点后画 —— 端点要盖住线头, 顺序反了会看到线从圆里穿出来
	w := c.strokeRadius(stroke)
	c.solid(sdfCapsule(hubX, midY, forkX, midY, w), colTrunk)
	c.solid(sdfCapsule(forkX, midY, leafX, leafUpY, w), colCF)
	c.solid(sdfCapsule(forkX, midY, leafX, leafDnY, w), colDirect)

	r := c.strokeRadius(nodeR)
	c.solid(sdfCircle(hubX, midY, r), colTrunk)
	c.solid(sdfCircle(leafX, leafUpY, r), colCF)
	c.solid(sdfCircle(leafX, leafDnY, r), colDirect)

	return c.img
}

// rgba 是直通 (非预乘) 的浮点颜色, 0..1。
type rgba struct{ r, g, b, a float64 }

type canvas struct {
	size int
	img  *image.NRGBA
}

func newCanvas(size int) *canvas {
	return &canvas{size: size, img: image.NewNRGBA(image.Rect(0, 0, size, size))}
}

// strokeRadius 保证线条在小尺寸下不会细到看不见: 至少占住一个像素多一点。
func (c *canvas) strokeRadius(r float64) float64 {
	return math.Max(r, 0.75/float64(c.size))
}

// solid 用单色填充一个形状。
func (c *canvas) solid(sdf func(x, y float64) float64, col rgba) {
	c.fill(sdf, func(_, _ float64) rgba { return col })
}

// fill 按形状的有符号距离算覆盖率再混色。
// 距离换算成像素单位后, 0.5-d 就是这个像素被覆盖的比例, 边缘天然是抗锯齿的。
func (c *canvas) fill(sdf func(x, y float64) float64, colorAt func(x, y float64) rgba) {
	n := float64(c.size)
	for py := 0; py < c.size; py++ {
		for px := 0; px < c.size; px++ {
			x := (float64(px) + 0.5) / n
			y := (float64(py) + 0.5) / n
			cover := clamp01(0.5 - sdf(x, y)*n)
			if cover <= 0 {
				continue
			}
			col := colorAt(x, y)
			c.blend(px, py, col, cover*col.a)
		}
	}
}

// blend 把一层颜色按 alpha 叠到画布上 (source-over)。
func (c *canvas) blend(px, py int, col rgba, alpha float64) {
	dst := c.img.NRGBAAt(px, py)
	da := float64(dst.A) / 255
	out := alpha + da*(1-alpha)
	if out <= 0 {
		c.img.SetNRGBA(px, py, color.NRGBA{})
		return
	}
	ch := func(s float64, d uint8) uint8 {
		v := (s*alpha + float64(d)/255*da*(1-alpha)) / out
		return uint8(clamp01(v)*255 + 0.5)
	}
	c.img.SetNRGBA(px, py, color.NRGBA{
		R: ch(col.r, dst.R),
		G: ch(col.g, dst.G),
		B: ch(col.b, dst.B),
		A: uint8(clamp01(out)*255 + 0.5),
	})
}

// sdfRoundRect 圆角矩形的有符号距离。
func sdfRoundRect(cx, cy, hw, hh, r float64) func(x, y float64) float64 {
	return func(x, y float64) float64 {
		dx := math.Abs(x-cx) - (hw - r)
		dy := math.Abs(y-cy) - (hh - r)
		outside := math.Hypot(math.Max(dx, 0), math.Max(dy, 0))
		return outside + math.Min(math.Max(dx, dy), 0) - r
	}
}

// sdfCircle 圆的有符号距离。
func sdfCircle(cx, cy, r float64) func(x, y float64) float64 {
	return func(x, y float64) float64 { return math.Hypot(x-cx, y-cy) - r }
}

// sdfCapsule 带圆头的线段 (点到线段的距离减去半径), 两条线在分叉点自然接成圆角。
func sdfCapsule(x1, y1, x2, y2, r float64) func(x, y float64) float64 {
	dx, dy := x2-x1, y2-y1
	length2 := dx*dx + dy*dy
	return func(x, y float64) float64 {
		t := 0.0
		if length2 > 0 {
			t = clamp01(((x-x1)*dx + (y-y1)*dy) / length2)
		}
		return math.Hypot(x-(x1+t*dx), y-(y1+t*dy)) - r
	}
}

func mix(a, b rgba, t float64) rgba {
	return rgba{
		r: a.r + (b.r-a.r)*t,
		g: a.g + (b.g-a.g)*t,
		b: a.b + (b.b-a.b)*t,
		a: a.a + (b.a-a.a)*t,
	}
}

func smoothstep(edge0, edge1, x float64) float64 {
	t := clamp01((x - edge0) / (edge1 - edge0))
	return t * t * (3 - 2*t)
}

func clamp01(v float64) float64 { return math.Min(math.Max(v, 0), 1) }

// writePNG 写出一张 PNG。
func writePNG(path string, img *image.NRGBA) error {
	buf := new(bytes.Buffer)
	if err := png.Encode(buf, img); err != nil {
		return err
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		return fmt.Errorf("写 %s 失败: %w", path, err)
	}
	fmt.Printf("已生成 %s (%dpx)\n", path, img.Bounds().Dx())
	return nil
}

// writeICO 把多个尺寸打包成一个 .ico。
// 256 用 PNG 压缩条目, 更小的尺寸用 DIB —— 部分打包器 (NSIS) 对小尺寸的 PNG 条目并不总是买账。
func writeICO(path string, imgs []*image.NRGBA) error {
	buf := new(bytes.Buffer)
	if err := binary.Write(buf, binary.LittleEndian, [3]uint16{0, 1, uint16(len(imgs))}); err != nil {
		return err
	}

	payloads := make([][]byte, len(imgs))
	offset := 6 + 16*len(imgs)
	for i, img := range imgs {
		size := img.Bounds().Dx()
		if size >= 256 {
			encoded := new(bytes.Buffer)
			if err := png.Encode(encoded, img); err != nil {
				return err
			}
			payloads[i] = encoded.Bytes()
		} else {
			payloads[i] = dibBytes(img)
		}

		// 宽高各占一字节, 256 记为 0
		dim := byte(size)
		if size >= 256 {
			dim = 0
		}
		entry := []any{
			dim, dim, byte(0), byte(0),
			uint16(1), uint16(32),
			uint32(len(payloads[i])), uint32(offset),
		}
		for _, v := range entry {
			if err := binary.Write(buf, binary.LittleEndian, v); err != nil {
				return err
			}
		}
		offset += len(payloads[i])
	}
	for _, p := range payloads {
		buf.Write(p)
	}

	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		return fmt.Errorf("写 %s 失败: %w", path, err)
	}
	fmt.Printf("已生成 %s (%d 个尺寸)\n", path, len(imgs))
	return nil
}

// dibBytes 把一张图编码成 ICO 里的 DIB 条目: 信息头 + 自下而上的 BGRA + 全零 AND 掩码。
// 32 位色下 AND 掩码不参与显示, 但结构上必须存在, 缺了资源解析器会读错后面的数据。
func dibBytes(img *image.NRGBA) []byte {
	w := img.Bounds().Dx()
	h := img.Bounds().Dy()
	maskRow := ((w + 31) / 32) * 4

	buf := new(bytes.Buffer)
	put := func(v any) { _ = binary.Write(buf, binary.LittleEndian, v) }
	put(uint32(40))                // biSize
	put(int32(w))                  // biWidth
	put(int32(h * 2))              // biHeight: XOR 与 AND 两段叠在一起
	put(uint16(1))                 // biPlanes
	put(uint16(32))                // biBitCount
	put(uint32(0))                 // biCompression = BI_RGB
	put(uint32(w*h*4 + maskRow*h)) // biSizeImage
	put([4]uint32{})               // 分辨率与调色板信息, 用不上
	for y := h - 1; y >= 0; y-- {
		for x := 0; x < w; x++ {
			c := img.NRGBAAt(x, y)
			buf.Write([]byte{c.B, c.G, c.R, c.A})
		}
	}
	buf.Write(make([]byte, maskRow*h))
	return buf.Bytes()
}
