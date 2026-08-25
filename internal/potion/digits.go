package potion

import (
	"image"
	"math"
	"sort"
	"strconv"
	"sync"
)

const (
	matchMin = 0.58
	// tmplSimilar 判重用：内置模板逐档缩放时会撞出一模一样的位图。
	sameTmpl = 0.92
	// 原始字形的字身是白到淡蓝的渐变（最暗一档 #99CCFF，亮度约 195、饱和度 0.40），
	// 阈值必须罩住整段渐变，否则每个数字的下半截都会被判成背景。
	lumaInk   = 190.0
	satInkMax = 0.45
	// 渐变里有色的几档色相都在 180°-216°，留一点余量。
	countInkHueMin = 165.0
	countInkHueMax = 240.0
	countInkSMin   = 0.10
	inkRadius      = 1
)

type tmplPt struct{ x, y int }

type digitTmpl struct {
	Digit int
	W, H  int
	Bits  []bool
	// Anti 是原始字形的黑色描边，和 Bits 同尺寸。描边位置上出现亮点
	// 说明那里其实是背景（药水图标的高光），用来扣分。
	Anti  []bool
	Ink   []tmplPt
	NInk  int
	NAnti int
}

func (t *digitTmpl) ensure() {
	if t == nil || t.Ink != nil {
		return
	}
	t.Ink = make([]tmplPt, 0, t.W*t.H/3)
	for y := 0; y < t.H; y++ {
		for x := 0; x < t.W; x++ {
			if t.Bits[y*t.W+x] {
				t.Ink = append(t.Ink, tmplPt{x, y})
			}
		}
	}
	t.NInk = len(t.Ink)
	t.NAnti = 0
	for _, on := range t.Anti {
		if on {
			t.NAnti++
		}
	}
}

func (t digitTmpl) usable() bool {
	t.ensure()
	if t.H < 6 || t.NInk < 4 {
		return false
	}
	if t.Digit == 1 {
		return t.W >= 2
	}
	return t.W >= 3
}

// stencilHint 是读数时的一点运行期缓存：记住上一次原始字形匹配命中的字号档位，
// 下次先试它。游戏跑起来后 UI 缩放不会变，命中一次就能一直省下逐档试的开销。
// size 为 -1 表示还没命中过。识别本身只靠内置字形，不依赖任何积累下来的状态。
type stencilHint struct {
	size int
}

func newStencilHint() *stencilHint {
	return &stencilHint{size: -1}
}

func tmplSimilar(a, b digitTmpl) bool {
	if a.W != b.W || a.H != b.H || len(a.Bits) != len(b.Bits) {
		return false
	}
	a.ensure()
	b.ensure()
	if a.NInk == 0 && b.NInk == 0 {
		return true
	}
	inter := 0
	for i := range a.Bits {
		if a.Bits[i] && b.Bits[i] {
			inter++
		}
	}
	den := a.NInk + b.NInk
	if den == 0 {
		return true
	}
	return float64(2*inter)/float64(den) >= sameTmpl
}

var (
	builtinOnce  sync.Once
	builtinTmpls [10][]digitTmpl
)

func builtins() [10][]digitTmpl {
	builtinOnce.Do(buildBuiltins)
	return builtinTmpls
}

// builtinHeights 覆盖 1x-3x 的字高。游戏按整数倍放大 UI，
// 但窗口尺寸对不齐时截图会零星丢行，实测 2x 的数字常是 17 而不是 22 像素高，
// 所以整数倍之外还要铺几档中间高度。
var builtinHeights = []int{11, 14, 17, 20, 22, 25, 28, 33}

// buildBuiltins 用解包出来的原始字形建模板：每档高度一份，
// 最小的两档再加一份加粗版，补偿描边把笔画吃掉一圈的情况。
func buildBuiltins() {
	glyphs, err := nativeDigits()
	if err != nil {
		return
	}
	for d := 0; d <= 9; d++ {
		for i, h := range builtinHeights {
			t := glyphs[d].tmplAtHeight(h)
			addBuiltin(d, t)
			if i < 2 {
				addBuiltin(d, thicken(t))
			}
		}
	}
}

func addBuiltin(d int, t digitTmpl) {
	t.Digit = d
	t.ensure()
	if !t.usable() {
		return
	}
	for _, old := range builtinTmpls[d] {
		if tmplSimilar(old, t) {
			return
		}
	}
	builtinTmpls[d] = append(builtinTmpls[d], t)
}

