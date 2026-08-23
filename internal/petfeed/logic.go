package petfeed

import "time"

const (
	// DecayInterval 是饱满感自然下降的间隔。
	DecayInterval = 1 * time.Minute
	// DecayAmount 是每次下降的点数。
	DecayAmount = 1
	// FeedThreshold 低于该值时自动喂食。
	FeedThreshold = 70
	// FeedAmount 每次喂食增加的饱满感。
	FeedAmount = 30
	// MaxFullness 饱满感上限。
	MaxFullness = 100
)

// ClampFullness 将饱满感限制在 [0, 100]。
func ClampFullness(n int) int {
	if n < 0 {
		return 0
	}
	if n > MaxFullness {
		return MaxFullness
	}
	return n
}

// Decay 每 1 分钟减 1 点，不低于 0。
func Decay(fullness int) int {
	return ClampFullness(fullness - DecayAmount)
}

// NeedsFeed 报告当前是否应自动喂食。
func NeedsFeed(fullness int) bool {
	return fullness < FeedThreshold
}

// AfterFeed 返回喂食后的饱满感。
func AfterFeed(fullness int) int {
	return ClampFullness(fullness + FeedAmount)
}
