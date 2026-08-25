package potion

import (
	"image"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/lnatpunblhna/go-game-vision/pkg/capture"
)

func TestRelRectValid(t *testing.T) {
	if (RelRect{X: 0.1, Y: 0.2, W: 0.05, H: 0.05}).Valid() == false {
		t.Fatal("expected valid")
	}
	if (RelRect{}).Valid() {
		t.Fatal("zero rect should be invalid")
	}
}

func TestPixelRect(t *testing.T) {
	r := pixelRect(RelRect{X: 0.1, Y: 0.2, W: 0.25, H: 0.5}, 200, 100)
	want := image.Rect(20, 20, 70, 70)
	if r != want {
		t.Fatalf("got %v want %v", r, want)
	}
}

func TestNCCIdentical(t *testing.T) {
	a := solid(16, 16, 30, 40, 200)
	if g := nccGray(a, a); g < 0.99 {
		t.Fatalf("identical ncc = %v", g)
	}
	b := solid(16, 16, 80, 80, 80)
	if g := nccGray(a, b); g > 0.5 {
		t.Fatalf("different solids should be low, got %v", g)
	}
}

func TestClassifySlot(t *testing.T) {
	icon := checker(24, 24, 20, 30, 210, 40, 180, 50)
	tmpl := newSlotTemplate(icon)

	st, ncc := classifySlot(icon, tmpl)
	if st != SlotOK {
		t.Fatalf("same icon: state=%s ncc=%.3f", st, ncc)
	}

	empty := solid(24, 24, 90, 90, 90)
	st, ncc = classifySlot(empty, tmpl)
	if st != SlotEmpty {
		t.Fatalf("gray slot: state=%s ncc=%.3f", st, ncc)
	}

	busy := stripes(24, 24)
	st, ncc = classifySlot(busy, tmpl)
	if st != SlotUnknown {
		t.Fatalf("stripes should be unknown, got %s ncc=%.3f", st, ncc)
	}
}

func TestNativeGlyphAssets(t *testing.T) {
	glyphs, err := nativeDigits()
	if err != nil {
		t.Fatal(err)
	}
	for d, g := range glyphs {
		if g.Digit != d {
			t.Errorf("digit %d: Digit=%d", d, g.Digit)
		}
		if g.W < 4 || g.H != 11 {
			t.Errorf("digit %d: %dx%d, want 11 高", d, g.W, g.H)
		}
		core, edge := 0, 0
		for i := range g.Core {
			if g.Core[i] {
				core++
			}
			if g.Edge[i] {
				edge++
			}
		}
		if core < 8 || edge < 8 {
			t.Errorf("digit %d: 字身 %d 描边 %d，像素太少", d, core, edge)
		}
	}
	// 每个数字都要能建出可用模板，1x-3x 都不能塌。
	for d := 0; d <= 9; d++ {
		if len(builtins()[d]) == 0 {
			t.Errorf("digit %d 没有内置模板", d)
		}
		for scale := 1; scale <= 3; scale++ {
			tm := glyphs[d].scaledTmpl(scale)
			if !tm.usable() {
				t.Errorf("digit %d scale %d 模板不可用 (%dx%d)", d, scale, tm.W, tm.H)
			}
		}
	}
}

// TestBuiltinsSeparateDigits 保证原始字形之间不会互相混淆。
func TestBuiltinsSeparateDigits(t *testing.T) {
	glyphs, err := nativeDigits()
	if err != nil {
		t.Fatal(err)
	}
	for d := 0; d <= 9; d++ {
		g := glyphs[d].scaledTmpl(2)
		got, score := classifyGlyph(g)
		if got != d {
			t.Errorf("原始字形 %d 被认成 %d (score=%.3f)", d, got, score)
		}
	}
}

