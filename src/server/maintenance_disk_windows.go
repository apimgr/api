//go:build windows

package server

import (
	"errors"
	"path/filepath"

	"golang.org/x/sys/windows"
)

func diskUsagePercent(path string) (int, error) {
	widePath, err := windows.UTF16PtrFromString(filepath.Clean(path))
	if err != nil {
		return 0, err
	}
	var free, total, totalFree uint64
	if err := windows.GetDiskFreeSpaceEx(widePath, &free, &total, &totalFree); err != nil {
		return 0, err
	}
	if total == 0 {
		return 0, nil
	}
	return int((total - totalFree) * 100 / total), nil
}

func isDiskFull(err error) bool {
	return errors.Is(err, windows.ERROR_DISK_FULL) || errors.Is(err, windows.ERROR_HANDLE_DISK_FULL)
}
