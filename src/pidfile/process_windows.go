//go:build windows

package pidfile

import (
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows"
)

// binaryName is the exact process name isOurProcess matches against, so a
// PID reused by an unrelated process is never mistaken for ours.
const binaryName = "api"

// isProcessRunning checks if a process with the given PID exists (Windows)
func isProcessRunning(pid int) bool {
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	var exitCode uint32
	handle := windows.Handle(uintptr(process.Pid))
	err = windows.GetExitCodeProcess(handle, &exitCode)
	return err == nil && exitCode == windows.STILL_ACTIVE
}

// isOurProcess verifies the process is actually our binary (Windows)
func isOurProcess(pid int) bool {
	handle, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return false
	}
	defer windows.CloseHandle(handle)

	var buf [windows.MAX_PATH]uint16
	size := uint32(windows.MAX_PATH)
	err = windows.QueryFullProcessImageName(handle, 0, &buf[0], &size)
	if err != nil {
		return false
	}
	exePath := windows.UTF16ToString(buf[:size])
	// Exact match (case-insensitive) - substring matching would also match api-cli.exe
	base := filepath.Base(exePath)
	return strings.EqualFold(base, binaryName+".exe") || strings.EqualFold(base, binaryName)
}
