//go:build windows

package scheduler

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/sys/windows"
)

func replaceSecurityFileAtomically(source, target string) error {
	from, err := windows.UTF16PtrFromString(source)
	if err != nil {
		return err
	}
	to, err := windows.UTF16PtrFromString(target)
	if err != nil {
		return err
	}
	return windows.MoveFileEx(from, to, windows.MOVEFILE_REPLACE_EXISTING|windows.MOVEFILE_WRITE_THROUGH)
}

func replaceTrivyDirectory(source, target string) error {
	backup := filepath.Join(filepath.Dir(target), fmt.Sprintf(".trivy-old-%d", time.Now().UnixNano()))
	if err := os.Rename(target, backup); err != nil && !os.IsNotExist(err) {
		return err
	} else if err == nil {
		if err := os.Rename(source, target); err != nil {
			if restoreErr := os.Rename(backup, target); restoreErr != nil {
				return fmt.Errorf("install staged Trivy database: %v; restore previous database: %w", err, restoreErr)
			}
			return err
		}
		return os.RemoveAll(backup)
	}
	return os.Rename(source, target)
}
