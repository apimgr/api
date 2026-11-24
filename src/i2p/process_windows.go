//go:build windows

package i2p

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

func setParentDeathSignal(*exec.Cmd) {}
func terminateProcess(cmd *exec.Cmd) { _ = cmd.Process.Kill() }
func terminatePID(pid int) {
	if process, err := os.FindProcess(pid); err == nil {
		_ = process.Kill()
	}
}
func processMatches(pid int, binary, confPath string) bool {
	escape := func(value string) string { return strings.ReplaceAll(value, "'", "''") }
	query := "$p=Get-CimInstance Win32_Process -Filter 'ProcessId=" + strconv.Itoa(pid) + "'; if ($p.Name -eq '" + escape(filepath.Base(binary)) + "' -and $p.CommandLine.Contains('--conf " + escape(confPath) + "')) { exit 0 } else { exit 1 }"
	err := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command", query).Run()
	return err == nil
}
