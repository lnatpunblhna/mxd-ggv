package potion

// RelRect 是相对窗口客户区的矩形，取值 0–1。
type RelRect struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
	W float64 `json:"w"`
	H float64 `json:"h"`
}

// Valid 报告矩形是否大到能用来检测。
func (r RelRect) Valid() bool {
	return r.W >= 0.004 && r.H >= 0.004 &&
		r.X >= 0 && r.Y >= 0 &&
		r.X+r.W <= 1.02 && r.Y+r.H <= 1.02
}

func (r RelRect) clamp() RelRect {
	if r.X < 0 {
		r.X = 0
	}
	if r.Y < 0 {
		r.Y = 0
	}
	if r.W < 0 {
		r.W = 0
	}
	if r.H < 0 {
		r.H = 0
	}
	if r.X > 1 {
		r.X = 1
	}
	if r.Y > 1 {
		r.Y = 1
	}
	if r.X+r.W > 1 {
		r.W = 1 - r.X
	}
	if r.Y+r.H > 1 {
		r.H = 1 - r.Y
	}
	return r
}

// CalibSpec 是前端提交的框选。数量一律由校准时自动识别，前端不提交。
type CalibSpec struct {
	HPSlot RelRect `json:"hpSlot"`
	MPSlot RelRect `json:"mpSlot"`
	HPBar  RelRect `json:"hpBar"`
	MPBar  RelRect `json:"mpBar"`
}

// WatchOptions 控制采样、防抖与提醒阈值。
type WatchOptions struct {
	LowCount       int     `json:"lowCount"`
	EmptyFrames    int     `json:"emptyFrames"`
	CooldownSec    int     `json:"cooldownSec"`
	BarLow         float64 `json:"barLow"`
	BarStuckFrames int     `json:"barStuckFrames"`
	IntervalMS     int     `json:"intervalMS"`
}

func (o WatchOptions) normalize() WatchOptions {
	if o.LowCount < 0 {
		o.LowCount = 0
	}
	if o.EmptyFrames <= 0 {
		o.EmptyFrames = 6
	}
	if o.CooldownSec <= 0 {
		o.CooldownSec = 180
	}
	if o.BarLow <= 0 || o.BarLow >= 1 {
		o.BarLow = 0.40
	}
	if o.BarStuckFrames <= 0 {
		o.BarStuckFrames = 6
	}
	if o.IntervalMS <= 0 {
		o.IntervalMS = 500
	}
	if o.IntervalMS < 200 {
		o.IntervalMS = 200
	}
	if o.IntervalMS > 3000 {
		o.IntervalMS = 3000
	}
	return o
}

// SlotState 是单个药槽的稳定状态。
type SlotState string

const (
	SlotAbsent  SlotState = "absent"
	SlotUnknown SlotState = "unknown"
	SlotOK      SlotState = "ok"
	SlotLow     SlotState = "low"
	SlotEmpty   SlotState = "empty"
)

// SlotStatus 是暴露给前端的单槽快照。
type SlotStatus struct {
	State  SlotState `json:"state"`
	Count  int       `json:"count"`
	NCC    float64   `json:"ncc"`
	Bar    float64   `json:"bar"`
	Reason string    `json:"reason"`
}

// Alert 是一次提醒。
type Alert struct {
	Kind   string `json:"kind"`
	Level  string `json:"level"`
	Reason string `json:"reason"`
	Count  int    `json:"count"`
	At     int64  `json:"at"`
}

// Status 是暴露给前端的运行快照。
type Status struct {
	Enabled     bool       `json:"enabled"`
	Handle      uint64     `json:"handle"`
	Calibrated  bool       `json:"calibrated"`
	StartedAt   int64      `json:"startedAt"`
	LastError   string     `json:"lastError"`
	HP          SlotStatus `json:"hp"`
	MP          SlotStatus `json:"mp"`
	LastAlert   *Alert     `json:"lastAlert,omitempty"`
	HasHPSlot   bool       `json:"hasHPSlot"`
	HasMPSlot   bool       `json:"hasMPSlot"`
	HasHPBar    bool       `json:"hasHPBar"`
	HasMPBar    bool       `json:"hasMPBar"`
	HPSlot      RelRect    `json:"hpSlot"`
	MPSlot      RelRect    `json:"mpSlot"`
	HPBar       RelRect    `json:"hpBar"`
	MPBar       RelRect    `json:"mpBar"`
	LowCount    int        `json:"lowCount"`
	EmptyFrames int        `json:"emptyFrames"`
	CooldownSec int        `json:"cooldownSec"`
}

// CalibrationView 是校准结果的前端视图，不含原始像素。
type CalibrationView struct {
	HPSlot    RelRect `json:"hpSlot"`
	MPSlot    RelRect `json:"mpSlot"`
	HPBar     RelRect `json:"hpBar"`
	MPBar     RelRect `json:"mpBar"`
	HasHPSlot bool    `json:"hasHPSlot"`
	HasMPSlot bool    `json:"hasMPSlot"`
	HasHPBar  bool    `json:"hasHPBar"`
	HasMPBar  bool    `json:"hasMPBar"`
	FrameW    int     `json:"frameW"`
	FrameH    int     `json:"frameH"`
	HPPreview string  `json:"hpPreview"`
	MPPreview string  `json:"mpPreview"`
	HPCount   int     `json:"hpCount"`
	MPCount   int     `json:"mpCount"`
}

// ColorRange 是 HSV 阈值。H 为 0–360。
type ColorRange struct {
	HMin int     `json:"hMin"`
	HMax int     `json:"hMax"`
	SMin float64 `json:"sMin"`
	VMin float64 `json:"vMin"`
}

func (c ColorRange) wraps() bool { return c.HMin > c.HMax }

func (c ColorRange) valid() bool {
	return c.SMin > 0 || c.VMin > 0 || c.HMax > 0
}

var (
	presetRed  = ColorRange{HMin: 350, HMax: 18, SMin: 0.35, VMin: 0.25}
	presetBlue = ColorRange{HMin: 195, HMax: 255, SMin: 0.30, VMin: 0.25}
)
