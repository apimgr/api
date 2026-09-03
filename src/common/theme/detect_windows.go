//go:build windows

package theme

import "golang.org/x/sys/windows/registry"

// detectPlatformDarkTheme - Windows system dark-mode detection via the
// AppsUseLightTheme registry value (0 = dark, 1 = light). Any read failure
// falls back to dark per the project-wide "default to dark" rule.
func detectPlatformDarkTheme() bool {
	key, err := registry.OpenKey(
		registry.CURRENT_USER,
		`Software\Microsoft\Windows\CurrentVersion\Themes\Personalize`,
		registry.QUERY_VALUE,
	)
	if err != nil {
		return true
	}
	defer key.Close()

	value, _, err := key.GetIntegerValue("AppsUseLightTheme")
	if err != nil {
		return true
	}
	return value == 0
}
