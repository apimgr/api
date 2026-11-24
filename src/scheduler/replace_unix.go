//go:build !windows

package scheduler

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

func replaceSecurityFileAtomically(source, target string) error {
	return os.Rename(source, target)
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
