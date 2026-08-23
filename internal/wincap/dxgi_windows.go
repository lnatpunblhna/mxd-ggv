//go:build windows

package wincap

import (
	"fmt"
	"image"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"github.com/lnatpunblhna/go-game-vision/pkg/capture"
	"golang.org/x/sys/windows"
)

// DXGI 桌面复制：从 DWM 已合成的桌面取像素，不经过游戏窗口的 GDI/PrintWindow，
// 因此不会在游戏 Present 上插入 GPU 同步点。GDI BitBlt/GetDIBits 才会让 DirectX 游戏卡顿。

const (
	sOK                  = 0
	dxgiErrorNotFound    = 0x887A0002
	dxgiErrorAccessLost  = 0x887A0026
	dxgiErrorWaitTimeout = 0x887A0027

	d3d11SDKVersion       = 7
	d3d11UsageStaging     = 3
	d3d11CPUAccessRead    = 0x20000
	d3d11MapRead          = 1
	dxgiFormatRGBA16Float = 10
	dxgiFormatRGBA        = 28
	dxgiFormatRGBAsRGB    = 29
	dxgiFormatBGRA        = 87
	dxgiFormatBGRAsRGB    = 91
	d3d11CreateBGRA       = 0x20
)

var (
	dxgiDLL  = windows.NewLazySystemDLL("dxgi.dll")
	d3d11DLL = windows.NewLazySystemDLL("d3d11.dll")

	procCreateDXGIFactory1 = dxgiDLL.NewProc("CreateDXGIFactory1")
	procCreateDXGIFactory2 = dxgiDLL.NewProc("CreateDXGIFactory2")
	procD3D11CreateDevice  = d3d11DLL.NewProc("D3D11CreateDevice")

	iidIDXGIFactory1 = windows.GUID{Data1: 0x770AAE78, Data2: 0xF26F, Data3: 0x4DBA, Data4: [8]byte{0xA8, 0x29, 0x25, 0x3C, 0x83, 0xD1, 0xB3, 0x87}}
	iidIDXGIFactory2 = windows.GUID{Data1: 0x50C83A1C, Data2: 0xE072, Data3: 0x4C48, Data4: [8]byte{0x87, 0xB0, 0x36, 0x30, 0xFA, 0x36, 0xA6, 0xD0}}
	iidID3D11Tex2D   = windows.GUID{Data1: 0x6F15AAF2, Data2: 0xD208, Data3: 0x4E89, Data4: [8]byte{0x9A, 0xB4, 0x48, 0x95, 0x35, 0xD3, 0x4F, 0x9C}}

	d3dFeatureLevels = [...]uint32{0xb000, 0xa100, 0xa000, 0x9300}

	factoryOnce sync.Once
	factoryPtr  uintptr
	factoryErr  error

	dupPoolMu sync.Mutex
	dupPool   = map[uintptr]*dxgiOutput{}

	errAccessLost = fmt.Errorf("DXGI 访问丢失")
)

type dxgiFrameInfo struct {
	LastPresentTime           int64
	LastMouseUpdateTime       int64
	AccumulatedFrames         uint32
	RectsCoalesced            int32
	ProtectedContentMaskedOut int32
	PointerX                  int32
	PointerY                  int32
	PointerVisible            int32
	TotalMetadataBufferSize   uint32
	PointerShapeBufferSize    uint32
}

type dxgiOutputDesc struct {
	DeviceName         [32]uint16
	DesktopCoordinates rect
	AttachedToDesktop  int32
	Rotation           uint32
	Monitor            uintptr
}

type d3d11TexDesc struct {
	Width          uint32
	Height         uint32
	MipLevels      uint32
	ArraySize      uint32
	Format         uint32
	SampleCount    uint32
	SampleQuality  uint32
	Usage          uint32
	BindFlags      uint32
	CPUAccessFlags uint32
	MiscFlags      uint32
}

type d3d11Mapped struct {
	PData      uintptr
	RowPitch   uint32
	DepthPitch uint32
}

type bindError struct{ err error }

func (e bindError) Error() string { return e.err.Error() }
func (e bindError) Unwrap() error { return e.err }

type dxgiCap struct {
	mu     sync.Mutex
	hwnd   uintptr
	out    *dxgiOutput
	mon    uintptr
	buf    []byte
	gdi    *session
	closed bool
}

