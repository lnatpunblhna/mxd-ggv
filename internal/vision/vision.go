package vision

import (
	"bytes"
	"image"
	"path/filepath"
	"strings"
	"sync"

	"mxd/internal/wincap"

	"github.com/lnatpunblhna/go-game-vision/pkg/capture"
	"github.com/lnatpunblhna/go-game-vision/pkg/mouse"
	"github.com/lnatpunblhna/go-game-vision/pkg/process"
	"github.com/lnatpunblhna/go-game-vision/pkg/utils"
)

// 窗口标题 / 进程名中用于识别冒险岛客户端的关键字。
var gameKeywords = []string{
	"maplestory",
	"maple story",
	"冒险岛",
	"冒險島",
	"新楓之谷",
	"메이플스토리",
	"メイプルストーリー",
}

// WindowInfo 是暴露给前端的窗口摘要。
type WindowInfo struct {
	PID     uint32 `json:"pid"`
	Handle  uint64 `json:"handle"`
	Title   string `json:"title"`
	Process string `json:"process"`
	Width   int    `json:"width"`
	Height  int    `json:"height"`
	IsGame  bool   `json:"isGame"`
	Hidden  bool   `json:"hidden"`
}

// Options 控制实时预览的采样与编码。
type Options struct {
	FPS      int `json:"fps"`
	Quality  int `json:"quality"`
	MaxWidth int `json:"maxWidth"`
}

// FramePayload 是一帧 JPEG 预览。
type FramePayload struct {
	Seq       int     `json:"seq"`
	Data      string  `json:"data"`
	Width     int     `json:"width"`
	Height    int     `json:"height"`
	SrcWidth  int     `json:"srcWidth"`
	SrcHeight int     `json:"srcHeight"`
	FPS       float64 `json:"fps"`
	CaptureMS float64 `json:"captureMS"`
	Method    string  `json:"method"`
	Error     string  `json:"error"`
}

// Service 封装 go-game-vision 的进程、截图与鼠标能力。
type Service struct {
	mouse mouse.MouseClicker

	mu       sync.Mutex
	capturer wincap.Capturer
	stop     chan struct{}
	done     chan struct{}
	running  bool
	opts     Options

	rgbaBuf *image.RGBA
	jpegBuf bytes.Buffer

	frameMu   sync.Mutex
	latest    FramePayload
	seq       int
	frameWait chan struct{}
}

// New 创建视觉服务。
func New() *Service {
	utils.SetLogLevel(utils.WARN)
	return &Service{
		mouse:     mouse.NewMouseClicker(),
		opts:      DefaultOptions(),
		frameWait: make(chan struct{}),
	}
}

// DefaultOptions 返回预览默认参数。
func DefaultOptions() Options {
	return Options{FPS: 8, Quality: 70, MaxWidth: 960}
}

// ListWindows 列出当前可见的应用窗口。
func (s *Service) ListWindows() ([]WindowInfo, error) {
	found, err := capture.FindWindows(&capture.WindowQuery{
		MinWidth:  160,
		MinHeight: 80,
	})
	if err != nil {
		return []WindowInfo{}, err
	}

	out := make([]WindowInfo, 0, len(found))
	for _, w := range found {
		title := strings.TrimSpace(w.Title)
		if title == "" {
			continue
		}
		owner := filepath.Base(strings.ReplaceAll(w.OwnerName, "/", `\`))
		out = append(out, WindowInfo{
			PID:     w.PID,
			Handle:  uint64(w.Handle),
			Title:   title,
			Process: owner,
			Width:   w.Rect.Dx(),
			Height:  w.Rect.Dy(),
			IsGame:  isGameWindow(title, owner),
			Hidden:  w.IsHidden,
		})
	}
	return out, nil
}

// FindGamePID 按进程名模糊查找冒险岛进程。
func (s *Service) FindGamePID() (uint32, error) {
	return process.GetProcessPIDByName("MapleStory", process.FuzzyMatch)
}

// Click 在屏幕坐标执行后台左键点击。
func (s *Service) Click(x, y int) error {
	return s.mouse.BackgroundClick(x, y, mouse.DefaultClickOptions())
}

// ClickWindow 向指定窗口客户区发送后台左键点击，不抢焦点。
func (s *Service) ClickWindow(handle uint64, x, y int) error {
	return s.mouse.PostMessageClick(uintptr(handle), x, y, mouse.DefaultClickOptions())
}

// Close 停止预览并释放截图会话。
func (s *Service) Close() {
	_ = s.StopCapture()
}

func isGameWindow(title, processName string) bool {
	haystack := strings.ToLower(title + " " + processName)
	for _, k := range gameKeywords {
		if strings.Contains(haystack, strings.ToLower(k)) {
			return true
		}
	}
	base := strings.ToLower(processName)
	return base == "maple.exe" || base == "maple"
}
