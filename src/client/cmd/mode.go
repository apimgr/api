package cmd

import (
	"os"
	"strings"

	"golang.org/x/term"
)

// Mode is the CLI's auto-detected display mode.
type Mode string

const (
	// ModeCLI runs a single command and exits (plain text with colors).
	ModeCLI Mode = "cli"
	// ModeTUI launches the interactive bubbletea application.
	ModeTUI Mode = "tui"
	// ModePlain runs like ModeCLI but stdout is not a terminal, so
	// colors and interactive prompts are disabled.
	ModePlain Mode = "plain"
)

// detectMode implements the PART 32 mode-detection algorithm: help/
// version always exit immediately as CLI; a non-terminal stdout forces
// plain output; a bare command/arg forces CLI mode; anything else
// (nothing, or only config-only flags) launches the TUI.
func detectMode(argv []string) Mode {
	for _, arg := range argv {
		name, _, _ := strings.Cut(arg, "=")
		if name == "-h" || name == "--help" || name == "-v" || name == "--version" {
			return ModeCLI
		}
	}

	if !term.IsTerminal(int(os.Stdout.Fd())) {
		return ModePlain
	}

	for i := 0; i < len(argv); i++ {
		arg := argv[i]
		if !strings.HasPrefix(arg, "-") {
			return ModeCLI
		}
		name, _, hasEquals := strings.Cut(arg, "=")
		if !isConfigOnlyFlag(name) {
			return ModeCLI
		}
		if globalValueFlags[name] && !hasEquals && i+1 < len(argv) {
			i++
		}
	}

	return ModeTUI
}
