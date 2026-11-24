package display

import "testing"

func TestDisplayModeHierarchy(t *testing.T) {
	tests := []struct {
		name string
		env  DisplayEnv
		want DisplayMode
	}{
		{"headless", DisplayEnv{}, DisplayModeHeadless},
		{"command line without terminal", DisplayEnv{HasDisplay: true, IsSSH: true}, DisplayModeCLI},
		{"dumb terminal forces CLI", DisplayEnv{IsTerminal: true, HasDisplay: true, TerminalType: "dumb"}, DisplayModeCLI},
		{"interactive terminal", DisplayEnv{IsTerminal: true}, DisplayModeTUI},
		{"screen session is terminal UI", DisplayEnv{IsTerminal: true, IsScreen: true}, DisplayModeTUI},
		{"local graphical display", DisplayEnv{IsTerminal: true, HasDisplay: true, DisplayType: "wayland"}, DisplayModeGUI},
		{"remote graphical session stays TUI", DisplayEnv{IsTerminal: true, HasDisplay: true, IsSSH: true}, DisplayModeTUI},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := test.env.autoDetectDisplayMode(); got != test.want {
				t.Fatalf("mode=%d want %d", got, test.want)
			}
		})
	}
}

func TestCanUseANSI(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	if CanUseANSI(DisplayEnv{IsTerminal: false}) {
		t.Fatal("ANSI enabled without a terminal")
	}
	t.Setenv("NO_COLOR", "1")
	if CanUseANSI(DisplayEnv{IsTerminal: true}) {
		t.Fatal("ANSI enabled with NO_COLOR")
	}
	t.Setenv("NO_COLOR", "")
	if CanUseANSI(DisplayEnv{IsTerminal: true, TerminalType: "dumb"}) {
		t.Fatal("ANSI enabled for TERM=dumb")
	}
	if !CanUseANSI(DisplayEnv{IsTerminal: true, TerminalType: "xterm-256color"}) {
		t.Fatal("ANSI disabled for capable terminal")
	}
}