type dxgiOutput struct {
	mu      sync.Mutex
	refs    int
	monitor uintptr

	adapter uintptr
	output  uintptr
	dup     uintptr
	device  uintptr
	ctx     uintptr
	staging uintptr

	stagingW   int
	stagingH   int
	stagingFmt uint32
	left       int32
	top        int32

	cache   []byte
	cacheW  int
	cacheH  int
	cacheOK bool
	cacheAt time.Time

	stop    chan struct{}
	done    chan struct{}
	loopErr error
}

func dxgiAvailable() bool {
	if procCreateDXGIFactory1.Find() != nil || procD3D11CreateDevice.Find() != nil {
		return false
	}
	maj, min, _ := windows.RtlGetNtVersionNumbers()
	return maj > 6 || (maj == 6 && min >= 2)
}

func (c *dxgiCap) Method() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.gdi != nil {
		return Method
	}
	return "DXGI"
}

func (c *dxgiCap) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closed = true
	if c.out != nil {
		releaseOutput(c.out)
		c.out = nil
		c.mon = 0
	}
	if c.gdi != nil {
		err := c.gdi.Close()
		c.gdi = nil
		return err
	}
	c.buf = nil
	return nil
}

func (c *dxgiCap) Capture() (*capture.RawFrame, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return nil, fmt.Errorf("截图会话已关闭")
	}
	if c.gdi != nil {
		return c.gdi.Capture()
	}
	frame, err := c.captureDXGILocked()
	if err != nil {
		if _, ok := err.(bindError); ok {
			gdi, gerr := newGDI(c.hwnd)
			if gerr == nil {
				c.gdi = gdi
				return c.gdi.Capture()
			}
		}
		return nil, err
	}
	return frame, nil
}

func (c *dxgiCap) captureDXGILocked() (*capture.RawFrame, error) {
	if ret, _, _ := procIsWindow.Call(c.hwnd); ret == 0 {
		return nil, fmt.Errorf("窗口已失效")
	}
	if ret, _, _ := procIsIconic.Call(c.hwnd); ret != 0 {
		return nil, fmt.Errorf("窗口已最小化")
	}
	var r rect
	if ret, _, err := procGetWindowRect.Call(c.hwnd, uintptr(unsafe.Pointer(&r))); ret == 0 {
		return nil, fmt.Errorf("GetWindowRect 失败: %v", err)
	}
	width := int(r.Right - r.Left)
	height := int(r.Bottom - r.Top)
	if width <= 0 || height <= 0 {
		return nil, fmt.Errorf("无效窗口尺寸 %dx%d", width, height)
	}

	mon, _, _ := procMonitorFromWindow.Call(c.hwnd, monitorDefaultNearest)
	if mon == 0 {
		return nil, bindError{fmt.Errorf("找不到窗口所在显示器")}
	}
	if err := c.bindLocked(mon); err != nil {
		return nil, bindError{err}
	}

	if err := c.out.waitFrame(400 * time.Millisecond); err != nil {
		return nil, err
	}

	pix, w, h, err := c.out.cropTo(&c.buf, r)
	if err != nil {
		return nil, err
	}
	return &capture.RawFrame{
		Pix:    pix,
		Width:  w,
		Height: h,
		Stride: w * 4,
		Window: capture.WindowInfo{
			Handle:      c.hwnd,
			Rect:        image.Rect(int(r.Left), int(r.Top), int(r.Right), int(r.Bottom)),
			ScaleFactor: 1,
		},
	}, nil
}

func (c *dxgiCap) bindLocked(mon uintptr) error {
	if c.out != nil && c.mon == mon {
		return nil
	}
	if c.out != nil {
		releaseOutput(c.out)
		c.out = nil
		c.mon = 0
	}
	out, err := acquireOutput(mon)
	if err != nil {
		return err
	}
	c.out = out
	c.mon = mon
	return nil
}

func acquireOutput(monitor uintptr) (*dxgiOutput, error) {
	dupPoolMu.Lock()
	defer dupPoolMu.Unlock()
	if o, ok := dupPool[monitor]; ok {
		o.refs++
		return o, nil
	}
	o, err := openOutput(monitor)
	if err != nil {
		return nil, err
	}
	o.refs = 1
	dupPool[monitor] = o
	return o, nil
}

