//go:build windows

package wincap

import (
	"fmt"
	"image"
	"unsafe"

	"github.com/lnatpunblhna/go-game-vision/pkg/capture"
	"github.com/lnatpunblhna/go-game-vision/pkg/platform"
	"golang.org/x/sys/windows"
)

// Method 是预览状态栏展示的截图方式。
const Method = "屏幕"

const (
	srcCopy                   = 0x00CC0020
	dibRGBColors              = 0
	threadPriorityBelowNormal = ^uintptr(0) // -1
	monitorDefaultNearest     = 2
)

var (
	user32   = windows.NewLazySystemDLL("user32.dll")
	gdi32    = windows.NewLazySystemDLL("gdi32.dll")
	kernel32 = windows.NewLazySystemDLL("kernel32.dll")

	procIsWindow               = user32.NewProc("IsWindow")
	procIsIconic               = user32.NewProc("IsIconic")
	procGetWindowRect          = user32.NewProc("GetWindowRect")
	procMonitorFromWindow      = user32.NewProc("MonitorFromWindow")
	procGetDC                  = user32.NewProc("GetDC")
	procReleaseDC              = user32.NewProc("ReleaseDC")
	procCreateCompatibleDC     = gdi32.NewProc("CreateCompatibleDC")
	procCreateCompatibleBitmap = gdi32.NewProc("CreateCompatibleBitmap")
	procSelectObject           = gdi32.NewProc("SelectObject")
	procBitBlt                 = gdi32.NewProc("BitBlt")
	procGetDIBits              = gdi32.NewProc("GetDIBits")
	procDeleteObject           = gdi32.NewProc("DeleteObject")
	procDeleteDC               = gdi32.NewProc("DeleteDC")
	procGetCurrentThread       = kernel32.NewProc("GetCurrentThread")
	procSetThreadPriority      = kernel32.NewProc("SetThreadPriority")
)

type rect struct {
	Left, Top, Right, Bottom int32
}

type bitmapInfoHeader struct {
	BiSize          uint32
	BiWidth         int32
	BiHeight        int32
	BiPlanes        uint16
	BiBitCount      uint16
	BiCompression   uint32
	BiSizeImage     uint32
	BiXPelsPerMeter int32
	BiYPelsPerMeter int32
	BiClrUsed       uint32
	BiClrImportant  uint32
}

type bitmapInfo struct {
	BmiHeader bitmapInfoHeader
	BmiColors [1]uint32
}

// session 用桌面 DC 的 BitBlt 截取窗口所在屏幕区域。
// 不向游戏窗口发 PrintWindow，因此不会逼它重绘。
type session struct {
	hwnd uintptr

	screenDC  uintptr
	memDC     uintptr
	bitmap    uintptr
	oldBitmap uintptr
	bmpW      int
	bmpH      int
	buf       []byte
	closed    bool
}

// New 创建绑定到 hwnd 的截图会话。
// Windows 上优先走 DXGI 桌面复制（不卡住游戏 Present）；失败再回退 GDI BitBlt。
func New(handle uint64) (Capturer, error) {
	platform.EnsureDPIAware()
	if handle == 0 {
		return nil, fmt.Errorf("未选择窗口")
	}
	hwnd := uintptr(handle)
	if ret, _, _ := procIsWindow.Call(hwnd); ret == 0 {
		return nil, fmt.Errorf("窗口已失效")
	}
	if dxgiAvailable() {
		return &dxgiCap{hwnd: hwnd}, nil
	}
	return newGDI(hwnd)
}

func newGDI(hwnd uintptr) (*session, error) {
	screenDC, _, err := procGetDC.Call(0)
	if screenDC == 0 {
		return nil, fmt.Errorf("GetDC 失败: %v", err)
	}
	memDC, _, err := procCreateCompatibleDC.Call(screenDC)
	if memDC == 0 {
		procReleaseDC.Call(0, screenDC)
		return nil, fmt.Errorf("CreateCompatibleDC 失败: %v", err)
	}
	return &session{hwnd: hwnd, screenDC: screenDC, memDC: memDC}, nil
}

