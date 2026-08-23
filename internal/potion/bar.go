package potion

import (
	"math"
	"sort"
)

func sampleBarColor(im *bgra, fallback ColorRange) ColorRange {
	if im == nil || im.empty() {
		return fallback
	}
	limit := im.W / 4
	if limit < 2 {
		limit = im.W
	}
	var hs, ss, vs []float64
	for y := 0; y < im.H; y++ {
		row := im.Pix[y*im.Stride:]
		for x := 0; x < limit; x++ {
			i := x * 4
			h, s, v := bgraHSV(row[i], row[i+1], row[i+2])
			if s < 0.20 || v < 0.18 {
				continue
			}
			hs = append(hs, h)
			ss = append(ss, s)
			vs = append(vs, v)
		}
	}
	if len(hs) < 8 {
		return fallback
	}
	sort.Float64s(hs)
	sort.Float64s(ss)
	sort.Float64s(vs)
	mh := hs[len(hs)/2]
	ms := ss[len(ss)/2]
	mv := vs[len(vs)/2]
	span := 22.0
	hmin := int(math.Round(mh - span))
	hmax := int(math.Round(mh + span))
	if hmin < 0 {
		hmin += 360
	}
	if hmax >= 360 {
		hmax -= 360
	}
	smin := math.Max(0.18, ms*0.45)
	vmin := math.Max(0.16, mv*0.40)
	return ColorRange{HMin: hmin, HMax: hmax, SMin: smin, VMin: vmin}
}

func barFillRatio(im *bgra, rng ColorRange) float64 {
	if im == nil || im.empty() || !rng.valid() {
		return -1
	}
	filled := 0
	for x := 0; x < im.W; x++ {
		hit := 0
		for y := 0; y < im.H; y++ {
			i := y*im.Stride + x*4
			h, s, v := bgraHSV(im.Pix[i], im.Pix[i+1], im.Pix[i+2])
			if inRange(h, s, v, rng) {
				hit++
			}
		}
		if float64(hit)/float64(im.H) >= 0.22 {
			filled++
		}
	}
	return float64(filled) / float64(im.W)
}
