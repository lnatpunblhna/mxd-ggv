package potion

import (
	"encoding/json"
	"image"
	"image/png"
	"os"
	"path/filepath"
)

type persisted struct {
	HPSlot  RelRect    `json:"hpSlot"`
	MPSlot  RelRect    `json:"mpSlot"`
	HPBar   RelRect    `json:"hpBar"`
	MPBar   RelRect    `json:"mpBar"`
	HPCol   ColorRange `json:"hpCol"`
	MPCol   ColorRange `json:"mpCol"`
	FrameW  int        `json:"frameW"`
	FrameH  int        `json:"frameH"`
	Digits  []pglyph   `json:"digits"`
	HPCount int        `json:"hpCount,omitempty"`
	MPCount int        `json:"mpCount,omitempty"`
}

type pglyph struct {
	Digit int    `json:"digit"`
	W     int    `json:"w,omitempty"`
	H     int    `json:"h,omitempty"`
	Bits  string `json:"bits"`
}

var calibDirFn = defaultCalibDir

func defaultCalibDir() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(base, "mxd", "potion")
	return dir, os.MkdirAll(dir, 0o755)
}

func calibDir() (string, error) {
	return calibDirFn()
}

func saveCalibration(c *calibration) error {
	if c == nil {
		return nil
	}
	dir, err := calibDir()
	if err != nil {
		return err
	}
	doc := persisted{
		HPSlot:  c.HPSlot,
		MPSlot:  c.MPSlot,
		HPBar:   c.HPBar,
		MPBar:   c.MPBar,
		HPCol:   c.HPCol,
		MPCol:   c.MPCol,
		FrameW:  c.FrameW,
		FrameH:  c.FrameH,
		HPCount: c.HPCount,
		MPCount: c.MPCount,
	}
	if c.Digits != nil {
		for d := 0; d <= 9; d++ {
			for _, g := range c.Digits.learned[d] {
				bits := make([]byte, len(g.Bits))
				for i, on := range g.Bits {
					if on {
						bits[i] = '1'
					} else {
						bits[i] = '0'
					}
				}
				doc.Digits = append(doc.Digits, pglyph{Digit: d, W: g.W, H: g.H, Bits: string(bits)})
			}
		}
	}
	raw, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "calib.json"), raw, 0o644); err != nil {
		return err
	}
	if c.HPTmpl.Img != nil {
		if err := writePNG(filepath.Join(dir, "hp.png"), c.HPTmpl.Img); err != nil {
			return err
		}
	} else {
		_ = os.Remove(filepath.Join(dir, "hp.png"))
	}
	if c.MPTmpl.Img != nil {
		if err := writePNG(filepath.Join(dir, "mp.png"), c.MPTmpl.Img); err != nil {
			return err
		}
	} else {
		_ = os.Remove(filepath.Join(dir, "mp.png"))
	}
	return nil
}

func loadCalibration() (*calibration, error) {
	dir, err := calibDir()
	if err != nil {
		return nil, err
	}
	raw, err := os.ReadFile(filepath.Join(dir, "calib.json"))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var doc persisted
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, err
	}
	c := &calibration{
		HPSlot:  doc.HPSlot,
		MPSlot:  doc.MPSlot,
		HPBar:   doc.HPBar,
		MPBar:   doc.MPBar,
		HPCol:   doc.HPCol,
		MPCol:   doc.MPCol,
		FrameW:  doc.FrameW,
		FrameH:  doc.FrameH,
		Digits:  newDigitBank(),
		HPCount: doc.HPCount,
		MPCount: doc.MPCount,
	}
	if !c.HPCol.valid() {
		c.HPCol = presetRed
	}
	if !c.MPCol.valid() {
		c.MPCol = presetBlue
	}
	if hp, err := readPNG(filepath.Join(dir, "hp.png")); err == nil && hp != nil {
		c.HPTmpl = newSlotTemplate(hp)
	}
	if mp, err := readPNG(filepath.Join(dir, "mp.png")); err == nil && mp != nil {
		c.MPTmpl = newSlotTemplate(mp)
	}
	for _, g := range doc.Digits {
		if g.Digit < 0 || g.Digit > 9 {
			continue
		}
		w, h := g.W, g.H
		if w <= 0 || h <= 0 {
			w, h = 8, 12
		}
		if w*h != len(g.Bits) {
			continue
		}
		bits := make([]bool, len(g.Bits))
		for i, ch := range g.Bits {
			bits[i] = ch == '1'
		}
		c.Digits.learn(g.Digit, digitTmpl{Digit: g.Digit, W: w, H: h, Bits: bits})
	}
	if !c.ready() {
		return nil, nil
	}
	return c, nil
}

func writePNG(path string, im *bgra) error {
	img := image.NewRGBA(image.Rect(0, 0, im.W, im.H))
	for y := 0; y < im.H; y++ {
		srow := im.Pix[y*im.Stride:]
		drow := img.Pix[y*img.Stride:]
		for x := 0; x < im.W; x++ {
			si, di := x*4, x*4
			drow[di+0] = srow[si+2]
			drow[di+1] = srow[si+1]
			drow[di+2] = srow[si+0]
			drow[di+3] = srow[si+3]
		}
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return png.Encode(f, img)
}

func readPNG(path string) (*bgra, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	img, err := png.Decode(f)
	if err != nil {
		return nil, err
	}
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	out := &bgra{Pix: make([]byte, w*h*4), W: w, H: h, Stride: w * 4}
	for y := 0; y < h; y++ {
		row := out.Pix[y*out.Stride:]
		for x := 0; x < w; x++ {
			r, g, bl, a := img.At(b.Min.X+x, b.Min.Y+y).RGBA()
			i := x * 4
			row[i+0] = byte(bl >> 8)
			row[i+1] = byte(g >> 8)
			row[i+2] = byte(r >> 8)
			row[i+3] = byte(a >> 8)
		}
	}
	return out, nil
}
