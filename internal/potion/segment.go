package potion

import "sort"

const (
	classMin = 0.66
	// antiWeight 是描边扣分的力度：亮点全落在描边上时最多扣掉这么多分。
	antiWeight = 0.55
)

// digitMask 把数量字从药格里抠出来：灰度 + 二值化（描边白字或黄色字）。
func digitMask(im *bgra) ([]bool, int, int) {
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
			if isYellowInk(hh, s, l) {
				mask[y*w+x] = true
				continue
			}
			if !isCountInk(hh, s, l) {
				continue
			}
			if nearDark(im, x, y, l) || nearDarkRad(im, x, y, l, 2) {
				mask[y*w+x] = true
			}
		}
	}
	closeMask(mask, w, h)
	return mask, w, h
}

func nearDarkRad(im *bgra, x, y int, luma float64, radius int) bool {
	if im == nil {
		return false
	}
	for dy := -radius; dy <= radius; dy++ {
		for dx := -radius; dx <= radius; dx++ {
			if dx == 0 && dy == 0 {
				continue
			}
			nx, ny := x+dx, y+dy
			if nx < 0 || ny < 0 || nx >= im.W || ny >= im.H {
				continue
			}
			i := ny*im.Stride + nx*4
			nl := luminance(im.Pix[i], im.Pix[i+1], im.Pix[i+2])
			if nl < 90 || luma-nl >= 70 {
				return true
			}
		}
	}
	return false
}

func closeMask(mask []bool, w, h int) {
	if w < 3 || h < 3 {
		return
	}
	tmp := make([]bool, len(mask))
	copy(tmp, mask)
	for y := 1; y < h-1; y++ {
		for x := 1; x < w-1; x++ {
			if mask[y*w+x] {
				continue
			}
			n := 0
			if mask[y*w+x-1] {
				n++
			}
			if mask[y*w+x+1] {
				n++
			}
			if mask[(y-1)*w+x] {
				n++
			}
			if mask[(y+1)*w+x] {
				n++
			}
			if n >= 3 {
				tmp[y*w+x] = true
			}
		}
	}
	copy(mask, tmp)
}

// readBySegment 按轮廓分割单个数字，再和 0–9 模板做像素匹配。
func readBySegment(im *bgra) []tmplHit {
	mask, w, h := digitMask(im)
	if w < 6 || h < 8 {
		return nil
	}
	parts := segmentDigits(mask, w, h)
	if len(parts) == 0 {
		return nil
	}
	hits := classifyParts(mask, w, parts)
	if !segmentConfident(hits) {
		return nil
	}
	return hits
}

func segmentConfident(hits []tmplHit) bool {
	// 单字容易把瓶身轮廓认成 1/0，只在 2–4 位且每个数字都够像时采用分割结果。
	if len(hits) < 2 || len(hits) > 4 {
		return false
	}
	sum := 0.0
	for _, h := range hits {
		if h.s < classMin {
			return false
		}
		if h.w <= 2 {
			return false
		}
		sum += h.s
	}
	return sum/float64(len(hits)) >= 0.70
}

func segmentDigits(mask []bool, w, h int) []tmplHit {
	minH := 8
	maxH := 18
	if h >= 28 {
		minH = 10
	}
	if h >= 40 {
		minH = 12
	}
	if h < 20 {
		minH = 6
		maxH = h
	}
	if maxH > h {
		maxH = h
	}

	bestScore := -1.0
	var best []tmplHit
	for hh := minH; hh <= maxH; hh++ {
		for y0 := 0; y0+hh <= h; y0++ {
			band := sliceMask(mask, w, 0, y0, w, y0+hh)
			stripFatRows(band, w, hh)
			parts := projectSplit(band, w, hh)
			parts = splitWideParts(band, w, hh, parts)
			parts = keepDigitParts(parts, hh)
			groups := groupParts(parts, hh)
			for _, g := range groups {
				score := scoreDigitParts(g, hh, y0, h)
				if score <= bestScore {
					continue
				}
				bestScore = score
				best = offsetParts(g, 0, y0)
			}
		}
	}
	if len(best) == 0 {
		return nil
	}
	return best
}

