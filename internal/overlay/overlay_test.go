package overlay

import "testing"

func TestFromSlots(t *testing.T) {
	if got := FromSlots("ok", 40, "ok", 40); len(got) != 0 {
		t.Fatalf("ok slots should be silent, got %+v", got)
	}
	lines := FromSlots("empty", 0, "low", 6)
	if len(lines) != 2 {
		t.Fatalf("want 2 lines, got %+v", lines)
	}
	if lines[0].Kind != "hp" || lines[0].Level != "empty" || lines[0].Text != "血药已空  快补药！" {
		t.Fatalf("hp line = %+v", lines[0])
	}
	if lines[1].Kind != "mp" || lines[1].Text != "蓝药不足  剩余 6" {
		t.Fatalf("mp line = %+v", lines[1])
	}
	low := FromSlots("low", -1, "unknown", 0)
	if len(low) != 1 || low[0].Text != "血药不足  请及时补给" {
		t.Fatalf("unknown count = %+v", low)
	}
}

func TestEngineKeepsSpawning(t *testing.T) {
	e := &engine{}
	lines := FromSlots("empty", 0, "absent", 0)
	var seen int
	for i := 0; i < 200; i++ {
		e.tick(0.05, 800, 600, lines, nil)
		if n := len(e.bullets); n > seen {
			seen = n
		}
	}
	if seen < 3 {
		t.Fatalf("expected looping danmaku, max bullets = %d", seen)
	}
	alive := 0
	for _, b := range e.bullets {
		if b.kind != "hp" || b.text == "" {
			t.Fatalf("bad bullet %+v", b)
		}
		if b.y < 600*laneTopFrac-1 || b.y > 600*(laneTopFrac+laneBandFrac)+1 {
			t.Fatalf("bullet left the top band: y=%v", b.y)
		}
		alive++
	}
	if alive == 0 {
		t.Fatal("bullets should remain on screen")
	}

	e.tick(0.05, 800, 600, nil, nil)
	e.tick(8, 800, 600, nil, nil)
	if len(e.bullets) != 0 {
		t.Fatalf("cleared lines should drain, leftover %+v", e.bullets)
	}
}

func TestOverlayBandStaysInTop(t *testing.T) {
	_, y, w, h := overlayBand(1280, 720)
	if w != 1280 {
		t.Fatalf("width %d", w)
	}
	if y < 0 || y > 80 {
		t.Fatalf("band y=%d should sit near the top", y)
	}
	if y+h > 720/2 {
		t.Fatalf("band should not cover the playfield, y=%d h=%d", y, h)
	}
	if h < 80 {
		t.Fatalf("band too short: %d", h)
	}
}

func TestEngineDoesNotOverlapLanes(t *testing.T) {
	e := &engine{}
	lines := []Line{{Kind: "hp", Level: "low", Text: "血药不足  剩余 8"}}
	e.tick(2, 640, 400, lines, nil)
	e.tick(0.01, 640, 400, lines, nil)
	if len(e.bullets) != 1 {
		t.Fatalf("second spawn should wait for lane gap, got %d", len(e.bullets))
	}
}
