//go:build windows

package overlay

import (
	"runtime"
	"sync/atomic"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	wsExLayered     = 0x00080000
	wsExTransparent = 0x00000020
	wsExNoActivate  = 0x08000000
	wsExToolWindow  = 0x00000080
	wsExTopmost     = 0x00000008
	wsPopup         = 0x80000000

	wmDestroy    = 0x0002
	wmClose      = 0x0010
	wmQuit       = 0x0012
	wmEraseBkgnd = 0x0014
	wmTimer      = 0x0113

	swpNoSize     = 0x0001
	swpNoMove     = 0x0002
	swpNoZOrder   = 0x0004
	swpNoActivate = 0x0010
	swpShowWindow = 0x0040
	swpHideWindow = 0x0080
	swpNoOwnerZ   = 0x0200

	hwndTopmost = ^uintptr(0) // -1

	lwaColorKey = 0x0001
	lwaAlpha    = 0x0002
	ulwAlpha    = 0x00000002

	srcCopy      = 0x00CC0020
	transparent  = 1
	fwBold       = 700
	defaultCS    = 1
	antiAliasQ   = 4
	outTTPrecis  = 4
	clipDefault  = 0
	defaultPitch = 0
	dibRGBColors = 0
	swShowNoAct  = 4
	swHide       = 0

	colorKey     = 0x00FF00FF // magenta, 0x00BBGGRR
	colorBlack   = 0x00101820
	timerID      = 1
	frameMS      = 50
	overlayAlpha = 235
)

var (
	user32   = windows.NewLazySystemDLL("user32.dll")
	gdi32    = windows.NewLazySystemDLL("gdi32.dll")
	kernel32 = windows.NewLazySystemDLL("kernel32.dll")

	procRegisterClassEx          = user32.NewProc("RegisterClassExW")
	procCreateWindowEx           = user32.NewProc("CreateWindowExW")
	procDestroyWindow            = user32.NewProc("DestroyWindow")
	procDefWindowProc            = user32.NewProc("DefWindowProcW")
	procGetMessage               = user32.NewProc("GetMessageW")
	procTranslateMessage         = user32.NewProc("TranslateMessage")
	procDispatchMessage          = user32.NewProc("DispatchMessageW")
	procPostThreadMessage        = user32.NewProc("PostThreadMessageW")
	procSetTimer                 = user32.NewProc("SetTimer")
	procKillTimer                = user32.NewProc("KillTimer")
	procGetDC                    = user32.NewProc("GetDC")
	procReleaseDC                = user32.NewProc("ReleaseDC")
	procUpdateLayeredWindow      = user32.NewProc("UpdateLayeredWindow")
	procShowWindow               = user32.NewProc("ShowWindow")
	procSetWindowPos             = user32.NewProc("SetWindowPos")
	procGetClientRect            = user32.NewProc("GetClientRect")
	procClientToScreen           = user32.NewProc("ClientToScreen")
	procGetForegroundWindow      = user32.NewProc("GetForegroundWindow")
	procGetWindowThreadProcessId = user32.NewProc("GetWindowThreadProcessId")
	procGetParent                = user32.NewProc("GetParent")
	procIsWindow                 = user32.NewProc("IsWindow")
	procIsIconic                 = user32.NewProc("IsIconic")
	procFillRect                 = user32.NewProc("FillRect")

	procCreateCompatibleDC = gdi32.NewProc("CreateCompatibleDC")
	procCreateDIBSection   = gdi32.NewProc("CreateDIBSection")
	procSelectObject       = gdi32.NewProc("SelectObject")
	procDeleteObject       = gdi32.NewProc("DeleteObject")
	procDeleteDC           = gdi32.NewProc("DeleteDC")
	procCreateSolidBrush   = gdi32.NewProc("CreateSolidBrush")
	procCreateFont         = gdi32.NewProc("CreateFontW")
	procSetBkMode          = gdi32.NewProc("SetBkMode")
	procSetTextColor       = gdi32.NewProc("SetTextColor")
	procTextOut            = gdi32.NewProc("TextOutW")
	procGetTextExtent      = gdi32.NewProc("GetTextExtentPoint32W")

	procGetModuleHandle    = kernel32.NewProc("GetModuleHandleW")
	procGetCurrentThreadId = kernel32.NewProc("GetCurrentThreadId")

	wndProcCallback = windows.NewCallback(overlayWndProc)
	className       *uint16
	classOnce       uint32
	live            atomic.Pointer[Service]
)

func init() {
	className, _ = windows.UTF16PtrFromString("mxdDanmakuOverlay")
}

type winRect struct {
	Left, Top, Right, Bottom int32
}

type point struct {
	X, Y int32
}

type size struct {
	CX, CY int32
}

