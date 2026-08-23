package potion

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestReadUserCalib339And163(t *testing.T) {
	bank := loadTestBank(t, filepath.Join("testdata", "calib339.json"))
	mp, err := readPNG(filepath.Join("testdata", "mp339.png"))
	if err != nil {
		t.Fatal(err)
	}
	if got := readCount(mp, bank); got != 339 {
		dumpCountDebug(t, "mp339", mp, bank)
		t.Fatalf("mp339=%d want 339", got)
	}
	hp, err := readPNG(filepath.Join("testdata", "hp163.png"))
	if err != nil {
		t.Fatal(err)
	}
	if got := readCount(hp, bank); got != 163 {
		dumpCountDebug(t, "hp163", hp, bank)
		t.Fatalf("hp163=%d want 163", got)
	}
}

func loadTestBank(t *testing.T, path string) *digitBank {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var doc persisted
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	bank := newDigitBank()
	for _, g := range doc.Digits {
		if g.W*g.H != len(g.Bits) || g.Digit < 0 || g.Digit > 9 {
			continue
		}
		bits := make([]bool, len(g.Bits))
		for i, ch := range g.Bits {
			bits[i] = ch == '1'
		}
		bank.learn(g.Digit, digitTmpl{Digit: g.Digit, W: g.W, H: g.H, Bits: bits})
	}
	return bank
}
