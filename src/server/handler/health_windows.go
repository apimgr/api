//go:build windows

package handler

// checkDisk checks disk space (Windows implementation)
func checkDisk() string {
	// On Windows, always return ok for now
	// TODO: Implement GetDiskFreeSpaceEx for Windows
	return "ok"
}