type blendFunction struct {
	BlendOp             byte
	BlendFlags          byte
	SourceConstantAlpha byte
	AlphaFormat         byte
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

type dibInfo struct {
	Header bitmapInfoHeader
	Colors [1]uint32
}

type msg struct {
	Hwnd    uintptr
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Pt      point
}

type wndClassEx struct {
	CbSize        uint32
	Style         uint32
	LpfnWndProc   uintptr
	CbClsExtra    int32
	CbWndExtra    int32
	HInstance     uintptr
	HIcon         uintptr
	HCursor       uintptr
	HbrBackground uintptr
	LpszMenuName  *uint16
	LpszClassName *uint16
	HIconSm       uintptr
}

func (s *Service) wake() {
	s.mu.Lock()
	if s.closed || s.started {
		s.mu.Unlock()
		return
	}
	s.started = true
	s.mu.Unlock()
	go s.loop()
}

func (s *Service) requestQuit() {
	tid := atomic.LoadUintptr(&s.tid)
	if tid != 0 {
		procPostThreadMessage.Call(tid, wmQuit, 0, 0)
	}
}

func (s *Service) loop() {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	defer close(s.done)

	tid, _, _ := procGetCurrentThreadId.Call()
	atomic.StoreUintptr(&s.tid, tid)
	live.Store(s)
	defer live.Store(nil)

	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.mu.Unlock()

	if err := s.setup(); err != nil {
		return
	}
	defer s.teardown()

	var m msg
	for {
		ret, _, _ := procGetMessage.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(ret) <= 0 {
			break
		}
		procTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		procDispatchMessage.Call(uintptr(unsafe.Pointer(&m)))
	}
}

func (s *Service) setup() error {
	hInstance, _, _ := procGetModuleHandle.Call(0)
	s.hInstance = hInstance
	s.magenta, _, _ = procCreateSolidBrush.Call(colorKey)
	registerOverlayClass(hInstance)

	title, _ := windows.UTF16PtrFromString("")
	ex := uintptr(wsExLayered | wsExTransparent | wsExNoActivate | wsExToolWindow | wsExTopmost)
	hwnd, _, _ := procCreateWindowEx.Call(
		ex,
		uintptr(unsafe.Pointer(className)),
		uintptr(unsafe.Pointer(title)),
		wsPopup,
		0, 0, 8, 8,
		0, 0, hInstance, 0,
	)
	if hwnd == 0 {
		return errWin("CreateWindowEx")
	}
	s.hwnd = hwnd
	screenDC, _, _ := procGetDC.Call(0)
	if screenDC != 0 {
		s.memDC, _, _ = procCreateCompatibleDC.Call(screenDC)
		procReleaseDC.Call(0, screenDC)
	}
	procSetTimer.Call(hwnd, timerID, frameMS, 0)
	s.engine = &engine{}
	return nil
}

type winErr string

func (e winErr) Error() string { return string(e) }

func errWin(op string) error { return winErr(op) }

func (s *Service) teardown() {
	if s.hwnd != 0 {
		procKillTimer.Call(s.hwnd, timerID)
		procDestroyWindow.Call(s.hwnd)
		s.hwnd = 0
	}
	s.releaseFont()
	s.releaseBitmap()
	if s.magenta != 0 {
		procDeleteObject.Call(s.magenta)
		s.magenta = 0
	}
}

func (s *Service) onTick() {
	handle, enabled, lines, closed := s.snapshot()
	if closed {
		procPostThreadMessage.Call(atomic.LoadUintptr(&s.tid), wmQuit, 0, 0)
		return
	}

	x, y, gw, gh, ok := clientScreenRect(uintptr(handle))
	front := ok && windowAlive(uintptr(handle)) && !windowIconic(uintptr(handle)) && overlayVisibleFor(uintptr(handle))
	show := enabled && ok && front && len(lines) > 0
	if !show {
		s.hideOverlay()
		s.engine.tick(float64(frameMS)/1000, 8, 8, nil, nil)
		return
	}

	ox, oy, ow, oh := overlayBand(gw, gh)
	s.ensureFont(gh)
	s.paint(x+ox, y+oy, ow, oh, gw, gh, ox, oy, lines)
}

func (s *Service) hideOverlay() {
	if !s.visible {
		return
	}
	procShowWindow.Call(s.hwnd, swHide)
	s.visible = false
}

func (s *Service) paint(x, y, w, h, gameW, gameH, ox, oy int, lines []Line) {
	if w <= 0 || h <= 0 || !s.ensureBitmap(w, h) {
		return
	}

	dt := float64(frameMS) / 1000
	s.engine.tick(dt, float64(gameW), float64(gameH), lines, s.measure)
	fill := winRect{Right: int32(w), Bottom: int32(h)}
	procFillRect.Call(s.memDC, uintptr(unsafe.Pointer(&fill)), s.magenta)
	if s.font != 0 {
		procSelectObject.Call(s.memDC, s.font)
	}
	procSetBkMode.Call(s.memDC, transparent)
	for _, b := range s.engine.bullets {
		nb := b
		nb.x -= float64(ox)
		nb.y -= float64(oy)
		drawBullet(s.memDC, nb)
	}
	s.bakeAlpha(w, h)

	dst := point{X: int32(x), Y: int32(y)}
	sz := size{CX: int32(w), CY: int32(h)}
	src := point{}
	blend := blendFunction{BlendOp: 0, SourceConstantAlpha: 255, AlphaFormat: 1}
	procUpdateLayeredWindow.Call(
		s.hwnd, 0,
		uintptr(unsafe.Pointer(&dst)),
		uintptr(unsafe.Pointer(&sz)),
		s.memDC,
		uintptr(unsafe.Pointer(&src)),
		0,
		uintptr(unsafe.Pointer(&blend)),
		ulwAlpha,
	)
	if !s.visible {
		procShowWindow.Call(s.hwnd, swShowNoAct)
		s.visible = true
	}
	s.zTick++
	if s.zTick%12 == 0 {
		procSetWindowPos.Call(s.hwnd, hwndTopmost, 0, 0, 0, 0, swpNoMove|swpNoSize|swpNoActivate)
	}
	s.lastX, s.lastY, s.lastW, s.lastH = x, y, w, h
}

func (s *Service) measure(text string, px int) float64 {
	if s.memDC == 0 || text == "" {
		return defaultMeasure(text, px)
	}
	if s.font != 0 {
		procSelectObject.Call(s.memDC, s.font)
	}
	p, err := windows.UTF16FromString(text)
	if err != nil || len(p) < 2 {
		return defaultMeasure(text, px)
	}
	var sz size
	procGetTextExtent.Call(s.memDC, uintptr(unsafe.Pointer(&p[0])), uintptr(len(p)-1), uintptr(unsafe.Pointer(&sz)))
	if sz.CX <= 0 {
		return defaultMeasure(text, px)
	}
	return float64(sz.CX)
}

func drawBullet(hdc uintptr, b bullet) {
	p, err := windows.UTF16FromString(b.text)
	if err != nil || len(p) < 2 {
		return
	}
	n := uintptr(len(p) - 1)
	x := int(b.x)
	y := int(b.y)
	procSetTextColor.Call(hdc, colorBlack)
	for _, d := range [4][2]int{{-2, 0}, {2, 0}, {0, -2}, {0, 2}} {
		procTextOut.Call(hdc, uintptr(int32(x+d[0])), uintptr(int32(y+d[1])), uintptr(unsafe.Pointer(&p[0])), n)
	}
	procSetTextColor.Call(hdc, bulletColor(b.kind, b.level))
	procTextOut.Call(hdc, uintptr(int32(x)), uintptr(int32(y)), uintptr(unsafe.Pointer(&p[0])), n)
}

func bulletColor(kind, level string) uintptr {
	if kind == "mp" {
		if level == "empty" {
			return 0x00FF8C3C // RGB 60,140,255
		}
		return 0x00FFC878 // RGB 120,200,255
	}
	if level == "empty" {
		return 0x003C3CFF // RGB 255,60,60
	}
	return 0x0048B4FF // RGB 255,180,72
}

func (s *Service) ensureFont(height int) {
	px := fontPx(float64(height))
	if s.font != 0 && s.fontPx == px {
		return
	}
	s.releaseFont()
	face, _ := windows.UTF16PtrFromString("Microsoft YaHei")
	s.font, _, _ = procCreateFont.Call(
		uintptr(int32(-px)), 0, 0, 0, fwBold,
		0, 0, 0, defaultCS, outTTPrecis, clipDefault, antiAliasQ, defaultPitch,
		uintptr(unsafe.Pointer(face)),
	)
	s.fontPx = px
}

func (s *Service) releaseFont() {
	if s.font != 0 {
		procDeleteObject.Call(s.font)
		s.font = 0
		s.fontPx = 0
	}
}

func (s *Service) ensureBitmap(w, h int) bool {
	if w <= 0 || h <= 0 {
		return false
	}
	if s.memDC != 0 && s.bmp != 0 && s.bmpW == w && s.bmpH == h && s.dibBits != 0 {
		return true
	}
	if s.memDC == 0 {
		screenDC, _, _ := procGetDC.Call(0)
		if screenDC == 0 {
			return false
		}
		s.memDC, _, _ = procCreateCompatibleDC.Call(screenDC)
		procReleaseDC.Call(0, screenDC)
		if s.memDC == 0 {
			return false
		}
	}
	s.releaseDIB()
	var bi dibInfo
	bi.Header.BiSize = 40
	bi.Header.BiWidth = int32(w)
	bi.Header.BiHeight = -int32(h)
	bi.Header.BiPlanes = 1
	bi.Header.BiBitCount = 32
	var bits uintptr
	bmp, _, _ := procCreateDIBSection.Call(
		s.memDC,
		uintptr(unsafe.Pointer(&bi)),
		dibRGBColors,
		uintptr(unsafe.Pointer(&bits)),
		0, 0,
	)
	if bmp == 0 || bits == 0 {
		if bmp != 0 {
			procDeleteObject.Call(bmp)
		}
		return false
	}
	old, _, _ := procSelectObject.Call(s.memDC, bmp)
	s.bmp = bmp
	s.oldBmp = old
	s.dibBits = bits
	s.bmpW = w
	s.bmpH = h
	return true
}

func (s *Service) bakeAlpha(w, h int) {
	if s.dibBits == 0 || w <= 0 || h <= 0 {
		return
	}
	pix := unsafe.Slice((*byte)(unsafe.Pointer(s.dibBits)), w*h*4)
	for i := 0; i+3 < len(pix); i += 4 {
		b, g, r := pix[i], pix[i+1], pix[i+2]
		if r == 0xFF && g == 0 && b == 0xFF {
			pix[i], pix[i+1], pix[i+2], pix[i+3] = 0, 0, 0, 0
			continue
		}
		pix[i+3] = 255
	}
}

func (s *Service) releaseDIB() {
	if s.memDC != 0 && s.oldBmp != 0 {
		procSelectObject.Call(s.memDC, s.oldBmp)
		s.oldBmp = 0
	}
	if s.bmp != 0 {
		procDeleteObject.Call(s.bmp)
		s.bmp = 0
	}
	s.dibBits = 0
	s.bmpW, s.bmpH = 0, 0
}

func (s *Service) releaseBitmap() {
	s.releaseDIB()
	if s.memDC != 0 {
		procDeleteDC.Call(s.memDC)
		s.memDC = 0
	}
}

func overlayWndProc(hwnd, msg, wparam, lparam uintptr) uintptr {
	s := live.Load()
	switch msg {
	case wmTimer:
		if s != nil && hwnd == s.hwnd {
			s.onTick()
		}
		return 0
	case wmEraseBkgnd:
		return 1
	case wmClose:
		procDestroyWindow.Call(hwnd)
		return 0
	case wmDestroy:
		return 0
	}
	ret, _, _ := procDefWindowProc.Call(hwnd, msg, wparam, lparam)
	return ret
}

func registerOverlayClass(hInstance uintptr) {
	if !atomic.CompareAndSwapUint32(&classOnce, 0, 1) {
		return
	}
	wc := wndClassEx{
		Style:         0,
		LpfnWndProc:   wndProcCallback,
		HInstance:     hInstance,
		LpszClassName: className,
	}
	wc.CbSize = uint32(unsafe.Sizeof(wc))
	procRegisterClassEx.Call(uintptr(unsafe.Pointer(&wc)))
}

func clientScreenRect(hwnd uintptr) (x, y, w, h int, ok bool) {
	if hwnd == 0 {
		return
	}
	var cr winRect
	if ret, _, _ := procGetClientRect.Call(hwnd, uintptr(unsafe.Pointer(&cr))); ret == 0 {
		return
	}
	pt := point{X: cr.Left, Y: cr.Top}
	procClientToScreen.Call(hwnd, uintptr(unsafe.Pointer(&pt)))
	w = int(cr.Right - cr.Left)
	h = int(cr.Bottom - cr.Top)
	if w <= 8 || h <= 8 {
		return
	}
	return int(pt.X), int(pt.Y), w, h, true
}

func windowAlive(hwnd uintptr) bool {
	ret, _, _ := procIsWindow.Call(hwnd)
	return ret != 0
}

func windowIconic(hwnd uintptr) bool {
	ret, _, _ := procIsIconic.Call(hwnd)
	return ret != 0
}

func overlayVisibleFor(game uintptr) bool {
	fg, _, _ := procGetForegroundWindow.Call()
	if ownsWindow(game, fg) {
		return true
	}
	if fg == 0 {
		return false
	}
	var pid uint32
	procGetWindowThreadProcessId.Call(fg, uintptr(unsafe.Pointer(&pid)))
	return pid == windows.GetCurrentProcessId()
}

func ownsWindow(root, hwnd uintptr) bool {
	if root == 0 || hwnd == 0 {
		return false
	}
	cur := hwnd
	for i := 0; i < 16 && cur != 0; i++ {
		if cur == root {
			return true
		}
		next, _, _ := procGetParent.Call(cur)
		if next == 0 || next == cur {
			break
		}
		cur = next
	}
	return false
}
