package potion

import (
	"context"
	"fmt"
	"sync"
	"time"

	"mxd/internal/wincap"

	"github.com/lnatpunblhna/go-game-vision/pkg/capture"
)

// Emitter 推送状态变更。
type Emitter func(Status)

// Alerter 在确认提醒时调用。
type Alerter func(Alert)

// Service 周期性截图并检测血药/蓝药。
type Service struct {
	now     func() time.Time
	grab    func(uint64) (*capture.RawFrame, error)
	alert   Alerter
	onAlert Alerter

	mu      sync.Mutex
	running bool
	cancel  context.CancelFunc
	status  Status
	opts    WatchOptions
	cal     *calibration
	hpTrk   *tracker
	mpTrk   *tracker
	emit    Emitter
}

// New 创建药量提醒服务，并尝试加载上次校准。
func New() *Service {
	s := &Service{
		now:   time.Now,
		grab:  grabFrame,
		alert: notifyAlert,
	}
	if cal, err := loadCalibration(); err == nil && cal != nil {
		s.cal = cal
		s.seedCountsLocked()
		s.status = s.snapshotLocked()
	}
	return s
}

// SetEmitter 设置状态回调。
func (s *Service) SetEmitter(emit Emitter) {
	s.mu.Lock()
	s.emit = emit
	s.mu.Unlock()
}

// SetAlerter 覆盖提醒实现，测试可注入。
func (s *Service) SetAlerter(fn Alerter) {
	s.mu.Lock()
	s.alert = fn
	s.mu.Unlock()
}

// SetAlertEmitter 在默认系统通知之外再推送一次提醒（给前端）。
func (s *Service) SetAlertEmitter(fn Alerter) {
	s.mu.Lock()
	s.onAlert = fn
	s.mu.Unlock()
}

// Calibrate 按当前窗口截图保存药槽模板与血蓝条色域。
func (s *Service) Calibrate(handle uint64, spec CalibSpec) (CalibrationView, error) {
	frame, err := s.grab(handle)
	if err != nil {
		return CalibrationView{}, err
	}
	s.mu.Lock()
	var prev *digitBank
	if s.cal != nil {
		prev = s.cal.Digits.clone()
	}
	s.mu.Unlock()
	cal, err := buildCalibrationFrom(frame, spec, prev)
	if err != nil {
		return CalibrationView{}, err
	}
	if err := saveCalibration(cal); err != nil {
		return CalibrationView{}, fmt.Errorf("保存校准失败: %w", err)
	}

	s.mu.Lock()
	s.cal = cal
	s.status.LastError = ""
	s.seedCountsLocked()
	if spec.HPCount > 0 {
		s.status.HP.Count = spec.HPCount
		s.status.HP.State = SlotOK
	}
	if spec.MPCount > 0 {
		s.status.MP.Count = spec.MPCount
		s.status.MP.State = SlotOK
	}
	view := cal.view()
	st := s.snapshotLocked()
	s.mu.Unlock()
	s.push(st)
	return view, nil
}

