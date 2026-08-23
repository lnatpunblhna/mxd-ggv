//go:build windows

package petfeed

import (
	"fmt"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	inputKeyboard = 1

	keyeventfExtended = 0x0001
	keyeventfKeyUp    = 0x0002
	keyeventfScancode = 0x0008

	mapvkVkToVsc = 0
	swRestore    = 9

	keyHold     = 120 * time.Millisecond
	focusSettle = 50 * time.Millisecond
)

var (
	user32   = windows.NewLazySystemDLL("user32.dll")
	kernel32 = windows.NewLazySystemDLL("kernel32.dll")

	procSendInput              = user32.NewProc("SendInput")
	procMapVirtualK            = user32.NewProc("MapVirtualKeyW")
	procShowWindow             = user32.NewProc("ShowWindow")
	procSetForegroundWindow    = user32.NewProc("SetForegroundWindow")
	procGetForegroundWindow    = user32.NewProc("GetForegroundWindow")
	procGetWindowThreadProcess = user32.NewProc("GetWindowThreadProcessId")
	procAttachThreadInput      = user32.NewProc("AttachThreadInput")
	procBringWindowToTop       = user32.NewProc("BringWindowToTop")
	procGetParent              = user32.NewProc("GetParent")
	procGetCurrentThreadId     = kernel32.NewProc("GetCurrentThreadId")
	procAllowSetForeground     = user32.NewProc("AllowSetForegroundWindow")
)

// KEYBDINPUT matches the Win32 KEYBDINPUT structure.
type KEYBDINPUT struct {
	WVk         uint16
	WScan       uint16
	DwFlags     uint32
	Time        uint32
	DwExtraInfo uintptr
}

// INPUT matches the Win32 INPUT structure for keyboard events.
type INPUT struct {
	Type uint32
	_    [4]byte
	Ki   KEYBDINPUT
	_    [8]byte
}

func pressKey(handle uint64, vk virtualKey) error {
	if handle == 0 {
		return fmt.Errorf("未选择窗口")
	}
	if err := focusWindow(uintptr(handle)); err != nil {
		return err
	}
	time.Sleep(focusSettle)
	if err := sendInputKey(vk, false); err != nil {
		return err
	}
	time.Sleep(keyHold)
	if err := sendInputKey(vk, true); err != nil {
		return err
	}
	return nil
}

func sendInputKey(vk virtualKey, up bool) error {
	scan, _, _ := procMapVirtualK.Call(uintptr(vk), mapvkVkToVsc)

	var flags uint32 = keyeventfScancode
	if isExtendedKey(vk) {
		flags |= keyeventfExtended
	}
	if up {
		flags |= keyeventfKeyUp
	}

	input := INPUT{
		Type: inputKeyboard,
		Ki: KEYBDINPUT{
			WVk:     uint16(vk),
			WScan:   uint16(scan),
			DwFlags: flags,
		},
	}

	ret, _, err := procSendInput.Call(1, uintptr(unsafe.Pointer(&input)), unsafe.Sizeof(input))
	if ret != 1 {
		return fmt.Errorf("%s", pressDeniedMessage(IsElevated(), err))
	}
	return nil
}

func focusWindow(hwnd uintptr) error {
	if hwnd == 0 {
		return fmt.Errorf("未选择窗口")
	}

	procShowWindow.Call(hwnd, swRestore)
	procAllowSetForeground.Call(0xFFFFFFFF) // ASFW_ANY

	fg, _, _ := procGetForegroundWindow.Call()
	if ownsWindow(hwnd, fg) {
		return nil
	}

	curTid, _, _ := procGetCurrentThreadId.Call()
	fgTid, _, _ := procGetWindowThreadProcess.Call(fg, 0)
	targetTid, _, _ := procGetWindowThreadProcess.Call(hwnd, 0)

	if fgTid != 0 && fgTid != curTid {
		procAttachThreadInput.Call(curTid, fgTid, 1)
	}
	if targetTid != 0 && targetTid != curTid && targetTid != fgTid {
		procAttachThreadInput.Call(curTid, targetTid, 1)
	}

	procBringWindowToTop.Call(hwnd)
	procSetForegroundWindow.Call(hwnd)

	if fgTid != 0 && fgTid != curTid {
		procAttachThreadInput.Call(curTid, fgTid, 0)
	}
	if targetTid != 0 && targetTid != curTid && targetTid != fgTid {
		procAttachThreadInput.Call(curTid, targetTid, 0)
	}

	fg, _, _ = procGetForegroundWindow.Call()
	if ownsWindow(hwnd, fg) {
		return nil
	}
	return fmt.Errorf("无法切换到游戏窗口，请先手动点一下冒险岛窗口后再试")
}

func ownsWindow(root, hwnd uintptr) bool {
	if root == 0 || hwnd == 0 {
		return false
	}
	cur := hwnd
	for i := 0; i < 16 && cur != 0; i++ {
		if cur == root {
			return true
		}
		next, _, _ := procGetParent.Call(cur)
		if next == 0 || next == cur {
			break
		}
		cur = next
	}
	return false
}
