//go:build !windows

package server

import (
	"errors"
	"syscall"
)

func diskUsagePercent(path string) (int, error) {
	var stats syscall.Statfs_t
	if err := syscall.Statfs(path, &stats); err != nil {
		return 0, err
	}
	total := uint64(stats.Blocks) * uint64(stats.Bsize)
	free := uint64(stats.Bavail) * uint64(stats.Bsize)
	if total == 0 {
		return 0, nil
	}
	return int((total - free) * 100 / total), nil
}

func isDiskFull(err error) bool {
	return errors.Is(err, syscall.ENOSPC) || errors.Is(err, syscall.EDQUOT)
}
