package petfeed

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// PressFunc 向指定窗口发送喂食快捷键。
type PressFunc func(handle uint64, vk virtualKey) error

// Status 是暴露给前端的运行快照。
type Status struct {
	Enabled   bool   `json:"enabled"`
	Fullness  int    `json:"fullness"`
	Hotkey    string `json:"hotkey"`
	Handle    uint64 `json:"handle"`
	FeedCount int    `json:"feedCount"`
	NextDecay int64  `json:"nextDecay"`
	LastFeed  int64  `json:"lastFeed"`
	LastError string `json:"lastError"`
	StartedAt int64  `json:"startedAt"`
}

// Emitter 推送状态变更。
type Emitter func(Status)

// Service 在后台按固定节奏衰减饱满感并自动喂食。
type Service struct {
	press   PressFunc
	decay   time.Duration
	feedGap time.Duration
	now     func() time.Time

	mu      sync.Mutex
	running bool
	cancel  context.CancelFunc
	status  Status
	vk      virtualKey
	emit    Emitter
}

// New 创建喂食服务，默认每 1 分钟衰减 1 点。
func New() *Service {
	return &Service{
		press:   pressKey,
		decay:   DecayInterval,
		feedGap: 400 * time.Millisecond,
		now:     time.Now,
	}
}

// SetEmitter 设置状态回调，可在任意时刻调用。
func (s *Service) SetEmitter(emit Emitter) {
	s.mu.Lock()
	s.emit = emit
	s.mu.Unlock()
}

// Start 开启自动喂食。已在运行时返回错误。
func (s *Service) Start(handle uint64, fullness int, hotkey string) error {
	if handle == 0 {
		return fmt.Errorf("请先选择窗口")
	}
	if fullness < 0 || fullness > MaxFullness {
		return fmt.Errorf("饱满感应在 0–100 之间")
	}
	vk, err := ParseKey(hotkey)
	if err != nil {
		return err
	}

	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return fmt.Errorf("宠物喂食已在运行")
	}

	ctx, cancel := context.WithCancel(context.Background())
	now := s.now()
	s.cancel = cancel
	s.running = true
	s.vk = vk
	s.status = Status{
		Enabled:   true,
		Fullness:  fullness,
		Hotkey:    hotkey,
		Handle:    handle,
		NextDecay: now.Add(s.decay).UnixMilli(),
		StartedAt: now.UnixMilli(),
	}
	s.mu.Unlock()

	s.push()
	go s.loop(ctx)
	return nil
}

// Stop 关闭自动喂食。未运行时是空操作。
func (s *Service) Stop() {
	s.mu.Lock()
	cancel := s.cancel
	s.cancel = nil
	s.running = false
	s.status.Enabled = false
	s.status.NextDecay = 0
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	s.push()
}

// Status 返回当前快照。
func (s *Service) Status() Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.status
}

// Close 停止服务，供应用退出时调用。
func (s *Service) Close() {
	s.Stop()
}

func (s *Service) loop(ctx context.Context) {
	s.feedIfNeeded(ctx)

	ticker := time.NewTicker(s.decay)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case t := <-ticker.C:
			s.applyDecay(t)
			s.feedIfNeeded(ctx)
		}
	}
}

func (s *Service) applyDecay(t time.Time) {
	s.mu.Lock()
	if !s.running {
		s.mu.Unlock()
		return
	}
	s.status.Fullness = Decay(s.status.Fullness)
	s.status.NextDecay = t.Add(s.decay).UnixMilli()
	s.mu.Unlock()
	s.push()
}

func (s *Service) feedIfNeeded(ctx context.Context) {
	for {
		s.mu.Lock()
		if !s.running || !NeedsFeed(s.status.Fullness) {
			s.mu.Unlock()
			return
		}
		handle := s.status.Handle
		vk := s.vk
		s.mu.Unlock()

		if err := ctx.Err(); err != nil {
			return
		}

		if err := s.press(handle, vk); err != nil {
			s.mu.Lock()
			s.status.LastError = err.Error()
			s.mu.Unlock()
			s.push()
			return
		}

		s.mu.Lock()
		s.status.Fullness = AfterFeed(s.status.Fullness)
		s.status.FeedCount++
		s.status.LastFeed = s.now().UnixMilli()
		s.status.LastError = ""
		s.mu.Unlock()
		s.push()

		if err := wait(ctx, s.feedGap); err != nil {
			return
		}
	}
}

func (s *Service) push() {
	s.mu.Lock()
	st := s.status
	emit := s.emit
	s.mu.Unlock()
	if emit != nil {
		emit(st)
	}
}

func wait(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
