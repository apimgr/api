// Package tui implements the api-cli interactive terminal application,
// launched automatically when the binary is invoked with a bare command
// or no arguments in a real terminal, per AI.md PART 32.
package tui

import (
	"github.com/charmbracelet/lipgloss"

	sharedtheme "github.com/apimgr/api/src/common/theme"
)

// TUITheme defines lipgloss colors for TUI rendering. Colors are derived
// from the single canonical ThemePalette in src/common/theme/colors.go
// (see AI.md PART 16) rather than hardcoded here.
type TUITheme struct {
	Name       string
	Background lipgloss.Color
	Foreground lipgloss.Color
	Primary    lipgloss.Color
	Secondary  lipgloss.Color
	Accent     lipgloss.Color
	Error      lipgloss.Color
	Success    lipgloss.Color
	Warning    lipgloss.Color
	Muted      lipgloss.Color
}

// tuiThemeFromPalette converts a shared theme.ThemePalette into lipgloss
// colors, keeping the TUI in lockstep with Web/Swagger/GraphQL.
func tuiThemeFromPalette(name string, p sharedtheme.ThemePalette) TUITheme {
	return TUITheme{
		Name:       name,
		Background: lipgloss.Color(p.Background),
		Foreground: lipgloss.Color(p.Foreground),
		Primary:    lipgloss.Color(p.Primary),
		Secondary:  lipgloss.Color(p.Secondary),
		Accent:     lipgloss.Color(p.Accent),
		Error:      lipgloss.Color(p.Error),
		Success:    lipgloss.Color(p.Success),
		Warning:    lipgloss.Color(p.Warning),
		Muted:      lipgloss.Color(p.Muted),
	}
}

// TUIThemeDark is the dark theme (default), matching ThemePaletteDark.
var TUIThemeDark = tuiThemeFromPalette("dark", sharedtheme.ThemePaletteDark)

// TUIThemeLight is the light theme (optional), matching ThemePaletteLight.
var TUIThemeLight = tuiThemeFromPalette("light", sharedtheme.ThemePaletteLight)

// themeByName resolves a cli.yml tui.theme value to a TUITheme, defaulting
// to dark for anything unrecognized.
func themeByName(name string) TUITheme {
	if name == "light" {
		return TUIThemeLight
	}
	return TUIThemeDark
}