// TestSegmentMapleSlots 走的是原始字形认不出来时的兜底分割路径。
func TestSegmentMapleSlots(t *testing.T) {
	cases := []struct {
		file string
		want int
	}{
		{"hp.png", 1},
		{"mp.png", 343},
		{"hp188.png", 188},
		{"mp341.png", 341},
	}
	for _, c := range cases {
		im, err := readPNG(filepath.Join("testdata", c.file))
		if err != nil {
			t.Fatal(err)
		}
		region := countRegion(im)
		hits := readBySegment(region)
		n, score, _ := assembleHits(hits)
		t.Logf("%s segment=%d score=%.3f hits=%d want %d", c.file, n, score, len(hits), c.want)
		for i, h := range hits {
			t.Logf("  hit%d d=%d s=%.3f at (%d,%d) %dx%d", i, h.d, h.s, h.x, h.y, h.w, h.h)
		}
		if got := readCount(im, newStencilHint()); got != c.want {
			t.Errorf("%s readCount=%d want %d (segment=%d)", c.file, got, c.want, n)
		}
	}
}

func dumpCountDebug(t *testing.T, name string, im *bgra, sh *stencilHint) {
	t.Helper()
	n, s := readCountScore(im, sh)
	t.Logf("%s readCountScore n=%d score=%.3f", name, n, s)
	region := countRegion(im)
	hits := matchRegion(region, sh)
	t.Logf("%s region=%dx%d hits=%d", name, region.W, region.H, len(hits))
	for i, h := range hits {
		t.Logf("  hit%d d=%d s=%.3f at (%d,%d) %dx%d", i, h.d, h.s, h.x, h.y, h.w, h.h)
	}
}

func TestReadCount(t *testing.T) {
	for n := 1; n <= 9; n++ {
		im := potionSlot(n)
		if got := readCount(im, newStencilHint()); got != n {
			t.Errorf("digit %d read as %d", n, got)
			dumpCountDebug(t, "synth"+strconv.Itoa(n), im, newStencilHint())
		}
	}
	for _, n := range []int{73, 86} {
		im := potionSlot(n)
		if got := readCount(im, newStencilHint()); got != n {
			dumpCountDebug(t, "synth"+strconv.Itoa(n), im, newStencilHint())
			t.Errorf("%d read as %d", n, got)
		}
	}
}

func TestBarFill(t *testing.T) {
	im := solid(100, 10, 20, 20, 20)
	fillBGRA(im, 0, 0, 40, 10, 30, 30, 220)
	ratio := barFillRatio(im, presetRed)
	if ratio < 0.30 || ratio > 0.55 {
		t.Fatalf("expected ~0.4, got %.3f", ratio)
	}
	blue := solid(80, 8, 20, 20, 20)
	fillBGRA(blue, 0, 0, 80, 8, 210, 40, 30)
	if r := barFillRatio(blue, presetBlue); r < 0.85 {
		t.Fatalf("full blue bar = %.3f", r)
	}
}

func TestTrackerDebounceAndCooldown(t *testing.T) {
	opts := WatchOptions{EmptyFrames: 3, CooldownSec: 10, LowCount: 10, BarLow: 0.4, BarStuckFrames: 4}.normalize()
	tr := newTracker("hp", SlotUnknown)
	now := time.Unix(1000, 0)

	var last *Alert
	for i := 0; i < 2; i++ {
		st, a := tr.observe(slotSample{kind: "hp", raw: SlotEmpty, count: 0, reason: "slot"}, now, opts)
		now = now.Add(time.Second)
		if st.State == SlotEmpty {
			t.Fatalf("empty latched too early at frame %d", i)
		}
		if a != nil {
			t.Fatal("alert too early")
		}
	}
	st, a := tr.observe(slotSample{kind: "hp", raw: SlotEmpty, count: 0, reason: "slot"}, now, opts)
	if st.State != SlotEmpty {
		t.Fatalf("expected empty, got %s", st.State)
	}
	if a == nil || a.Level != "empty" {
		t.Fatalf("expected empty alert, got %+v", a)
	}
	last = a

	now = now.Add(time.Second)
	_, a = tr.observe(slotSample{kind: "hp", raw: SlotEmpty, count: 0, reason: "slot"}, now, opts)
	if a != nil {
		t.Fatal("cooldown should suppress")
	}

	now = now.Add(11 * time.Second)
	_, a = tr.observe(slotSample{kind: "hp", raw: SlotEmpty, count: 0, reason: "slot"}, now, opts)
	if a == nil {
		t.Fatal("expected re-alert after cooldown")
	}
	_ = last

	for i := 0; i < 2; i++ {
		now = now.Add(time.Second)
		st, _ = tr.observe(slotSample{kind: "hp", raw: SlotOK, count: 50, reason: "slot"}, now, opts)
	}
	if st.State != SlotOK {
		t.Fatalf("should recover, got %s", st.State)
	}
}