func sliceMask(mask []bool, w, x0, y0, x1, y1 int) []bool {
	if x1 > w {
		x1 = w
	}
	bw, bh := x1-x0, y1-y0
	out := make([]bool, bw*bh)
	for y := 0; y < bh; y++ {
		copy(out[y*bw:(y+1)*bw], mask[(y0+y)*w+x0:(y0+y)*w+x0+bw])
	}
	return out
}

func stripFatRows(mask []bool, w, h int) {
	lim := h + h/2
	if lim < 12 {
		lim = 12
	}
	for y := 0; y < h; y++ {
		run, maxRun, ink := 0, 0, 0
		for x := 0; x < w; x++ {
			if mask[y*w+x] {
				run++
				ink++
				if run > maxRun {
					maxRun = run
				}
			} else {
				run = 0
			}
		}
		if maxRun < lim && ink*100 < w*55 {
			continue
		}
		for x := 0; x < w; x++ {
			mask[y*w+x] = false
		}
	}
}

func projectSplit(mask []bool, w, h int) []tmplHit {
	hist := make([]int, w)
	for x := 0; x < w; x++ {
		c := 0
		for y := 0; y < h; y++ {
			if mask[y*w+x] {
				c++
			}
		}
		hist[x] = c
	}
	var runs [][2]int
	in, start := false, 0
	for x := 0; x <= w; x++ {
		on := false
		if x < w && hist[x] > 0 {
			on = true
		} else if x > 0 && x < w-1 && hist[x-1] > 0 && hist[x+1] > 0 {
			// 数字内部 1 像素缝不切断。
			on = true
		}
		if on && !in {
			start = x
			in = true
		} else if !on && in {
			if x-start >= 2 {
				runs = append(runs, [2]int{start, x - 1})
			}
			in = false
		}
	}
	out := make([]tmplHit, 0, len(runs))
	for _, r := range runs {
		nb := tightRect(mask, w, r[0], 0, r[1], h-1)
		if nb.w >= 2 && nb.h >= 6 {
			out = append(out, nb)
		}
	}
	return out
}

func splitWideParts(mask []bool, w, h int, parts []tmplHit) []tmplHit {
	out := make([]tmplHit, 0, len(parts)+2)
	for _, p := range parts {
		if p.w <= h+3 && p.w <= 16 {
			out = append(out, p)
			continue
		}
		subs := splitAtValleys(mask, w, h, p)
		if len(subs) >= 2 {
			out = append(out, subs...)
			continue
		}
		out = append(out, p)
	}
	return out
}

func splitAtValleys(mask []bool, w, h int, p tmplHit) []tmplHit {
	hist := make([]int, p.w)
	peak := 0
	for x := 0; x < p.w; x++ {
		c := 0
		for y := 0; y < p.h; y++ {
			if mask[(p.y+y)*w+(p.x+x)] {
				c++
			}
		}
		hist[x] = c
		if c > peak {
			peak = c
		}
	}
	if peak < 3 {
		return nil
	}
	lo := p.w * 30 / 100
	hi := p.w * 70 / 100
	if hi <= lo+1 {
		return nil
	}
	bestX, bestV := -1, peak+1
	for x := lo; x <= hi; x++ {
		v := hist[x]
		if x > 0 && hist[x-1] < v {
			v = hist[x-1]
		}
		if x+1 < p.w && hist[x+1] < v {
			v = hist[x+1]
		}
		if v < bestV {
			bestV = v
			bestX = x
		}
	}
	if bestX < 2 || bestX > p.w-3 {
		return nil
	}
	if bestV > peak*40/100 && bestV > 2 {
		return nil
	}
	left := tightRect(mask, w, p.x, p.y, p.x+bestX-1, p.y+p.h-1)
	right := tightRect(mask, w, p.x+bestX+1, p.y, p.x+p.w-1, p.y+p.h-1)
	if left.w < 2 || left.h < 6 || right.w < 2 || right.h < 6 {
		return nil
	}
	return []tmplHit{left, right}
}

