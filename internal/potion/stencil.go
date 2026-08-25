package potion

import (
	"sort"
	"sync"
)

// 直接拿解包出来的原始字形当模板，在像素上做匹配：
// 字身位置必须是字体配色的亮点，描边位置必须是近黑。
// 药水图标的白色高光能骗过单纯的亮度阈值，但骗不过整圈描边，
// 所以这条路子比"先二值化再切分"稳得多。
const (
	stencilDarkMax = 96.0 // 描边是纯黑，压在图标上也顶多到这个亮度
	stencilCoreMin = 0.86 // 字身命中率下限
	stencilEdgeMin = 0.68 // 描边命中率下限
	stencilMin     = 0.82 // 综合得分下限
	stencilCoreW   = 0.34 // 字身在综合得分里的权重（描边区分度更高，占大头）
	stencilVisMin  = 0.55 // 被裁掉的数字至少要露出这么多才认
)

// stencilSize 描述一档渲染尺寸：UI 放大 scale 倍，
// 再被截图缩放吃掉 dh 行、dw 列。实测 2x 的数字常是 21 行而不是 22 行。
type stencilSize struct{ scale, dh, dw int }

// stencilSizes 覆盖 1x-3x 及各自可能丢掉的行列组合。
// 同一串数字必须用同一档比，差一档就会出现"3 里认出个 1"这种事。
var stencilSizes = func() []stencilSize {
	var out []stencilSize
	for scale := 1; scale <= 3; scale++ {
		if scale == 1 {
			out = append(out, stencilSize{1, 0, 0})
			continue
		}
		for dh := 0; dh <= scale; dh++ {
			for dw := 0; dw <= scale; dw++ {
				out = append(out, stencilSize{scale, dh, dw})
			}
		}
	}
	return out
}()

var (
	stencilOnce  sync.Once
	stencilBank  [][10]digitTmpl // 按 stencilSizes 下标组织
	stencilReady bool
)

func stencils() ([][10]digitTmpl, bool) {
	stencilOnce.Do(func() {
		glyphs, err := nativeDigits()
		if err != nil {
			return
		}
		stencilBank = make([][10]digitTmpl, len(stencilSizes))
		for i, sz := range stencilSizes {
			for d := 0; d <= 9; d++ {
				g := glyphs[d]
				t := g.tmplAtSize(g.W*sz.scale-sz.dw, g.H*sz.scale-sz.dh)
				t.ensure()
				stencilBank[i][d] = t
			}
		}
		stencilReady = true
	})
	return stencilBank, stencilReady
}

// stencilMasks 把区域拆成"字体亮点"和"近黑"两张图。
// 截图缩放对不齐时游戏会整行整列地丢像素（实测 2x 的数字常是 17 行而不是 22 行），
// 所以字身那张按十字胀 1 像素：少一行横笔、少一列竖笔都还能对上。
// 竖笔尤其要紧——8 的左半圆丢掉一列就会让字身命中率跌破下限，
// 8 直接落选，剩下笔画是它子集的 3 顶上，读出来就成了 3。
// 描边那张保持严格，它才是区分 8 和 0 这类字形的关键。
func stencilMasks(im *bgra) (ink, dark []bool, w, h int) {
	if im == nil || im.empty() {
		return nil, nil, 0, 0
	}
	w, h = im.W, im.H
	raw := make([]bool, w*h)
	dark = make([]bool, w*h)
	for y := 0; y < h; y++ {
		row := im.Pix[y*im.Stride:]
		for x := 0; x < w; x++ {
			i := x * 4
			hh, s, _ := bgraHSV(row[i], row[i+1], row[i+2])
			l := luminance(row[i], row[i+1], row[i+2])
			j := y*w + x
			raw[j] = isCountInk(hh, s, l) || isYellowInk(hh, s, l)
			dark[j] = l <= stencilDarkMax
		}
	}
	ink = make([]bool, w*h)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			j := y*w + x
			ink[j] = raw[j] ||
				(y > 0 && raw[j-w]) || (y+1 < h && raw[j+w]) ||
				(x > 0 && raw[j-1]) || (x+1 < w && raw[j+1])
		}
	}
	return ink, dark, w, h
}

