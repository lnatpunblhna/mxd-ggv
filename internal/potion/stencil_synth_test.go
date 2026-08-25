package potion

import (
	"fmt"
	"testing"
)

// synthDigits 用解包出来的原始字形画一串数量到深色底上，模拟药格。
func synthDigits(n, scale int) *bgra {
	w := numberWidth(n, scale) + 8*scale
	h := numberHeight(scale) + 6*scale
	im := solid(w, h, 30, 24, 20)
	renderNumber(im, n, 4*scale, 3*scale, scale)
	return im
}

// dropPixels 模拟截图缩放对不齐时丢掉的整行整列。
func dropPixels(im *bgra, dh, dw int) *bgra {
	if dh <= 0 && dw <= 0 {
		return im
	}
	keep := func(n, drop int) []int {
		out := make([]int, 0, n)
		for i := 0; i < n; i++ {
			if drop > 0 && i%(n/drop+1) == n/drop/2 && len(out) < n-drop {
				continue
			}
			out = append(out, i)
		}
		return out
	}
	keepY, keepX := keep(im.H, dh), keep(im.W, dw)
	out := &bgra{W: len(keepX), H: len(keepY), Stride: len(keepX) * 4}
	out.Pix = make([]byte, out.Stride*out.H)
	for y, sy := range keepY {
		for x, sx := range keepX {
			si := sy*im.Stride + sx*4
			di := y*out.Stride + x*4
			copy(out.Pix[di:di+4], im.Pix[si:si+4])
		}
	}
	return out
}

// TestStencilEightNotThree 盯死 8 被读成 3 这个坑。
// 3 的字身整个是 8 的字身的子集，8 的左半圆是唯一区别，而 3 的模板在那一列
// 大半是"透明"（既非字身也非描边）不参与打分。所以只要 8 的左竖笔在字身图上
// 掉出下限，8 就落选、3 顶上。字身图按十字胀 1 像素正是为了守住这一条。
func TestStencilEightNotThree(t *testing.T) {
	tmpls, ok := stencils()
	if !ok {
		t.Fatal("字形未就绪")
	}
	for _, scale := range []int{1, 2, 3} {
		si := -1
		for i, sz := range stencilSizes {
			if sz.scale == scale && sz.dh == 0 && sz.dw == 0 {
				si = i
				break
			}
		}
		if si < 0 {
			t.Fatalf("scale=%d 没有对应档位", scale)
		}
		v, ok := newStencilView(synthDigits(8, scale))
		if !ok {
			t.Fatalf("scale=%d 建视图失败", scale)
		}
		best := func(d int) float64 {
			top := -1.0
			for _, h := range v.matchStencil(tmpls[si][d]) {
				if hitRank(h) > top {
					top = hitRank(h)
				}
			}
			return top
		}
		got8, got3 := best(8), best(3)
		if got8 <= got3 {
			t.Errorf("scale=%d 画的是 8，但 3 的排名 %.3f 不低于 8 的 %.3f", scale, got3, got8)
		}
	}
}

// TestStencilSynthDropped 丢行丢列时也不能读错，尤其是含 8 的数。
// 只测 2x 起：stencilSizes 按设计不给 1x 铺丢行档位——11 像素高的字形再少一行
// 就变形太重，铺出来的模板只会互相串味。
func TestStencilSynthDropped(t *testing.T) {
	nums := []int{3, 8, 18, 38, 80, 83, 88, 188, 380, 838, 888, 983}
	var bad []string
	tot := 0
	for scale := 2; scale <= 3; scale++ {
		for dh := 0; dh <= scale; dh++ {
			for dw := 0; dw <= scale; dw++ {
				for _, n := range nums {
					im := dropPixels(synthDigits(n, scale), dh, dw)
					got, s, _ := assembleHits(readByStencil(im, newStencilHint()))
					tot++
					if got != n {
						bad = append(bad, fmt.Sprintf("scale=%d drop(%d,%d) %d→%d s=%.3f",
							scale, dh, dw, n, got, s))
					}
				}
			}
		}
	}
	if len(bad) > 0 {
		t.Errorf("%d/%d 例读错：", len(bad), tot)
		for _, b := range bad {
			t.Log("  " + b)
		}
	}
}

// TestStencilSynthClean 干净合成图必须全对。
func TestStencilSynthClean(t *testing.T) {
	for scale := 1; scale <= 3; scale++ {
		for n := 0; n <= 999; n++ {
			got, s, k := assembleHits(readByStencil(synthDigits(n, scale), newStencilHint()))
			if got != n {
				t.Errorf("scale=%d 画 %d 读成 %d (s=%.3f k=%d)", scale, n, got, s, k)
			}
		}
	}
}