func keepDigitParts(parts []tmplHit, bandH int) []tmplHit {
	if len(parts) == 0 {
		return nil
	}
	maxH := 0
	for _, p := range parts {
		if p.h > maxH {
			maxH = p.h
		}
	}
	minH := maxH * 78 / 100
	if minH < bandH*55/100 {
		minH = bandH * 55 / 100
	}
	if minH < 6 {
		minH = 6
	}
	out := parts[:0]
	for _, p := range parts {
		if p.h < minH {
			continue
		}
		if p.w < 2 {
			continue
		}
		if p.w*100 > p.h*110 && p.w > 12 {
			continue
		}
		ar := float64(p.h) / float64(p.w)
		if ar < 0.55 || ar > 9 {
			continue
		}
		out = append(out, p)
	}
	if len(out) == 0 {
		return nil
	}
	sort.Slice(out, func(i, j int) bool { return out[i].x < out[j].x })
	return mergeCloseParts(out)
}

func groupParts(parts []tmplHit, bandH int) [][]tmplHit {
	if len(parts) == 0 {
		return nil
	}
	gapLim := bandH / 3
	if gapLim < 3 {
		gapLim = 3
	}
	var groups [][]tmplHit
	cur := []tmplHit{parts[0]}
	for i := 1; i < len(parts); i++ {
		gap := parts[i].x - cur[len(cur)-1].maxX()
		if gap <= gapLim && len(cur) < 4 {
			cur = append(cur, parts[i])
			continue
		}
		groups = append(groups, cur)
		cur = []tmplHit{parts[i]}
	}
	groups = append(groups, cur)
	return groups
}

func mergeCloseParts(parts []tmplHit) []tmplHit {
	if len(parts) <= 1 {
		return parts
	}
	out := []tmplHit{parts[0]}
	for i := 1; i < len(parts); i++ {
		p := parts[i]
		prev := &out[len(out)-1]
		gap := p.x - prev.maxX()
		if gap < 0 {
			gap = 0
		}
		if absInt(p.h-prev.h) > 4 {
			out = append(out, p)
			continue
		}
		small, large := prev.w, p.w
		if small > large {
			small, large = large, small
		}
		maxH := prev.h
		if p.h > maxH {
			maxH = p.h
		}
		comb := prev.w + p.w + gap
		// 1 和 8 这类完整数字不要粘回去。
		looksPair := small <= 4 && large >= 8 && large*100 >= maxH*50
		// 两个完整数字（3 和 9）不要粘回去。
		shouldMerge := !looksPair && small <= 4 && gap <= 3 && comb <= maxH+4
		if !shouldMerge && gap <= 2 && small <= 3 && large <= 8 {
			shouldMerge = true
		}
		if shouldMerge {
			x0 := prev.x
			if p.x < x0 {
				x0 = p.x
			}
			y0 := prev.y
			if p.y < y0 {
				y0 = p.y
			}
			x1 := prev.maxX()
			if p.maxX() > x1 {
				x1 = p.maxX()
			}
			y1 := prev.maxY()
			if p.maxY() > y1 {
				y1 = p.maxY()
			}
			*prev = tmplHit{x: x0, y: y0, w: x1 - x0 + 1, h: y1 - y0 + 1}
			continue
		}
		out = append(out, p)
	}
	return out
}