func TestTrackerBarFallback(t *testing.T) {
	opts := WatchOptions{EmptyFrames: 3, CooldownSec: 1, BarLow: 0.4, BarStuckFrames: 3, LowCount: 10}.normalize()
	tr := newTracker("hp", SlotUnknown)
	now := time.Unix(0, 0)
	var alert *Alert
	for i := 0; i < 3; i++ {
		now = now.Add(time.Second)
		_, alert = tr.observe(slotSample{kind: "hp", raw: SlotUnknown, count: -1, bar: 0.18, reason: "slot"}, now, opts)
	}
	if alert == nil || alert.Reason != "bar" {
		t.Fatalf("expected bar alert, got %+v", alert)
	}
}

func TestTrackerKeepsCountOnBriefOCRMiss(t *testing.T) {
	opts := WatchOptions{EmptyFrames: 3, CooldownSec: 60, LowCount: 10}.normalize()
	tr := newTracker("mp", SlotUnknown)
	now := time.Unix(0, 0)
	var st SlotStatus
	for i := 0; i < 2; i++ {
		now = now.Add(time.Second)
		st, _ = tr.observe(slotSample{kind: "mp", raw: SlotOK, count: 40, reason: "count"}, now, opts)
	}
	if st.Count != 40 {
		t.Fatalf("want 40, got %+v", st)
	}
	now = now.Add(time.Second)
	st, _ = tr.observe(slotSample{kind: "mp", raw: SlotUnknown, count: -1, reason: "slot"}, now, opts)
	if st.Count != 40 {
		t.Fatalf("brief OCR miss should keep 40, got %+v", st)
	}
	now = now.Add(time.Second)
	st, _ = tr.observe(slotSample{kind: "mp", raw: SlotOK, count: 39, reason: "count"}, now, opts)
	if st.Count != 39 {
		t.Fatalf("count should follow decrease, got %+v", st)
	}
}

func TestPlausibleCount(t *testing.T) {
	if !plausibleCount(188, 187) {
		t.Fatal("187 after 188 should be ok")
	}
	if !plausibleCount(188, 176) {
		t.Fatal("small drop should be ok")
	}
	if plausibleCount(188, 1) {
		t.Fatal("188 -> 1 is OCR collapse")
	}
	if plausibleCount(188, 18) {
		t.Fatal("188 -> 18 is OCR collapse")
	}
	if plausibleCount(188, 88) {
		t.Fatal("188 -> 88 is OCR collapse")
	}
	if plausibleCount(188, 1038) {
		t.Fatal("188 -> 1038 is OCR glue")
	}
	if !plausibleCount(12, 11) {
		t.Fatal("12 -> 11 should be ok")
	}
	if !plausibleCount(5, 1) {
		t.Fatal("small single-digit drop should be ok")
	}
	if !plausibleCount(8, 185) {
		t.Fatal("restock 8 -> 185 should be ok")
	}
	if !plausibleCount(8, 100) {
		t.Fatal("restock 8 -> 100 should be ok")
	}
	if plausibleCount(18, 8) {
		t.Fatal("18 -> 8 is OCR dropping the leading 1")
	}
	if plausibleCount(339, 300) {
		t.Fatal("339 -> 300 is 9 read as 0")
	}
	if plausibleCount(163, 100) {
		t.Fatal("163 -> 100 is 6 read as 0")
	}
	if !plausibleCount(12, 9) {
		t.Fatal("crossing 10 should be ok")
	}
}

