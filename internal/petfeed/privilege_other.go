//go:build !windows

package petfeed

import "fmt"

// IsElevated 在非 Windows 上始终为 false。
func IsElevated() bool { return false }

// RelaunchElevated 仅支持 Windows。
func RelaunchElevated() error {
	return fmt.Errorf("仅支持 Windows")
}