func scoreDigitParts(parts []tmplHit, bandH, y0, imgH int) float64 {
	if len(parts) == 0 {
		return -1
	}
	maxH, minH, sumW, thin := 0, parts[0].h, 0, 0
	for i, p := range parts {
		if p.h > maxH {
			maxH = p.h
		}
		if p.h < minH {
			minH = p.h
		}
		sumW += p.w
		if p.w <= 3 && i > 0 && i < len(parts)-1 {
			thin++
		} else if p.w <= 3 {
			thin++
		}
	}
	if maxH > 0 && minH*100 < maxH*75 {
		return -1
	}
	n := float64(len(parts))
	bonus := 0.0
	switch len(parts) {
	case 3:
		bonus = 3.2
	case 2:
		bonus = 2.4
	case 4:
		bonus = 1.2
	case 1:
		bonus = 0.3
	}
	avgW := float64(sumW) / n
	if avgW >= 4 && avgW <= 16 {
		bonus += 0.8
	}
	if thin > 0 && len(parts) >= 2 {
		bonus -= float64(thin) * 1.8
	}
	pos := float64(y0) / float64(imgH)
	if pos >= 0.30 && pos <= 0.72 {
		bonus += 1.0
	} else if pos < 0.16 {
		bonus -= 2.5
	}
	return n*2.0 + bonus + float64(maxH)*0.12 + float64(maxH)/float64(bandH)
}

func offsetParts(parts []tmplHit, dx, dy int) []tmplHit {
	out := make([]tmplHit, len(parts))
	for i, p := range parts {
		p.x += dx
		p.y += dy
		out[i] = p
	}
	return out
}

func classifyParts(mask []bool, w int, parts []tmplHit) []tmplHit {
	hits := make([]tmplHit, 0, len(parts))
	for _, p := range parts {
		g := tightFromMask(mask, w, p.x, p.y, p.x+p.w-1, p.y+p.h-1, 0)
		g.ensure()
		if g.H < 6 || g.NInk < 4 || g.W < 2 {
			return nil
		}
		d, s := classifyGlyph(g)
		if d == 1 && g.W*2 > g.H {
			s = 0
			d = -1
		}
		if d < 0 || s < classMin {
			return nil
		}
		hits = append(hits, tmplHit{x: p.x, y: p.y, w: p.w, h: p.h, d: d, s: s})
	}
	return hits
}

func classifyGlyph(g digitTmpl) (digit int, score float64) {
	g.ensure()
	bestD, bestS := -1, -1.0
	for d := 0; d <= 9; d++ {
		best := 0.0
		for _, t := range builtins()[d] {
			if s := glyphScore(g, t); s > best {
				best = s
			}
		}
		if best < 0.40 {
			continue
		}
		best += topologyBonus(g, d)
		if best > bestS {
			bestS = best
			bestD = d
		}
	}
	if bestS > 1 {
		bestS = 1
	}
	if bestS < 0 {
		bestS = 0
	}
	return bestD, bestS
}

func bottomLR(g digitTmpl) (left, right int) {
	g.ensure()
	if g.W < 3 || g.H < 8 {
		return 0, 0
	}
	y0 := g.H * 68 / 100
	mid := g.W / 2
	for y := y0; y < g.H; y++ {
		for x := 0; x < g.W; x++ {
			if !g.Bits[y*g.W+x] {
				continue
			}
			if x < mid {
				left++
			} else {
				right++
			}
		}
	}
	return
}

func topologyBonus(g digitTmpl, d int) float64 {
	left, right := bottomLR(g)
	switch d {
	case 9:
		if right >= left*2 && right >= 4 {
			return 0.16
		}
		if left >= right && left >= 4 {
			return -0.16
		}
	case 0:
		if right >= left*2 && right >= 4 {
			return -0.20
		}
		if left >= 3 && right >= 3 {
			return 0.06
		}
	case 6:
		if left >= right*2 && left >= 4 {
			return 0.10
		}
		if right >= left*2 && right >= 4 {
			return -0.12
		}
	}
	return 0
}