func thicken(t digitTmpl) digitTmpl {
	out := make([]bool, t.W*t.H)
	copy(out, t.Bits)
	for y := 0; y < t.H; y++ {
		for x := 0; x < t.W; x++ {
			if !t.Bits[y*t.W+x] {
				continue
			}
			for _, d := range [][2]int{{1, 0}, {-1, 0}, {0, 1}, {0, -1}} {
				nx, ny := x+d[0], y+d[1]
				if nx < 0 || ny < 0 || nx >= t.W || ny >= t.H {
					continue
				}
				out[ny*t.W+nx] = true
			}
		}
	}
	// 字身胀了一圈，描边要相应让位，否则自己就把自己扣分了。
	var anti []bool
	if t.Anti != nil {
		anti = make([]bool, len(t.Anti))
		for i, on := range t.Anti {
			anti[i] = on && !out[i]
		}
	}
	return digitTmpl{Digit: t.Digit, W: t.W, H: t.H, Bits: out, Anti: anti}
}

func padTmpl(t digitTmpl, p int) digitTmpl {
	if p <= 0 {
		return t
	}
	w, h := t.W+2*p, t.H+2*p
	bits := make([]bool, w*h)
	for y := 0; y < t.H; y++ {
		for x := 0; x < t.W; x++ {
			if t.Bits[y*t.W+x] {
				bits[(y+p)*w+(x+p)] = true
			}
		}
	}
	return digitTmpl{Digit: t.Digit, W: w, H: h, Bits: bits}
}

type inkMode int

const (
	inkYellow inkMode = iota
	inkOutlined
)

func isYellowInk(h, s, l float64) bool {
	if l < 95 || l > 252 || h < 16 || h > 82 {
		return false
	}
	if s >= 0.22 {
		return true
	}
	return s >= 0.08 && l >= 145
}

// isCountInk 按解包出来的原始字形配色判断字身像素。
// 字身只有 6 种颜色（#FFFFFF #EEFFFF #DDEEFF #BBDDFF #AACCFF #99CCFF），
// 全部是高亮度、低饱和、色相落在青蓝一段，靠这条件能把药水图标的
// 暖色高光和深色底纹直接排掉。
func isCountInk(h, s, l float64) bool {
	if l < lumaInk || s > satInkMax {
		return false
	}
	if s < countInkSMin {
		// 近乎纯白，没有可靠色相。
		return true
	}
	return h >= countInkHueMin && h <= countInkHueMax
}

func nearDark(im *bgra, x, y int, luma float64) bool {
	if im == nil {
		return false
	}
	for dy := -inkRadius; dy <= inkRadius; dy++ {
		for dx := -inkRadius; dx <= inkRadius; dx++ {
			if dx == 0 && dy == 0 {
				continue
			}
			nx, ny := x+dx, y+dy
			if nx < 0 || ny < 0 || nx >= im.W || ny >= im.H {
				continue
			}
			i := ny*im.Stride + nx*4
			nl := luminance(im.Pix[i], im.Pix[i+1], im.Pix[i+2])
			if nl < 85 || luma-nl >= 80 {
				return true
			}
		}
	}
	return false
}

func inkMaskMode(im *bgra, mode inkMode) ([]bool, int, int) {
	if im == nil || im.empty() {
		return nil, 0, 0
	}
	w, h := im.W, im.H
	mask := make([]bool, w*h)
	for y := 0; y < h; y++ {
		row := im.Pix[y*im.Stride:]
		for x := 0; x < w; x++ {
			i := x * 4
			hh, s, _ := bgraHSV(row[i], row[i+1], row[i+2])
			l := luminance(row[i], row[i+1], row[i+2])
			on := false
			switch mode {
			case inkYellow:
				on = isYellowInk(hh, s, l)
			default:
				on = isCountInk(hh, s, l) && nearDark(im, x, y, l)
			}
			mask[y*w+x] = on
		}
	}
	return mask, w, h
}

func cleanDigitMask(mask []bool, w, h int) {
	if w < 4 || h < 4 {
		return
	}
	thresh := h * 45 / 100
	if thresh < 8 {
		thresh = 8
	}
	for x := 0; x < w; x++ {
		if x > 3 && x < w-4 {
			continue
		}
		n := 0
		for y := 0; y < h; y++ {
			if mask[y*w+x] {
				n++
			}
		}
		if n < thresh {
			continue
		}
		for y := 0; y < h; y++ {
			mask[y*w+x] = false
		}
	}
}

type integ struct {
	W, H int
	S    []int
}

