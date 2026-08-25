package potion

import (
	"path/filepath"
	"testing"
)

// realSlots 是从游戏里截的真实药格，数量已知。
var realSlots = []struct {
	file string
	want int
}{
	{"hp.png", 1},
	{"mp.png", 343},
	{"hp188.png", 188},
	{"mp341.png", 341},
	{"mp339.png", 339},
	{"hp163.png", 163},
}

// TestStencilReadsRealSlots 验证只靠解包出来的原始字形（不预先学习）
// 就能把真实药格的数量读对。
func TestStencilReadsRealSlots(t *testing.T) {
	for _, c := range realSlots {
		im, err := readPNG(filepath.Join("testdata", c.file))
		if err != nil {
			t.Fatal(err)
		}
		hits := readByStencil(countRegion(im), newStencilHint())
		got, score, k := assembleHits(hits)
		if got != c.want {
			t.Errorf("%s 原始字形匹配得到 %d，期望 %d（score=%.3f hits=%d）", c.file, got, c.want, score, k)
			for i, h := range hits {
				t.Logf("  hit%d d=%d s=%.3f at(%d,%d) %dx%d", i, h.d, h.s, h.x, h.y, h.w, h.h)
			}
		}
	}
}

// TestReadCountRealSlotsColdBank 走完整读数流程，同样不预先学习。
func TestReadCountRealSlotsColdBank(t *testing.T) {
	for _, c := range realSlots {
		im, err := readPNG(filepath.Join("testdata", c.file))
		if err != nil {
			t.Fatal(err)
		}
		if got := readCount(im, newStencilHint()); got != c.want {
			t.Errorf("%s readCount=%d 期望 %d", c.file, got, c.want)
			dumpCountDebug(t, c.file, im, newStencilHint())
		}
	}
}

// TestStencilSizeHint 命中过的字号档位要记下来，下次直接用。
func TestStencilSizeHint(t *testing.T) {
	im, err := readPNG(filepath.Join("testdata", "mp339.png"))
	if err != nil {
		t.Fatal(err)
	}
	hint := newStencilHint()
	if hint.size != -1 {
		t.Fatalf("新建的 hint 不该有字号提示，got %d", hint.size)
	}
	region := countRegion(im)
	if n, _, _ := assembleHits(readByStencil(region, hint)); n != 339 {
		t.Fatalf("首次读数 %d，期望 339", n)
	}
	got := hint.size
	if got < 0 || got >= len(stencilSizes) {
		t.Fatalf("字号提示 %d 越界", got)
	}
	if n, _, _ := assembleHits(readByStencil(region, hint)); n != 339 {
		t.Fatal("带提示重读失败")
	}
	if hint.size != got {
		t.Errorf("提示被改成 %d，原为 %d", hint.size, got)
	}
}

// TestStencilRejectsPlainIcon 没有数量的药格不能凭空读出数字。
func TestStencilRejectsPlainIcon(t *testing.T) {
	icon := checker(40, 40, 25, 40, 210, 50, 160, 45)
	if hits := readByStencil(countRegion(icon), newStencilHint()); len(hits) > 0 {
		n, s, _ := assembleHits(hits)
		t.Errorf("空药格读出了 %d (score=%.3f)", n, s)
	}
}

// BenchmarkReadCountWarmHint 测的是字号提示命中后的稳态开销。
func BenchmarkReadCountWarmHint(b *testing.B) {
	im, err := readPNG(filepath.Join("testdata", "hp188.png"))
	if err != nil {
		b.Fatal(err)
	}
	hint := newStencilHint()
	readCount(im, hint)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		readCount(im, hint)
	}
}
