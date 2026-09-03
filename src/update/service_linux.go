//go:build linux

package update

import "os/exec"

// RestartService restarts the api service after an update, on Linux. It
// prefers systemd and falls back to the generic SysV/OpenRC/runit "service"
// command when systemctl is unavailable.
func RestartService() error {
	if _, err := exec.LookPath("systemctl"); err == nil {
		return exec.Command("systemctl", "restart", "api").Run()
	}
	return exec.Command("service", "api", "restart").Run()
}