func releaseOutput(o *dxgiOutput) {
	if o == nil {
		return
	}
	dupPoolMu.Lock()
	defer dupPoolMu.Unlock()
	o.refs--
	if o.refs > 0 {
		return
	}
	delete(dupPool, o.monitor)
	o.close()
}

func dxgiFactory() (uintptr, error) {
	factoryOnce.Do(func() {
		var f uintptr
		if procCreateDXGIFactory2.Find() == nil {
			hr, _, _ := procCreateDXGIFactory2.Call(
				0,
				uintptr(unsafe.Pointer(&iidIDXGIFactory2)),
				uintptr(unsafe.Pointer(&f)),
			)
			if uint32(hr) == sOK && f != 0 {
				factoryPtr = f
				return
			}
		}
		hr, _, _ := procCreateDXGIFactory1.Call(
			uintptr(unsafe.Pointer(&iidIDXGIFactory1)),
			uintptr(unsafe.Pointer(&f)),
		)
		if uint32(hr) != sOK || f == 0 {
			factoryErr = fmt.Errorf("CreateDXGIFactory 失败: 0x%08X", uint32(hr))
			return
		}
		factoryPtr = f
	})
	if factoryPtr == 0 {
		if factoryErr != nil {
			return 0, factoryErr
		}
		return 0, fmt.Errorf("CreateDXGIFactory 失败")
	}
	return factoryPtr, nil
}

func openOutput(monitor uintptr) (*dxgiOutput, error) {
	factory, err := dxgiFactory()
	if err != nil {
		return nil, err
	}

	var (
		adapter uintptr
		output  uintptr
		desc    dxgiOutputDesc
	)
	found := false
	seen := map[uintptr]bool{}
	for ai := uint32(0); ai < 8 && !found; ai++ {
		adp, hr := enumAdapter(factory, ai)
		if hr == dxgiErrorNotFound || adp == 0 {
			break
		}
		if seen[adp] {
			comRelease(adp)
			continue
		}
		seen[adp] = true
		for oi := uint32(0); oi < 16; oi++ {
			out, ohr := comCall2(adp, 7, uintptr(oi))
			if ohr == dxgiErrorNotFound {
				break
			}
			if ohr != sOK || out == 0 {
				continue
			}
			var d dxgiOutputDesc
			syscall.SyscallN(vtblOf(out)[7], out, uintptr(unsafe.Pointer(&d)))
			if d.Monitor == monitor {
				adapter, output, desc, found = adp, out, d, true
				break
			}
			comRelease(out)
		}
		if !found {
			comRelease(adp)
		}
	}
	if !found {
		return nil, fmt.Errorf("显示器没有可用的 DXGI 输出")
	}

	o := &dxgiOutput{
		monitor: monitor,
		adapter: adapter,
		output:  output,
		left:    desc.DesktopCoordinates.Left,
		top:     desc.DesktopCoordinates.Top,
	}
	if err := o.createDevice(); err != nil {
		o.close()
		return nil, err
	}
	if err := o.duplicate(); err != nil {
		o.close()
		return nil, err
	}
	o.startPump()
	return o, nil
}

func (o *dxgiOutput) createDevice() error {
	var device, ctx uintptr
	var level uint32
	hr, _, _ := procD3D11CreateDevice.Call(
		o.adapter,
		0, // D3D_DRIVER_TYPE_UNKNOWN（指定 adapter 时必须）
		0,
		d3d11CreateBGRA,
		uintptr(unsafe.Pointer(&d3dFeatureLevels[0])),
		uintptr(len(d3dFeatureLevels)),
		d3d11SDKVersion,
		uintptr(unsafe.Pointer(&device)),
		uintptr(unsafe.Pointer(&level)),
		uintptr(unsafe.Pointer(&ctx)),
	)
	if uint32(hr) != sOK || device == 0 || ctx == 0 {
		return fmt.Errorf("D3D11CreateDevice 失败: 0x%08X", uint32(hr))
	}
	o.device = device
	o.ctx = ctx
	return nil
}

func enumAdapter(factory uintptr, index uint32) (uintptr, uint32) {
	if adp, hr := comCall2(factory, 12, uintptr(index)); hr == sOK && adp != 0 {
		return adp, hr
	}
	return comCall2(factory, 7, uintptr(index))
}

