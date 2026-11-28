package cmd

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestDetectMode_HelpAndVersionAlwaysCLI verifies help/version win over
// every other signal, anywhere in argv, in either --flag or -f form.
func TestDetectMode_HelpAndVersionAlwaysCLI(t *testing.T) {
	tests := []struct {
		name string
		argv []string
	}{
		{"bare -h", []string{"-h"}},
		{"bare --help", []string{"--help"}},
		{"bare -v", []string{"-v"}},
		{"bare --version", []string{"--version"}},
		{"help after other flags", []string{"--debug", "--help"}},
		{"help as trailing token", []string{"text", "uuid", "-h"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, ClientDisplayModeCLI, detectMode(tc.argv))
		})
	}
}

// TestDetectMode_NonTerminal distinguishes headless startup from commands
// that should emit plain output when stdout is not a terminal.
func TestDetectMode_NonTerminal(t *testing.T) {
	tests := []struct {
		name string
		argv []string
	}{
		{"no args", nil},
		{"bare command", []string{"text", "uuid"}},
		{"config-only flags", []string{"--server", "https://example.com", "--debug"}},
		{"mixed flags and command", []string{"--output", "json", "text", "uuid"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			want := ClientDisplayModePlain
			if tc.name == "no args" || tc.name == "config-only flags" {
				want = ClientDisplayModeHeadless
			}
			assert.Equal(t, want, detectMode(tc.argv))
		})
	}
}

func TestResolveDisplayMode(t *testing.T) {
	tests := []struct {
		name      string
		detected  ClientDisplayMode
		requested string
		gui       bool
		wantMode  ClientDisplayMode
		wantError string
	}{
		{"auto keeps detected mode", ClientDisplayModeTUI, "auto", false, ClientDisplayModeTUI, ""},
		{"unsupported GUI is an explicit error", ClientDisplayModeTUI, "gui", false, ClientDisplayModeTUI, "gui_unavailable"},
		{"GUI mode is selected only by a GUI build", ClientDisplayModeTUI, "gui", true, ClientDisplayModeGUI, ""},
		{"TUI override selects TUI", ClientDisplayModeCLI, "tui", false, ClientDisplayModeTUI, ""},
		{"TUI override fails without an interactive mode", ClientDisplayModeHeadless, "tui", false, ClientDisplayModeHeadless, "tui_unavailable"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			mode, errKey := resolveDisplayMode(test.detected, test.requested, test.gui)
			assert.Equal(t, test.wantMode, mode)
			assert.Equal(t, test.wantError, errKey)
		})
	}
}
