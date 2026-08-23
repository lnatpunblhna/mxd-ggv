package potion

import "time"

type tracker struct {
	kind           string
	emptyRun       int
	lowRun         int
	okRun          int
	unknownRun     int
	barLowRun      int
	latched        SlotState
	lastBar        float64
	lastAlertAt    time.Time
	lastAlertLvl   string
	hasLastBar     bool
	lastCount      int
	missRun        int
	implausibleRun int
}

func newTracker(kind string, initial SlotState) *tracker {
	return &tracker{kind: kind, latched: initial, lastBar: -1, lastCount: -1}
}

func (t *tracker) observe(s slotSample, now time.Time, opts WatchOptions) (SlotStatus, *Alert) {
	if t.latched == "" {
		t.latched = SlotUnknown
	}
	if s.raw == SlotAbsent {
		if s.bar < 0 {
			t.resetRuns()
			t.latched = SlotAbsent
			return SlotStatus{State: SlotAbsent, Count: -1, Bar: -1}, nil
		}
		s.raw = SlotUnknown
		s.reason = "bar"
	}

	if s.count >= 0 && t.lastCount > 0 && !plausibleCount(t.lastCount, s.count) {
		t.implausibleRun++
		if t.implausibleRun < 8 {
			s.count = t.lastCount
			if s.raw == SlotLow || s.raw == SlotEmpty {
				s.raw = SlotOK
				s.reason = "hold"
			}
		} else {
			t.implausibleRun = 0
		}
	} else if s.count >= 0 {
		t.implausibleRun = 0
	}

	switch s.raw {
	case SlotEmpty:
		t.emptyRun++
		t.lowRun, t.okRun, t.unknownRun = 0, 0, 0
	case SlotLow:
		t.lowRun++
		t.emptyRun, t.okRun, t.unknownRun = 0, 0, 0
	case SlotOK:
		t.okRun++
		t.emptyRun, t.lowRun, t.unknownRun = 0, 0, 0
	default:
		t.unknownRun++
		t.emptyRun, t.lowRun, t.okRun = 0, 0, 0
	}

	need := opts.EmptyFrames
	if need < 1 {
		need = 1
	}

	restocked := s.count > opts.LowCount && (t.latched == SlotLow || t.latched == SlotEmpty)
	switch {
	case t.okRun >= 2 || (t.okRun >= 1 && restocked):
		t.latched = SlotOK
	case t.emptyRun >= need:
		t.latched = SlotEmpty
	case t.lowRun >= need:
		t.latched = SlotLow
	case t.unknownRun >= need && t.latched != SlotEmpty && t.latched != SlotLow:
		t.latched = SlotUnknown
	}

	reason := s.reason
	if t.applyBar(s, opts) {
		reason = "bar"
	}

	count := s.count
	if count >= 0 {
		t.lastCount = count
		t.missRun = 0
	} else if t.latched != SlotEmpty && s.raw != SlotEmpty && t.lastCount > 0 {
		t.missRun++
		if t.missRun <= 3 {
			count = t.lastCount
		}
	} else if t.latched == SlotEmpty {
		t.lastCount = 0
		if count < 0 {
			count = 0
		}
	}

	st := SlotStatus{
		State:  t.latched,
		Count:  count,
		NCC:    s.ncc,
		Bar:    s.bar,
		Reason: reason,
	}

	alert := t.maybeAlert(st, now, opts)
	return st, alert
}

func (t *tracker) applyBar(s slotSample, opts WatchOptions) bool {
	if s.bar < 0 {
		t.barLowRun = 0
		t.hasLastBar = false
		return false
	}
	slotKnown := t.latched == SlotOK || t.latched == SlotLow || t.latched == SlotEmpty
	if slotKnown && t.latched != SlotUnknown {
		t.barLowRun = 0
		t.lastBar = s.bar
		t.hasLastBar = true
		return false
	}

	recovered := t.hasLastBar && s.bar > t.lastBar+0.05
	low := s.bar < opts.BarLow
	if recovered || !low {
		t.barLowRun = 0
	} else {
		t.barLowRun++
	}
	t.lastBar = s.bar
	t.hasLastBar = true

	if t.barLowRun >= opts.BarStuckFrames && (t.latched == SlotUnknown || t.latched == SlotAbsent) {
		t.latched = SlotEmpty
		return true
	}
	return false
}

func (t *tracker) maybeAlert(st SlotStatus, now time.Time, opts WatchOptions) *Alert {
	if st.State != SlotEmpty && st.State != SlotLow {
		t.lastAlertLvl = ""
		return nil
	}
	level := string(st.State)
	cool := time.Duration(opts.CooldownSec) * time.Second
	escalated := t.lastAlertLvl == string(SlotLow) && level == string(SlotEmpty)
	due := t.lastAlertAt.IsZero() || now.Sub(t.lastAlertAt) >= cool || escalated
	if !due {
		return nil
	}
	if t.lastAlertLvl == level && !t.lastAlertAt.IsZero() && now.Sub(t.lastAlertAt) < cool {
		return nil
	}
	t.lastAlertAt = now
	t.lastAlertLvl = level
	return &Alert{
		Kind:   t.kind,
		Level:  level,
		Reason: st.Reason,
		Count:  st.Count,
		At:     now.UnixMilli(),
	}
}

func (t *tracker) resetRuns() {
	t.emptyRun, t.lowRun, t.okRun, t.unknownRun, t.barLowRun = 0, 0, 0, 0, 0
}

func digitLen(n int) int {
	if n < 0 {
		return 0
	}
	if n == 0 {
		return 1
	}
	d := 0
	for n > 0 {
		d++
		n /= 10
	}
	return d
}

// plausibleCount 判断一帧内数量变化是否像真实用药/补货，而不是 OCR 把 188 读成 1、把 185 读成 8。
func plausibleCount(prev, next int) bool {
	if prev <= 0 || next < 0 {
		return true
	}
	if next == prev {
		return true
	}
	if next > prev {
		// 买药/捡药会一次加上几十上百瓶。只拦截三位数再被粘上高位，例如 188→1038。
		if prev >= 100 && digitLen(next) > digitLen(prev) && next > prev*3 {
			return false
		}
		return true
	}
	drop := prev - next
	if digitLen(next) < digitLen(prev) {
		// 12→9 是跨过 10 的正常用药；18→8、185→8 是丢掉了首位。
		if prev <= 19 && drop <= 9 && next > 0 {
			return true
		}
		return false
	}
	if drop <= 12 {
		return true
	}
	// 339→300、163→100 是 9/6 被认成 0，不是一次喝掉几十瓶。
	return false
}
