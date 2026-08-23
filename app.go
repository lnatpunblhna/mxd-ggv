package main

import (
	"context"
	"time"

	"mxd/internal/overlay"
	"mxd/internal/petfeed"
	"mxd/internal/potion"
	"mxd/internal/vision"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// App 是 Wails 绑定的应用入口。
type App struct {
	ctx     context.Context
	vision  *vision.Service
	petfeed *petfeed.Service
	potion  *potion.Service
	overlay *overlay.Service
}

// NewApp 创建应用实例。
func NewApp() *App {
	return &App{
		vision:  vision.New(),
		petfeed: petfeed.New(),
		potion:  potion.New(),
		overlay: overlay.New(),
	}
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	a.petfeed.SetEmitter(func(st petfeed.Status) {
		runtime.EventsEmit(ctx, "petfeed:status", st)
	})
	a.potion.SetEmitter(func(st potion.Status) {
		runtime.EventsEmit(ctx, "potion:status", st)
		if a.overlay != nil {
			a.overlay.Sync(st.Handle, st.Enabled, overlay.FromSlots(
				string(st.HP.State), st.HP.Count,
				string(st.MP.State), st.MP.Count,
			))
		}
	})
	a.potion.SetAlertEmitter(func(al potion.Alert) {
		runtime.EventsEmit(ctx, "potion:alert", al)
	})
}

func (a *App) shutdown(ctx context.Context) {
	if a.overlay != nil {
		a.overlay.Close()
	}
	if a.petfeed != nil {
		a.petfeed.Close()
	}
	if a.potion != nil {
		a.potion.Close()
	}
	if a.vision != nil {
		a.vision.Close()
	}
}

// ListWindows 列出当前可见窗口。
func (a *App) ListWindows() ([]vision.WindowInfo, error) {
	return a.vision.ListWindows()
}

// StartCapture 开始对指定窗口实时截图。
func (a *App) StartCapture(handle uint64, opts vision.Options) error {
	return a.vision.StartCapture(handle, opts)
}

// StopCapture 停止实时截图。
func (a *App) StopCapture() error {
	return a.vision.StopCapture()
}

// NextFrame 长轮询下一帧预览。
func (a *App) NextFrame(seq int) vision.FramePayload {
	return a.vision.NextFrame(seq)
}

// UpdateOptions 热更新预览参数。
func (a *App) UpdateOptions(opts vision.Options) error {
	return a.vision.UpdateOptions(opts)
}

// StartPetFeed 开启宠物自动喂食。
func (a *App) StartPetFeed(handle uint64, fullness int, hotkey string) error {
	return a.petfeed.Start(handle, fullness, hotkey)
}

// StopPetFeed 关闭宠物自动喂食。
func (a *App) StopPetFeed() {
	a.petfeed.Stop()
}

// PetFeedStatus 返回宠物喂食当前状态。
func (a *App) PetFeedStatus() petfeed.Status {
	return a.petfeed.Status()
}

// IsElevated 报告本程序是否已以管理员身份运行。
func (a *App) IsElevated() bool {
	return petfeed.IsElevated()
}

// CalibratePotions 按当前画面截取药槽模板和血蓝条色域。
func (a *App) CalibratePotions(handle uint64, spec potion.CalibSpec) (potion.CalibrationView, error) {
	return a.potion.Calibrate(handle, spec)
}

// TeachPotionDigits 用填写的当前数量训练 0–9 模板，可反复调用。
func (a *App) TeachPotionDigits(handle uint64, hpCount, mpCount int) (potion.CalibrationView, error) {
	return a.potion.Teach(handle, hpCount, mpCount)
}

// StartPotionWatch 开启血药蓝药监测。
func (a *App) StartPotionWatch(handle uint64, opts potion.WatchOptions) error {
	return a.potion.Start(handle, opts)
}

// StopPotionWatch 关闭血药蓝药监测。
func (a *App) StopPotionWatch() {
	a.potion.Stop()
}

// PotionStatus 返回药量监测当前状态。
func (a *App) PotionStatus() potion.Status {
	return a.potion.Status()
}

// LoadPotionCalibration 返回已保存的校准框选。
func (a *App) LoadPotionCalibration() potion.CalibrationView {
	return a.potion.LoadCalibration()
}

// RestartAsAdmin 弹出 UAC 并以管理员身份重启。用户取消则返回错误。
func (a *App) RestartAsAdmin() error {
	if petfeed.IsElevated() {
		return nil
	}
	if err := petfeed.RelaunchElevated(); err != nil {
		return err
	}
	go func() {
		time.Sleep(300 * time.Millisecond)
		runtime.Quit(a.ctx)
	}()
	return nil
}
