//go:build windows

package scheduler

import (
	"path/filepath"

	"golang.org/x/sys/windows"
)

// diskSpaceAt reports free space and volume identity on Windows.
func diskSpaceAt(path string) (volume string, percentFree float64, freeBytes, totalBytes uint64, ok bool) {
	rootPath, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return "", 0, 0, 0, false
	}

	var freeBytesAvailable uint64
	if err := windows.GetDiskFreeSpaceEx(rootPath, &freeBytesAvailable, &totalBytes, &freeBytes); err != nil || totalBytes == 0 {
		return "", 0, 0, 0, false
	}
	volumePath := make([]uint16, windows.MAX_PATH)
	if err := windows.GetVolumePathName(rootPath, &volumePath[0], uint32(len(volumePath))); err != nil {
		return "", 0, 0, 0, false
	}
	volume = filepath.Clean(windows.UTF16ToString(volumePath))
	percentFree = float64(freeBytes) / float64(totalBytes) * 100
	return volume, percentFree, freeBytes, totalBytes, true
}
