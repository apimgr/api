//go:build windows

package scheduler

import (
	"golang.org/x/sys/windows"

	"github.com/apimgr/api/src/paths"
)

// checkDiskSpace checks disk space (Windows implementation)
func checkDiskSpace() (percentFree float64, ok bool) {
	rootPath, err := windows.UTF16PtrFromString(paths.DataDir())
	if err != nil {
		return 0, false
	}

	var freeBytesAvailable, totalBytes, totalFreeBytes uint64
	if err := windows.GetDiskFreeSpaceEx(rootPath, &freeBytesAvailable, &totalBytes, &totalFreeBytes); err != nil {
		return 0, false
	}

	percentFree = float64(totalFreeBytes) / float64(totalBytes) * 100

	return percentFree, true
}
