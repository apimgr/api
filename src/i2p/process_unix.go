//go:build !windows

package i2p

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

func terminateProcess(cmd *exec.Cmd) { _ = cmd.Process.Signal(syscall.SIGTERM) }

func processMatches(pid int, binary, confPath string) bool {
	output, err := exec.Command("ps", "-o", "args=", "-p", strconv.Itoa(pid)).Output()
	if err != nil {
		return false
	}
	args := strings.TrimSpace(string(output))
	return strings.Contains(args, filepath.Base(binary)) && strings.Contains(args, "--conf "+confPath)
}

func terminatePID(pid int) {
	if process, err := os.FindProcess(pid); err == nil {
		_ = process.Signal(syscall.SIGTERM)
	}
}
