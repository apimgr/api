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
			assert.Equal(t, ModeCLI, detectMode(tc.argv))
		})
	}
}

// TestDetectMode_NonTerminalStdoutForcesPlain verifies that when stdout is
// not a terminal (always true under `go test`), detectMode returns
// ModePlain for any argv that doesn't contain help/version, regardless of
// whether it holds a bare command or only config-only flags. This is the
// only mode reachable in this test binary since os.Stdout is never a real
// TTY here; the interactive-terminal branch (bare command -> ModeCLI, or
// no args/config-only flags -> ModeTUI) requires a real TTY on stdout and
// cannot be exercised from `go test` without a pty, so it is not covered
// here.
func TestDetectMode_NonTerminalStdoutForcesPlain(t *testing.T) {
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
			assert.Equal(t, ModePlain, detectMode(tc.argv))
		})
	}
}
