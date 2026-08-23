package wincap

import "github.com/lnatpunblhna/go-game-vision/pkg/capture"

// Capturer 绑定到一个窗口，可重复截图。
// Capture 返回的帧像素在下次 Capture 时失效，需要留存请 Clone。
type Capturer interface {
	Capture() (*capture.RawFrame, error)
	Close() error
	Method() string
}
