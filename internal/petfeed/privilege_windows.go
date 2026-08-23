//go:build windows

package petfeed

import (
	"errors"
	"fmt"
	"os"

	"golang.org/x/sys/windows"
)

// IsElevated 报告当前进程是否已通过 UAC 提权。
func IsElevated() bool {
	var token windows.Token
	if err := windows.OpenProcessToken(windows.CurrentProcess(), windows.TOKEN_QUERY, &token); err != nil {
		return false
	}
	defer token.Close()
	return token.IsElevated()
}

// RelaunchElevated 弹出 UAC，用管理员身份再启动一份本程序。
func RelaunchElevated() error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("找不到当前程序: %w", err)
	}
	verb, err := windows.UTF16PtrFromString("runas")
	if err != nil {
		return err
	}
	file, err := windows.UTF16PtrFromString(exe)
	if err != nil {
		return err
	}
	cwd, err := os.Getwd()
	if err != nil {
		cwd = ""
	}
	var dir *uint16
	if cwd != "" {
		dir, err = windows.UTF16PtrFromString(cwd)
		if err != nil {
			return err
		}
	}
	if err := windows.ShellExecute(0, verb, file, nil, dir, windows.SW_SHOWNORMAL); err != nil {
		if errors.Is(err, windows.ERROR_CANCELLED) {
			return fmt.Errorf("已取消管理员授权")
		}
		return fmt.Errorf("无法以管理员身份启动: %w", err)
	}
	return nil
}
