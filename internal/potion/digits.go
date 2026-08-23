package potion

import (
	"image"
	"math"
	"sort"
	"strconv"
	"sync"
)

const (
	matchMin   = 0.58
	learnSim   = 0.92
	maxLearned = 8
	lumaInk    = 230.0
	satInkMax  = 0.45
	inkRadius  = 1
)

// 5x7 点阵，用作数量识别的内置字形。
var builtin5x7 = [10]string{
	"01110100011000110001100011000101110",
	"00100011000010000100001000010001110",
	"01110100010000100110010001000011111",
	"01110100010000100110000011000101110",
	"10001100011000111111000010000100001",
	"11111100001000011110000011000101110",
	"01110100001000011110100011000101110",
	"11111000010001000100010000100001000",
	"01110100011000101110100011000101110",
	"01110100011000101111000011000101110",
}

type mapleSpec struct {
	d, w, h int
	bits    string
}

// 冒险岛快捷栏数量的真实字形（从药格截图提取）。
var mapleNative = []mapleSpec{
	{1, 5, 18, ".......##...##..###..###...##...##...##...##........##...##...##...##...##...##...##......"},
	{1, 7, 19, "...........##.....##..#####..#####.....##.....##.....##.....##.....##.....##.....##.....##.....##.....##.....##.....##.....##........"},
	{1, 8, 19, ".............##......##..######..######......##......##......##......##......##......##......##......##......##......##......##......##......##........."},
	{1, 13, 11, "...................##..#........##..##..#......##....#......##...........##...........##...........##...........##.........##..##.............."},
	{1, 14, 12, ".........................##........######........######............##............##............##............##............##..##........##...#........##..............."},
	{3, 13, 19, "................#######......#######....##.......##..##.......##......#....##......#....##...........##...........##......######............##...........##...........##...........##..##.......##..##.......##....#######......#######................"},
	{3, 13, 13, "................#.....#......#######....##.......##..##.......##......#....##......#....##...........##...........##......#####.............##...........##.............."},
	{3, 15, 13, "..................#.....#..#.....#######..##..##.......##....##.......##........###..##........###..##.............##.............##........#####..##...........##.............##.................."},
	{3, 13, 12, "..............#######..##.........##...........##......###..##......###..##...........##...........##......#####..##.........##...........##................"},
	{4, 13, 12, "..............#....###.##..#....#..###..#..###..###..#..#..##.##..####..##.##..##..##...##..##..##...##....##.....##....#########....#......................"},
	{4, 15, 13, ".......................#.##....#....###.##....#....#..###....#..###..###....#..#..##.##....####..##.##....##..##...##....##..##...##......##.....##......###########....#.........#................"},
	{8, 13, 18, "................#######......#######....##.......##..##.......##..##..###..##..##..###..##..##.......##..##.......##..###########..##.......##..##.......##..##.......##..##.......##..##.......##..##.......##....#######................"},
	{8, 14, 18, ".................########......########....##........##..##........##..##..####..##..##..####..##..##........##..##........##..############..##........##..##........##..##........##..##........##..##........##..##........##....########................."},
	{8, 15, 20, "................####...........####...........##..#######....##..#######......##.......##....##.......##....##..###..##....##..###..##....##.......##....##.......##...................##.......##....##.......##....##.......##....##.......##....##.......##....##.......##......#######.................."},
	{8, 14, 11, ".................#......#......########....##........##..##........##..##........##..##........##..##........##..##........##....########................."},
	{8, 13, 11, "..................#........#####.............##...........##......###..##......###..##...........##...........##......#####..####.............."},
	{9, 13, 16, "................#######......#######....##.......##..##.......##..##..##...##..##..##...##..##.......##..##.......##..##.......##....#########....#########...........##...........##...........##.............."},
}

type tmplPt struct{ x, y int }

