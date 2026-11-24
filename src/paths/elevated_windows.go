//go:build windows

package paths

import "golang.org/x/sys/windows"

func isElevatedNow() bool {
	return windows.GetCurrentProcessToken().IsElevated()
}
