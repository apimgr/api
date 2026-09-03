//go:build darwin

package update

import "os/exec"

// darwinServiceLabel is the LaunchDaemon/LaunchAgent label for this
// project, per AI.md PART 3 ({plist_name} = io.github.apimgr.api).
const darwinServiceLabel = "io.github.apimgr.api"

// RestartService restarts the api service after an update, on macOS.
// "kickstart -k" kills the existing instance and starts a fresh one under
// launchd.
func RestartService() error {
	return exec.Command("launchctl", "kickstart", "-k", "system/"+darwinServiceLabel).Run()
}
