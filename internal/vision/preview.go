package vision

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"image/jpeg"
	"time"

	"mxd/internal/wincap"

	"github.com/lnatpunblhna/go-game-vision/pkg/capture"
)

// StartCapture 对指定窗口启动实时截图预览。
func (s *Service) StartCapture(handle uint64, opts Options) error {
	if handle == 0 {
		return fmt.Errorf("未选择窗口")
	}
	opts.normalize()

	if err := s.StopCapture(); err != nil {
		return err
	}

	capturer, err := wincap.New(handle)
	if err != nil {
		return fmt.Errorf("创建截图会话失败: %w", err)
	}

	stop := make(chan struct{})
	done := make(chan struct{})

	s.mu.Lock()
	s.capturer = capturer
	s.stop = stop
	s.done = done
	s.running = true
	s.opts = opts
	s.mu.Unlock()

	s.frameMu.Lock()
	s.latest = FramePayload{}
	s.seq = 0
	s.frameWait = make(chan struct{})
	s.frameMu.Unlock()

	go s.loop(capturer, stop, done)
	return nil
}

// StopCapture 停止预览。
func (s *Service) StopCapture() error {
	s.mu.Lock()
	if !s.running {
		s.mu.Unlock()
		return nil
	}
	stop := s.stop
	done := s.done
	capturer := s.capturer
	s.running = false
	s.stop = nil
	s.done = nil
	s.capturer = nil
	s.mu.Unlock()

	if stop != nil {
		close(stop)
	}
	if done != nil {
		<-done
	}
	if capturer != nil {
		return capturer.Close()
	}
	return nil
}

// UpdateOptions 热更新预览参数。
func (s *Service) UpdateOptions(opts Options) error {
	opts.normalize()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.opts = opts
	return nil
}

// NextFrame 长轮询下一帧。seq 为前端已收到的序号。
func (s *Service) NextFrame(seq int) FramePayload {
	s.frameMu.Lock()
	if s.latest.Seq > seq {
		f := s.latest
		s.frameMu.Unlock()
		return f
	}
	wait := s.frameWait
	s.frameMu.Unlock()

	if wait == nil {
		return FramePayload{Seq: seq}
	}

	timer := time.NewTimer(200 * time.Millisecond)
	defer timer.Stop()
	select {
	case <-wait:
		s.frameMu.Lock()
		f := s.latest
		s.frameMu.Unlock()
		return f
	case <-timer.C:
		return FramePayload{Seq: seq}
	}
}

