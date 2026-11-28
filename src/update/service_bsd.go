//go:build freebsd || openbsd || netbsd

package update

import "github.com/apimgr/api/src/sysservice"

// RestartService restarts the api service after an update, on BSD, via the
// rc.d "service" command.
func RestartService() error {
	return sysservice.Restart()
}