// Teach 用用户填写的当前数量，把这一帧药格上的数字学进 0–9 模板。可反复调用，不会丢掉已学数字。
func (s *Service) Teach(handle uint64, hpCount, mpCount int) (CalibrationView, error) {
	if handle == 0 {
		return CalibrationView{}, fmt.Errorf("请先选择窗口")
	}
	if hpCount <= 0 && mpCount <= 0 {
		return CalibrationView{}, fmt.Errorf("请填写当前血药或蓝药数量")
	}
	s.mu.Lock()
	cal := s.cal
	if cal == nil || !cal.ready() {
		s.mu.Unlock()
		return CalibrationView{}, fmt.Errorf("请先框选药槽并校准")
	}
	hpSlot, mpSlot := cal.HPSlot, cal.MPSlot
	fw, fh := cal.FrameW, cal.FrameH
	hasHP := hpSlot.Valid() && cal.HPTmpl.Img != nil
	hasMP := mpSlot.Valid() && cal.MPTmpl.Img != nil
	bank := cal.Digits.clone()
	s.mu.Unlock()

	frame, err := s.grab(handle)
	if err != nil {
		return CalibrationView{}, err
	}
	if frame != nil {
		fw, fh = frame.Width, frame.Height
	}

	taught := 0
	if hpCount > 0 && hasHP {
		im := cropFrame(frame, pixelRect(hpSlot, fw, fh))
		taught += teachCount(im, hpCount, bank)
	}
	if mpCount > 0 && hasMP {
		im := cropFrame(frame, pixelRect(mpSlot, fw, fh))
		taught += teachCount(im, mpCount, bank)
	}
	if taught == 0 {
		return CalibrationView{}, fmt.Errorf("没切出数字。请确认填写的数量和药格上一致，并把药格框紧一点")
	}

	s.mu.Lock()
	if s.cal == nil {
		s.mu.Unlock()
		return CalibrationView{}, fmt.Errorf("请先框选药槽并校准")
	}
	s.cal.Digits.mergeFrom(bank)
	if hpCount > 0 && hasHP {
		s.cal.HPCount = hpCount
	}
	if mpCount > 0 && hasMP {
		s.cal.MPCount = mpCount
	}
	cal = s.cal
	s.mu.Unlock()
	if err := saveCalibration(cal); err != nil {
		return CalibrationView{}, fmt.Errorf("保存数字模板失败: %w", err)
	}

	s.mu.Lock()
	s.status.LastError = ""
	if hpCount > 0 {
		s.status.HP.Count = hpCount
		s.status.HP.State = SlotOK
		s.status.HP.Reason = "teach"
		if s.hpTrk != nil {
			s.hpTrk.lastCount = hpCount
		}
	}
	if mpCount > 0 {
		s.status.MP.Count = mpCount
		s.status.MP.State = SlotOK
		s.status.MP.Reason = "teach"
		if s.mpTrk != nil {
			s.mpTrk.lastCount = mpCount
		}
	}
	view := cal.view()
	st := s.snapshotLocked()
	s.mu.Unlock()
	s.push(st)
	return view, nil
}

func (s *Service) seedCountsLocked() {
	if s.cal == nil {
		return
	}
	if s.cal.HPTmpl.Img != nil {
		s.status.HP = statusFromTemplate(s.cal.HPTmpl.Img, s.cal.Digits)
		if s.cal.HPCount > 0 {
			s.status.HP.Count = s.cal.HPCount
			s.status.HP.State = SlotOK
			s.status.HP.Reason = "calib"
		}
	}
	if s.cal.MPTmpl.Img != nil {
		s.status.MP = statusFromTemplate(s.cal.MPTmpl.Img, s.cal.Digits)
		if s.cal.MPCount > 0 {
			s.status.MP.Count = s.cal.MPCount
			s.status.MP.State = SlotOK
			s.status.MP.Reason = "calib"
		}
	}
}

func statusFromTemplate(im *bgra, bank *digitBank) SlotStatus {
	n := readCount(im, bank)
	st := SlotStatus{State: SlotUnknown, Count: n, Bar: -1, Reason: "calib"}
	if n >= 0 {
		st.State = SlotOK
	}
	return st
}

// LoadCalibration 返回已保存的校准视图。
func (s *Service) LoadCalibration() CalibrationView {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cal == nil {
		return CalibrationView{}
	}
	return s.cal.view()
}

// Start 开启监测。已在运行时返回错误。
func (s *Service) Start(handle uint64, opts WatchOptions) error {
	if handle == 0 {
		return fmt.Errorf("请先选择窗口")
	}
	opts = opts.normalize()

	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return fmt.Errorf("药量提醒已在运行")
	}
	if s.cal == nil || !s.cal.ready() {
		s.mu.Unlock()
		return fmt.Errorf("请先框选药槽或血蓝条并校准")
	}

	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	s.running = true
	s.opts = opts
	hpInit, mpInit := SlotUnknown, SlotUnknown
	if s.cal != nil {
		if s.cal.HPCount > 0 {
			hpInit = SlotOK
		}
		if s.cal.MPCount > 0 {
			mpInit = SlotOK
		}
	}
	s.hpTrk = newTracker("hp", hpInit)
	s.mpTrk = newTracker("mp", mpInit)
	if s.cal != nil {
		if s.cal.HPCount > 0 {
			s.hpTrk.lastCount = s.cal.HPCount
			s.status.HP.Count = s.cal.HPCount
			s.status.HP.State = SlotOK
			s.status.HP.Reason = "calib"
			if opts.LowCount > 0 && s.cal.HPCount <= opts.LowCount {
				s.status.HP.State = SlotLow
				s.hpTrk.latched = SlotLow
			}
		}
		if s.cal.MPCount > 0 {
			s.mpTrk.lastCount = s.cal.MPCount
			s.status.MP.Count = s.cal.MPCount
			s.status.MP.State = SlotOK
			s.status.MP.Reason = "calib"
			if opts.LowCount > 0 && s.cal.MPCount <= opts.LowCount {
				s.status.MP.State = SlotLow
				s.mpTrk.latched = SlotLow
			}
		}
	}
	s.status.Enabled = true
	s.status.Handle = handle
	s.status.StartedAt = s.now().UnixMilli()
	s.status.LastError = ""
	s.status.LastAlert = nil
	s.mu.Unlock()

	s.push(s.Status())
	go s.loop(ctx, handle)
	return nil
}