type digitTmpl struct {
	Digit int
	W, H  int
	Bits  []bool
	Ink   []tmplPt
	NInk  int
	Img   *bgra
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

type digitBank struct {
	learned [10][]digitTmpl
}

func newDigitBank() *digitBank {
	return &digitBank{}
}

func (b *digitBank) clone() *digitBank {
	out := newDigitBank()
	out.mergeFrom(b)
	return out
}

func (b *digitBank) mergeFrom(src *digitBank) {
	if b == nil || src == nil {
		return
	}
	for d := 0; d <= 9; d++ {
		for _, t := range src.learned[d] {
			b.learn(d, t)
		}
	}
}

func (b *digitBank) coverage() []int {
	if b == nil {
		return nil
	}
	var have []int
	for d := 0; d <= 9; d++ {
		if len(b.learned[d]) > 0 {
			have = append(have, d)
		}
	}
	return have
}

func (b *digitBank) size() int {
	if b == nil {
		return 0
	}
	n := 0
	for d := 0; d <= 9; d++ {
		n += len(b.learned[d])
	}
	return n
}

func (b *digitBank) learn(d int, t digitTmpl) {
	if b == nil || d < 0 || d > 9 {
		return
	}
	t.Digit = d
	t.Ink = nil
	if t.Img != nil && (t.Img.W < 4 || t.Img.H < 7) {
		t.Img = nil
	}
	t.ensure()
	if !t.usable() {
		return
	}
	if !shapeFitsDigit(t, d) {
		return
	}
	b.dropConflicts(d, t)
	for _, old := range b.learned[d] {
		if tmplSimilar(old, t) {
			return
		}
	}
	if len(b.learned[d]) >= maxLearned {
		b.learned[d] = b.learned[d][1:]
	}
	b.learned[d] = append(b.learned[d], t)
}

func (b *digitBank) dropConflicts(d int, t digitTmpl) {
	if b == nil {
		return
	}
	for od := 0; od <= 9; od++ {
		if od == d {
			continue
		}
		kept := b.learned[od][:0]
		for _, old := range b.learned[od] {
			if glyphScore(t, old) >= 0.88 {
				continue
			}
			kept = append(kept, old)
		}
		if len(kept) == 0 {
			b.learned[od] = nil
			continue
		}
		b.learned[od] = kept
	}
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
	return float64(2*inter)/float64(den) >= learnSim
}

var (
	builtinOnce  sync.Once
	builtinTmpls [10][]digitTmpl
)

func builtins() [10][]digitTmpl {
	builtinOnce.Do(buildBuiltins)
	return builtinTmpls
}

func buildBuiltins() {
	for d := 0; d <= 9; d++ {
		for scale := 1; scale <= 3; scale++ {
			t := scaleGlyph(d, scale)
			addBuiltin(d, t)
			if scale <= 2 {
				addBuiltin(d, thicken(t))
			}
		}
	}
	for _, sp := range mapleNative {
		addBuiltin(sp.d, mapleTmpl(sp))
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

func scaleGlyph(d, scale int) digitTmpl {
	src := builtin5x7[d]
	if scale < 1 {
		scale = 1
	}
	w, h := 5*scale, 7*scale
	bits := make([]bool, w*h)
	for y := 0; y < 7; y++ {
		for x := 0; x < 5; x++ {
			if src[y*5+x] != '1' {
				continue
			}
			for dy := 0; dy < scale; dy++ {
				for dx := 0; dx < scale; dx++ {
					bits[(y*scale+dy)*w+(x*scale+dx)] = true
				}
			}
		}
	}
	return padTmpl(digitTmpl{Digit: d, W: w, H: h, Bits: bits}, 1)
}

func mapleTmpl(sp mapleSpec) digitTmpl {
	need := sp.w * sp.h
	if need <= 0 || len(sp.bits) != need {
		return digitTmpl{Digit: sp.d}
	}
	bits := make([]bool, need)
	for j, c := range sp.bits {
		bits[j] = c == '1' || c == '#'
	}
	return digitTmpl{Digit: sp.d, W: sp.w, H: sp.h, Bits: bits}
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
	return digitTmpl{Digit: t.Digit, W: t.W, H: t.H, Bits: out}
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
	return digitTmpl{Digit: t.Digit, W: w, H: h, Bits: bits, Img: t.Img}
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

func isWhiteInk(s, l float64) bool {
	return l >= lumaInk && s <= satInkMax
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

func inkMask(im *bgra) ([]bool, int, int) {
	return inkMaskMode(im, inkOutlined)
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
				on = isWhiteInk(s, l) && nearDark(im, x, y, l)
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

func matchGray(im *bgra, t digitTmpl) []tmplHit {
	if im == nil || t.Img == nil || t.Img.empty() {
		return nil
	}
	if t.Img.W > im.W || t.Img.H > im.H {
		return nil
	}
	maxX := im.W - t.Img.W
	maxY := im.H - t.Img.H
	var hits []tmplHit
	for y := 0; y <= maxY; y++ {
		for x := 0; x <= maxX; x++ {
			s := nccGrayWindow(im, t.Img, x, y)
			if s < matchMin+0.04 {
				continue
			}
			hits = append(hits, tmplHit{x: x, y: y, w: t.Img.W, h: t.Img.H, d: t.Digit, s: s})
		}
	}
	return hits
}

func scanMask(im *bgra, bank *digitBank, mode inkMode) []tmplHit {
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
		if bank == nil {
			continue
		}
		for _, t := range bank.learned[d] {
			if t.Img != nil {
				continue
			}
			hits = append(hits, matchMask(mask, in, w, h, t)...)
		}
	}
	return hits
}

func scanColor(im *bgra, bank *digitBank) []tmplHit {
	if im == nil || bank == nil {
		return nil
	}
	var hits []tmplHit
	for d := 0; d <= 9; d++ {
		for _, t := range bank.learned[d] {
			if t.Img == nil || t.Img.W < 4 || t.Img.H < 7 {
				continue
			}
			hits = append(hits, matchGray(im, t)...)
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

func matchRegion(im *bgra, bank *digitBank) []tmplHit {
	if im == nil || im.empty() {
		return nil
	}
	if bank == nil {
		bank = newDigitBank()
	}
	if hits := readBySegment(im, bank); len(hits) > 0 {
		return hits
	}
	hits := scanColor(im, bank)
	hits = append(hits, scanMask(im, bank, inkYellow)...)
	hits = append(hits, scanMask(im, bank, inkOutlined)...)
	hits = nmsHits(hits)
	return clusterDigitHits(hits)
}

func readByMatch(im *bgra, bank *digitBank) (n int, score float64, digits int) {
	return assembleHits(matchRegion(im, bank))
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

func readCount(slot *bgra, bank *digitBank) int {
	n, _ := readCountScore(slot, bank)
	return n
}

func readCountScore(slot *bgra, bank *digitBank) (int, float64) {
	return readCountHint(slot, bank, 0)
}

func readCountHint(slot *bgra, bank *digitBank, hint int) (int, float64) {
	if bank == nil {
		bank = newDigitBank()
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
		n, s, k := readByMatch(im, bank)
		consider(n, s, k)
	}
	try(region)
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

func learnCount(slot *bgra, value int, bank *digitBank) {
	if bank == nil || value < 0 || slot == nil {
		return
	}
	s := strconv.Itoa(value)
	region := countRegion(slot)
	if region == nil {
		return
	}
	n := len(s)
	if tryLearnSegment(region, s, n, bank) {
		return
	}
	hits := matchRegion(region, bank)
	if len(hits) == n {
		got := 0
		for _, h := range hits {
			got = got*10 + h.d
		}
		if got == value {
			for i, h := range hits {
				d := int(s[i] - '0')
				if tmplTooBig(digitTmpl{W: h.w, H: h.h}, region.W, region.H) {
					continue
				}
				if t, ok := cropLearned(region, h, d); ok {
					bank.learn(d, t)
				}
			}
			return
		}
	}
	for _, mode := range []inkMode{inkOutlined, inkYellow} {
		if tryLearnSplit(region, s, n, bank, mode) {
			return
		}
	}
}

func teachCount(slot *bgra, value int, bank *digitBank) int {
	if bank == nil || value < 0 || slot == nil {
		return 0
	}
	before := bank.size()
	learnCount(slot, value, bank)
	s := strconv.Itoa(value)
	have := 0
	for _, ch := range s {
		d := int(ch - '0')
		if d >= 0 && d <= 9 && len(bank.learned[d]) > 0 {
			have++
		}
	}
	if have == len(s) || bank.size() > before {
		return have
	}
	return 0
}

func tryLearnSegment(region *bgra, s string, n int, bank *digitBank) bool {
	if region == nil || region.empty() || n < 1 {
		return false
	}
	mask, w, h := digitMask(region)
	if w < 4 || h < 6 {
		return false
	}
	parts := segmentDigits(mask, w, h)
	if len(parts) != n {
		parts = splitInkParts(mask, w, h, n)
	}
	if len(parts) != n {
		return false
	}
	ok := 0
	for i, p := range parts {
		d := int(s[i] - '0')
		t := tightFromMask(mask, w, p.x, p.y, p.x+p.w-1, p.y+p.h-1, d)
		t.ensure()
		if tmplTooBig(t, region.W, region.H) {
			continue
		}
		t.Img = cropImage(region, image.Rect(p.x, p.y, p.x+p.w, p.y+p.h))
		t.Digit = d
		if t.usable() {
			bank.learn(d, t)
			ok++
		}
	}
	return ok == n
}

func cropLearned(im *bgra, h tmplHit, d int) (digitTmpl, bool) {
	if im == nil {
		return digitTmpl{}, false
	}
	r := image.Rect(h.x, h.y, h.x+h.w, h.y+h.h).Intersect(image.Rect(0, 0, im.W, im.H))
	if r.Empty() {
		return digitTmpl{}, false
	}
	sub := cropImage(im, r)
	mask, w, ht := inkMaskMode(sub, inkOutlined)
	if w < 2 || ht < 2 {
		mask, w, ht = inkMaskMode(sub, inkYellow)
	}
	t := tightTmpl(sub, mask, w, ht, d)
	if !t.usable() {
		return digitTmpl{}, false
	}
	return t, true
}

func tryLearnSplit(region *bgra, s string, n int, bank *digitBank, mode inkMode) bool {
	if region == nil || region.empty() || n < 1 {
		return false
	}
	src := region
	if band := cropDigitBand(region, mode); band != nil {
		src = band
	}
	mask, w, h := inkMaskMode(src, mode)
	if w < 4 || h < 4 {
		return false
	}
	cleanDigitMask(mask, w, h)
	parts := splitInkParts(mask, w, h, n)
	if len(parts) != n {
		return false
	}
	ok := 0
	for i, p := range parts {
		d := int(s[i] - '0')
		t := tightFromMask(mask, w, p.x, p.y, p.x+p.w-1, p.y+p.h-1, d)
		if tmplTooBig(t, region.W, region.H) {
			continue
		}
		if t.H < 7 && t.H*100 < region.H*18 {
			continue
		}
		t.Img = cropImage(src, image.Rect(p.x, p.y, p.x+p.w, p.y+p.h))
		if t.usable() {
			bank.learn(d, t)
			ok++
		}
	}
	return ok == n
}

func tightTmpl(im *bgra, mask []bool, w, h, d int) digitTmpl {
	if mask == nil && im != nil {
		mask, w, h = inkMaskMode(im, inkOutlined)
		if countOn(mask) < 4 {
			mask, w, h = inkMaskMode(im, inkYellow)
		}
	}
	if mask == nil || w < 1 || h < 1 {
		return digitTmpl{Digit: d}
	}
	t := tightFromMask(mask, w, 0, 0, w-1, h-1, d)
	if im != nil && t.W > 0 {
		minX, minY, maxX, maxY := inkBBox(mask, w, h)
		if maxX >= minX {
			t.Img = cropImage(im, image.Rect(minX, minY, maxX+1, maxY+1))
		} else {
			t.Img = cloneBGRA(im)
		}
	}
	return t
}

func countOn(mask []bool) int {
	n := 0
	for _, v := range mask {
		if v {
			n++
		}
	}
	return n
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

func splitInkParts(mask []bool, w, h, n int) []tmplHit {
	if n < 1 {
		return nil
	}
	minX, minY, maxX, maxY := inkBBox(mask, w, h)
	if maxX < minX {
		return nil
	}
	bw := maxX - minX + 1
	bh := maxY - minY + 1
	if n == 1 {
		return []tmplHit{{x: minX, y: minY, w: bw, h: bh}}
	}
	if bw < n*3 {
		return nil
	}
	hist := make([]int, bw)
	peak := 0
	for x := 0; x < bw; x++ {
		c := 0
		for y := minY; y <= maxY; y++ {
			if mask[y*w+(minX+x)] {
				c++
			}
		}
		hist[x] = c
		if c > peak {
			peak = c
		}
	}
	runs := projectionRuns(hist, peak)
	if len(runs) != n {
		// 只在已经裁到数字带、墨迹比较完整时才等分。
		if bh >= 6 && bw >= n*4 {
			runs = equalRuns(bw, n)
		}
	}
	if len(runs) != n {
		return nil
	}
	out := make([]tmplHit, 0, n)
	for _, r := range runs {
		x0 := minX + r.x0
		x1 := minX + r.x1
		nb := tightRect(mask, w, x0, minY, x1, maxY)
		if nb.w < 1 || nb.h < 1 {
			return nil
		}
		out = append(out, nb)
	}
	return out
}

func equalRuns(bw, n int) []struct{ x0, x1 int } {
	if n < 1 || bw < n {
		return nil
	}
	out := make([]struct{ x0, x1 int }, n)
	for i := 0; i < n; i++ {
		out[i].x0 = bw * i / n
		out[i].x1 = bw*(i+1)/n - 1
		if out[i].x1 < out[i].x0 {
			out[i].x1 = out[i].x0
		}
	}
	return out
}

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

func projectionRuns(hist []int, peak int) []struct{ x0, x1 int } {
	if peak < 3 || len(hist) < 4 {
		return nil
	}
	type run struct{ x0, x1 int }
	var runs []run
	in := false
	start := 0
	for x := 0; x <= len(hist); x++ {
		on := x < len(hist) && hist[x] > 1
		if on && !in {
			start = x
			in = true
		} else if !on && in {
			runs = append(runs, run{start, x - 1})
			in = false
		}
	}
	out := make([]struct{ x0, x1 int }, 0, len(runs))
	for _, r := range runs {
		if r.x1-r.x0+1 >= 2 {
			out = append(out, struct{ x0, x1 int }{r.x0, r.x1})
		}
	}
	if len(out) >= 2 {
		return out
	}
	return nil
}

func renderDigitOn(im *bgra, d, originX, originY, scale int) {
	if d < 0 || d > 9 || scale < 1 {
		return
	}
	src := builtin5x7[d]
	for y := 0; y < 7; y++ {
		for x := 0; x < 5; x++ {
			if src[y*5+x] != '1' {
				continue
			}
			for dy := 0; dy < scale; dy++ {
				for dx := 0; dx < scale; dx++ {
					px := originX + x*scale + dx
					py := originY + y*scale + dy
					if px < 0 || py < 0 || px >= im.W || py >= im.H {
						continue
					}
					i := py*im.Stride + px*4
					im.Pix[i] = 255
					im.Pix[i+1] = 255
					im.Pix[i+2] = 255
					im.Pix[i+3] = 255
				}
			}
		}
	}
}

func renderNumber(im *bgra, n, originX, originY, scale int) {
	s := strconv.Itoa(n)
	x := originX
	for _, ch := range s {
		renderDigitOn(im, int(ch-'0'), x, originY, scale)
		x += 6 * scale
	}
}
