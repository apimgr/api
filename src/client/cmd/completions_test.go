package cmd

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// captureStdout redirects os.Stdout for the duration of fn and returns
// whatever was written to it.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	orig := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w
	defer func() { os.Stdout = orig }()

	fn()

	require.NoError(t, w.Close())
	var buf bytes.Buffer
	_, err = io.Copy(&buf, r)
	require.NoError(t, err)
	return buf.String()
}

// TestDetectShell covers both the $SHELL-set and $SHELL-unset boundary.
func TestDetectShell(t *testing.T) {
	t.Run("SHELL set to a full path", func(t *testing.T) {
		t.Setenv("SHELL", "/usr/bin/zsh")
		assert.Equal(t, "zsh", detectShell())
	})

	t.Run("SHELL unset defaults to bash", func(t *testing.T) {
		t.Setenv("SHELL", "")
		assert.Equal(t, "bash", detectShell())
	})
}

// TestNormalizeShell covers every aliasing rule plus the passthrough
// default.
func TestNormalizeShell(t *testing.T) {
	tests := []struct{ in, want string }{
		{"sh", "sh"},
		{"dash", "sh"},
		{"ksh", "sh"},
		{"pwsh", "powershell"},
		{"powershell", "powershell"},
		{"bash", "bash"},
		{"zsh", "zsh"},
		{"fish", "fish"},
		{"unknown-shell", "unknown-shell"},
		{"", ""},
	}
	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			assert.Equal(t, tc.want, normalizeShell(tc.in))
		})
	}
}

// TestCompletionWords verifies every registered category and
// "category command" pair appears, using the real registry populated by
// the category files' init() functions.
func TestCompletionWords(t *testing.T) {
	words := completionWords()
	require.NotEmpty(t, words)
	assert.Contains(t, words, "text")
	assert.Contains(t, words, "text uuid")
	assert.Contains(t, words, "crypto")
	assert.Contains(t, words, "crypto bcrypt")
}

// TestCompletionScript_SupportedShells verifies every documented shell
// produces a non-empty script mentioning the binary name, and that
// unsupported shells error.
func TestCompletionScript_SupportedShells(t *testing.T) {
	for _, shell := range []string{"bash", "zsh", "fish", "sh", "powershell"} {
		t.Run(shell, func(t *testing.T) {
			script, err := completionScript(shell)
			require.NoError(t, err)
			assert.NotEmpty(t, script)
			assert.Contains(t, script, binName)
		})
	}
}

// TestCompletionScript_UnsupportedShell covers the error path for a shell
// name that isn't recognized.
func TestCompletionScript_UnsupportedShell(t *testing.T) {
	_, err := completionScript("cmd.exe")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cmd.exe")
}

// TestBashCompletionScript verifies the generated function name and that
// only the first token of each candidate ("category", not "category cmd")
// is offered for completion.
func TestBashCompletionScript(t *testing.T) {
	got := bashCompletionScript([]string{"text", "text uuid", "crypto"})
	assert.Contains(t, got, "_api_cli_completions()")
	assert.Contains(t, got, "complete -F _api_cli_completions api-cli")
	assert.Contains(t, got, `words="text text crypto "`)
}

// TestZshCompletionScript verifies deduplication of the first token across
// repeated categories.
func TestZshCompletionScript(t *testing.T) {
	got := zshCompletionScript([]string{"text", "text uuid", "text hash"})
	assert.Contains(t, got, "#compdef api-cli")
	assert.Contains(t, got, `"text"`)
	// Only one occurrence of the quoted "text" entry despite three words
	// sharing the same first token.
	assert.Equal(t, 1, strings.Count(got, `"text"`))
}

// TestFishCompletionScript verifies one `complete` line per distinct first
// token.
func TestFishCompletionScript(t *testing.T) {
	got := fishCompletionScript([]string{"text", "text uuid", "network"})
	assert.Contains(t, got, "complete -c api-cli -n '__fish_use_subcommand' -a 'text'")
	assert.Contains(t, got, "complete -c api-cli -n '__fish_use_subcommand' -a 'network'")
	assert.Equal(t, 1, strings.Count(got, "-a 'text'"))
}

// TestShCompletionScript verifies the fallback comment-only listing
// mentions every distinct command and the binary name.
func TestShCompletionScript(t *testing.T) {
	got := shCompletionScript([]string{"text", "text uuid"})
	assert.Contains(t, got, "POSIX sh has no native completion")
	assert.Contains(t, got, binName)
	assert.Contains(t, got, "#   text\n")
}

// TestPowershellCompletionScript verifies the ArgumentCompleter block
// references the binary name and lists distinct commands.
func TestPowershellCompletionScript(t *testing.T) {
	got := powershellCompletionScript([]string{"text", "text uuid"})
	assert.Contains(t, got, "Register-ArgumentCompleter -Native -CommandName api-cli")
	assert.Contains(t, got, `"text"`)
}

// TestPrintCompletionsHelp verifies the help text names all three actions.
func TestPrintCompletionsHelp(t *testing.T) {
	out := captureStdout(t, printCompletionsHelp)
	assert.Contains(t, out, "completions [SHELL]")
	assert.Contains(t, out, "init [SHELL]")
	assert.Contains(t, out, "help")
}

// TestPrintInitSnippet covers every supported shell's snippet plus the
// unsupported-shell error path.
func TestPrintInitSnippet(t *testing.T) {
	tests := []struct {
		shell   string
		wantErr bool
		want    string
	}{
		{"bash", false, "source <(api-cli --shell completions bash)"},
		{"zsh", false, "source <(api-cli --shell completions zsh)"},
		{"fish", false, "api-cli --shell completions fish | source"},
		{"sh", false, ". <(api-cli --shell completions sh)"},
		{"powershell", false, "Invoke-Expression"},
		{"cmd.exe", true, ""},
	}
	for _, tc := range tests {
		t.Run(tc.shell, func(t *testing.T) {
			var err error
			out := captureStdout(t, func() {
				err = printInitSnippet(tc.shell)
			})
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Contains(t, out, tc.want)
		})
	}
}

// TestRunCompletions covers the three documented actions plus the
// unknown-action error path, and verifies shell auto-detection from
// $SHELL kicks in when shell is empty.
func TestRunCompletions(t *testing.T) {
	t.Run("help action", func(t *testing.T) {
		out := captureStdout(t, func() {
			err := runCompletions("help", "bash")
			require.NoError(t, err)
		})
		assert.Contains(t, out, "shell completions")
	})

	t.Run("completions action with explicit shell", func(t *testing.T) {
		out := captureStdout(t, func() {
			err := runCompletions("completions", "fish")
			require.NoError(t, err)
		})
		assert.Contains(t, out, "complete -c api-cli")
	})

	t.Run("completions action with unsupported shell errors", func(t *testing.T) {
		err := runCompletions("completions", "cmd.exe")
		require.Error(t, err)
	})

	t.Run("init action with explicit shell", func(t *testing.T) {
		out := captureStdout(t, func() {
			err := runCompletions("init", "zsh")
			require.NoError(t, err)
		})
		assert.Contains(t, out, "completions zsh")
	})

	t.Run("shell empty falls back to $SHELL detection", func(t *testing.T) {
		t.Setenv("SHELL", "/bin/zsh")
		out := captureStdout(t, func() {
			err := runCompletions("init", "")
			require.NoError(t, err)
		})
		assert.Contains(t, out, "completions zsh")
	})

	t.Run("unknown action errors", func(t *testing.T) {
		err := runCompletions("bogus", "bash")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "unknown --shell action")
	})
}
