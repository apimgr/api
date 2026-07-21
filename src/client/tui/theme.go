// Package tui implements the api-cli interactive terminal application,
// launched automatically when the binary is invoked with a bare command
// or no arguments in a real terminal, per AI.md PART 32.
package tui

import "github.com/charmbracelet/lipgloss"

// TUITheme defines lipgloss colors for TUI rendering. Colors match
// ThemePalette from the server frontend (see AI.md PART 16).
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

// TUIThemeDark is the dark theme (default), matching ThemePaletteDark.
var TUIThemeDark = TUITheme{
	Name:       "dark",
	Background: lipgloss.Color("#282a36"),
	Foreground: lipgloss.Color("#f8f8f2"),
	Primary:    lipgloss.Color("#bd93f9"),
	Secondary:  lipgloss.Color("#6272a4"),
	Accent:     lipgloss.Color("#8be9fd"),
	Error:      lipgloss.Color("#ff5555"),
	Success:    lipgloss.Color("#50fa7b"),
	Warning:    lipgloss.Color("#f1fa8c"),
	Muted:      lipgloss.Color("#44475a"),
}

// TUIThemeLight is the light theme (optional), matching ThemePaletteLight.
var TUIThemeLight = TUITheme{
	Name:       "light",
	Background: lipgloss.Color("#ffffff"),
	Foreground: lipgloss.Color("#282a36"),
	Primary:    lipgloss.Color("#6c5ce7"),
	Secondary:  lipgloss.Color("#636e72"),
	Accent:     lipgloss.Color("#0984e3"),
	Error:      lipgloss.Color("#d63031"),
	Success:    lipgloss.Color("#00b894"),
	Warning:    lipgloss.Color("#fdcb6e"),
	Muted:      lipgloss.Color("#dfe6e9"),
}

// themeByName resolves a cli.yml tui.theme value to a TUITheme, defaulting
// to dark for anything unrecognized.
func themeByName(name string) TUITheme {
	if name == "light" {
		return TUIThemeLight
	}
	return TUIThemeDark
}
