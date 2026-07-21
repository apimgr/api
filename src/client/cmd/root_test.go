package cmd

import (
	"errors"
	"io"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/apimgr/api/src/client/api"
	"github.com/apimgr/api/src/client/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// captureOutput redirects both stdout and stderr for the duration of fn and
// returns what each captured, plus fn's own return value. Execute returns an
// int exit code rather than an error, so this can't reuse output.Capture.
func captureOutput(t *testing.T, fn func() int) (stdout string, stderr string, code int) {
	t.Helper()

	origOut, origErr := os.Stdout, os.Stderr
	outR, outW, err := os.Pipe()
	require.NoError(t, err)
	errR, errW, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = outW
	os.Stderr = errW
	t.Cleanup(func() {
		os.Stdout = origOut
		os.Stderr = origErr
	})

	code = fn()

	require.NoError(t, outW.Close())
	require.NoError(t, errW.Close())
	os.Stdout = origOut
	os.Stderr = origErr

	outBytes, err := io.ReadAll(outR)
	require.NoError(t, err)
	errBytes, err := io.ReadAll(errR)
	require.NoError(t, err)

	return string(outBytes), string(errBytes), code
}

// isolatedHome points HOME (and Windows APPDATA/LOCALAPPDATA) at a fresh
// temp dir so config/paths code under test never touches the real user
// environment.
func isolatedHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	if runtime.GOOS == "windows" {
		t.Setenv("APPDATA", home)
		t.Setenv("LOCALAPPDATA", home)
	}
	return home
}

// TestExecute_Help verifies -h/--help short-circuits before any config or
// network work and exits ExitSuccess.
func TestExecute_Help(t *testing.T) {
	isolatedHome(t)
	stdout, _, code := captureOutput(t, func() int {
		return Execute([]string{"api-cli", "--help"}, BuildInfo{Version: "1.2.3"})
	})
	assert.Equal(t, ExitSuccess, code)
	assert.Contains(t, stdout, "api-cli 1.2.3")
	assert.Contains(t, stdout, "Usage:")
}

// TestExecute_Version verifies -v/--version prints and exits ExitSuccess.
func TestExecute_Version(t *testing.T) {
	isolatedHome(t)
	stdout, _, code := captureOutput(t, func() int {
		return Execute([]string{"api-cli", "--version"}, BuildInfo{Version: "1.2.3", Commit: "abc123", BuildDate: "2026-01-01"})
	})
	assert.Equal(t, ExitSuccess, code)
	assert.Contains(t, stdout, "api-cli 1.2.3 (abc123) built 2026-01-01")
}

// TestExecute_ShellCompletions verifies --shell dispatch happens before
// config loading and succeeds for a supported shell.
func TestExecute_ShellCompletions(t *testing.T) {
	isolatedHome(t)
	stdout, _, code := captureOutput(t, func() int {
		return Execute([]string{"api-cli", "--shell", "completions", "bash"}, BuildInfo{Version: "1.0.0"})
	})
	assert.Equal(t, ExitSuccess, code)
	assert.Contains(t, stdout, "_api_cli_completions")
}

// TestExecute_ShellUnsupportedShell verifies an unsupported shell name maps
// to ExitUsageBad with an error on stderr.
func TestExecute_ShellUnsupportedShell(t *testing.T) {
	isolatedHome(t)
	_, stderr, code := captureOutput(t, func() int {
		return Execute([]string{"api-cli", "--shell", "completions", "cobol"}, BuildInfo{Version: "1.0.0"})
	})
	assert.Equal(t, ExitUsageBad, code)
	assert.Contains(t, stderr, "Error:")
}

