//go:build windows

package wincap

import (
	"testing"

	"github.com/lnatpunblhna/go-game-vision/pkg/capture"
)

func TestDXGIBind(t *testing.T) {
	found, err := capture.FindWindows(&capture.WindowQuery{MinWidth: 160, MinHeight: 80})
	if err != nil || len(found) == 0 {
		t.Skip("no windows")
	}
	var hwnd uintptr
	for _, w := range found {
		if !w.IsHidden && w.Rect.Dx() >= 160 && w.Rect.Dy() >= 80 {
			hwnd = w.Handle
			break
		}
	}
	if hwnd == 0 {
		t.Skip("no visible window")
	}
	c := &dxgiCap{hwnd: hwnd}
	t.Cleanup(func() { _ = c.Close() })
	frame, err := c.captureDXGILocked()
	if err != nil {
		t.Fatalf("DXGI capture failed: %v", err)
	}
	if frame.Width < 1 || frame.Height < 1 || len(frame.Pix) < 4 {
		t.Fatalf("empty frame %dx%d", frame.Width, frame.Height)
	}
	if c.Method() != "DXGI" {
		t.Fatalf("method %s", c.Method())
	}
}

func TestHalfToByte(t *testing.T) {
	if got := halfToByte(0x00, 0x00); got != 0 {
		t.Fatalf("0 got %d", got)
	}
	if got := halfToByte(0x00, 0x3C); got != 255 { // 1.0
		t.Fatalf("1.0 got %d", got)
	}
	got := halfToByte(0x00, 0x38) // 0.5
	if got < 120 || got > 135 {
		t.Fatalf("0.5 got %d", got)
	}
}

func TestWindowInDesktopClips(t *testing.T) {
	o := &dxgiOutput{left: 0, top: 0, cacheW: 1920, cacheH: 1080}
	x, y, w, h := o.windowInDesktop(rect{Left: 100, Top: 50, Right: 300, Bottom: 250})
	if x != 100 || y != 50 || w != 200 || h != 200 {
		t.Fatalf("got %d,%d %dx%d", x, y, w, h)
	}

	x, y, w, h = o.windowInDesktop(rect{Left: -40, Top: -20, Right: 80, Bottom: 60})
	if x != 0 || y != 0 || w != 80 || h != 60 {
		t.Fatalf("offscreen got %d,%d %dx%d", x, y, w, h)
	}

	o.left, o.top = -1920, 0
	x, y, w, h = o.windowInDesktop(rect{Left: -1920, Top: 0, Right: -960, Bottom: 540})
	if x != 0 || y != 0 || w != 960 || h != 540 {
		t.Fatalf("left monitor got %d,%d %dx%d", x, y, w, h)
	}
}
