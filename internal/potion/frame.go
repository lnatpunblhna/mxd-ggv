package potion

import (
	"fmt"
	"image"
	"math"
	"sync"

	"mxd/internal/wincap"

	"github.com/lnatpunblhna/go-game-vision/pkg/capture"
)

// bgra 是一份独立的 BGRA 像素缓冲。
type bgra struct {
	Pix    []byte
	W, H   int
	Stride int
}

func (im *bgra) empty() bool {
	return im == nil || im.W <= 0 || im.H <= 0 || len(im.Pix) == 0
}

var (
	liveMu   sync.Mutex
	liveCap  wincap.Capturer
	liveHWND uint64
)

func grabFrame(handle uint64) (*capture.RawFrame, error) {
	if handle == 0 {
		return nil, fmt.Errorf("未选择窗口")
	}
	liveMu.Lock()
	defer liveMu.Unlock()
	if liveCap == nil || liveHWND != handle {
		if liveCap != nil {
			_ = liveCap.Close()
			liveCap = nil
		}
		cap, err := wincap.New(handle)
		if err != nil {
			return nil, fmt.Errorf("创建截图会话失败: %w", err)
		}
		liveCap = cap
		liveHWND = handle
	}
	frame, err := liveCap.Capture()
	if err != nil {
		return nil, fmt.Errorf("截图失败: %w", err)
	}
	return frame.Clone(), nil
}

func closeGrabber() {
	liveMu.Lock()
	defer liveMu.Unlock()
	if liveCap != nil {
		_ = liveCap.Close()
		liveCap = nil
		liveHWND = 0
	}
}

func pixelRect(r RelRect, fw, fh int) image.Rectangle {
	r = r.clamp()
	x := int(math.Round(r.X * float64(fw)))
	y := int(math.Round(r.Y * float64(fh)))
	w := int(math.Round(r.W * float64(fw)))
	h := int(math.Round(r.H * float64(fh)))
	if w < 1 {
		w = 1
	}
	if h < 1 {
		h = 1
	}
	return image.Rect(x, y, x+w, y+h).Intersect(image.Rect(0, 0, fw, fh))
}

func cropFrame(frame *capture.RawFrame, r image.Rectangle) *bgra {
	if frame == nil {
		return nil
	}
	r = r.Intersect(image.Rect(0, 0, frame.Width, frame.Height))
	if r.Empty() {
		return nil
	}
	w, h := r.Dx(), r.Dy()
	out := &bgra{Pix: make([]byte, w*h*4), W: w, H: h, Stride: w * 4}
	for y := 0; y < h; y++ {
		src := frame.Pix[(r.Min.Y+y)*frame.Stride+r.Min.X*4:]
		dst := out.Pix[y*out.Stride:]
		copy(dst[:w*4], src[:w*4])
	}
	return out
}

func cropImage(im *bgra, r image.Rectangle) *bgra {
	if im == nil {
		return nil
	}
	r = r.Intersect(image.Rect(0, 0, im.W, im.H))
	if r.Empty() {
		return nil
	}
	w, h := r.Dx(), r.Dy()
	out := &bgra{Pix: make([]byte, w*h*4), W: w, H: h, Stride: w * 4}
	for y := 0; y < h; y++ {
		src := im.Pix[(r.Min.Y+y)*im.Stride+r.Min.X*4:]
		dst := out.Pix[y*out.Stride:]
		copy(dst[:w*4], src[:w*4])
	}
	return out
}

func scaleNearest(im *bgra, dw, dh int) *bgra {
	if im == nil || im.empty() || dw < 1 || dh < 1 {
		return im
	}
	if im.W == dw && im.H == dh {
		return im
	}
	out := &bgra{Pix: make([]byte, dw*dh*4), W: dw, H: dh, Stride: dw * 4}
	for y := 0; y < dh; y++ {
		sy := y * im.H / dh
		src := im.Pix[sy*im.Stride:]
		dst := out.Pix[y*out.Stride:]
		for x := 0; x < dw; x++ {
			sx := x * im.W / dw
			si, di := sx*4, x*4
			dst[di], dst[di+1], dst[di+2], dst[di+3] = src[si], src[si+1], src[si+2], src[si+3]
		}
	}
	return out
}

func luminance(b, g, r byte) float64 {
	return 0.114*float64(b) + 0.587*float64(g) + 0.299*float64(r)
}

func quantityInkAt(im *bgra, x, y int) bool {
	if im == nil || x < 0 || y < 0 || x >= im.W || y >= im.H {
		return false
	}
	i := y*im.Stride + x*4
	h, s, _ := bgraHSV(im.Pix[i], im.Pix[i+1], im.Pix[i+2])
	l := luminance(im.Pix[i], im.Pix[i+1], im.Pix[i+2])
	if isYellowInk(h, s, l) {
		return true
	}
	return isCountInk(h, s, l) && nearDark(im, x, y, l)
}