type stencilView struct {
	ink, dark      []bool
	w, h           int
	inkI, darkI    integ
	bandY0, bandY1 int // 有字体亮点的行区间，滑窗只在这段里走
}

func newStencilView(im *bgra) (stencilView, bool) {
	ink, dark, w, h := stencilMasks(im)
	if w < 4 || h < 6 {
		return stencilView{}, false
	}
	v := stencilView{
		ink: ink, dark: dark, w: w, h: h,
		inkI:  buildInteg(ink, w, h),
		darkI: buildInteg(dark, w, h),
	}
	v.bandY0, v.bandY1 = h, -1
	for y := 0; y < h; y++ {
		if v.inkI.sum(0, y, w, y+1) > 0 {
			if y < v.bandY0 {
				v.bandY0 = y
			}
			v.bandY1 = y
		}
	}
	if v.bandY1 < v.bandY0 {
		return stencilView{}, false
	}
	return v, true
}

// matchStencil 用一枚字形在整张图上滑窗。x 允许伸出左右边界，
// 这样被框选裁掉半个的首位数字也能认出来。
func (v stencilView) matchStencil(t digitTmpl) []tmplHit {
	if t.NInk < 4 || t.NAnti < 4 || t.H > v.h {
		return nil
	}
	minVis := t.W * stencilVisMin100 / 100
	if minVis < 3 {
		minVis = 3
	}
	if minVis > t.W {
		minVis = t.W
	}
	// 字身必须落在有亮点的行区间里，容 1 像素。
	y0 := max(v.bandY0-1, 0)
	y1 := min(v.bandY1+1, v.h-1)
	// 窗口完整时按整枚字形要求，伸出边界时按可见比例放宽。
	fullInk := int(float64(t.NInk)*stencilCoreMin) - 1
	fullDark := int(float64(t.NAnti)*stencilEdgeMin) - 1
	cutInk := int(float64(fullInk) * stencilVisMin)
	cutDark := int(float64(fullDark) * stencilVisMin)
	var hits []tmplHit
	for y := y0; y+t.H <= y1+1; y++ {
		for x := minVis - t.W; x+minVis <= v.w; x++ {
			// 积分图粗筛：窗口里的亮点/黑点数量连下限都不到就不用细算。
			needInk, needDark := fullInk, fullDark
			wx0, wx1 := x, x+t.W
			if wx0 < 0 || wx1 > v.w {
				wx0, wx1 = max(wx0, 0), min(wx1, v.w)
				needInk, needDark = cutInk, cutDark
			}
			if v.inkI.sum(wx0, y, wx1, y+t.H) < needInk {
				continue
			}
			if v.darkI.sum(wx0, y, wx1, y+t.H) < needDark {
				continue
			}
			if ht, ok := v.scoreAt(t, x, y); ok {
				hits = append(hits, ht)
			}
		}
	}
	return hits
}

func (v stencilView) scoreAt(t digitTmpl, x, y int) (tmplHit, bool) {
	core, coreVis := 0, 0
	for _, p := range t.Ink {
		px := x + p.x
		if px < 0 || px >= v.w {
			continue
		}
		coreVis++
		if v.ink[(y+p.y)*v.w+px] {
			core++
		}
	}
	if coreVis*100 < t.NInk*stencilVisMin100 || coreVis < 4 {
		return tmplHit{}, false
	}
	if float64(core) < float64(coreVis)*stencilCoreMin {
		return tmplHit{}, false
	}
	edge, edgeVis := 0, 0
	for j, on := range t.Anti {
		if !on {
			continue
		}
		px := x + j%t.W
		if px < 0 || px >= v.w {
			continue
		}
		edgeVis++
		if v.dark[(y+j/t.W)*v.w+px] {
			edge++
		}
	}
	if edgeVis < 4 || float64(edge) < float64(edgeVis)*stencilEdgeMin {
		return tmplHit{}, false
	}
	s := stencilCoreW*float64(core)/float64(coreVis) +
		(1-stencilCoreW)*float64(edge)/float64(edgeVis)
	if coreVis < t.NInk {
		// 只看到一部分，证据少，压一点分免得盖过完整匹配。
		s -= 0.02
	}
	if s < stencilMin {
		return tmplHit{}, false
	}
	// 裁掉的部分不算进外框，后面的排布检查才对得上。
	x0, x1 := max(x, 0), min(x+t.W, v.w)
	return tmplHit{x: x0, y: y, w: x1 - x0, h: t.H, d: t.Digit, s: s}, true
}