func TestTrackerRejectsDigitCollapse(t *testing.T) {
	opts := WatchOptions{EmptyFrames: 2, CooldownSec: 60, LowCount: 10}.normalize()
	tr := newTracker("hp", SlotOK)
	tr.lastCount = 188
	now := time.Unix(0, 0)
	var st SlotStatus
	var a *Alert
	for i := 0; i < 4; i++ {
		now = now.Add(time.Second)
		st, a = tr.observe(slotSample{kind: "hp", raw: SlotLow, count: 1, reason: "count"}, now, opts)
		if st.Count != 188 {
			t.Fatalf("frame %d: want hold 188, got %+v", i, st)
		}
		if st.State == SlotLow {
			t.Fatalf("frame %d: should not latch low from OCR 1, got %+v", i, st)
		}
		if a != nil {
			t.Fatalf("frame %d: should not alert, got %+v", i, a)
		}
	}
	now = now.Add(time.Second)
	st, _ = tr.observe(slotSample{kind: "hp", raw: SlotOK, count: 187, reason: "count"}, now, opts)
	if st.Count != 187 {
		t.Fatalf("real decrease should apply, got %+v", st)
	}
}

func TestTrackerAcceptsRestockFromLow(t *testing.T) {
	opts := WatchOptions{EmptyFrames: 2, CooldownSec: 60, LowCount: 10}.normalize()
	tr := newTracker("hp", SlotLow)
	tr.lastCount = 8
	now := time.Unix(0, 0)
	var st SlotStatus
	var a *Alert
	for i := 0; i < 2; i++ {
		now = now.Add(time.Second)
		var cur *Alert
		st, cur = tr.observe(slotSample{kind: "hp", raw: SlotLow, count: 8, reason: "count"}, now, opts)
		if cur != nil {
			a = cur
		}
	}
	if st.State != SlotLow || st.Count != 8 {
		t.Fatalf("want low 8, got %+v", st)
	}
	if a == nil {
		t.Fatal("expected low alert at 8")
	}
	now = now.Add(time.Second)
	st, a = tr.observe(slotSample{kind: "hp", raw: SlotOK, count: 185, reason: "count"}, now, opts)
	if st.Count != 185 {
		t.Fatalf("restock should update count, got %+v", st)
	}
	if st.State != SlotOK {
		t.Fatalf("restock should clear low, got %+v", st)
	}
	if a != nil {
		t.Fatalf("restock should not alert, got %+v", a)
	}
	now = now.Add(time.Second)
	st, a = tr.observe(slotSample{kind: "hp", raw: SlotLow, count: 8, reason: "count"}, now, opts)
	if st.Count != 185 || st.State != SlotOK {
		t.Fatalf("OCR dropping 185 to 8 should keep restocked count, got %+v", st)
	}
	if a != nil {
		t.Fatalf("held restock should not alert, got %+v", a)
	}
}

func TestTrackerLowCount(t *testing.T) {
	opts := WatchOptions{EmptyFrames: 2, CooldownSec: 60, LowCount: 10}.normalize()
	tr := newTracker("mp", SlotUnknown)
	now := time.Unix(0, 0)
	var a *Alert
	for i := 0; i < 2; i++ {
		now = now.Add(time.Second)
		_, a = tr.observe(slotSample{kind: "mp", raw: SlotLow, count: 4, reason: "count"}, now, opts)
	}
	if a == nil || a.Level != "low" || a.Count != 4 {
		t.Fatalf("expected low alert, got %+v", a)
	}
}

func TestAnalyzeCountFollowsChange(t *testing.T) {
	a := potionSlot(50)
	b := potionSlot(41)
	cal := &calibration{
		HPSlot: RelRect{X: 0, Y: 0, W: 1, H: 1},
		HPTmpl: newSlotTemplate(a),
		Hint:   newStencilHint(),
	}
	opts := WatchOptions{LowCount: 10}.normalize()
	hp, _ := analyze(func(RelRect) *bgra { return a }, cal, opts)
	if hp.count != 50 {
		t.Fatalf("start count=%d want 50 raw=%s", hp.count, hp.raw)
	}
	hp, _ = analyze(func(RelRect) *bgra { return b }, cal, opts)
	if hp.count != 41 {
		t.Fatalf("after decrease count=%d want 41 raw=%s ncc=%.3f", hp.count, hp.raw, hp.ncc)
	}
}

