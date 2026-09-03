//go:build windows

// Package update - Windows binary replacement. Windows cannot delete or
// rename a running executable in place, so the current binary is renamed
// aside to ".old", the new binary takes its place, and the ".old" file is
// scheduled for deletion on next reboot.
package update

import (
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
	os.Remove(oldPath)

	// Rename the running binary to .old - this works on Windows even while
	// the process is executing from it.
	if err := os.Rename(currentPath, oldPath); err != nil {
		return fmt.Errorf("failed to rename current binary: %w", err)
	}

	// Move the new binary into place.
	if err := os.Rename(newBinaryPath, currentPath); err != nil {
		// Best-effort restore of the original binary.
		os.Rename(oldPath, currentPath)
		return fmt.Errorf("failed to move new binary: %w", err)
	}

	// Schedule the old binary for deletion on reboot.
	oldPathPtr, err := windows.UTF16PtrFromString(oldPath)
	if err == nil {
		windows.MoveFileEx(oldPathPtr, nil, windows.MOVEFILE_DELAY_UNTIL_REBOOT)
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
