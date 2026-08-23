package vision

import (
	"testing"

	"github.com/lnatpunblhna/go-game-vision/pkg/capture"
)

func TestIsGameWindow(t *testing.T) {
	cases := []struct {
		title, process string
		want           bool
	}{
		{"MapleStory", "MapleStory.exe", true},
		{"冒险岛 Online", "Maple.exe", true},
		{"新楓之谷", "MapleStoryT.exe", true},
		{"记事本", "notepad.exe", false},
		{"", "maple.exe", true},
	}
	for _, c := range cases {
		if got := isGameWindow(c.title, c.process); got != c.want {
			t.Errorf("isGameWindow(%q, %q) = %v, want %v", c.title, c.process, got, c.want)
		}
	}
}

func TestListWindows(t *testing.T) {
	svc := New()
	t.Cleanup(svc.Close)
	list, err := svc.ListWindows()
	if err != nil {
		t.Fatalf("ListWindows: %v", err)
	}
	if list == nil {
		t.Fatal("ListWindows returned nil slice")
	}
}

func TestDefaultOptions(t *testing.T) {
	opts := DefaultOptions()
	if opts.FPS != 8 {
		t.Fatalf("default fps %d, want 8", opts.FPS)
	}
	opts.FPS = 60
	opts.normalize()
	if opts.FPS != 30 {
		t.Fatalf("capped fps %d, want 30", opts.FPS)
	}
}

func TestEncodeJPEG(t *testing.T) {
	const w, h = 8, 4
	pix := make([]byte, w*h*4)
	for i := 0; i < len(pix); i += 4 {
		pix[i+0] = 255 // B
		pix[i+1] = 0
		pix[i+2] = 0
		pix[i+3] = 255
	}
	frame := &capture.RawFrame{Pix: pix, Width: w, Height: h, Stride: w * 4}
	data, dw, dh, err := encodeJPEG(frame, 70, 960)
	if err != nil {
		t.Fatalf("encodeJPEG: %v", err)
	}
	if dw != w || dh != h {
		t.Fatalf("size %dx%d, want %dx%d", dw, dh, w, h)
	}
	if len(data) == 0 {
		t.Fatal("empty jpeg")
	}

	data, dw, dh, err = encodeJPEG(frame, 70, 4)
	if err != nil {
		t.Fatalf("encodeJPEG scaled: %v", err)
	}
	if dw != 4 || dh != 2 {
		t.Fatalf("scaled size %dx%d, want 4x2", dw, dh)
	}
	if len(data) == 0 {
		t.Fatal("empty scaled jpeg")
	}
}