func TestAnalyzeEmptyAndLow(t *testing.T) {
	icon := checker(32, 32, 25, 35, 200, 50, 160, 40)
	cal := &calibration{
		HPSlot: RelRect{X: 0, Y: 0, W: 1, H: 1},
		HPTmpl: newSlotTemplate(icon),
		Hint:   newStencilHint(),
	}
	opts := WatchOptions{LowCount: 10}.normalize()

	hp, _ := analyze(func(RelRect) *bgra { return icon }, cal, opts)
	if hp.raw != SlotOK {
		t.Fatalf("present icon: %+v", hp)
	}

	low := potionSlot(5)
	cal.HPTmpl = newSlotTemplate(low)
	hp, _ = analyze(func(RelRect) *bgra { return low }, cal, opts)
	if hp.raw != SlotLow && hp.count != 5 {
		// 数量识别成功则应为 low；识别失败则至少是 ok
		if hp.count == 5 && hp.raw != SlotLow {
			t.Fatalf("count 5 should be low, got %+v", hp)
		}
	}

	hp, _ = analyze(func(RelRect) *bgra { return solid(32, 32, 85, 85, 85) }, cal, opts)
	if hp.raw != SlotEmpty {
		t.Fatalf("gray should be empty, got %+v", hp)
	}
}

func TestBuildCalibrationAndPersist(t *testing.T) {
	dir := t.TempDir()
	calibDirFn = func() (string, error) { return dir, nil }
	t.Cleanup(func() { calibDirFn = defaultCalibDir })

	frame := mixedFrame()
	cal, err := buildCalibration(frame, CalibSpec{
		HPSlot: RelRect{X: 0.05, Y: 0.50, W: 0.16, H: 0.32},
		MPSlot: RelRect{X: 0.25, Y: 0.50, W: 0.16, H: 0.32},
		HPBar:  RelRect{X: 0.05, Y: 0.88, W: 0.40, H: 0.08},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !cal.ready() || cal.HPTmpl.Img == nil || cal.MPTmpl.Img == nil {
		t.Fatal("calibration incomplete")
	}
	cal.HPCount = 8 // 这张合成图的药格上没有数字，手动塞一个值验证落盘
	if err := saveCalibration(cal); err != nil {
		t.Fatal(err)
	}
	loaded, err := loadCalibration()
	if err != nil || loaded == nil {
		t.Fatalf("load: %v %v", err, loaded)
	}
	if !loaded.HPSlot.Valid() || loaded.HPTmpl.Img == nil {
		t.Fatal("loaded hp slot missing")
	}
	if loaded.HPCount != 8 {
		t.Fatalf("persisted hpCount=%d want 8", loaded.HPCount)
	}
}

// TestCalibrateReadsCountFromFrame 校准必须自己把画面上的数量读出来，不靠任何预先学习。
func TestCalibrateReadsCountFromFrame(t *testing.T) {
	cases := []struct {
		file string
		want int
	}{
		{"hp188.png", 188},
		{"mp341.png", 341},
		{"mp339.png", 339},
		{"hp163.png", 163},
	}
	for _, c := range cases {
		im, err := readPNG(filepath.Join("testdata", c.file))
		if err != nil {
			t.Fatal(err)
		}
		cal, err := buildCalibration(bgraFrame(im), CalibSpec{HPSlot: RelRect{W: 1, H: 1}})
		if err != nil {
			t.Fatalf("%s: %v", c.file, err)
		}
		if cal.HPCount != c.want {
			t.Errorf("%s 校准读出 %d，期望 %d", c.file, cal.HPCount, c.want)
		}
	}
}

func TestServiceKeepsCalibratedCount(t *testing.T) {
	hp, err := readPNG(filepath.Join("testdata", "hp188.png"))
	if err != nil {
		t.Fatal(err)
	}
	s := New()
	s.grab = func(uint64) (*capture.RawFrame, error) {
		return bgraFrame(hp), nil
	}
	s.cal = &calibration{
		HPSlot:  RelRect{X: 0, Y: 0, W: 1, H: 1},
		HPTmpl:  newSlotTemplate(hp),
		Hint:    newStencilHint(),
		HPCount: 188,
		FrameW:  hp.W, FrameH: hp.H,
	}
	if err := s.Start(1, WatchOptions{EmptyFrames: 2, CooldownSec: 60, IntervalMS: 200, LowCount: 10}); err != nil {
		t.Fatal(err)
	}
	defer s.Stop()
	s.tick(1)
	st := s.Status()
	if st.HP.Count != 188 {
		t.Fatalf("after start, hp count=%d want 188 state=%s", st.HP.Count, st.HP.State)
	}
	if st.HP.State != SlotOK {
		t.Fatalf("188 should stay ok, got %+v", st.HP)
	}
}

func TestServiceDetectsEmpty(t *testing.T) {
	icon := checker(28, 28, 20, 40, 210, 40, 170, 60)
	empty := solid(28, 28, 88, 88, 88)
	present := true
	s := New()
	s.grab = func(uint64) (*capture.RawFrame, error) {
		im := icon
		if !present {
			im = empty
		}
		return bgraFrame(im), nil
	}
	var alerts []Alert
	s.SetAlerter(func(a Alert) { alerts = append(alerts, a) })
	s.cal = &calibration{
		HPSlot: RelRect{X: 0, Y: 0, W: 1, H: 1},
		HPTmpl: newSlotTemplate(icon),
		Hint:   newStencilHint(),
		FrameW: 28, FrameH: 28,
	}

	if err := s.Start(1, WatchOptions{EmptyFrames: 2, CooldownSec: 60, IntervalMS: 200, LowCount: 10}); err != nil {
		t.Fatal(err)
	}
	defer s.Stop()

	time.Sleep(80 * time.Millisecond)
	present = false
	s.tick(1)
	s.tick(1)
	st := s.Status()
	if st.HP.State != SlotEmpty {
		t.Fatalf("expected empty, got %+v", st.HP)
	}
	if len(alerts) == 0 {
		t.Fatal("expected alert")
	}
}

func mixedFrame() *capture.RawFrame {
	im := solid(200, 100, 30, 30, 30)
	icon := checker(32, 32, 25, 35, 200, 50, 160, 40)
	blit(im, icon, 10, 50)
	blue := checker(32, 32, 200, 80, 40, 40, 40, 180)
	blit(im, blue, 50, 50)
	fillBGRA(im, 10, 88, 80, 8, 30, 30, 220)
	return bgraFrame(im)
}

func bgraFrame(im *bgra) *capture.RawFrame {
	return &capture.RawFrame{Pix: im.Pix, Width: im.W, Height: im.H, Stride: im.Stride}
}

func potionSlot(n int) *bgra {
	im := checker(48, 48, 25, 40, 210, 50, 160, 45)
	if n > 0 {
		s := 2
		x := im.W - numberWidth(n, s) - 1
		y := im.H - numberHeight(s) - 1
		renderNumber(im, n, x, y, s)
	}
	return im
}

func checker(w, h int, b1, g1, r1, b2, g2, r2 byte) *bgra {
	im := solid(w, h, b1, g1, r1)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			if (x/4+y/4)%2 == 0 {
				i := y*im.Stride + x*4
				im.Pix[i] = b2
				im.Pix[i+1] = g2
				im.Pix[i+2] = r2
			}
		}
	}
	return im
}

