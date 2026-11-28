//go:build darwin

package update

import "github.com/apimgr/api/src/sysservice"

// RestartService restarts the api service after an update, on macOS.
// "kickstart -k" kills the existing instance and starts a fresh one under
// launchd.
func RestartService() error {
	return sysservice.Restart()
}