func buildInteg(mask []bool, w, h int) integ {
	iw := w + 1
	s := make([]int, (h+1)*iw)
	for y := 1; y <= h; y++ {
		row := 0
		for x := 1; x <= w; x++ {
			if mask[(y-1)*w+(x-1)] {
				row++
			}
			s[y*iw+x] = s[(y-1)*iw+x] + row
		}
	}
	return integ{W: w, H: h, S: s}
}

func (in integ) sum(x0, y0, x1, y1 int) int {
	iw := in.W + 1
	return in.S[y1*iw+x1] - in.S[y0*iw+x1] - in.S[y1*iw+x0] + in.S[y0*iw+x0]
}

type tmplHit struct {
	x, y, w, h int
	d          int
	s          float64
}

func (h tmplHit) maxX() int { return h.x + h.w - 1 }
func (h tmplHit) maxY() int { return h.y + h.h - 1 }

func nccBinaryAt(mask []bool, in integ, x, y int, t digitTmpl) float64 {
	t.ensure()
	n := t.W * t.H
	if n < 8 || t.NInk < 4 {
		return 0
	}
	sumT := t.NInk
	sumH := in.sum(x, y, x+t.W, y+t.H)
	if sumH < t.NInk*2/5 {
		return 0
	}
	dot := 0
	for _, p := range t.Ink {
		if mask[(y+p.y)*in.W+(x+p.x)] {
			dot++
		}
	}
	if dot*5 < t.NInk*2 {
		return 0
	}
	num := float64(n*dot - sumH*sumT)
	denT := float64(sumT * (n - sumT))
	denH := float64(sumH * (n - sumH))
	if denT <= 0 || denH <= 0 {
		return 0
	}
	v := num / math.Sqrt(denT*denH)
	// 描边位置上的亮点是背景漏进来的，按比例扣分。
	if t.NAnti > 0 {
		bleed := 0
		for i, on := range t.Anti {
			if !on {
				continue
			}
			if mask[(y+i/t.W)*in.W+(x+i%t.W)] {
				bleed++
			}
		}
		v -= antiWeight * float64(bleed) / float64(t.NAnti)
	}
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

func tmplTooBig(t digitTmpl, w, h int) bool {
	if t.W*t.H > w*h*28/100 {
		return true
	}
	if t.H > h*55/100 && t.W > 8 {
		return true
	}
	return false
}

func matchMask(mask []bool, in integ, w, h int, t digitTmpl) []tmplHit {
	t.ensure()
	if !t.usable() || t.W > w || t.H > h || tmplTooBig(t, w, h) {
		return nil
	}
	need := matchMin
	if t.Digit == 1 {
		need = matchMin + 0.06
	}
	maxX := w - t.W
	maxY := h - t.H
	var hits []tmplHit
	for y := 0; y <= maxY; y++ {
		for x := 0; x <= maxX; x++ {
			if t.Digit == 1 && x <= 1 {
				continue
			}
			s := nccBinaryAt(mask, in, x, y, t)
			if s < need {
				continue
			}
			hits = append(hits, tmplHit{x: x, y: y, w: t.W, h: t.H, d: t.Digit, s: s})
		}
	}
	return hits
}

func scanMask(im *bgra, mode inkMode) []tmplHit {
	mask, w, h := inkMaskMode(im, mode)
	if w < 4 || h < 4 {
		return nil
	}
	cleanDigitMask(mask, w, h)
	in := buildInteg(mask, w, h)
	var hits []tmplHit
	for d := 0; d <= 9; d++ {
		for _, t := range builtins()[d] {
			hits = append(hits, matchMask(mask, in, w, h, t)...)
		}
	}
	return hits
}

func hitOverlap(a, b tmplHit) float64 {
	x0 := a.x
	if b.x > x0 {
		x0 = b.x
	}
	y0 := a.y
	if b.y > y0 {
		y0 = b.y
	}
	x1 := a.maxX()
	if b.maxX() < x1 {
		x1 = b.maxX()
	}
	y1 := a.maxY()
	if b.maxY() < y1 {
		y1 = b.maxY()
	}
	if x1 < x0 || y1 < y0 {
		return 0
	}
	inter := (x1 - x0 + 1) * (y1 - y0 + 1)
	aa, ba := a.w*a.h, b.w*b.h
	minA := aa
	if ba < minA {
		minA = ba
	}
	if minA <= 0 {
		return 0
	}
	return float64(inter) / float64(minA)
}

func hitRank(h tmplHit) float64 {
	r := h.s
	if h.d == 1 {
		r -= 0.07
	}
	area := h.w * h.h
	if area > 120 {
		area = 120
	}
	return r + 0.001*float64(area)
}

func nmsHits(hits []tmplHit) []tmplHit {
	if len(hits) <= 1 {
		return mergeColumns(hits)
	}
	sort.Slice(hits, func(i, j int) bool { return hitRank(hits[i]) > hitRank(hits[j]) })
	kept := make([]tmplHit, 0, len(hits))
	for _, h := range hits {
		ok := true
		for _, k := range kept {
			if hitOverlap(h, k) >= 0.28 {
				ok = false
				break
			}
		}
		if ok {
			kept = append(kept, h)
		}
	}
	sort.Slice(kept, func(i, j int) bool { return kept[i].x < kept[j].x })
	return mergeColumns(kept)
}

func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

func overlapX(a, b tmplHit) int {
	lo := a.x
	if b.x > lo {
		lo = b.x
	}
	hi := a.maxX()
	if b.maxX() < hi {
		hi = b.maxX()
	}
	n := hi - lo + 1
	if n < 0 {
		return 0
	}
	return n
}

func mergeColumns(hits []tmplHit) []tmplHit {
	if len(hits) <= 1 {
		return hits
	}
	sort.Slice(hits, func(i, j int) bool { return hits[i].x < hits[j].x })
	out := []tmplHit{hits[0]}
	for i := 1; i < len(hits); i++ {
		h := hits[i]
		prev := &out[len(out)-1]
		ov := overlapX(*prev, h)
		minW := prev.w
		if h.w < minW {
			minW = h.w
		}
		gap := h.x - prev.maxX()
		sameCol := minW > 0 && ov*100 >= minW*50
		touching := gap <= 1 && h.d == prev.d && minW <= 4 && absInt(h.y-prev.y) <= 2
		if sameCol || touching {
			if hitRank(h) > hitRank(*prev) {
				*prev = h
			}
			continue
		}
		out = append(out, h)
	}
	return out
}

func clusterDigitHits(hits []tmplHit) []tmplHit {
	if len(hits) <= 1 {
		return hits
	}
	strong := false
	for _, ht := range hits {
		if ht.s >= 0.90 {
			strong = true
			break
		}
	}
	if strong {
		filtered := hits[:0]
		for _, ht := range hits {
			if ht.s >= 0.80 {
				filtered = append(filtered, ht)
			}
		}
		hits = append([]tmplHit(nil), filtered...)
		if len(hits) <= 1 {
			return hits
		}
	}
	best := hits[0]
	for _, ht := range hits[1:] {
		if hitRank(ht) > hitRank(best) {
			best = ht
		}
	}
	baseY := best.maxY()
	maxH := best.h
	kept := make([]tmplHit, 0, len(hits))
	for _, ht := range hits {
		if ht.h*100 < maxH*40 {
			continue
		}
		dy := ht.maxY() - baseY
		if dy < 0 {
			dy = -dy
		}
		tol := maxH / 3
		if tol < 3 {
			tol = 3
		}
		if dy > tol {
			continue
		}
		kept = append(kept, ht)
	}
	if len(kept) <= 1 {
		return kept
	}
	sort.Slice(kept, func(i, j int) bool { return kept[i].x < kept[j].x })
	return bestHitGroup(kept)
}

func bestHitGroup(hits []tmplHit) []tmplHit {
	if len(hits) <= 1 {
		return hits
	}
	maxH := 1
	for _, ht := range hits {
		if ht.h > maxH {
			maxH = ht.h
		}
	}
	gapLim := maxH * 2 / 3
	if gapLim < 3 {
		gapLim = 3
	}
	type grp struct{ hits []tmplHit }
	var groups []grp
	cur := []tmplHit{hits[0]}
	for i := 1; i < len(hits); i++ {
		gap := hits[i].x - cur[len(cur)-1].maxX()
		if gap <= gapLim && len(cur) < 4 {
			cur = append(cur, hits[i])
			continue
		}
		groups = append(groups, grp{cur})
		cur = []tmplHit{hits[i]}
	}
	groups = append(groups, grp{cur})
	bestI := 0
	bestR := -1.0
	for i, g := range groups {
		r := groupRank(g.hits)
		if r > bestR {
			bestR = r
			bestI = i
		}
	}
	return groups[bestI].hits
}

func groupRank(hits []tmplHit) float64 {
	avg := avgHitScore(hits)
	n := len(hits)
	bonus := 0.0
	switch n {
	case 2, 3:
		bonus = 0.16
	case 4:
		bonus = 0.10
	}
	return avg + bonus
}

func avgHitScore(hits []tmplHit) float64 {
	if len(hits) == 0 {
		return 0
	}
	sum := 0.0
	for _, h := range hits {
		sum += h.s
	}
	return sum / float64(len(hits))
}

func assembleHits(hits []tmplHit) (n int, score float64, digits int) {
	if len(hits) == 0 {
		return -1, 0, 0
	}
	n = 0
	sum := 0.0
	for _, ht := range hits {
		n = n*10 + ht.d
		sum += ht.s
	}
	return n, sum / float64(len(hits)), len(hits)
}

func matchRegion(im *bgra, hint *stencilHint) []tmplHit {
	if im == nil || im.empty() {
		return nil
	}
	if hint == nil {
		hint = newStencilHint()
	}
	// 先用解包出来的原始字形直接在像素上比对，这条路最准；
	// 认不出来（字号超出预期、被别的东西挡住）再退回二值化那套。
	if hits := readByStencil(im, hint); len(hits) > 0 {
		return hits
	}
	if hits := readBySegment(im); len(hits) > 0 {
		return hits
	}
	hits := scanMask(im, inkYellow)
	hits = append(hits, scanMask(im, inkOutlined)...)
	hits = nmsHits(hits)
	return clusterDigitHits(hits)
}

func readByMatch(im *bgra, hint *stencilHint) (n int, score float64, digits int) {
	return assembleHits(matchRegion(im, hint))
}

func countRegion(slot *bgra) *bgra {
	if slot == nil || slot.empty() {
		return nil
	}
	// 跳过顶部快捷键文字（Ins/Del 等），数量可能在左侧大字或右下角小字。
	y := int(float64(slot.H) * 0.22)
	if y < 0 {
		y = 0
	}
	if y >= slot.H {
		y = 0
	}
	r := image.Rect(0, y, slot.W, slot.H)
	return cropImage(slot, r)
}

func readCount(slot *bgra, sh *stencilHint) int {
	n, _ := readCountScore(slot, sh)
	return n
}

func readCountScore(slot *bgra, sh *stencilHint) (int, float64) {
	return readCountHint(slot, sh, 0)
}

func readCountHint(slot *bgra, sh *stencilHint, hint int) (int, float64) {
	if sh == nil {
		sh = newStencilHint()
	}
	region := countRegion(slot)
	if region == nil || region.W < 4 || region.H < 4 {
		return -1, 0
	}
	type cand struct {
		n, k int
		s    float64
	}
	var best cand
	consider := func(n int, s float64, k int) {
		if n < 0 || n > 9999 || k < 1 || k > 4 || s < matchMin {
			return
		}
		if best.k == 0 {
			best = cand{n, k, s}
			return
		}
		if k > best.k && s+0.04 >= best.s {
			best = cand{n, k, s}
			return
		}
		if best.k == 1 && k >= 2 && s >= 0.62 && n > best.n {
			best = cand{n, k, s}
			return
		}
		if hint > 0 && hint <= 12 && n > 12 && k > best.k && s >= 0.62 {
			best = cand{n, k, s}
			return
		}
		if k == best.k && s > best.s {
			best = cand{n, k, s}
			return
		}
		if k < best.k && s >= best.s+0.10 {
			if best.k >= 2 && best.s >= 0.62 && k == 1 {
				return
			}
			best = cand{n, k, s}
		}
	}
	try := func(im *bgra) {
		if im == nil || im.empty() {
			return
		}
		n, s, k := readByMatch(im, sh)
		consider(n, s, k)
	}
	try(region)
	// 原始字形直接匹配上了就不用再折腾各种裁法。
	if best.k > 0 && best.s >= stencilSure {
		return best.n, best.s
	}
	if band := cropDigitBand(region, inkOutlined); band != nil {
		try(band)
	}
	if band := cropDigitBand(region, inkYellow); band != nil {
		try(band)
	}
	leftW := region.W * 72 / 100
	if leftW >= 8 && leftW < region.W {
		try(cropImage(region, image.Rect(0, 0, leftW, region.H)))
	}
	if best.k == 0 {
		return -1, 0
	}
	return best.n, best.s
}

func cropDigitBand(im *bgra, mode inkMode) *bgra {
	if im == nil || im.empty() {
		return nil
	}
	mask, w, h := inkMaskMode(im, mode)
	if h < 10 || w < 8 {
		return nil
	}
	cleanDigitMask(mask, w, h)
	lim := w * 75 / 100
	if lim < 8 {
		lim = w
	}
	hist := make([]int, h)
	peak := 0
	for y := 0; y < h; y++ {
		n := 0
		for x := 0; x < lim; x++ {
			if mask[y*w+x] {
				n++
			}
		}
		hist[y] = n
		if n > peak {
			peak = n
		}
	}
	if peak < 4 {
		return nil
	}
	minH := h * 20 / 100
	if minH < 8 {
		minH = 8
	}
	maxH := h * 60 / 100
	if maxH > h {
		maxH = h
	}
	if maxH < minH {
		maxH = minH
	}
	bestAvg, bestY, bestHH := -1.0, 0, minH
	for hh := minH; hh <= maxH; hh++ {
		sum := 0
		for y := 0; y < hh; y++ {
			sum += hist[y]
		}
		for y0 := 0; y0+hh <= h; y0++ {
			if y0 > 0 {
				sum += hist[y0+hh-1] - hist[y0-1]
			}
			avg := float64(sum) / float64(hh)
			if avg > bestAvg {
				bestAvg = avg
				bestY = y0
				bestHH = hh
			}
		}
	}
	if bestAvg < float64(peak)*0.35 {
		return nil
	}
	return cropImage(im, image.Rect(0, bestY, lim, bestY+bestHH))
}

func inkBBox(mask []bool, w, h int) (minX, minY, maxX, maxY int) {
	minX, minY, maxX, maxY = w, h, -1, -1
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			if !mask[y*w+x] {
				continue
			}
			if x < minX {
				minX = x
			}
			if y < minY {
				minY = y
			}
			if x > maxX {
				maxX = x
			}
			if y > maxY {
				maxY = y
			}
		}
	}
	return
}