// TestExecute_InvalidServerFlag verifies a malformed --server value is
// rejected before a client is ever constructed.
func TestExecute_InvalidServerFlag(t *testing.T) {
	isolatedHome(t)
	_, stderr, code := captureOutput(t, func() int {
		return Execute([]string{"api-cli", "--server", "not-a-url", "--output", "json", "system", "health"}, BuildInfo{Version: "1.0.0"})
	})
	assert.Equal(t, ExitUsageBad, code)
	assert.Contains(t, stderr, "invalid server URL")
}

// TestExecute_SuccessfulDispatch drives Execute end-to-end against a real
// httptest.Server: config directories get created, the server URL and a
// token get persisted to cli.yml, and the command actually reaches the
// fake server.
func TestExecute_SuccessfulDispatch(t *testing.T) {
	home := isolatedHome(t)
	srv := httptest.NewServer(nil)
	t.Cleanup(srv.Close)

	stdout, stderr, code := captureOutput(t, func() int {
		return Execute([]string{
			"api-cli",
			"--server", srv.URL,
			"--token", "secret-token",
			"--output", "json",
			"system", "health",
		}, BuildInfo{Version: "1.0.0"})
	})

	// The bare httptest.NewServer(nil) handler 404s every request, which
	// the client surfaces as a *api.Error mapped to ExitNotFound - this
	// still proves the request reached the fake server and the exit-code
	// mapping in exitCodeForError is wired up end-to-end.
	assert.Equal(t, ExitNotFound, code)
	assert.Contains(t, stderr, "Error:")
	assert.Empty(t, stdout)

	cfgPath := filepath.Join(home, ".config", "apimgr", "api", "cli.yml")
	_, err := os.Stat(cfgPath)
	require.NoError(t, err, "cli.yml should have been persisted")
}

// TestExecute_UnknownCategory verifies dispatch of a category with no
// registered commands exits ExitUsageBad.
func TestExecute_UnknownCategory(t *testing.T) {
	isolatedHome(t)
	srv := httptest.NewServer(nil)
	t.Cleanup(srv.Close)

	_, stderr, code := captureOutput(t, func() int {
		return Execute([]string{"api-cli", "--server", srv.URL, "does-not-exist"}, BuildInfo{Version: "1.0.0"})
	})
	assert.Equal(t, ExitUsageBad, code)
	assert.Contains(t, stderr, "unknown command: does-not-exist")
}

// TestExecute_CategoryOnlyListsSubcommands verifies a known category with
// no subcommand name prints a usage listing and exits ExitSuccess.
func TestExecute_CategoryOnlyListsSubcommands(t *testing.T) {
	isolatedHome(t)
	srv := httptest.NewServer(nil)
	t.Cleanup(srv.Close)

	stdout, _, code := captureOutput(t, func() int {
		return Execute([]string{"api-cli", "--server", srv.URL, "system"}, BuildInfo{Version: "1.0.0"})
	})
	assert.Equal(t, ExitSuccess, code)
	assert.Contains(t, stdout, "Usage: system <subcommand> [args]")
	assert.Contains(t, stdout, "health")
}

// TestExecute_UnknownCommandInKnownCategory verifies a known category with
// an unregistered subcommand name exits ExitUsageBad.
func TestExecute_UnknownCommandInKnownCategory(t *testing.T) {
	isolatedHome(t)
	srv := httptest.NewServer(nil)
	t.Cleanup(srv.Close)

	_, stderr, code := captureOutput(t, func() int {
		return Execute([]string{"api-cli", "--server", srv.URL, "system", "does-not-exist"}, BuildInfo{Version: "1.0.0"})
	})
	assert.Equal(t, ExitUsageBad, code)
	assert.Contains(t, stderr, "unknown command: system does-not-exist")
}

// TestExecute_NoCommandGiven verifies plain mode with zero positional args
// (config-only flags supplied, forcing ModePlain via non-tty stdout) prints
// a usage hint and exits ExitUsageBad rather than hanging on a TUI launch.
func TestExecute_NoCommandGiven(t *testing.T) {
	isolatedHome(t)
	srv := httptest.NewServer(nil)
	t.Cleanup(srv.Close)

	_, stderr, code := captureOutput(t, func() int {
		return Execute([]string{"api-cli", "--server", srv.URL, "--debug"}, BuildInfo{Version: "1.0.0"})
	})
	assert.Equal(t, ExitUsageBad, code)
	assert.Contains(t, stderr, "no command given")
}

