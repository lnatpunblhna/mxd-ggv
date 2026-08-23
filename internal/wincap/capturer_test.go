package wincap

import (
	"testing"

	"github.com/lnatpunblhna/go-game-vision/pkg/capture"
)

func TestNewRejectsZero(t *testing.T) {
	if _, err := New(0); err == nil {
		t.Fatal("expected error for handle 0")
	}
}

func TestCaptureVisibleWindow(t *testing.T) {
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
	c, err := New(uint64(hwnd))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	frame, err := c.Capture()
	if err != nil {
		t.Fatalf("Capture: %v", err)
	}
	if frame.Width < 1 || frame.Height < 1 || len(frame.Pix) < 4 {
		t.Fatalf("empty frame %dx%d pix=%d", frame.Width, frame.Height, len(frame.Pix))
	}
	if m := c.Method(); m == "" {
		t.Fatal("empty capture method")
	} else {
		t.Log("method", m)
	}
}