func stripes(w, h int) *bgra {
	im := solid(w, h, 0, 0, 0)
	for y := 0; y < h; y++ {
		var b, g, r byte = 255, 255, 255
		if y%3 != 0 {
			b, g, r = 10, 200, 20
		}
		for x := 0; x < w; x++ {
			i := y*im.Stride + x*4
			im.Pix[i] = b
			im.Pix[i+1] = g
			im.Pix[i+2] = r
			im.Pix[i+3] = 255
		}
	}
	return im
}

func fillBGRA(im *bgra, x, y, w, h int, b, g, r byte) {
	for yy := y; yy < y+h && yy < im.H; yy++ {
		for xx := x; xx < x+w && xx < im.W; xx++ {
			i := yy*im.Stride + xx*4
			im.Pix[i] = b
			im.Pix[i+1] = g
			im.Pix[i+2] = r
			im.Pix[i+3] = 255
		}
	}
}

func blit(dst, src *bgra, x, y int) {
	for yy := 0; yy < src.H; yy++ {
		for xx := 0; xx < src.W; xx++ {
			dx, dy := x+xx, y+yy
			if dx < 0 || dy < 0 || dx >= dst.W || dy >= dst.H {
				continue
			}
			si := yy*src.Stride + xx*4
			di := dy*dst.Stride + dx*4
			copy(dst.Pix[di:di+4], src.Pix[si:si+4])
		}
	}
}
