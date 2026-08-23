//go:build !windows

package petfeed

import "fmt"

func pressKey(handle uint64, vk virtualKey) error {
	return fmt.Errorf("宠物喂食按键仅支持 Windows")
}
