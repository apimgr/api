//go:build !windows

package handler

import "syscall"

// checkDisk checks disk space (Unix implementation)
func checkDisk() string {
	var stat syscall.Statfs_t
	if err := syscall.Statfs("/", &stat); err != nil {
		return "error"
	}

	// Check if less than 10% free
	totalBytes := stat.Blocks * uint64(stat.Bsize)
	freeBytes := stat.Bfree * uint64(stat.Bsize)
	percentFree := float64(freeBytes) / float64(totalBytes) * 100

	if percentFree < 10 {
		return "warning"
	}
	return "ok"
}
