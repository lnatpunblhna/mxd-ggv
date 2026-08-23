package overlay

import "sync"

// Line 是一条要循环投放的弹幕。
type Line struct {
	Kind  string
	Level string
	Text  string
}

// Service 把药量提醒画到游戏窗口上（点击穿透、不抢焦点）。
type Service struct {
	mu      sync.Mutex
	handle  uint64
	enabled bool
	lines   []Line
	closed  bool
	started bool
	done    chan struct{}

	// 以下字段仅弹幕线程读写（Windows）。
	tid       uintptr
	hwnd      uintptr
	hInstance uintptr
	magenta   uintptr
	engine    *engine
	font      uintptr
	fontPx    int
	memDC     uintptr
	bmp       uintptr
	oldBmp    uintptr
	dibBits   uintptr
	bmpW      int
	bmpH      int
	visible   bool
	lastX     int
	lastY     int
	lastW     int
	lastH     int
	zTick     int
}

// New 创建游戏画面弹幕服务。
func New() *Service {
	return &Service{done: make(chan struct{})}
}

// Sync 按当前药量状态更新弹幕。enabled 为 false 或没有提醒时隐藏。
func (s *Service) Sync(handle uint64, enabled bool, lines []Line) {
	if s == nil {
		return
	}
	copied := append([]Line(nil), lines...)
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.handle = handle
	s.enabled = enabled && handle != 0 && len(copied) > 0
	s.lines = copied
	need := s.enabled
	s.mu.Unlock()
	if need {
		s.wake()
	}
}

// Close 停止弹幕窗口。
func (s *Service) Close() {
	if s == nil {
		return
	}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.closed = true
	s.enabled = false
	s.handle = 0
	s.lines = nil
	started := s.started
	s.mu.Unlock()
	if started {
		s.requestQuit()
		<-s.done
	}
}

func (s *Service) snapshot() (handle uint64, enabled bool, lines []Line, closed bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.handle, s.enabled, append([]Line(nil), s.lines...), s.closed
}

// FromSlots 把血药/蓝药状态转成循环弹幕文案。
func FromSlots(hpState string, hpCount int, mpState string, mpCount int) []Line {
	out := make([]Line, 0, 2)
	if l, ok := slotLine("hp", hpState, hpCount); ok {
		out = append(out, l)
	}
	if l, ok := slotLine("mp", mpState, mpCount); ok {
		out = append(out, l)
	}
	return out
}

func slotLine(kind, state string, count int) (Line, bool) {
	if state != "low" && state != "empty" {
		return Line{}, false
	}
	name := "血药"
	if kind == "mp" {
		name = "蓝药"
	}
	text := name + "已空  快补药！"
	if state == "low" {
		if count >= 0 {
			text = name + "不足  剩余 " + itoa(count)
		} else {
			text = name + "不足  请及时补给"
		}
	}
	return Line{Kind: kind, Level: state, Text: text}, true
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	if n < 0 {
		return "-" + itoa(-n)
	}
	var buf [16]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