func tightFromMask(mask []bool, w, x0, y0, x1, y1, d int) digitTmpl {
	minX, minY, maxX, maxY := x1, y1, x0, y0
	found := false
	for y := y0; y <= y1; y++ {
		for x := x0; x <= x1; x++ {
			if !mask[y*w+x] {
				continue
			}
			found = true
			if x < minX {
				minX = x
			}
			if y < minY {
				minY = y
			}
			if x > maxX {
				maxX = x
			}
			if y > maxY {
				maxY = y
			}
		}
	}
	if !found {
		return digitTmpl{Digit: d}
	}
	tw, th := maxX-minX+1, maxY-minY+1
	bits := make([]bool, tw*th)
	for y := 0; y < th; y++ {
		for x := 0; x < tw; x++ {
			bits[y*tw+x] = mask[(minY+y)*w+(minX+x)]
		}
	}
	return padTmpl(digitTmpl{Digit: d, W: tw, H: th, Bits: bits}, 1)
}

// tightRect 把一段范围收紧到其中真正有墨迹的外框。
func tightRect(mask []bool, w, x0, y0, x1, y1 int) tmplHit {
	minX, minY, maxX, maxY := x1, y1, x0, y0
	found := false
	for y := y0; y <= y1; y++ {
		for x := x0; x <= x1; x++ {
			if !mask[y*w+x] {
				continue
			}
			found = true
			if x < minX {
				minX = x
			}
			if y < minY {
				minY = y
			}
			if x > maxX {
				maxX = x
			}
			if y > maxY {
				maxY = y
			}
		}
	}
	if !found {
		return tmplHit{}
	}
	return tmplHit{x: minX, y: minY, w: maxX - minX + 1, h: maxY - minY + 1}
}

// renderNumber 用解包出来的原始字形画一串数量，合成测试素材时用。
func renderNumber(im *bgra, n, originX, originY, scale int) {
	s := strconv.Itoa(n)
	x := originX
	for _, ch := range s {
		d := int(ch - '0')
		drawGlyph(im, d, x, originY, scale)
		x += glyphAdvance(d, scale)
	}
}

// numberWidth 是 renderNumber 画出来的总宽度。
func numberWidth(n, scale int) int {
	w := 0
	for _, ch := range strconv.Itoa(n) {
		w += glyphAdvance(int(ch-'0'), scale)
	}
	if w > 0 {
		w -= scale // 末位后面不留间距
	}
	return w
}

// numberHeight 是原始字形的行高。
func numberHeight(scale int) int {
	glyphs, err := nativeDigits()
	if err != nil {
		return 11 * scale
	}
	return glyphs[0].H * scale
}
