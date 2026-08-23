package potion

const (
	presentNCC = 0.80
	emptyNCC   = 0.70
	satEmpty   = 0.50
	stdBusy    = 1.65
)

type slotTemplate struct {
	Img     *bgra
	MeanSat float64
	StdLuma float64
}

func newSlotTemplate(im *bgra) slotTemplate {
	sat, std := stats(im)
	return slotTemplate{Img: cloneBGRA(im), MeanSat: sat, StdLuma: std}
}

type slotSample struct {
	kind   string
	raw    SlotState
	count  int
	ncc    float64
	bar    float64
	reason string
}

func classifySlot(cur *bgra, tmpl slotTemplate) (state SlotState, ncc float64) {
	if cur == nil || tmpl.Img == nil || tmpl.Img.empty() {
		return SlotUnknown, 0
	}
	cmp := cur
	if cur.W != tmpl.Img.W || cur.H != tmpl.Img.H {
		cmp = scaleNearest(cur, tmpl.Img.W, tmpl.Img.H)
	}
	ncc = nccGraySkipInk(cmp, tmpl.Img)
	if ncc >= presentNCC {
		return SlotOK, ncc
	}
	sat, std := stats(cmp)
	if tmpl.StdLuma > 4 && std > tmpl.StdLuma*stdBusy {
		return SlotUnknown, ncc
	}
	satRatio := 1.0
	if tmpl.MeanSat > 0.05 {
		satRatio = sat / tmpl.MeanSat
	}
	if ncc < emptyNCC && satRatio < satEmpty {
		return SlotEmpty, ncc
	}
	if ncc < 0.55 {
		return SlotEmpty, ncc
	}
	return SlotUnknown, ncc
}