// TestExecute_TUILauncherMissing documents the guard: none of the tests in
// this package wire up TUILauncher (that would require importing tui, which
// itself imports cmd), so ModeTUI dispatch is exercised only in the tui
// package's own tests. This test just confirms the assumption holds.
func TestExecute_TUILauncherMissing(t *testing.T) {
	isolatedHome(t)
	require.Nil(t, TUILauncher, "test assumes no launcher has been wired up")
}

// TestRunCLI_NoCommand verifies runCLI directly with empty Rest.
func TestRunCLI_NoCommand(t *testing.T) {
	client := api.New("http://127.0.0.1:1", "")
	_, stderr, code := captureOutput(t, func() int {
		return runCLI(parsedFlags{}, client)
	})
	assert.Equal(t, ExitUsageBad, code)
	assert.Contains(t, stderr, "no command given")
}

// TestRunCLI_DefaultOutputFormat verifies an empty --output falls back to
// "table" rather than being passed through empty to OutputOptions.
func TestRunCLI_DefaultOutputFormat(t *testing.T) {
	srv, rec := newRecordingServer(t, 200, `{"status":"ok"}`)
	client := api.New(srv.URL, "")

	_, _, code := captureOutput(t, func() int {
		return runCLI(parsedFlags{Rest: []string{"system", "health"}}, client)
	})
	assert.Equal(t, ExitSuccess, code)
	assert.Equal(t, "/api/v1/system/health", rec.Path)
}

// TestExitCodeForError table-drives the PART 32 exit-code mapping.
func TestExitCodeForError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want int
	}{
		{"401 unauthorized maps to ExitAuth", &api.Error{StatusCode: 401, Body: "no"}, ExitAuth},
		{"403 forbidden maps to ExitAuth", &api.Error{StatusCode: 403, Body: "no"}, ExitAuth},
		{"404 not found maps to ExitNotFound", &api.Error{StatusCode: 404, Body: "no"}, ExitNotFound},
		{"500 maps to ExitGeneral", &api.Error{StatusCode: 500, Body: "no"}, ExitGeneral},
		{"no server configured maps to ExitConn", errors.New("no server configured"), ExitConn},
		{"cannot connect maps to ExitConn", errors.New("cannot connect to server at x: dial refused"), ExitConn},
		{"missing required argument maps to ExitUsageBad", errors.New("missing required argument: text"), ExitUsageBad},
		{"generic error maps to ExitGeneral", errors.New("something went wrong"), ExitGeneral},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, stderr, code := captureOutput(t, func() int {
				return exitCodeForError(tc.err)
			})
			assert.Equal(t, tc.want, code)
			assert.Contains(t, stderr, "Error:")
			assert.Contains(t, stderr, tc.err.Error())
		})
	}
}

