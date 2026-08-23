package potion

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"image/jpeg"

	"github.com/lnatpunblhna/go-game-vision/pkg/capture"
)

type calibration struct {
	HPSlot RelRect
	MPSlot RelRect
	HPBar  RelRect
	MPBar  RelRect

	HPTmpl slotTemplate
	MPTmpl slotTemplate
	HPCol  ColorRange
	MPCol  ColorRange

	FrameW  int
	FrameH  int
	Digits  *digitBank
	HPCount int
	MPCount int
}

func (c *calibration) view() CalibrationView {
	if c == nil {
		return CalibrationView{}
	}
	v := CalibrationView{
		HPSlot:        c.HPSlot,
		MPSlot:        c.MPSlot,
		HPBar:         c.HPBar,
		MPBar:         c.MPBar,
		HasHPSlot:     c.HPSlot.Valid() && c.HPTmpl.Img != nil,
		HasMPSlot:     c.MPSlot.Valid() && c.MPTmpl.Img != nil,
		HasHPBar:      c.HPBar.Valid(),
		HasMPBar:      c.MPBar.Valid(),
		FrameW:        c.FrameW,
		FrameH:        c.FrameH,
		HPCount:       c.HPCount,
		MPCount:       c.MPCount,
		LearnedDigits: c.Digits.coverage(),
	}
	if c.HPTmpl.Img != nil {
		v.HPPreview = jpegThumb(c.HPTmpl.Img)
	}
	if c.MPTmpl.Img != nil {
		v.MPPreview = jpegThumb(c.MPTmpl.Img)
	}
	return v
}

func (c *calibration) ready() bool {
	if c == nil {
		return false
	}
	return (c.HPSlot.Valid() && c.HPTmpl.Img != nil) ||
		(c.MPSlot.Valid() && c.MPTmpl.Img != nil) ||
		c.HPBar.Valid() || c.MPBar.Valid()
}

func buildCalibration(frame *capture.RawFrame, spec CalibSpec) (*calibration, error) {
	return buildCalibrationFrom(frame, spec, nil)
}

func buildCalibrationFrom(frame *capture.RawFrame, spec CalibSpec, prev *digitBank) (*calibration, error) {
	if frame == nil || frame.Width <= 0 || frame.Height <= 0 {
		return nil, fmt.Errorf("截图像素无效")
	}
	spec.HPSlot = spec.HPSlot.clamp()
	spec.MPSlot = spec.MPSlot.clamp()
	spec.HPBar = spec.HPBar.clamp()
	spec.MPBar = spec.MPBar.clamp()
	if !spec.HPSlot.Valid() && !spec.MPSlot.Valid() && !spec.HPBar.Valid() && !spec.MPBar.Valid() {
		return nil, fmt.Errorf("请至少框选一个药槽或血蓝条")
	}

	digits := newDigitBank()
	digits.mergeFrom(prev)
	c := &calibration{
		HPSlot:  spec.HPSlot,
		MPSlot:  spec.MPSlot,
		HPBar:   spec.HPBar,
		MPBar:   spec.MPBar,
		HPCol:   presetRed,
		MPCol:   presetBlue,
		FrameW:  frame.Width,
		FrameH:  frame.Height,
		Digits:  digits,
		HPCount: spec.HPCount,
		MPCount: spec.MPCount,
	}

	if spec.HPSlot.Valid() {
		im := cropFrame(frame, pixelRect(spec.HPSlot, frame.Width, frame.Height))
		if im == nil || im.empty() {
			return nil, fmt.Errorf("血药格截取失败")
		}
		c.HPTmpl = newSlotTemplate(im)
		n := spec.HPCount
		if n <= 0 {
			n = readCount(im, c.Digits)
		}
		if n > 0 {
			learnCount(im, n, c.Digits)
		}
	}
	if spec.MPSlot.Valid() {
		im := cropFrame(frame, pixelRect(spec.MPSlot, frame.Width, frame.Height))
		if im == nil || im.empty() {
			return nil, fmt.Errorf("蓝药格截取失败")
		}
		c.MPTmpl = newSlotTemplate(im)
		n := spec.MPCount
		if n <= 0 {
			n = readCount(im, c.Digits)
		}
		if n > 0 {
			learnCount(im, n, c.Digits)
		}
	}
	if spec.HPBar.Valid() {
		im := cropFrame(frame, pixelRect(spec.HPBar, frame.Width, frame.Height))
		c.HPCol = sampleBarColor(im, presetRed)
	}
	if spec.MPBar.Valid() {
		im := cropFrame(frame, pixelRect(spec.MPBar, frame.Width, frame.Height))
		c.MPCol = sampleBarColor(im, presetBlue)
	}
	return c, nil
}

