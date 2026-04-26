//go:build windows

package scheduler

// checkDiskSpace checks disk space (Windows implementation)
func checkDiskSpace() (percentFree float64, ok bool) {
	// On Windows, always return ok for now
	// TODO: Implement GetDiskFreeSpaceEx for Windows
	return 100, true
}