// Stop 关闭监测。
func (s *Service) Stop() {
	s.mu.Lock()
	cancel := s.cancel
	s.cancel = nil
	s.running = false
	s.status.Enabled = false
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	s.push(s.Status())
}

// Status 返回当前快照。
func (s *Service) Status() Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.snapshotLocked()
}

// Close 停止服务。
func (s *Service) Close() {
	s.Stop()
	closeGrabber()
}

func (s *Service) loop(ctx context.Context, handle uint64) {
	wincap.LowerThreadPriority()
	s.tick(handle)

	s.mu.Lock()
	interval := time.Duration(s.opts.IntervalMS) * time.Millisecond
	s.mu.Unlock()
	if interval <= 0 {
		interval = 500 * time.Millisecond
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.tick(handle)
		}
	}
}

func (s *Service) tick(handle uint64) {
	s.mu.Lock()
	if !s.running {
		s.mu.Unlock()
		return
	}
	cal := s.cal
	opts := s.opts
	hpTrk := s.hpTrk
	mpTrk := s.mpTrk
	s.mu.Unlock()

	frame, err := s.grab(handle)
	if err != nil {
		s.mu.Lock()
		s.status.LastError = err.Error()
		st := s.snapshotLocked()
		s.mu.Unlock()
		s.push(st)
		return
	}

	hpHint, mpHint := -1, -1
	if hpTrk != nil {
		hpHint = hpTrk.lastCount
	}
	if mpTrk != nil {
		mpHint = mpTrk.lastCount
	}
	hpS, mpS := analyzeFrameHint(frame, cal, opts, hpHint, mpHint)
	now := s.now()
	hp, hpAlert := hpTrk.observe(hpS, now, opts)
	mp, mpAlert := mpTrk.observe(mpS, now, opts)

	s.mu.Lock()
	s.status.LastError = ""
	s.status.HP = hp
	s.status.MP = mp
	if mpAlert != nil {
		s.status.LastAlert = mpAlert
	}
	if hpAlert != nil {
		s.status.LastAlert = hpAlert
	}
	alerter := s.alert
	onAlert := s.onAlert
	st := s.snapshotLocked()
	s.mu.Unlock()

	s.push(st)
	fire := func(a *Alert) {
		if a == nil {
			return
		}
		if alerter != nil {
			alerter(*a)
		}
		if onAlert != nil {
			onAlert(*a)
		}
	}
	fire(hpAlert)
	fire(mpAlert)
}

func (s *Service) snapshotLocked() Status {
	st := s.status
	st.Calibrated = s.cal != nil && s.cal.ready()
	if s.cal != nil {
		st.HasHPSlot = s.cal.HPSlot.Valid() && s.cal.HPTmpl.Img != nil
		st.HasMPSlot = s.cal.MPSlot.Valid() && s.cal.MPTmpl.Img != nil
		st.HasHPBar = s.cal.HPBar.Valid()
		st.HasMPBar = s.cal.MPBar.Valid()
		st.HPSlot = s.cal.HPSlot
		st.MPSlot = s.cal.MPSlot
		st.HPBar = s.cal.HPBar
		st.MPBar = s.cal.MPBar
	}
	st.LowCount = s.opts.LowCount
	st.EmptyFrames = s.opts.EmptyFrames
	st.CooldownSec = s.opts.CooldownSec
	if s.cal != nil {
		st.LearnedDigits = s.cal.Digits.coverage()
	}
	if !st.Enabled {
		// 未运行时仍报告校准后的空状态，方便前端恢复框选。
		if st.HP.State == "" {
			st.HP = SlotStatus{State: SlotAbsent, Count: -1, Bar: -1}
		}
		if st.MP.State == "" {
			st.MP = SlotStatus{State: SlotAbsent, Count: -1, Bar: -1}
		}
	}
	return st
}

func (s *Service) push(st Status) {
	s.mu.Lock()
	emit := s.emit
	s.mu.Unlock()
	if emit != nil {
		emit(st)
	}
}