func (o *dxgiOutput) duplicate() error {
	if o.dup != 0 {
		comRelease(o.dup)
		o.dup = 0
	}
	// IDXGIOutput1::DuplicateOutput 在 vtable 22。部分驱动 QI Output1 会失败，
	// 但同一对象上直接调用 DuplicateOutput 仍然可用。
	var dup uintptr
	r, _, _ := syscall.SyscallN(vtblOf(o.output)[22], o.output, o.device, uintptr(unsafe.Pointer(&dup)))
	if uint32(r) != sOK || dup == 0 {
		return fmt.Errorf("DuplicateOutput 失败: 0x%08X", uint32(r))
	}
	o.dup = dup
	o.cacheOK = false
	return nil
}

func (o *dxgiOutput) startPump() {
	o.stop = make(chan struct{})
	o.done = make(chan struct{})
	go o.pump()
}

func (o *dxgiOutput) stopPump() {
	if o.stop == nil {
		return
	}
	select {
	case <-o.stop:
	default:
		close(o.stop)
	}
	if o.done != nil {
		<-o.done
		o.done = nil
	}
	o.stop = nil
}

func (o *dxgiOutput) pump() {
	defer close(o.done)
	LowerThreadPriority()
	lastCopy := time.Time{}
	for {
		select {
		case <-o.stop:
			return
		default:
		}
		wantCopy := time.Since(lastCopy) >= 50*time.Millisecond
		o.mu.Lock()
		copied, err := o.acquireLocked(wantCopy, 0)
		if err != nil {
			o.loopErr = err
		} else if copied {
			o.loopErr = nil
			lastCopy = time.Now()
		}
		o.mu.Unlock()
		if err != nil {
			select {
			case <-o.stop:
				return
			case <-time.After(15 * time.Millisecond):
			}
		}
	}
}

func (o *dxgiOutput) waitFrame(d time.Duration) error {
	deadline := time.Now().Add(d)
	for {
		o.mu.Lock()
		ok := o.cacheOK
		age := time.Since(o.cacheAt)
		err := o.loopErr
		o.mu.Unlock()
		if ok && age < 250*time.Millisecond {
			return nil
		}
		if !ok && err != nil {
			return err
		}
		if time.Now().After(deadline) {
			if ok {
				return nil
			}
			if err != nil {
				return err
			}
			return fmt.Errorf("等待桌面帧超时")
		}
		time.Sleep(2 * time.Millisecond)
	}
}