// TestResolveToken_Priority verifies the documented priority order:
// --token-file > --token flag > API_TOKEN env > cli.yml auth.token.
func TestResolveToken_Priority(t *testing.T) {
	tmp := t.TempDir()
	tokenFile := filepath.Join(tmp, "token.txt")
	require.NoError(t, os.WriteFile(tokenFile, []byte("from-file\n"), 0o600))

	t.Run("token file wins over everything", func(t *testing.T) {
		t.Setenv("API_TOKEN", "from-env")
		cfg := &config.CLIConfig{}
		cfg.Auth.Token = "from-config"
		got := resolveToken(cfg, parsedFlags{TokenFile: tokenFile, Token: "from-flag"})
		assert.Equal(t, "from-file", got)
	})

	t.Run("flag wins over env and config", func(t *testing.T) {
		t.Setenv("API_TOKEN", "from-env")
		cfg := &config.CLIConfig{}
		cfg.Auth.Token = "from-config"
		got := resolveToken(cfg, parsedFlags{Token: "from-flag"})
		assert.Equal(t, "from-flag", got)
	})

	t.Run("env wins over config", func(t *testing.T) {
		t.Setenv("API_TOKEN", "from-env")
		cfg := &config.CLIConfig{}
		cfg.Auth.Token = "from-config"
		got := resolveToken(cfg, parsedFlags{})
		assert.Equal(t, "from-env", got)
	})

	t.Run("falls back to config", func(t *testing.T) {
		t.Setenv("API_TOKEN", "")
		cfg := &config.CLIConfig{}
		cfg.Auth.Token = "from-config"
		got := resolveToken(cfg, parsedFlags{})
		assert.Equal(t, "from-config", got)
	})

	t.Run("token file read failure falls through to flag", func(t *testing.T) {
		t.Setenv("API_TOKEN", "")
		cfg := &config.CLIConfig{}
		got := resolveToken(cfg, parsedFlags{TokenFile: filepath.Join(tmp, "does-not-exist.txt"), Token: "from-flag"})
		assert.Equal(t, "from-flag", got)
	})
}

// TestValidServerURL table-drives URL validation.
func TestValidServerURL(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want bool
	}{
		{"valid http", "http://localhost:8080", true},
		{"valid https", "https://api.example.com", true},
		{"empty string", "", false},
		{"missing scheme", "localhost:8080", false},
		{"unsupported scheme", "ftp://example.com", false},
		{"scheme with no host", "http://", false},
		{"malformed", "http://[::1", false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, validServerURL(tc.in))
		})
	}
}

// TestBinaryName table-drives the invoked-binary-name resolution: unix
// paths, windows paths with backslashes, .exe suffix trimming, and the
// empty-argv/empty-string fallbacks.
func TestBinaryName(t *testing.T) {
	tests := []struct {
		name string
		argv []string
		want string
	}{
		{"unix path", []string{"/usr/local/bin/api-cli"}, "api-cli"},
		{"windows path with backslash", []string{`C:\Program Files\api-cli.exe`}, "api-cli"},
		{"bare name", []string{"api-cli"}, "api-cli"},
		{"renamed binary", []string{"/usr/bin/mycli"}, "mycli"},
		{"empty argv", []string{}, "api-cli"},
		{"empty string arg0", []string{""}, "api-cli"},
		{"trailing slash yields empty base", []string{"/usr/local/bin/"}, "api-cli"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, binaryName(tc.argv))
		})
	}
}

// TestPrintVersion verifies the exact version output format.
func TestPrintVersion(t *testing.T) {
	stdout, _, _ := captureOutput(t, func() int {
		printVersion("api-cli", BuildInfo{Version: "9.9.9", Commit: "deadbeef", BuildDate: "2026-07-20"})
		return 0
	})
	assert.Equal(t, "api-cli 9.9.9 (deadbeef) built 2026-07-20\n", stdout)
}

// TestPrintHelp smoke-tests help output includes usage chrome and every
// registered category's commands, using the real registry.
func TestPrintHelp(t *testing.T) {
	stdout, _, _ := captureOutput(t, func() int {
		printHelp("api-cli", BuildInfo{Version: "1.0.0"})
		return 0
	})
	assert.Contains(t, stdout, "api-cli 1.0.0 - CLI for api")
	assert.Contains(t, stdout, "Usage:")
	assert.Contains(t, stdout, "--shell completions [SHELL]")
	assert.Contains(t, stdout, "Commands:")
	assert.Contains(t, stdout, "system")
	assert.Contains(t, stdout, "Run without arguments for interactive TUI mode.")
	for _, cat := range categories() {
		assert.Contains(t, stdout, cat)
	}
}
