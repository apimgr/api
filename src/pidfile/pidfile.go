// Package pidfile implements PID file creation, staleness detection, and
// removal per AI.md PART 8 "PID File Handling". Containers are skipped
// entirely — the container runtime supervises the process, and a PID file
// on a mounted volume read from the host or another container namespace
// points at the wrong process or produces a false "already running".
package pidfile

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/apimgr/api/src/paths"
)

// CheckPIDFile checks if a PID file exists and if the process it names is
// still running. A stale file (corrupt content, dead process, or PID reused
// by a different binary) is removed and reported as not running.
func CheckPIDFile(pidPath string) (bool, int, error) {
	data, err := os.ReadFile(pidPath)
	if os.IsNotExist(err) {
		return false, 0, nil
	}
	if err != nil {
		return false, 0, fmt.Errorf("reading pid file: %w", err)
	}

	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		os.Remove(pidPath)
		return false, 0, nil
	}

	if !isProcessRunning(pid) {
		os.Remove(pidPath)
		return false, 0, nil
	}

	if !isOurProcess(pid) {
		os.Remove(pidPath)
		return false, 0, nil
	}

	return true, pid, nil
}

// WritePIDFile writes the current process PID to pidPath. Skipped entirely
// when running inside a container. Returns an error if another instance of
// this binary is already running per the PID file.
func WritePIDFile(pidPath string) error {
	if paths.IsRunningInContainer() {
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
	return os.WriteFile(pidPath, []byte(strconv.Itoa(pid)), 0644)
}

// RemovePIDFile removes the PID file on shutdown. A no-op in containers,
// since WritePIDFile never created one.
func RemovePIDFile(pidPath string) error {
	if paths.IsRunningInContainer() {
		return nil
	}
	return os.Remove(pidPath)
}
