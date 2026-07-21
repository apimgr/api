package cmd

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestParseGlobalFlags_EqualsSyntax covers the `--flag=value` parsing path
// for every value-taking global flag, plus the boolean-only flags.
func TestParseGlobalFlags_EqualsSyntax(t *testing.T) {
	argv := []string{
		"--server=https://example.com",
		"--token=abc123",
		"--token-file=/tmp/token",
		"--config=dev",
		"--color=yes",
		"--lang=en",
		"--output=json",
		"--debug",
		"-h",
	}
	p := parseGlobalFlags(argv)

	assert.True(t, p.Help)
	assert.True(t, p.Debug)
	assert.Equal(t, "https://example.com", p.Server)
	assert.Equal(t, "abc123", p.Token)
	assert.Equal(t, "/tmp/token", p.TokenFile)
	assert.Equal(t, "dev", p.ConfigName)
	assert.Equal(t, "yes", p.Color)
	assert.Equal(t, "en", p.Lang)
	assert.Equal(t, "json", p.Output)
	assert.Empty(t, p.Rest)
}

// TestParseGlobalFlags_SpaceSyntax covers the `--flag value` two-token
// parsing path, which must consume the following argv element.
func TestParseGlobalFlags_SpaceSyntax(t *testing.T) {
	argv := []string{"--server", "https://example.com", "--output", "table", "text", "uuid"}
	p := parseGlobalFlags(argv)

	assert.Equal(t, "https://example.com", p.Server)
	assert.Equal(t, "table", p.Output)
	assert.Equal(t, []string{"text", "uuid"}, p.Rest)
}

// TestParseGlobalFlags_TrailingValueFlagWithNoValue covers a value flag
// given as the very last argv element: there is nothing to consume, so the
// value must resolve to empty rather than panicking or swallowing input.
func TestParseGlobalFlags_TrailingValueFlagWithNoValue(t *testing.T) {
	p := parseGlobalFlags([]string{"--server"})
	assert.Empty(t, p.Server)
	assert.Empty(t, p.Rest)
}

// TestParseGlobalFlags_ShellFlagConsumesOptionalArg covers --shell's special
// case: it may be followed by an optional SHELL argument, which must only
// be consumed when the next token is not itself a flag.
func TestParseGlobalFlags_ShellFlagConsumesOptionalArg(t *testing.T) {
	t.Run("shell arg present", func(t *testing.T) {
		p := parseGlobalFlags([]string{"--shell", "completions", "bash"})
		assert.Equal(t, "completions", p.Shell)
		assert.Equal(t, "bash", p.ShellArg)
		assert.Empty(t, p.Rest)
	})

	t.Run("shell arg omitted, followed by another flag", func(t *testing.T) {
		p := parseGlobalFlags([]string{"--shell", "help", "--debug"})
		assert.Equal(t, "help", p.Shell)
		assert.Empty(t, p.ShellArg)
		assert.True(t, p.Debug)
	})

	t.Run("shell arg omitted, nothing follows", func(t *testing.T) {
		p := parseGlobalFlags([]string{"--shell", "help"})
		assert.Equal(t, "help", p.Shell)
		assert.Empty(t, p.ShellArg)
	})
}

// TestParseGlobalFlags_DefaultColor verifies the documented default of
// "auto" for --color when the flag is never supplied.
func TestParseGlobalFlags_DefaultColor(t *testing.T) {
	p := parseGlobalFlags(nil)
	assert.Equal(t, "auto", p.Color)
}

// TestParseGlobalFlags_UnknownFlagsAndPositionalsPassThrough verifies that
// anything not recognized as a global flag (including unknown --flags)
// lands in Rest untouched, in argv order.
func TestParseGlobalFlags_UnknownFlagsAndPositionalsPassThrough(t *testing.T) {
	p := parseGlobalFlags([]string{"text", "uuid", "--unknown-flag", "arg1"})
	assert.Equal(t, []string{"text", "uuid", "--unknown-flag", "arg1"}, p.Rest)
}

// TestParseGlobalFlags_Empty covers the empty-argv boundary.
func TestParseGlobalFlags_Empty(t *testing.T) {
	p := parseGlobalFlags([]string{})
	assert.False(t, p.Help)
	assert.False(t, p.Version)
	assert.False(t, p.Debug)
	assert.Empty(t, p.Rest)
}

// TestIsConfigOnlyFlag covers every flag PART 32 designates as "does not
// force CLI mode on its own", plus a representative flag that does.
func TestIsConfigOnlyFlag(t *testing.T) {
	tests := []struct {
		name string
		flag string
		want bool
	}{
		{"config", "--config", true},
		{"server", "--server", true},
		{"token", "--token", true},
		{"debug", "--debug", true},
		{"token-file not in the config-only set", "--token-file", false},
		{"output forces CLI mode", "--output", false},
		{"unknown flag", "--bogus", false},
		{"empty string", "", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, isConfigOnlyFlag(tc.flag))
		})
	}
}
