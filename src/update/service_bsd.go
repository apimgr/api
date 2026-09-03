//go:build freebsd || openbsd || netbsd

package update

import "os/exec"

// RestartService restarts the api service after an update, on BSD, via the
// rc.d "service" command.
func RestartService() error {
	return exec.Command("service", "api", "restart").Run()
}
