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
		HttpOnly: false,               // JavaScript needs to read this
		Secure:   false,               // Set to true when SSL is enabled
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

// HandleThemeSwitch handles theme toggle requests
// POST /api/v1/theme
// Body: {"theme": "dark|light|auto"}
func HandleThemeSwitch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	theme := r.FormValue("theme")
	switch theme {
	case "dark":
		SetThemeCookie(w, ThemeDark)
	case "light":
		SetThemeCookie(w, ThemeLight)
	case "auto":
		SetThemeCookie(w, ThemeAuto)
	default:
		http.Error(w, "Invalid theme", http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{"success":true,"theme":"` + theme + `"}`))
}
