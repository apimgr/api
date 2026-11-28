//go:build windows

// Package update - Windows binary replacement. Windows cannot delete or
// rename a running executable in place, so the current binary is renamed
// aside to ".old", the new binary takes its place, and the ".old" file is
// scheduled for deletion on next reboot.
package update

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"time"

	"golang.org/x/sys/windows"
)

// replaceBinary replaces the running binary at currentPath with the
// downloaded, checksum-verified binary at newBinaryPath.
func replaceBinary(currentPath, newBinaryPath string) error {
	oldPath := currentPath + ".old"

	// Remove any existing .old file left over from a previous update.
	if err := os.Remove(oldPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove previous update backup: %w", err)
	}

	// Rename the running binary to .old - this works on Windows even while
	// the process is executing from it.
	if err := os.Rename(currentPath, oldPath); err != nil {
		return fmt.Errorf("failed to rename current binary: %w", err)
	}

	// Move the new binary into place.
	if err := os.Rename(newBinaryPath, currentPath); err != nil {
		restoreErr := os.Rename(oldPath, currentPath)
		return errors.Join(fmt.Errorf("failed to move new binary: %w", err), restoreErr)
	}

	// Schedule the old binary for deletion on reboot.
	oldPathPtr, err := windows.UTF16PtrFromString(oldPath)
	if err != nil {
		return fmt.Errorf("encode old binary path for deletion: %w", err)
	}
	if err := windows.MoveFileEx(oldPathPtr, nil, windows.MOVEFILE_DELAY_UNTIL_REBOOT); err != nil {
		rollbackErr := restoreOldBinary(currentPath, oldPath)
		return errors.Join(fmt.Errorf("schedule old binary deletion: %w", err), rollbackErr)
	}

	return nil
}

func restoreOldBinary(currentPath, oldPath string) error {
	failedPath := currentPath + ".failed"
	if err := os.Remove(failedPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove failed replacement: %w", err)
	}
	if err := os.Rename(currentPath, failedPath); err != nil {
		return fmt.Errorf("move failed replacement aside: %w", err)
	}
	if err := os.Rename(oldPath, currentPath); err != nil {
		return errors.Join(fmt.Errorf("restore original binary: %w", err), os.Rename(failedPath, currentPath))
	}
	if err := os.Remove(failedPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove failed replacement: %w", err)
	}
	return nil
}

// Restart starts a new instance of the current process and exits this one.
// Windows does not support exec()-style process replacement, so the new
// binary is spawned as a child before this process exits.
func Restart() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}

	cmd := exec.Command(exe, os.Args[1:]...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start new process: %w", err)
	}

	// Give the new process time to start before this one exits.
	time.Sleep(100 * time.Millisecond)

	os.Exit(0)
	return nil
}
