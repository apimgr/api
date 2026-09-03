package server

import (
	"net/http"
)

// Theme represents the current theme selection
type Theme string

const (
	// ThemeDark is the dark theme (default)
	ThemeDark Theme = "dark"
	// ThemeLight is the light theme
	ThemeLight Theme = "light"
	// ThemeAuto uses system preference
	ThemeAuto Theme = "auto"
)

// DefaultTheme is dark as per specification
const DefaultTheme = ThemeDark

// GetTheme retrieves the theme from cookie or returns default
// Cookie name: theme
// Valid values: dark, light, auto
// Default: dark
func GetTheme(r *http.Request) Theme {
	cookie, err := r.Cookie("theme")
	if err != nil {
		return DefaultTheme
	}

	switch cookie.Value {
	case "dark":
		return ThemeDark
	case "light":
		return ThemeLight
	case "auto":
		return ThemeAuto
	default:
		return DefaultTheme
	}
}

// SetThemeCookie sets the theme cookie
// MaxAge: 365 days
// Path: /
// SameSite: Lax
func SetThemeCookie(w http.ResponseWriter, theme Theme) {
	http.SetCookie(w, &http.Cookie{
		Name:     "theme",
		Value:    string(theme),
		Path:     "/",
		MaxAge:   365 * 24 * 60 * 60, // 1 year
		HttpOnly: false,              // JavaScript needs to read this
		Secure:   false,              // Set to true when SSL is enabled
		SameSite: http.SameSiteLaxMode,
	})
}

// ThemeClass returns the CSS class for the current theme
// Used in HTML: <html class="{{ .ThemeClass }}">
func ThemeClass(theme Theme) string {
	switch theme {
	case ThemeLight:
		return "theme-light"
	case ThemeAuto:
		return "theme-auto"
	case ThemeDark:
		fallthrough
	default:
		return "theme-dark"
	}
}

// NextTheme returns the next mode in the cycle: dark -> light -> auto -> dark.
// The theme toggle's POST target is rendered from this on every request so it
// always targets the mode after the one actually in effect. A hardcoded target
// would change the theme on the first click and then resubmit the same value
// forever, which is the bug PART 16 "Theme Cycle Logic" calls out.
func NextTheme(current string) string {
	switch current {
	case "dark":
		return "light"
	case "light":
		return "auto"
	default:
		return "dark"
	}
}

// ThemeData returns template data for theme system
// Include this in all template data maps
func ThemeData(r *http.Request) map[string]interface{} {
	theme := GetTheme(r)
	return map[string]interface{}{
		"Theme":      string(theme),
		"ThemeClass": ThemeClass(theme),
		"IsDark":     theme == ThemeDark || theme == ThemeAuto,
		"IsLight":    theme == ThemeLight,
		"IsAuto":     theme == ThemeAuto,
	}
}