func (s *session) Capture() (*capture.RawFrame, error) {
	if s == nil || s.closed {
		return nil, fmt.Errorf("截图会话已关闭")
	}
	if ret, _, _ := procIsWindow.Call(s.hwnd); ret == 0 {
		return nil, fmt.Errorf("窗口已失效")
	}
	if ret, _, _ := procIsIconic.Call(s.hwnd); ret != 0 {
		return nil, fmt.Errorf("窗口已最小化")
	}

	var r rect
	if ret, _, err := procGetWindowRect.Call(s.hwnd, uintptr(unsafe.Pointer(&r))); ret == 0 {
		return nil, fmt.Errorf("GetWindowRect 失败: %v", err)
	}
	width := int(r.Right - r.Left)
	height := int(r.Bottom - r.Top)
	if width <= 0 || height <= 0 {
		return nil, fmt.Errorf("无效窗口尺寸 %dx%d", width, height)
	}
	if err := s.ensureBitmap(width, height); err != nil {
		return nil, err
	}

	// 从桌面 DC 拷贝窗口矩形：不进入游戏消息循环。
	// 负坐标（主屏左侧显示器）必须按有符号指针宽度传入。
	ok, _, _ := procBitBlt.Call(
		s.memDC, 0, 0, uintptr(width), uintptr(height),
		s.screenDC, uintptr(int64(r.Left)), uintptr(int64(r.Top)),
		srcCopy,
	)
	if ok == 0 {
		return nil, fmt.Errorf("BitBlt 失败")
	}

	procSelectObject.Call(s.memDC, s.oldBitmap)

	var bi bitmapInfo
	bi.BmiHeader.BiSize = uint32(unsafe.Sizeof(bi.BmiHeader))
	bi.BmiHeader.BiWidth = int32(width)
	bi.BmiHeader.BiHeight = -int32(height)
	bi.BmiHeader.BiPlanes = 1
	bi.BmiHeader.BiBitCount = 32
	bi.BmiHeader.BiCompression = 0

	ret, _, _ := procGetDIBits.Call(
		s.screenDC,
		s.bitmap,
		0,
		uintptr(height),
		uintptr(unsafe.Pointer(&s.buf[0])),
		uintptr(unsafe.Pointer(&bi)),
		dibRGBColors,
	)
	procSelectObject.Call(s.memDC, s.bitmap)
	if ret == 0 {
		return nil, fmt.Errorf("GetDIBits 失败")
	}

	stride := width * 4
	return &capture.RawFrame{
		Pix:    s.buf[:stride*height],
		Width:  width,
		Height: height,
		Stride: stride,
		Window: capture.WindowInfo{
			Handle:      s.hwnd,
			Rect:        image.Rect(int(r.Left), int(r.Top), int(r.Right), int(r.Bottom)),
			ScaleFactor: 1,
		},
	}, nil
}

func (s *session) ensureBitmap(width, height int) error {
	if s.bitmap != 0 && s.bmpW == width && s.bmpH == height {
		return nil
	}
	s.releaseBitmap()

	bitmap, _, err := procCreateCompatibleBitmap.Call(s.screenDC, uintptr(width), uintptr(height))
	if bitmap == 0 {
		return fmt.Errorf("CreateCompatibleBitmap 失败: %v", err)
	}
	oldBitmap, _, _ := procSelectObject.Call(s.memDC, bitmap)
	if oldBitmap == 0 {
		procDeleteObject.Call(bitmap)
		return fmt.Errorf("SelectObject 失败")
	}
	s.bitmap = bitmap
	s.oldBitmap = oldBitmap
	s.bmpW = width
	s.bmpH = height
	need := width * height * 4
	if len(s.buf) < need {
		s.buf = make([]byte, need)
	}
	return nil
}

func (s *session) releaseBitmap() {
	if s.bitmap == 0 {
		return
	}
	if s.oldBitmap != 0 {
		procSelectObject.Call(s.memDC, s.oldBitmap)
		s.oldBitmap = 0
	}
	procDeleteObject.Call(s.bitmap)
	s.bitmap = 0
	s.bmpW, s.bmpH = 0, 0
}

func (s *session) Method() string { return Method }

func (s *session) Close() error {
	if s == nil || s.closed {
		return nil
	}
	s.closed = true
	s.releaseBitmap()
	if s.memDC != 0 {
		procDeleteDC.Call(s.memDC)
		s.memDC = 0
	}
	if s.screenDC != 0 {
		procReleaseDC.Call(0, s.screenDC)
		s.screenDC = 0
	}
	s.buf = nil
	return nil
}

// LowerThreadPriority 把当前线程降到 BelowNormal，避免抢游戏 CPU。
func LowerThreadPriority() {
	h, _, _ := procGetCurrentThread.Call()
	procSetThreadPriority.Call(h, threadPriorityBelowNormal)
}
