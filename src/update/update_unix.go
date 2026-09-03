//go:build !windows

// Package update - Unix binary replacement. Unix allows renaming over a
// running executable: the old binary stays mapped in memory until the
// process exits, and the new one takes over on next start.
package update

import (
	"fmt"
	"os"
	"syscall"
)

// replaceBinary replaces the running binary at currentPath with the
// downloaded, checksum-verified binary at newBinaryPath.
func replaceBinary(currentPath, newBinaryPath string) error {
	info, err := os.Stat(currentPath)
	if err != nil {
		return fmt.Errorf("failed to stat current binary: %w", err)
	}

	// Atomic rename: new binary replaces current.
	if err := os.Rename(newBinaryPath, currentPath); err != nil {
		return fmt.Errorf("failed to replace binary: %w", err)
	}

	if err := os.Chmod(currentPath, info.Mode()); err != nil {
		return fmt.Errorf("failed to restore permissions: %w", err)
	}

	return nil
}

// Restart re-executes the current process in place (Unix), replacing the
// running process image so the newly installed binary takes over
// immediately.
func Restart() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}

	return syscall.Exec(exe, os.Args, os.Environ())
}
