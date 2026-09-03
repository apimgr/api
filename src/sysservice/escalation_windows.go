//go:build windows

package sysservice

import "golang.org/x/sys/windows"

// isWindowsAdmin reports whether the current process token carries
// Administrator privileges.
func isWindowsAdmin() bool {
	token := windows.GetCurrentProcessToken()
	return token.IsElevated()
}
