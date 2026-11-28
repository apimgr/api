//go:build linux

package update

import "github.com/apimgr/api/src/sysservice"

// RestartService restarts the api service after an update, on Linux. It
// prefers systemd and falls back to the generic SysV/OpenRC/runit "service"
// command when systemctl is unavailable.
func RestartService() error {
	return sysservice.Restart()
}
