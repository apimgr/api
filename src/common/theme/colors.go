// Package theme defines the single canonical color palette shared by every
// surface of the project - WebUI CSS, Swagger, GraphiQL, TUI, CLI, and GUI.
// Colors are defined ONCE here; every other package must derive its colors
// from ThemePaletteDark/ThemePaletteLight rather than hardcoding hex values.
package theme

// ThemePalette holds one complete set of themed colors as hex strings
// (e.g. "#1a1b26"), suitable for direct use as CSS custom property values,
// lipgloss.Color literals, or ANSI-mapped terminal colors.
type ThemePalette struct {
	Background string `json:"background"`
	Foreground string `json:"foreground"`
	Primary    string `json:"primary"`
	Secondary  string `json:"secondary"`
	Accent     string `json:"accent"`
	Success    string `json:"success"`
	Warning    string `json:"warning"`
	Error      string `json:"error"`
	Info       string `json:"info"`
	Surface    string `json:"surface"`
	SurfaceAlt string `json:"surface_alt"`
	Border     string `json:"border"`
	Muted      string `json:"muted"`
	// OnColor is the foreground used for text and icons placed on top of a
	// saturated palette fill (Primary/Secondary/Accent/Success/Warning/
	// Error/Info) - buttons, badges, toggle knobs. It is theme-scoped, not a
	// fixed white: the dark palette's fills are light pastels, so white on
	// them measures 1.72:1-2.65:1 and fails WCAG 2.1 AA, while the dark
	// Background reaches 6.46:1-9.96:1 on those same fills. The light
	// palette's fills are dark enough that white passes there. Text on the
	// theme-invariant dark chip surface uses OnDark instead, never OnColor.
	OnColor string `json:"on_color"`
	// OnDark is the foreground for the theme-invariant dark chip surface
	// (toasts, code blocks, update banner) that stays dark in both themes,
	// so this stays white in both themes.
	OnDark string `json:"on_dark"`
}

// ThemePaletteDark is the default palette (WCAG AA compliant against
// Background/Surface) used when no theme preference is set.
var ThemePaletteDark = ThemePalette{
	Background: "#1a1b26", Foreground: "#c0caf5",
	Primary: "#7aa2f7", Secondary: "#9ece6a", Accent: "#bb9af7",
	Success: "#9ece6a", Warning: "#e0af68", Error: "#f7768e", Info: "#7dcfff",
	Surface: "#24283b", SurfaceAlt: "#1f2335", Border: "#656f9f", Muted: "#868eb3",
	OnColor: "#1a1b26", OnDark: "#ffffff",
}

// ThemePaletteLight is the light-mode palette (WCAG AA compliant against
// Background/Surface).
var ThemePaletteLight = ThemePalette{
	Background: "#ffffff", Foreground: "#1a1b26",
	Primary: "#186ce0", Secondary: "#587539", Accent: "#7847bd",
	Success: "#587539", Warning: "#8c6c3e", Error: "#c64343", Info: "#007197",
	Surface: "#f5f5f5", SurfaceAlt: "#e9e9ec", Border: "#7d8bca", Muted: "#5466a8",
	OnColor: "#ffffff", OnDark: "#ffffff",
}

// GetThemePalette resolves a theme mode string ("dark", "light", or "auto")
// to a concrete ThemePalette. "auto" resolves via IsSystemDarkTheme(), which
// callers on CLI/TUI/GUI surfaces use to follow the OS preference; web CSS
// handles "auto" itself via a prefers-color-scheme media query instead of
// calling this function. Any unrecognized mode falls back to the dark
// palette, matching the project-wide "default to dark" rule.
func GetThemePalette(themeMode string) ThemePalette {
	switch themeMode {
	case "light":
		return ThemePaletteLight
	case "auto":
		if IsSystemDarkTheme() {
			return ThemePaletteDark
		}
		return ThemePaletteLight
	case "dark":
		return ThemePaletteDark
	default:
		return ThemePaletteDark
	}
}