func glyphScore(a, b digitTmpl) float64 {
	a.ensure()
	b.ensure()
	if a.NInk < 3 || b.NInk < 3 {
		return 0
	}
	best := bitsDice(a.Bits, a.W, a.H, b.Bits, b.W, b.H, b.Anti)
	if a.W != b.W || a.H != b.H {
		ra := resizeBits(a.Bits, a.W, a.H, b.W, b.H)
		if s := bitsDice(ra, b.W, b.H, b.Bits, b.W, b.H, b.Anti); s > best {
			best = s
		}
		rb := resizeBits(b.Bits, b.W, b.H, a.W, a.H)
		ranti := resizeBits(b.Anti, b.W, b.H, a.W, a.H)
		if s := bitsDice(a.Bits, a.W, a.H, rb, a.W, a.H, ranti); s > best {
			best = s
		}
	}
	// 1 偏瘦，避免被 4/7 的竖笔抢走。
	if a.W*2 < a.H && b.Digit != 1 && b.W*2 >= b.H {
		best -= 0.12
	}
	if b.Digit == 1 && a.W*5 > a.H*3 {
		best -= 0.10
	}
	if best < 0 {
		return 0
	}
	if best > 1 {
		return 1
	}
	return best
}

// bitsDice 算两张点阵的 Dice 相似度，并在若干位移里取最好的一档。
// bAnti 是 b 的描边（可为 nil）：a 在描边上亮起来，说明那儿是背景杂色，按比例扣分。
func bitsDice(a []bool, aw, ah int, b []bool, bw, bh int, bAnti []bool) float64 {
	if aw <= 0 || ah <= 0 || bw <= 0 || bh <= 0 {
		return 0
	}
	if len(bAnti) != bw*bh {
		bAnti = nil
	}
	nAnti := 0
	for _, on := range bAnti {
		if on {
			nAnti++
		}
	}
	w, h := aw, ah
	if bw > w {
		w = bw
	}
	if bh > h {
		h = bh
	}
	best := 0.0
	maxDx, maxDy := 2, 2
	if w > 16 {
		maxDx = 3
	}
	for dy := -maxDy; dy <= maxDy; dy++ {
		for dx := -maxDx; dx <= maxDx; dx++ {
			inter, na, nb, bleed := 0, 0, 0, 0
			for y := 0; y < h; y++ {
				ay, by := y, y-dy
				for x := 0; x < w; x++ {
					ax, bx := x, x-dx
					av := ay >= 0 && ay < ah && ax >= 0 && ax < aw && a[ay*aw+ax]
					inB := by >= 0 && by < bh && bx >= 0 && bx < bw
					bv := inB && b[by*bw+bx]
					if av {
						na++
						if bAnti != nil && inB && bAnti[by*bw+bx] {
							bleed++
						}
					}
					if bv {
						nb++
					}
					if av && bv {
						inter++
					}
				}
			}
			if na+nb == 0 {
				continue
			}
			s := float64(2*inter) / float64(na+nb)
			if nAnti > 0 {
				s -= antiWeight * float64(bleed) / float64(nAnti)
			}
			if s > best {
				best = s
			}
		}
	}
	return best
}

func resizeBits(src []bool, sw, sh, dw, dh int) []bool {
	if sw <= 0 || sh <= 0 || dw <= 0 || dh <= 0 || len(src) != sw*sh {
		return nil
	}
	if sw == dw && sh == dh {
		out := make([]bool, len(src))
		copy(out, src)
		return out
	}
	out := make([]bool, dw*dh)
	for y := 0; y < dh; y++ {
		sy := y * sh / dh
		if sy >= sh {
			sy = sh - 1
		}
		for x := 0; x < dw; x++ {
			sx := x * sw / dw
			if sx >= sw {
				sx = sw - 1
			}
			out[y*dw+x] = src[sy*sw+sx]
		}
	}
	return out
}
