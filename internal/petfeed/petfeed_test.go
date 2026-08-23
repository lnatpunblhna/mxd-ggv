package petfeed

import (
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

func TestDecayAndFeedMath(t *testing.T) {
	if got := Decay(80); got != 79 {
		t.Fatalf("Decay(80) = %d, want 79", got)
	}
	if got := Decay(0); got != 0 {
		t.Fatalf("Decay(0) = %d, want 0", got)
	}
	if NeedsFeed(70) {
		t.Fatal("70 should not trigger feed")
	}
	if !NeedsFeed(69) {
		t.Fatal("69 should trigger feed")
	}
	if got := AfterFeed(69); got != 99 {
		t.Fatalf("AfterFeed(69) = %d, want 99", got)
	}
	if got := AfterFeed(85); got != 100 {
		t.Fatalf("AfterFeed(85) = %d, want 100", got)
	}
}

func TestPressDeniedMessage(t *testing.T) {
	msg := pressDeniedMessage(false, syscall.Errno(5))
	if !strings.Contains(msg, "以管理员身份运行") {
		t.Fatalf("unelevated message = %q", msg)
	}
	other := pressDeniedMessage(true, errBoom)
	if strings.Contains(other, "以管理员身份运行") {
		t.Fatalf("elevated non-denied should not ask for admin: %q", other)
	}
}

func TestExtendedKey(t *testing.T) {
	if !isExtendedKey(vkInsert) || !isExtendedKey(vkDelete) || !isExtendedKey(vkHome) {
		t.Fatal("Insert/Delete/Home should be extended")
	}
	vk1, err := ParseKey("1")
	if err != nil {
		t.Fatal(err)
	}
	if isExtendedKey(vk1) {
		t.Fatal("digit keys are not extended")
	}
}

func TestParseKey(t *testing.T) {
	cases := map[string]virtualKey{
		"Insert":    vkInsert,
		"insert":    vkInsert,
		"INS":       vkInsert,
		"1":         0x31,
		"Digit1":    0x31,
		"KeyA":      0x41,
		"a":         0x41,
		"F9":        0x78,
		"f12":       0x7B,
		"PageUp":    vkPrior,
		"PgDn":      vkNext,
		"Numpad0":   0x60,
		"Space":     vkSpace,
		"ArrowUp":   vkUp,
		"Delete":    vkDelete,
		"Home":      vkHome,
		"End":       vkEnd,
		"Page Down": vkNext,
	}
	for name, want := range cases {
		got, err := ParseKey(name)
		if err != nil {
			t.Errorf("ParseKey(%q) error: %v", name, err)
			continue
		}
		if got != want {
			t.Errorf("ParseKey(%q) = 0x%02X, want 0x%02X", name, got, want)
		}
	}

	if _, err := ParseKey(""); err == nil {
		t.Error("empty key should fail")
	}
	if _, err := ParseKey("Nope"); err == nil {
		t.Error("unknown key should fail")
	}
	if _, err := ParseKey("F13"); err == nil {
		t.Error("F13 should fail")
	}
}

func TestStartRejectsBadInput(t *testing.T) {
	s := newTestService(nil)
	if err := s.Start(0, 80, "Insert"); err == nil {
		t.Fatal("expected missing window error")
	}
	if err := s.Start(1, -1, "Insert"); err == nil {
		t.Fatal("expected fullness range error")
	}
	if err := s.Start(1, 101, "Insert"); err == nil {
		t.Fatal("expected fullness range error")
	}
	if err := s.Start(1, 80, ""); err == nil {
		t.Fatal("expected empty hotkey error")
	}
}

func TestStartFeedsUntilThreshold(t *testing.T) {
	var mu sync.Mutex
	var presses int
	s := newTestService(func(handle uint64, vk virtualKey) error {
		mu.Lock()
		presses++
		mu.Unlock()
		return nil
	})

	if err := s.Start(1, 10, "Insert"); err != nil {
		t.Fatal(err)
	}
	defer s.Stop()

	waitFor(t, func() bool { return s.Status().Fullness >= FeedThreshold })

	st := s.Status()
	if st.Fullness != 70 {
		t.Fatalf("fullness = %d, want 70", st.Fullness)
	}
	mu.Lock()
	got := presses
	mu.Unlock()
	if got != 2 {
		t.Fatalf("presses = %d, want 2 (10→40→70)", got)
	}
	if st.FeedCount != 2 {
		t.Fatalf("feedCount = %d, want 2", st.FeedCount)
	}
}

func TestNoImmediateFeedWhenFull(t *testing.T) {
	presses := 0
	s := newTestService(func(handle uint64, vk virtualKey) error {
		presses++
		return nil
	})
	if err := s.Start(1, 80, "1"); err != nil {
		t.Fatal(err)
	}
	defer s.Stop()
	time.Sleep(20 * time.Millisecond)
	if presses != 0 {
		t.Fatalf("unexpected immediate feed, presses=%d", presses)
	}
	if s.Status().Fullness != 80 {
		t.Fatalf("fullness = %d, want 80", s.Status().Fullness)
	}
}

func TestDecayThenFeed(t *testing.T) {
	var mu sync.Mutex
	var presses int
	s := newTestService(func(handle uint64, vk virtualKey) error {
		mu.Lock()
		presses++
		mu.Unlock()
		return nil
	})
	if err := s.Start(1, 70, "F9"); err != nil {
		t.Fatal(err)
	}
	defer s.Stop()

	waitFor(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return presses >= 1
	})

	st := s.Status()
	if st.Fullness != 99 {
		t.Fatalf("fullness = %d, want 99 (70-1+30)", st.Fullness)
	}
}

func TestStopPreventsFurtherFeeds(t *testing.T) {
	s := newTestService(func(handle uint64, vk virtualKey) error { return nil })
	if err := s.Start(1, 80, "Insert"); err != nil {
		t.Fatal(err)
	}
	s.Stop()
	if s.Status().Enabled {
		t.Fatal("still enabled after Stop")
	}
	if err := s.Start(1, 80, "Insert"); err != nil {
		t.Fatalf("restart after stop: %v", err)
	}
	s.Stop()
}

func TestDoubleStartRejected(t *testing.T) {
	s := newTestService(func(handle uint64, vk virtualKey) error { return nil })
	if err := s.Start(1, 80, "Insert"); err != nil {
		t.Fatal(err)
	}
	defer s.Stop()
	if err := s.Start(1, 90, "F1"); err == nil {
		t.Fatal("second Start should fail")
	}
}

func TestPressErrorKeepsFullness(t *testing.T) {
	s := newTestService(func(handle uint64, vk virtualKey) error {
		return errBoom
	})
	if err := s.Start(1, 50, "Insert"); err != nil {
		t.Fatal(err)
	}
	defer s.Stop()
	waitFor(t, func() bool { return s.Status().LastError != "" })
	st := s.Status()
	if st.Fullness != 50 {
		t.Fatalf("fullness changed after failed press: %d", st.Fullness)
	}
	if st.FeedCount != 0 {
		t.Fatalf("feedCount = %d, want 0", st.FeedCount)
	}
}

type boomError string

func (e boomError) Error() string { return string(e) }

const errBoom boomError = "boom"

func newTestService(press PressFunc) *Service {
	s := New()
	s.decay = 25 * time.Millisecond
	s.feedGap = time.Millisecond
	if press != nil {
		s.press = press
	} else {
		s.press = func(uint64, virtualKey) error { return nil }
	}
	return s
}

func waitFor(t *testing.T, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if ok() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("timed out waiting for condition")
}
