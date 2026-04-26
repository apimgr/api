//go:build !windows

package scheduler

import (
	"syscall"

	"github.com/apimgr/api/src/paths"
)

// checkDiskSpace checks disk space (Unix implementation)
func checkDiskSpace() (percentFree float64, ok bool) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(paths.DataDir(), &stat); err != nil {
		return 0, false
	}

	freeBytes := stat.Bfree * uint64(stat.Bsize)
	totalBytes := stat.Blocks * uint64(stat.Bsize)
	percentFree = float64(freeBytes) / float64(totalBytes) * 100

	return percentFree, true
}