const stencilVisMin100 = int(stencilVisMin * 100)

// stencilSure 是"这一档就是对的"的判定线，用来提前收工。
const stencilSure = 0.95

// readByStencil 读出一串数量。同一串数字字号一致，
// 所以逐档字号各自成行、各自打分，最后挑最好的一档，
// 避免小号模板在别的数字内部乱命中。
// 游戏跑起来后 UI 缩放不会变，命中的那一档记在 hint 里下次先试。
func readByStencil(im *bgra, hint *stencilHint) []tmplHit {
	tmpls, ok := stencils()
	if !ok {
		return nil
	}
	v, ok := newStencilView(im)
	if !ok {
		return nil
	}

	var best []tmplHit
	bestRank, bestSize := 0.0, -1
	for _, i := range stencilOrder(hint) {
		var hits []tmplHit
		for d := 0; d <= 9; d++ {
			hits = append(hits, v.matchStencil(tmpls[i][d])...)
		}
		line := stencilLine(hits)
		if len(line) == 0 {
			continue
		}
		if r := stencilRank(line); r > bestRank {
			bestRank, bestSize, best = r, i, line
		}
		if avgHitScore(line) >= stencilSure {
			break
		}
	}
	if bestSize >= 0 && hint != nil {
		hint.size = bestSize
	}
	return best
}

// stencilOrder 把上次命中的那一档排到最前面。
func stencilOrder(hint *stencilHint) []int {
	n := len(stencilSizes)
	out := make([]int, 0, n)
	first := -1
	if hint != nil && hint.size >= 0 && hint.size < n {
		first = hint.size
		out = append(out, first)
	}
	for i := 0; i < n; i++ {
		if i != first {
			out = append(out, i)
		}
	}
	return out
}

// stencilLine 把同一字号的候选收成一行数字。
func stencilLine(hits []tmplHit) []tmplHit {
	if len(hits) == 0 {
		return nil
	}
	hits = nmsHits(hits)
	// 以最强的一处定基线，同行的才留下。
	anchor := hits[0]
	for _, ht := range hits[1:] {
		if hitRank(ht) > hitRank(anchor) {
			anchor = ht
		}
	}
	tol := anchor.h / 4
	if tol < 2 {
		tol = 2
	}
	line := make([]tmplHit, 0, len(hits))
	for _, ht := range hits {
		if absInt(ht.maxY()-anchor.maxY()) > tol {
			continue
		}
		line = append(line, ht)
	}
	if len(line) == 0 {
		return nil
	}
	sort.Slice(line, func(i, j int) bool { return line[i].x < line[j].x })
	line = bestHitGroup(line)
	if len(line) == 0 || len(line) > 4 {
		return nil
	}
	// 数字之间不该有大缝，有就说明混进了别的东西。
	gapLim := anchor.h*2/3 + 2
	for i := 1; i < len(line); i++ {
		if line[i].x-line[i-1].maxX() > gapLim {
			return nil
		}
	}
	return line
}

func stencilRank(line []tmplHit) float64 {
	if len(line) == 0 {
		return 0
	}
	sum, maxH := 0.0, 0
	for _, ht := range line {
		sum += ht.s
		if ht.h > maxH {
			maxH = ht.h
		}
	}
	r := sum / float64(len(line))
	switch len(line) {
	case 2, 3:
		r += 0.12
	case 4:
		r += 0.08
	}
	// 同分时偏向更大的字号：小模板容易在别的数字内部凑出一行。
	r += 0.004 * float64(min(maxH, 24))
	return r
}