func (o *dxgiOutput) acquireLocked(wantCopy bool, depth int) (bool, error) {
	if o.dup == 0 {
		return false, fmt.Errorf("DXGI 复制会话无效")
	}

	timeout := uintptr(16)
	tries := 1
	if !o.cacheOK {
		timeout = 200
		tries = 6
	}

	var info dxgiFrameInfo
	var res uintptr
	var hr uint32
	for i := 0; i < tries; i++ {
		res = 0
		r, _, _ := syscall.SyscallN(
			vtblOf(o.dup)[8],
			o.dup,
			timeout,
			uintptr(unsafe.Pointer(&info)),
			uintptr(unsafe.Pointer(&res)),
		)
		hr = uint32(r)
		if hr == sOK && res != 0 {
			break
		}
		if res != 0 {
			comRelease(res)
			res = 0
		}
		if hr == dxgiErrorWaitTimeout {
			if o.cacheOK {
				return false, nil
			}
			continue
		}
		if hr == dxgiErrorAccessLost {
			if depth >= 1 {
				return false, errAccessLost
			}
			if err := o.duplicate(); err != nil {
				return false, err
			}
			return o.acquireLocked(wantCopy, depth+1)
		}
		return false, fmt.Errorf("AcquireNextFrame 失败: 0x%08X", hr)
	}
	if hr != sOK || res == 0 {
		if o.cacheOK {
			return false, nil
		}
		return false, fmt.Errorf("等待桌面帧超时")
	}
	defer comRelease(res)
	defer syscall.SyscallN(vtblOf(o.dup)[14], o.dup)
	if !wantCopy {
		return false, nil
	}

	tex, qhr := queryInterface(res, &iidID3D11Tex2D)
	if qhr != sOK || tex == 0 {
		return false, fmt.Errorf("桌面纹理 QI 失败: 0x%08X", qhr)
	}
	defer comRelease(tex)

	var desc d3d11TexDesc
	syscall.SyscallN(vtblOf(tex)[10], tex, uintptr(unsafe.Pointer(&desc)))
	if desc.Width == 0 || desc.Height == 0 {
		return false, fmt.Errorf("空桌面纹理")
	}
	switch desc.Format {
	case dxgiFormatBGRA, dxgiFormatBGRAsRGB, dxgiFormatRGBA, dxgiFormatRGBAsRGB, dxgiFormatRGBA16Float:
	default:
		return false, fmt.Errorf("不支持的桌面格式 %d", desc.Format)
	}
	if err := o.ensureStaging(&desc); err != nil {
		return false, err
	}

	syscall.SyscallN(vtblOf(o.ctx)[47], o.ctx, o.staging, tex)

	var mapped d3d11Mapped
	mr, _, _ := syscall.SyscallN(vtblOf(o.ctx)[14], o.ctx, o.staging, 0, d3d11MapRead, 0, uintptr(unsafe.Pointer(&mapped)))
	if uint32(mr) != sOK || mapped.PData == 0 {
		return false, fmt.Errorf("Map 失败: 0x%08X", uint32(mr))
	}
	defer syscall.SyscallN(vtblOf(o.ctx)[15], o.ctx, o.staging, 0)

	w, h := int(desc.Width), int(desc.Height)
	rowPitch := int(mapped.RowPitch)
	bpp := 4
	if desc.Format == dxgiFormatRGBA16Float {
		bpp = 8
	}
	if rowPitch < w*bpp {
		return false, fmt.Errorf("异常 RowPitch %d", rowPitch)
	}
	need := w * h * 4
	if cap(o.cache) < need {
		o.cache = make([]byte, need)
	} else {
		o.cache = o.cache[:need]
	}
	src := unsafe.Slice((*byte)(unsafe.Pointer(mapped.PData)), rowPitch*h)
	dstStride := w * 4
	for y := 0; y < h; y++ {
		s := src[y*rowPitch : y*rowPitch+w*bpp]
		d := o.cache[y*dstStride : (y+1)*dstStride]
		copyDesktopRow(d, s, w, desc.Format)
	}
	o.cacheW, o.cacheH, o.cacheOK = w, h, true
	o.cacheAt = time.Now()
	return true, nil
}

func copyDesktopRow(dst, src []byte, w int, format uint32) {
	switch format {
	case dxgiFormatRGBA16Float:
		for x := 0; x < w; x++ {
			si := x * 8
			di := x * 4
			r := halfToByte(src[si], src[si+1])
			g := halfToByte(src[si+2], src[si+3])
			b := halfToByte(src[si+4], src[si+5])
			a := halfToByte(src[si+6], src[si+7])
			dst[di+0], dst[di+1], dst[di+2], dst[di+3] = b, g, r, a
		}
	case dxgiFormatRGBA, dxgiFormatRGBAsRGB:
		for x := 0; x < w; x++ {
			si, di := x*4, x*4
			dst[di+0], dst[di+1], dst[di+2], dst[di+3] = src[si+2], src[si+1], src[si+0], src[si+3]
		}
	default:
		copy(dst[:w*4], src[:w*4])
	}
}

func halfToByte(lo, hi byte) byte {
	bits := uint16(lo) | uint16(hi)<<8
	if bits&0x8000 != 0 {
		return 0
	}
	exp := (bits >> 10) & 0x1f
	frac := bits & 0x3ff
	var f float64
	switch exp {
	case 0:
		f = float64(frac) / (1024 * 16384)
	case 31:
		return 255
	default:
		mant := 1 + float64(frac)/1024
		shift := int(exp) - 15
		if shift >= 0 {
			f = mant * float64(uint64(1)<<uint(shift))
		} else {
			f = mant / float64(uint64(1)<<uint(-shift))
		}
	}
	if f >= 1 {
		return 255
	}
	if f <= 0 {
		return 0
	}
	return byte(f*255 + 0.5)
}

