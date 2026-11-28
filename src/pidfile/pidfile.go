// Package pidfile implements PID file creation, staleness detection, and
// removal per AI.md PART 8 "PID File Handling". Containers and Windows are
// skipped entirely: containers use runtime supervision, while Windows uses
// the Service Control Manager.
package pidfile

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"github.com/apimgr/api/src/paths"
)

// CheckPIDFile checks if a PID file exists and if the process it names is
// still running. A stale file (corrupt content, dead process, or PID reused
// by a different binary) is removed and reported as not running.
func CheckPIDFile(pidPath string) (bool, int, error) {
	if shouldSkipPIDFile() {
		return false, 0, nil
	}
	data, err := os.ReadFile(pidPath)
	if os.IsNotExist(err) {
		return false, 0, nil
	}
	if err != nil {
		return false, 0, fmt.Errorf("reading pid file: %w", err)
	}

	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		if removeErr := os.Remove(pidPath); removeErr != nil && !os.IsNotExist(removeErr) {
			return false, 0, fmt.Errorf("removing invalid pid file: %w", removeErr)
		}
		return false, 0, nil
	}

	if !isProcessRunning(pid) {
		if removeErr := os.Remove(pidPath); removeErr != nil && !os.IsNotExist(removeErr) {
			return false, 0, fmt.Errorf("removing stale pid file: %w", removeErr)
		}
		return false, 0, nil
	}

	if !isOurProcess(pid) {
		if removeErr := os.Remove(pidPath); removeErr != nil && !os.IsNotExist(removeErr) {
			return false, 0, fmt.Errorf("removing foreign pid file: %w", removeErr)
		}
		return false, 0, nil
	}

	return true, pid, nil
}

// WritePIDFile writes the current process PID to pidPath. It is skipped in
// containers and on Windows. Returns an error if another instance of this
// binary is already running per the PID file.
func WritePIDFile(pidPath string) error {
	if shouldSkipPIDFile() {
		return nil
	}

	running, existingPID, err := CheckPIDFile(pidPath)
	if err != nil {
		return err
	}
	if running {
		return fmt.Errorf("already running (pid %d)", existingPID)
	}

	if err := paths.EnsureDir(filepath.Dir(pidPath)); err != nil {
		return fmt.Errorf("creating pid file directory: %w", err)
	}

	pid := os.Getpid()
	mode := os.FileMode(0600)
	if paths.IsElevated() {
		mode = 0644
	}
	if err := os.WriteFile(pidPath, []byte(strconv.Itoa(pid)), mode); err != nil {
		return err
	}
	if err := os.Chmod(pidPath, mode); err != nil {
		_ = os.Remove(pidPath)
		return fmt.Errorf("setting pid file permissions: %w", err)
	}
	return nil
}

// RemovePIDFile removes the PID file on shutdown. It is a no-op in
// containers and on Windows, where WritePIDFile never created one.
func RemovePIDFile(pidPath string) error {
	if shouldSkipPIDFile() {
		return nil
	}
	return os.Remove(pidPath)
}

func shouldSkipPIDFile() bool {
	return runtime.GOOS == "windows" || paths.IsRunningInContainer()
}
