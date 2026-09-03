//go:build windows

package update

import (
	"os/exec"
	"time"
)

// RestartService restarts the api service after an update, on Windows, via
// the Service Control Manager.
func RestartService() error {
	// Ignore the stop error - the service may not currently be running.
	exec.Command("sc", "stop", "api").Run()

	time.Sleep(2 * time.Second)

	return exec.Command("sc", "start", "api").Run()
}
