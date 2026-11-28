package cmd

import (
	"strings"

	"github.com/apimgr/api/src/client/caps"
	"github.com/apimgr/api/src/common/display"
)

// ClientDisplayMode is the CLI's auto-detected display mode.
type ClientDisplayMode string

const (
	// ClientDisplayModeCLI runs a single command and exits (plain text with colors).
	ClientDisplayModeCLI ClientDisplayMode = "cli"
	// ClientDisplayModeGUI identifies an optional native GUI build.
	ClientDisplayModeGUI ClientDisplayMode = "gui"
	// ClientDisplayModeTUI launches the interactive bubbletea application.
	ClientDisplayModeTUI ClientDisplayMode = "tui"
	// ClientDisplayModePlain runs like ClientDisplayModeCLI but stdout is not a terminal, so
	// colors and interactive prompts are disabled.
	ClientDisplayModePlain ClientDisplayMode = "plain"
	// ClientDisplayModeHeadless identifies an interactive invocation without a display or TTY.
	ClientDisplayModeHeadless ClientDisplayMode = "headless"
)

// detectMode implements the PART 32 mode-detection algorithm: help/version
// always exit immediately; commands use CLI or plain mode; an interactive
// invocation selects GUI only in a GUI-capable build, otherwise TUI; and an
// invocation without an interactive display or terminal is headless.
func detectMode(argv []string) ClientDisplayMode {
	for _, arg := range argv {
		name, _, _ := strings.Cut(arg, "=")
		if name == "-h" || name == "--help" || name == "-v" || name == "--version" {
			return ClientDisplayModeCLI
		}
	}

	env := display.DetectDisplayEnv()
	hasCommand := false
	for i := 0; i < len(argv); i++ {
		arg := argv[i]
		if !strings.HasPrefix(arg, "-") {
			hasCommand = true
			break
		}
		name, _, hasEquals := strings.Cut(arg, "=")
		if !isConfigOnlyFlag(name) {
			hasCommand = true
			break
		}
		if globalValueFlags[name] && !hasEquals && i+1 < len(argv) {
			i++
		}
	}

	if hasCommand {
		if !env.IsTerminal {
			return ClientDisplayModePlain
		}
		return ClientDisplayModeCLI
	}
	if env.IsDumbTerminal() {
		return ClientDisplayModeHeadless
	}
	if env.HasDisplay && !env.IsSSH && !env.IsMosh && caps.GUISupported {
		return ClientDisplayModeGUI
	}
	if env.IsTerminal {
		return ClientDisplayModeTUI
	}
	return ClientDisplayModeHeadless
}

func resolveDisplayMode(detected ClientDisplayMode, requested string, guiSupported bool) (ClientDisplayMode, string) {
	switch strings.ToLower(strings.TrimSpace(requested)) {
	case "", "auto":
		return detected, ""
	case "gui":
		if !guiSupported {
			return detected, "gui_unavailable"
		}
		return ClientDisplayModeGUI, ""
	case "tui":
		if detected == ClientDisplayModeHeadless || detected == ClientDisplayModePlain {
			return detected, "tui_unavailable"
		}
		return ClientDisplayModeTUI, ""
	default:
		return detected, ""
	}
}
