//go:build !windows

package scheduler

import (
	"fmt"
	"syscall"
)

// diskSpaceAt reports free space and filesystem identity on Unix.
func diskSpaceAt(path string) (volume string, percentFree float64, freeBytes, totalBytes uint64, ok bool) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return "", 0, 0, 0, false
	}
	availableBlocks, totalBlocks, blockSize := int64(stat.Bavail), int64(stat.Blocks), int64(stat.Bsize)
	if availableBlocks < 0 || totalBlocks <= 0 || blockSize <= 0 {
		return "", 0, 0, 0, false
	}
	available, blocks, size := uint64(availableBlocks), uint64(totalBlocks), uint64(blockSize)
	maxUint64 := ^uint64(0)
	if available > maxUint64/size || blocks > maxUint64/size {
		return "", 0, 0, 0, false
	}
	freeBytes = available * size
	totalBytes = blocks * size
	percentFree = float64(freeBytes) / float64(totalBytes) * 100
	volume = fmt.Sprintf("%v", stat.Fsid)
	return volume, percentFree, freeBytes, totalBytes, true
}
