//go:build windows

package handler

import "golang.org/x/sys/windows"

// checkDisk checks disk space (Windows implementation)
func checkDisk() string {
	rootPath, err := windows.UTF16PtrFromString(`C:\`)
	if err != nil {
		return "error"
	}

	var freeBytesAvailable, totalBytes, totalFreeBytes uint64
	if err := windows.GetDiskFreeSpaceEx(rootPath, &freeBytesAvailable, &totalBytes, &totalFreeBytes); err != nil {
		return "error"
	}

	// Check if less than 10% free
	percentFree := float64(totalFreeBytes) / float64(totalBytes) * 100

	if percentFree < 10 {
		return "warning"
	}
	return "ok"
}
