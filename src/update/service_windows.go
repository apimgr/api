//go:build windows

package update

import (
	"github.com/apimgr/api/src/sysservice"
)

// RestartService restarts the api service after an update, on Windows, via
// the Service Control Manager.
func RestartService() error {
	// Ignore the stop error - the service may not currently be running.
	return sysservice.Restart()
}