func jpegThumb(im *bgra) string {
	if im == nil || im.empty() {
		return ""
	}
	max := 56
	dw, dh := im.W, im.H
	if dw > max {
		dh = dh * max / dw
		dw = max
		if dh < 1 {
			dh = 1
		}
	}
	src := scaleNearest(im, dw, dh)
	img := image.NewRGBA(image.Rect(0, 0, src.W, src.H))
	for y := 0; y < src.H; y++ {
		srow := src.Pix[y*src.Stride:]
		drow := img.Pix[y*img.Stride:]
		for x := 0; x < src.W; x++ {
			si, di := x*4, x*4
			drow[di+0] = srow[si+2]
			drow[di+1] = srow[si+1]
			drow[di+2] = srow[si+0]
			drow[di+3] = srow[si+3]
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 70}); err != nil {
		return ""
	}
	return base64.StdEncoding.EncodeToString(buf.Bytes())
}

func analyzeFrame(frame *capture.RawFrame, cal *calibration, opts WatchOptions) (hp, mp slotSample) {
	return analyzeFrameHint(frame, cal, opts, 0, 0)
}

func analyzeFrameHint(frame *capture.RawFrame, cal *calibration, opts WatchOptions, hpHint, mpHint int) (hp, mp slotSample) {
	crop := func(r RelRect) *bgra {
		if frame == nil {
			return nil
		}
		return cropFrame(frame, pixelRect(r, frame.Width, frame.Height))
	}
	return analyzeHint(crop, cal, opts, hpHint, mpHint)
}

func analyze(crop func(RelRect) *bgra, cal *calibration, opts WatchOptions) (hp, mp slotSample) {
	return analyzeHint(crop, cal, opts, 0, 0)
}

func analyzeHint(crop func(RelRect) *bgra, cal *calibration, opts WatchOptions, hpHint, mpHint int) (hp, mp slotSample) {
	hp = slotSample{kind: "hp", raw: SlotAbsent, count: -1, bar: -1}
	mp = slotSample{kind: "mp", raw: SlotAbsent, count: -1, bar: -1}
	if cal == nil {
		return hp, mp
	}

	if cal.HPSlot.Valid() && cal.HPTmpl.Img != nil {
		hp = sampleSlot("hp", crop(cal.HPSlot), cal.HPTmpl, cal.Digits, opts, hpHint)
	}
	if cal.MPSlot.Valid() && cal.MPTmpl.Img != nil {
		mp = sampleSlot("mp", crop(cal.MPSlot), cal.MPTmpl, cal.Digits, opts, mpHint)
	}
	if cal.HPBar.Valid() {
		hp.bar = barFillRatio(crop(cal.HPBar), cal.HPCol)
	}
	if cal.MPBar.Valid() {
		mp.bar = barFillRatio(crop(cal.MPBar), cal.MPCol)
	}
	return hp, mp
}

func sampleSlot(kind string, im *bgra, tmpl slotTemplate, bank *digitBank, opts WatchOptions, hint int) slotSample {
	s := slotSample{kind: kind, raw: SlotAbsent, count: -1, bar: -1, reason: "slot"}
	st, ncc := classifySlot(im, tmpl)
	s.ncc = ncc
	s.raw = st
	n, score := readCountHint(im, bank, hint)
	s.count = n
	// 数量变化会改药格外观，模板 NCC 可能跌到 empty。读到数字就以数量为准。
	if n > 0 {
		s.raw = SlotOK
		s.reason = "count"
		if opts.LowCount > 0 && n <= opts.LowCount {
			s.raw = SlotLow
		}
		// 误读成 8 时不要把 185 的字形学成个位 8。
		if score >= 0.62 && (hint <= 0 || plausibleCount(hint, n) || n > hint) {
			learnCount(im, n, bank)
		}
		return s
	}
	if n == 0 {
		s.raw = SlotEmpty
		s.reason = "count"
		return s
	}
	if st == SlotEmpty {
		s.count = 0
	}
	return s
}
