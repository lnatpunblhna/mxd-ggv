package potion

import (
	"embed"
	"fmt"
	"image/png"
	"sync"
)

// assets/digits/[0-9].png 是从游戏资源里解包出来的数量字形原图
// （8x11，"1" 为 5x11；黑色描边 + 白→淡蓝渐变字身，其余像素全透明）。
// preview.png 只是拼版预览，不参与识别，所以用 [0-9] 精确匹配。
//
//go:embed assets/digits/[0-9].png
var digitAssets embed.FS

// nativeGlyph 是一枚原始字形：core 为字身，edge 为描边。
type nativeGlyph struct {
	Digit int
	W, H  int
	Core  []bool
	Edge  []bool
	Pix   [][4]byte // BGRA，原色，用于合成测试图
}

func (g nativeGlyph) at(x, y int) int { return y*g.W + x }

var (
	nativeOnce   sync.Once
	nativeGlyphs [10]nativeGlyph
	nativeErr    error
)

// nativeDigits 返回解包出来的 0-9 字形，只在首次调用时解码。
func nativeDigits() ([10]nativeGlyph, error) {
	nativeOnce.Do(loadNativeGlyphs)
	return nativeGlyphs, nativeErr
}

func loadNativeGlyphs() {
	for d := 0; d <= 9; d++ {
		g, err := decodeGlyph(d)
		if err != nil {
			nativeErr = err
			return
		}
		nativeGlyphs[d] = g
	}
}

func decodeGlyph(d int) (nativeGlyph, error) {
	name := fmt.Sprintf("assets/digits/%d.png", d)
	f, err := digitAssets.Open(name)
	if err != nil {
		return nativeGlyph{}, fmt.Errorf("打开字形 %s: %w", name, err)
	}
	defer f.Close()
	src, err := png.Decode(f)
	if err != nil {
		return nativeGlyph{}, fmt.Errorf("解码字形 %s: %w", name, err)
	}
	b := src.Bounds()
	g := nativeGlyph{
		Digit: d,
		W:     b.Dx(),
		H:     b.Dy(),
	}
	if g.W < 1 || g.H < 1 {
		return nativeGlyph{}, fmt.Errorf("字形 %s 尺寸无效", name)
	}
	g.Core = make([]bool, g.W*g.H)
	g.Edge = make([]bool, g.W*g.H)
	g.Pix = make([][4]byte, g.W*g.H)
	for y := 0; y < g.H; y++ {
		for x := 0; x < g.W; x++ {
			r16, g16, b16, a16 := src.At(b.Min.X+x, b.Min.Y+y).RGBA()
			r, gg, bb, a := byte(r16>>8), byte(g16>>8), byte(b16>>8), byte(a16>>8)
			i := g.at(x, y)
			g.Pix[i] = [4]byte{bb, gg, r, a}
			if a < 128 {
				continue
			}
			// 字身是白到淡蓝的渐变，描边接近纯黑，用亮度一刀切即可。
			if luminance(bb, gg, r) >= 128 {
				g.Core[i] = true
			} else {
				g.Edge[i] = true
			}
		}
	}
	return g, nil
}

// tmplAtSize 把字形最近邻缩放到 w×h，字身进 Bits，描边进 Anti。
// 横竖分开给尺寸是必须的：游戏按整数倍放大 UI，但窗口尺寸对不齐时
// 截图会零星丢行/丢列，两个方向丢的数量不一样。
func (g nativeGlyph) tmplAtSize(w, h int) digitTmpl {
	if w < 1 {
		w = 1
	}
	if h < 1 {
		h = 1
	}
	core := make([]bool, w*h)
	edge := make([]bool, w*h)
	for y := 0; y < h; y++ {
		sy := y * g.H / h
		for x := 0; x < w; x++ {
			sx := x * g.W / w
			i := g.at(sx, sy)
			core[y*w+x] = g.Core[i]
			edge[y*w+x] = g.Edge[i]
		}
	}
	return cropToCore(digitTmpl{Digit: g.Digit, W: w, H: h, Bits: core, Anti: edge})
}

// tmplAtHeight 按等比缩放到指定字高。
func (g nativeGlyph) tmplAtHeight(h int) digitTmpl {
	if h < 1 {
		h = 1
	}
	return g.tmplAtSize(g.W*h/g.H, h)
}

// scaledTmpl 是 tmplAtHeight 的整数倍写法。
func (g nativeGlyph) scaledTmpl(scale int) digitTmpl {
	if scale < 1 {
		scale = 1
	}
	return g.tmplAtSize(g.W*scale, g.H*scale)
}

// cropToCore 把模板收到字身外扩 1 像素的范围：正好留下一圈描边，
// 匹配时既能对齐字身，又能用描边排掉背景里的杂色。
func cropToCore(t digitTmpl) digitTmpl {
	minX, minY, maxX, maxY := inkBBox(t.Bits, t.W, t.H)
	if maxX < minX || maxY < minY {
		return t
	}
	minX, minY = max(minX-1, 0), max(minY-1, 0)
	maxX, maxY = min(maxX+1, t.W-1), min(maxY+1, t.H-1)
	tw, th := maxX-minX+1, maxY-minY+1
	bits := make([]bool, tw*th)
	anti := make([]bool, tw*th)
	for y := 0; y < th; y++ {
		for x := 0; x < tw; x++ {
			si := (minY+y)*t.W + (minX + x)
			bits[y*tw+x] = t.Bits[si]
			if t.Anti != nil {
				anti[y*tw+x] = t.Anti[si]
			}
		}
	}
	return digitTmpl{Digit: t.Digit, W: tw, H: th, Bits: bits, Anti: anti}
}

// drawGlyph 把原始字形（描边 + 字身）按整数倍画到图上，用于合成测试素材。
func drawGlyph(im *bgra, d, originX, originY, scale int) {
	if im == nil || d < 0 || d > 9 || scale < 1 {
		return
	}
	glyphs, err := nativeDigits()
	if err != nil {
		return
	}
	g := glyphs[d]
	for y := 0; y < g.H*scale; y++ {
		py := originY + y
		if py < 0 || py >= im.H {
			continue
		}
		for x := 0; x < g.W*scale; x++ {
			px := originX + x
			if px < 0 || px >= im.W {
				continue
			}
			c := g.Pix[g.at(x/scale, y/scale)]
			if c[3] < 128 {
				continue
			}
			i := py*im.Stride + px*4
			im.Pix[i], im.Pix[i+1], im.Pix[i+2], im.Pix[i+3] = c[0], c[1], c[2], 255
		}
	}
}

// glyphAdvance 是原始字体里相邻数字的步进（字宽 + 1 像素间距）。
func glyphAdvance(d, scale int) int {
	glyphs, err := nativeDigits()
	if err != nil {
		return 6 * scale
	}
	return (glyphs[d].W + 1) * scale
}
