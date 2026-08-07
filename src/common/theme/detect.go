package theme

import "os"

// IsSystemDarkTheme reports whether the host OS is currently set to a dark
// color scheme. It backs "auto" theme resolution on CLI/TUI/GUI surfaces
// (web "auto" is pure CSS via prefers-color-scheme and never calls this).
// Detection is platform-specific (see detect_unix.go / detect_windows.go);
// any detection failure falls back to dark, matching the project-wide
// "default to dark" rule.
func IsSystemDarkTheme() bool {
	if colorFGBG := os.Getenv("COLORFGBG"); colorFGBG != "" {
		if isDarkFromColorFGBG(colorFGBG) {
			return true
		}
	}
	return detectPlatformDarkTheme()
}

// isDarkFromColorFGBG interprets the terminal COLORFGBG env var
// ("foreground;background", e.g. "15;0") — a low background index (0-6)
// indicates a dark terminal background.
func isDarkFromColorFGBG(colorFGBG string) bool {
	for i := len(colorFGBG) - 1; i >= 0; i-- {
		if colorFGBG[i] == ';' {
			bg := colorFGBG[i+1:]
			return bg == "0" || bg == "1" || bg == "2" || bg == "3" || bg == "4" || bg == "5" || bg == "6"
		}
	}
	return false
}
