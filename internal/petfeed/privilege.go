package petfeed

import (
	"errors"
	"strings"
	"syscall"
)

const accessDeniedErrno = 5 // Windows ERROR_ACCESS_DENIED

func isAccessDenied(err error) bool {
	if err == nil {
		return false
	}
	var errno syscall.Errno
	if errors.As(err, &errno) && uint32(errno) == accessDeniedErrno {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "access is denied") || strings.Contains(msg, "permission denied")
}

func pressDeniedMessage(selfElevated bool, err error) string {
	if err != nil && !isAccessDenied(err) {
		return "发送按键失败: " + err.Error()
	}
	if !selfElevated {
		return "发送按键被系统拒绝（Access is denied）。冒险岛以管理员运行时，本程序也必须「以管理员身份运行」。这是普通管理员权限，不是系统超级管理员。"
	}
	if err != nil {
		return "发送按键被拒绝: " + err.Error()
	}
	return "发送按键被拒绝"
}