func (o *dxgiOutput) ensureStaging(desc *d3d11TexDesc) error {
	w, h, fmtID := int(desc.Width), int(desc.Height), desc.Format
	if o.staging != 0 && o.stagingW == w && o.stagingH == h && o.stagingFmt == fmtID {
		return nil
	}
	if o.staging != 0 {
		comRelease(o.staging)
		o.staging = 0
	}
	stg := d3d11TexDesc{
		Width:          desc.Width,
		Height:         desc.Height,
		MipLevels:      1,
		ArraySize:      1,
		Format:         desc.Format,
		SampleCount:    1,
		Usage:          d3d11UsageStaging,
		CPUAccessFlags: d3d11CPUAccessRead,
	}
	var tex uintptr
	hr, _, _ := syscall.SyscallN(
		vtblOf(o.device)[5],
		o.device,
		uintptr(unsafe.Pointer(&stg)),
		0,
		uintptr(unsafe.Pointer(&tex)),
	)
	if uint32(hr) != sOK || tex == 0 {
		return fmt.Errorf("CreateTexture2D 失败: 0x%08X", uint32(hr))
	}
	o.staging = tex
	o.stagingW, o.stagingH, o.stagingFmt = w, h, fmtID
	return nil
}

func (o *dxgiOutput) cropTo(dst *[]byte, win rect) ([]byte, int, int, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if !o.cacheOK || len(o.cache) == 0 {
		return nil, 0, 0, fmt.Errorf("还没有桌面帧")
	}

	x, y, w, h := o.windowInDesktop(win)
	if w <= 0 || h <= 0 {
		return nil, 0, 0, fmt.Errorf("窗口不在当前显示器内")
	}
	need := w * h * 4
	if cap(*dst) < need {
		*dst = make([]byte, need)
	} else {
		*dst = (*dst)[:need]
	}
	srcStride := o.cacheW * 4
	dstStride := w * 4
	for row := 0; row < h; row++ {
		si := (y+row)*srcStride + x*4
		di := row * dstStride
		copy((*dst)[di:di+dstStride], o.cache[si:si+dstStride])
	}
	return *dst, w, h, nil
}

func (o *dxgiOutput) windowInDesktop(win rect) (x, y, w, h int) {
	x = int(win.Left - o.left)
	y = int(win.Top - o.top)
	w = int(win.Right - win.Left)
	h = int(win.Bottom - win.Top)
	if x < 0 {
		w += x
		x = 0
	}
	if y < 0 {
		h += y
		y = 0
	}
	if x+w > o.cacheW {
		w = o.cacheW - x
	}
	if y+h > o.cacheH {
		h = o.cacheH - y
	}
	if w < 0 {
		w = 0
	}
	if h < 0 {
		h = 0
	}
	return x, y, w, h
}

func (o *dxgiOutput) close() {
	o.stopPump()
	if o.staging != 0 {
		comRelease(o.staging)
		o.staging = 0
	}
	if o.dup != 0 {
		comRelease(o.dup)
		o.dup = 0
	}
	if o.ctx != 0 {
		comRelease(o.ctx)
		o.ctx = 0
	}
	if o.device != 0 {
		comRelease(o.device)
		o.device = 0
	}
	if o.output != 0 {
		comRelease(o.output)
		o.output = 0
	}
	if o.adapter != 0 {
		comRelease(o.adapter)
		o.adapter = 0
	}
	o.cache = nil
	o.cacheOK = false
}

func vtblOf(obj uintptr) *[64]uintptr {
	return (*[64]uintptr)(unsafe.Pointer(*(*uintptr)(unsafe.Pointer(obj))))
}

func comRelease(obj uintptr) {
	if obj == 0 {
		return
	}
	syscall.SyscallN(vtblOf(obj)[2], obj)
}

func queryInterface(obj uintptr, iid *windows.GUID) (uintptr, uint32) {
	var out uintptr
	r, _, _ := syscall.SyscallN(vtblOf(obj)[0], obj, uintptr(unsafe.Pointer(iid)), uintptr(unsafe.Pointer(&out)))
	return out, uint32(r)
}

func comCall2(obj uintptr, idx, a1 uintptr) (uintptr, uint32) {
	var out uintptr
	r, _, _ := syscall.SyscallN(vtblOf(obj)[idx], obj, a1, uintptr(unsafe.Pointer(&out)))
	return out, uint32(r)
}
