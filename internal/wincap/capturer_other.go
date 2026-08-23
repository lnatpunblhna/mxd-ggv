//go:build !windows

package wincap

import (
	"fmt"

	"github.com/lnatpunblhna/go-game-vision/pkg/capture"
)

const Method = "系统"

type wrap struct {
	inner capture.FrameCapturer
}

// New 在非 Windows 上回退到库自带的窗口截图。
func New(handle uint64) (Capturer, error) {
	if handle == 0 {
		return nil, fmt.Errorf("未选择窗口")
	}
	inner, err := capture.NewFrameCapturerForWindow(uintptr(handle))
	if err != nil {
		return nil, err
	}
	return &wrap{inner: inner}, nil
}

func (w *wrap) Capture() (*capture.RawFrame, error) {
	return w.inner.Capture()
}

func (w *wrap) Close() error {
	return w.inner.Close()
}

func (w *wrap) Method() string { return Method }

func LowerThreadPriority() {}
