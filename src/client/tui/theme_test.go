package tui

import "testing"

import "github.com/stretchr/testify/assert"

// TestThemeByName table-drives theme resolution: recognized names return
// their matching palette, and anything unrecognized (including empty
// string) defaults to dark.
func TestThemeByName(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  TUITheme
	}{
		{"light returns light palette", "light", TUIThemeLight},
		{"dark returns dark palette", "dark", TUIThemeDark},
		{"empty string defaults to dark", "", TUIThemeDark},
		{"unrecognized name defaults to dark", "solarized", TUIThemeDark},
		{"case-sensitive Light does not match light", "Light", TUIThemeDark},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := themeByName(tc.input)
			assert.Equal(t, tc.want, got)
		})
	}
}

// TestTUIThemeDark_Name verifies the Name fields match their intended
// cli.yml tui.theme values, since themeByName compares against literal
// strings rather than the Name field.
func TestTUIThemeDark_Name(t *testing.T) {
	assert.Equal(t, "dark", TUIThemeDark.Name)
	assert.Equal(t, "light", TUIThemeLight.Name)
}
