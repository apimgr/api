//go:build !windows

package theme

import (
	"os/exec"
	"runtime"
	"strings"
)

// detectPlatformDarkTheme - Linux (GNOME gsettings) / macOS (AppleInterfaceStyle)
// system dark-mode detection.
func detectPlatformDarkTheme() bool {
	switch runtime.GOOS {
	case "darwin":
		// AppleInterfaceStyle only exists (returns "Dark") when dark mode is
		// active; the key is absent (command errors) in light mode. If the
		// "defaults" tool itself is missing, that is a real detection
		// failure, so fall back to dark per the project-wide default.
		if _, lookErr := exec.LookPath("defaults"); lookErr != nil {
			return true
		}
		out, err := exec.Command("defaults", "read", "-g", "AppleInterfaceStyle").Output()
		if err != nil {
			return false
		}
		return strings.Contains(strings.ToLower(string(out)), "dark")
	default:
		out, err := exec.Command("gsettings", "get", "org.gnome.desktop.interface", "color-scheme").Output()
		if err != nil {
			// gsettings missing/unavailable is a real detection failure.
			return true
		}
		lower := strings.ToLower(string(out))
		if strings.Contains(lower, "light") {
			return false
		}
		return true
	}
}