func (s *Service) loop(capturer wincap.Capturer, stop <-chan struct{}, done chan struct{}) {
	defer close(done)
	wincap.LowerThreadPriority()

	var (
		frames int
		window = time.Now()
		fps    float64
	)

	for {
		s.mu.Lock()
		opts := s.opts
		s.mu.Unlock()

		interval := time.Second / time.Duration(opts.FPS)
		start := time.Now()

		payload := s.grab(capturer, opts)
		elapsed := time.Since(start)

		frames++
		if d := time.Since(window); d >= time.Second {
			fps = float64(frames) / d.Seconds()
			frames = 0
			window = time.Now()
		}
		payload.FPS = fps
		payload.CaptureMS = float64(elapsed.Microseconds()) / 1000

		s.publish(payload)

		wait := interval - elapsed
		if wait < 0 {
			wait = 0
		}
		timer := time.NewTimer(wait)
		select {
		case <-stop:
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

func (s *Service) grab(capturer wincap.Capturer, opts Options) FramePayload {
	method := capturer.Method()
	frame, err := capturer.Capture()
	if err != nil {
		return FramePayload{Error: err.Error(), Method: method}
	}

	jpegBytes, w, h, err := encodeJPEGBuf(frame, opts.Quality, opts.MaxWidth, &s.rgbaBuf, &s.jpegBuf)
	if err != nil {
		return FramePayload{Error: err.Error(), Method: method}
	}

	return FramePayload{
		Data:      base64.StdEncoding.EncodeToString(jpegBytes),
		Width:     w,
		Height:    h,
		SrcWidth:  frame.Width,
		SrcHeight: frame.Height,
		Method:    capturer.Method(),
	}
}

func (s *Service) publish(payload FramePayload) {
	s.frameMu.Lock()
	s.seq++
	payload.Seq = s.seq
	s.latest = payload
	old := s.frameWait
	s.frameWait = make(chan struct{})
	s.frameMu.Unlock()
	if old != nil {
		close(old)
	}
}

func (o *Options) normalize() {
	if o.FPS < 1 {
		o.FPS = 1
	}
	if o.FPS > 30 {
		o.FPS = 30
	}
	if o.Quality < 20 {
		o.Quality = 20
	}
	if o.Quality > 95 {
		o.Quality = 95
	}
	if o.MaxWidth < 160 {
		o.MaxWidth = 160
	}
	if o.MaxWidth > 1920 {
		o.MaxWidth = 1920
	}
}

func encodeJPEG(frame *capture.RawFrame, quality, maxWidth int) ([]byte, int, int, error) {
	return encodeJPEGBuf(frame, quality, maxWidth, nil, nil)
}

func encodeJPEGBuf(frame *capture.RawFrame, quality, maxWidth int, dst **image.RGBA, buf *bytes.Buffer) ([]byte, int, int, error) {
	if frame == nil || frame.Width <= 0 || frame.Height <= 0 {
		return nil, 0, 0, fmt.Errorf("空画面")
	}

	srcW, srcH := frame.Width, frame.Height
	dstW, dstH := srcW, srcH
	if maxWidth > 0 && srcW > maxWidth {
		dstW = maxWidth
		dstH = srcH * maxWidth / srcW
		if dstH < 1 {
			dstH = 1
		}
	}

	var img *image.RGBA
	if dst != nil && *dst != nil && (*dst).Bounds().Dx() == dstW && (*dst).Bounds().Dy() == dstH {
		img = *dst
	} else {
		img = image.NewRGBA(image.Rect(0, 0, dstW, dstH))
		if dst != nil {
			*dst = img
		}
	}
	if dstW == srcW && dstH == srcH {
		copyBGRAToRGBA(frame, img)
	} else {
		scaleBGRAToRGBA(frame, img)
	}

	if buf == nil {
		var local bytes.Buffer
		buf = &local
	} else {
		buf.Reset()
	}
	if err := jpeg.Encode(buf, img, &jpeg.Options{Quality: quality}); err != nil {
		return nil, 0, 0, err
	}
	return buf.Bytes(), dstW, dstH, nil
}

func copyBGRAToRGBA(frame *capture.RawFrame, dst *image.RGBA) {
	w, h := frame.Width, frame.Height
	for y := 0; y < h; y++ {
		srcRow := frame.Pix[y*frame.Stride : y*frame.Stride+w*4]
		dstRow := dst.Pix[y*dst.Stride : y*dst.Stride+w*4]
		for x := 0; x < w*4; x += 4 {
			dstRow[x+0] = srcRow[x+2]
			dstRow[x+1] = srcRow[x+1]
			dstRow[x+2] = srcRow[x+0]
			dstRow[x+3] = srcRow[x+3]
		}
	}
}

func scaleBGRAToRGBA(frame *capture.RawFrame, dst *image.RGBA) {
	srcW, srcH := frame.Width, frame.Height
	dstW, dstH := dst.Bounds().Dx(), dst.Bounds().Dy()
	for y := 0; y < dstH; y++ {
		sy := y * srcH / dstH
		srcRow := frame.Pix[sy*frame.Stride:]
		dstRow := dst.Pix[y*dst.Stride:]
		for x := 0; x < dstW; x++ {
			sx := x * srcW / dstW
			si := sx * 4
			di := x * 4
			dstRow[di+0] = srcRow[si+2]
			dstRow[di+1] = srcRow[si+1]
			dstRow[di+2] = srcRow[si+0]
			dstRow[di+3] = srcRow[si+3]
		}
	}
}
