//go:build !windows

package pidfile

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

// binaryName is the exact process name isOurProcess matches against, so a
// PID reused by an unrelated process is never mistaken for ours.
const binaryName = "api"

// isProcessRunning checks if a process with the given PID exists (Unix)
func isProcessRunning(pid int) bool {
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	// On Unix, FindProcess always succeeds - send signal 0 to actually check
	err = process.Signal(syscall.Signal(0))
	if err == nil {
		return true
	}
	// EPERM means the process exists but belongs to another user - it IS running
	return errors.Is(err, syscall.EPERM)
}

// isOurProcess verifies the process is actually our binary (Unix)
func isOurProcess(pid int) bool {
	// Read /proc/{pid}/exe symlink (Linux)
	exePath, err := os.Readlink(fmt.Sprintf("/proc/%d/exe", pid))
	if err != nil {
		// On macOS/BSD, /proc doesn't exist - use ps instead
		return isOurProcessDarwin(pid)
	}
	// Exact match - substring matching would also match api-cli
	return filepath.Base(exePath) == binaryName
}

// isOurProcessDarwin checks the process command name via ps (macOS/BSD)
func isOurProcessDarwin(pid int) bool {
	cmd := exec.Command("ps", "-p", strconv.Itoa(pid), "-o", "comm=")
	output, err := cmd.Output()
	if err != nil {
		return false
	}
	// Exact match - substring matching would also match api-cli
	return strings.TrimSpace(string(output)) == binaryName
}
