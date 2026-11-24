//go:build !windows

package display

import (
	"os"
	"os/exec"
	"runtime"
	"strings"
)

func (e *DisplayEnv) detectPlatformDisplay() {
	if os.Getenv("WAYLAND_DISPLAY") != "" {
		e.HasDisplay, e.DisplayType = true, "wayland"
		return
	}
	if os.Getenv("DISPLAY") != "" {
		e.HasDisplay, e.DisplayType = true, "x11"
		return
	}
	if runtime.GOOS == "darwin" {
		if !e.IsSSH && os.Getenv("__CFBundleIdentifier") != "" {
			e.HasDisplay, e.DisplayType = true, "macos"
			return
		}
		output, err := exec.Command("launchctl", "managername").Output()
		if err == nil && strings.Contains(string(output), "Aqua") {
			e.HasDisplay, e.DisplayType = true, "macos"
			return
		}
	}
	e.HasDisplay, e.DisplayType = false, "none"
}
