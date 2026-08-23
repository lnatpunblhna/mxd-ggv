package overlay

const (
	crossSec     = 4.8
	emptySpawn   = 0.65
	lowSpawn     = 1.05
	laneTopFrac  = 0.07
	laneBandFrac = 0.30
	laneGapPx    = 96
)

type bullet struct {
	text  string
	kind  string
	level string
	x     float64
	y     float64
	w     float64
	speed float64
	lane  int
}

type engine struct {
	bullets []bullet
	acc     float64
	next    int
}

func overlayBand(gameW, gameH int) (xOff, yOff, w, h int) {
	if gameW < 8 || gameH < 8 {
		return 0, 0, gameW, gameH
	}
	px := fontPx(float64(gameH))
	top := int(float64(gameH) * laneTopFrac)
	band := int(float64(gameH) * laneBandFrac)
	yOff = top - 8
	if yOff < 0 {
		yOff = 0
	}
	h = band + px + 20
	if yOff+h > gameH {
		h = gameH - yOff
	}
	if h < px+8 {
		h = px + 8
		if h > gameH {
			h = gameH
			yOff = 0
		}
	}
	return 0, yOff, gameW, h
}

func fontPx(height float64) int {
	n := int(height * 0.052)
	if n < 18 {
		n = 18
	}
	if n > 48 {
		n = 48
	}
	return n
}

func laneCount(height float64) int {
	gap := fontPx(height) + 8
	if gap < 1 {
		gap = 1
	}
	n := int(height*laneBandFrac) / gap
	if n < 3 {
		n = 3
	}
	if n > 6 {
		n = 6
	}
	return n
}

func laneY(lane, nLane int, height float64) float64 {
	if nLane < 1 {
		nLane = 1
	}
	top := height * laneTopFrac
	band := height * laneBandFrac
	return top + band*float64(lane)/float64(nLane)
}

func spawnGap(lines []Line) float64 {
	for _, l := range lines {
		if l.Level == "empty" {
			return emptySpawn
		}
	}
	return lowSpawn
}

func defaultMeasure(text string, px int) float64 {
	n := 0
	for range text {
		n++
	}
	return float64(n)*float64(px)*0.90 + float64(px)*0.5
}

func (e *engine) tick(dt, width, height float64, lines []Line, measure func(string, int) float64) {
	if dt <= 0 || width < 8 || height < 8 {
		return
	}
	if measure == nil {
		measure = defaultMeasure
	}

	n := 0
	for _, b := range e.bullets {
		b.x -= b.speed * dt
		if b.x+b.w > 0 {
			e.bullets[n] = b
			n++
		}
	}
	e.bullets = e.bullets[:n]

	if len(lines) == 0 {
		e.acc = 0
		return
	}

	e.acc += dt
	gap := spawnGap(lines)
	if e.acc < gap {
		return
	}

	nLane := laneCount(height)
	lane := e.freeLane(nLane, width)
	if lane < 0 {
		return
	}

	line := lines[e.next%len(lines)]
	e.next++
	px := fontPx(height)
	w := measure(line.Text, px)
	speed := width / crossSec
	if speed < 80 {
		speed = 80
	}
	e.bullets = append(e.bullets, bullet{
		text:  line.Text,
		kind:  line.Kind,
		level: line.Level,
		x:     width,
		y:     laneY(lane, nLane, height),
		w:     w,
		speed: speed,
		lane:  lane,
	})
	e.acc = 0
}

func (e *engine) freeLane(nLane int, width float64) int {
	if nLane < 1 {
		return -1
	}
	busy := make([]bool, nLane)
	for _, b := range e.bullets {
		if b.lane >= 0 && b.lane < nLane && b.x+b.w > width-laneGapPx {
			busy[b.lane] = true
		}
	}
	start := e.next % nLane
	for i := 0; i < nLane; i++ {
		lane := (start + i) % nLane
		if !busy[lane] {
			return lane
		}
	}
	return -1
}