func nccGray(a, b *bgra) float64 {
	return nccGrayMasked(a, b, false)
}

func nccGraySkipInk(a, b *bgra) float64 {
	v := nccGrayMasked(a, b, true)
	if v > 0 {
		return v
	}
	return nccGrayMasked(a, b, false)
}

func nccGrayMasked(a, b *bgra, skipInk bool) float64 {
	if a == nil || b == nil || a.W != b.W || a.H != b.H || a.W == 0 {
		return 0
	}
	n := a.W * a.H
	var sumA, sumB float64
	la := make([]float64, n)
	lb := make([]float64, n)
	keep := make([]bool, n)
	used := 0
	i := 0
	for y := 0; y < a.H; y++ {
		ap := a.Pix[y*a.Stride:]
		bp := b.Pix[y*b.Stride:]
		for x := 0; x < a.W; x++ {
			ai, bi := x*4, x*4
			la[i] = luminance(ap[ai], ap[ai+1], ap[ai+2])
			lb[i] = luminance(bp[bi], bp[bi+1], bp[bi+2])
			if skipInk && (quantityInkAt(a, x, y) || quantityInkAt(b, x, y)) {
				i++
				continue
			}
			keep[i] = true
			sumA += la[i]
			sumB += lb[i]
			used++
			i++
		}
	}
	if skipInk && used < n*30/100 {
		return 0
	}
	if used < 8 {
		return 0
	}
	n = used
	meanA := sumA / float64(n)
	meanB := sumB / float64(n)
	var num, denA, denB float64
	for i = 0; i < len(keep); i++ {
		if !keep[i] {
			continue
		}
		da := la[i] - meanA
		db := lb[i] - meanB
		num += da * db
		denA += da * da
		denB += db * db
	}
	den := math.Sqrt(denA * denB)
	if den < 1e-9 {
		if math.Abs(meanA-meanB) < 1 {
			return 1
		}
		return 0
	}
	v := num / den
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

func stats(im *bgra) (meanSat, stdLuma float64) {
	if im == nil || im.empty() {
		return 0, 0
	}
	n := im.W * im.H
	lumas := make([]float64, 0, n)
	var sumSat, sumL, sumL2 float64
	var satN int
	for y := 0; y < im.H; y++ {
		row := im.Pix[y*im.Stride:]
		for x := 0; x < im.W; x++ {
			i := x * 4
			_, s, v := bgraHSV(row[i], row[i+1], row[i+2])
			l := luminance(row[i], row[i+1], row[i+2])
			lumas = append(lumas, l)
			sumL += l
			sumL2 += l * l
			if v > 0.12 {
				sumSat += s
				satN++
			}
		}
	}
	if satN > 0 {
		meanSat = sumSat / float64(satN)
	}
	mean := sumL / float64(n)
	v := sumL2/float64(n) - mean*mean
	if v > 0 {
		stdLuma = math.Sqrt(v)
	}
	return meanSat, stdLuma
}

func bgraHSV(b, g, r byte) (h, s, v float64) {
	rf := float64(r) / 255
	gf := float64(g) / 255
	bf := float64(b) / 255
	max := math.Max(rf, math.Max(gf, bf))
	min := math.Min(rf, math.Min(gf, bf))
	v = max
	d := max - min
	if max > 1e-6 {
		s = d / max
	}
	if d < 1e-6 {
		return 0, s, v
	}
	switch max {
	case rf:
		h = 60 * math.Mod((gf-bf)/d, 6)
	case gf:
		h = 60 * ((bf-rf)/d + 2)
	default:
		h = 60 * ((rf-gf)/d + 4)
	}
	if h < 0 {
		h += 360
	}
	return h, s, v
}

func inRange(h, s, v float64, r ColorRange) bool {
	if s < r.SMin || v < r.VMin {
		return false
	}
	hi := int(math.Round(h)) % 360
	if hi < 0 {
		hi += 360
	}
	if r.wraps() {
		return hi >= r.HMin || hi <= r.HMax
	}
	return hi >= r.HMin && hi <= r.HMax
}

func solid(w, h int, b, g, r byte) *bgra {
	im := &bgra{Pix: make([]byte, w*h*4), W: w, H: h, Stride: w * 4}
	for i := 0; i < len(im.Pix); i += 4 {
		im.Pix[i] = b
		im.Pix[i+1] = g
		im.Pix[i+2] = r
		im.Pix[i+3] = 255
	}
	return im
}

func cloneBGRA(im *bgra) *bgra {
	if im == nil {
		return nil
	}
	out := &bgra{Pix: make([]byte, len(im.Pix)), W: im.W, H: im.H, Stride: im.Stride}
	copy(out.Pix, im.Pix)
	return out
}
