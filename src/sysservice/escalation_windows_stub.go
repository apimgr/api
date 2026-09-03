//go:build !windows

package sysservice

// isWindowsAdmin always reports false outside GOOS=windows, since
// Administrator-token elevation is a Windows-only concept.
func isWindowsAdmin() bool {
	return false
}
