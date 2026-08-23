//go:build windows

package potion

import (
	"fmt"

	toast "git.sr.ht/~jackmordaunt/go-toast/v2"
	"golang.org/x/sys/windows"
)

var (
	user32          = windows.NewLazySystemDLL("user32.dll")
	procMessageBeep = user32.NewProc("MessageBeep")
)

func notifyAlert(a Alert) {
	title, body := alertText(a)
	n := toast.Notification{
		AppID:    "mxd",
		Title:    title,
		Body:     body,
		Audio:    toast.Reminder,
		Duration: toast.Short,
	}
	if err := n.Push(); err != nil {
		_, _, _ = procMessageBeep.Call(0x00000030) // MB_ICONWARNING
	}
}

func alertText(a Alert) (string, string) {
	name := "血药"
	if a.Kind == "mp" {
		name = "蓝药"
	}
	if a.Level == string(SlotLow) {
		n := a.Count
		if n < 0 {
			n = 0
		}
		return name + "不足", fmt.Sprintf("剩余大约 %d，请及时补给。", n)
	}
	switch a.Reason {
	case "bar":
		return name + "可能已空", "血/蓝条持续偏低且没有回升，请检查药水。"
	case "count":
		return name + "已用完", "快捷栏数量为 0。"
	default:
		return name + "已用完", "快捷栏里已经看不到药水图标。"
	}
}
