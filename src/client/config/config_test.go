package config

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/apimgr/api/src/client/paths"
)

// setHome points HOME (and the Windows env vars paths.go consults) at a
// fresh temp dir so config.Load/Save operate against an isolated
// filesystem rather than the real user's home directory.
func setHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	if runtime.GOOS == "windows" {
		t.Setenv("APPDATA", home)
		t.Setenv("LOCALAPPDATA", home)
	}
	return home
}

// TestDefault verifies Default() returns exactly the spec's documented
// defaults (PART 32 CLI Auto-Update / Modes / Defaults sections).
func TestDefault(t *testing.T) {
	cfg := Default()
	assert.Equal(t, "no", cfg.Update.Auto)
	assert.Equal(t, "per_invocation", cfg.Update.CheckInterval)
	assert.Equal(t, "stable", cfg.Update.Channel)
	assert.Equal(t, "auto", cfg.Display.Mode)
	assert.Equal(t, "dark", cfg.TUI.Theme)
	assert.Equal(t, "auto", cfg.Defaults.Lang)
	assert.Equal(t, "table", cfg.Defaults.Output)
	assert.Equal(t, 20, cfg.Defaults.Limit)
	assert.Empty(t, cfg.Server.Primary)
	assert.Empty(t, cfg.Auth.Token)
}

// TestLoadMissingFile verifies a missing config file is not an error and
// Load falls back to Default(), per the doc comment on Load.
func TestLoadMissingFile(t *testing.T) {
	setHome(t)

	cfg, err := Load("cli")
	require.NoError(t, err)
	assert.Equal(t, Default(), cfg)
}

// TestSaveThenLoad round-trips a config through Save/Load and verifies
// the file is written with 0600 permissions, per PART 32's CLI Config
// File Permissions table.
func TestSaveThenLoad(t *testing.T) {
	setHome(t)

	cfg := Default()
	cfg.Server.Primary = "https://api.example.com"
	cfg.Auth.Token = "secret-token"
	cfg.Defaults.Limit = 50

	require.NoError(t, Save("cli", cfg))

	path := paths.ConfigFile()
	info, err := os.Stat(path)
	require.NoError(t, err)
	if runtime.GOOS != "windows" {
		assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
	}

	loaded, err := Load("cli")
	require.NoError(t, err)
	assert.Equal(t, cfg, loaded)
}

// TestSaveNamedProfile verifies Save/Load respect the profile name,
// writing to "<name>.yml" rather than the default cli.yml, and that
// separate profiles do not clobber each other.
func TestSaveNamedProfile(t *testing.T) {
	setHome(t)

	devCfg := Default()
	devCfg.Server.Primary = "https://dev.example.com"
	require.NoError(t, Save("dev", devCfg))

	stagingCfg := Default()
	stagingCfg.Server.Primary = "https://staging.example.com"
	require.NoError(t, Save("staging", stagingCfg))

	_, err := os.Stat(filepath.Join(paths.ConfigDir(), "dev.yml"))
	require.NoError(t, err)
	_, err = os.Stat(filepath.Join(paths.ConfigDir(), "staging.yml"))
	require.NoError(t, err)

	loadedDev, err := Load("dev")
	require.NoError(t, err)
	assert.Equal(t, "https://dev.example.com", loadedDev.Server.Primary)

	loadedStaging, err := Load("staging")
	require.NoError(t, err)
	assert.Equal(t, "https://staging.example.com", loadedStaging.Server.Primary)
}

// TestLoadInvalidYAML verifies Load surfaces a wrapped error rather than
// panicking or silently returning defaults when the on-disk file is not
// valid YAML.
func TestLoadInvalidYAML(t *testing.T) {
	setHome(t)
	require.NoError(t, paths.EnsureDirs())

	require.NoError(t, os.WriteFile(paths.ConfigFile(), []byte("not: [valid: yaml"), 0o600))

	_, err := Load("cli")
	require.Error(t, err)
}

// TestSavePartialYAMLPreservesDefaults verifies that loading a config
// file which only sets a subset of fields still yields the documented
// defaults for the fields it omits, since Load unmarshals onto
// Default() rather than a zero-value struct.
func TestSavePartialYAMLPreservesDefaults(t *testing.T) {
	setHome(t)
	require.NoError(t, paths.EnsureDirs())

	partial := "server:\n  primary: https://api.example.com\n"
	require.NoError(t, os.WriteFile(paths.ConfigFile(), []byte(partial), 0o600))

	cfg, err := Load("cli")
	require.NoError(t, err)
	assert.Equal(t, "https://api.example.com", cfg.Server.Primary)
	assert.Equal(t, "no", cfg.Update.Auto)
	assert.Equal(t, "table", cfg.Defaults.Output)
}

// TestSaveIfEmptyOrInvalid covers every row of the PART 32 Flag-to-Config
// Save Rules table: empty flag, invalid flag, empty current, valid
// current, invalid current, and no-validator (validate == nil).
func TestSaveIfEmptyOrInvalid(t *testing.T) {
	isURL := func(s string) bool {
		return len(s) > 8 && s[:8] == "https://"
	}

	tests := []struct {
		name        string
		current     string
		flagValue   string
		validate    func(string) bool
		wantUse     string
		wantPersist bool
	}{
		{
			name:        "flag not provided keeps current",
			current:     "https://old.example.com",
			flagValue:   "",
			validate:    isURL,
			wantUse:     "https://old.example.com",
			wantPersist: false,
		},
		{
			name:        "empty current, valid flag saves",
			current:     "",
			flagValue:   "https://new.example.com",
			validate:    isURL,
			wantUse:     "https://new.example.com",
			wantPersist: true,
		},
		{
			name:        "empty current, invalid flag keeps empty",
			current:     "",
			flagValue:   "not-a-url",
			validate:    isURL,
			wantUse:     "",
			wantPersist: false,
		},
		{
			name:        "valid current, valid flag uses but does not persist",
			current:     "https://old.example.com",
			flagValue:   "https://new.example.com",
			validate:    isURL,
			wantUse:     "https://new.example.com",
			wantPersist: false,
		},
		{
			name:        "valid current, invalid flag keeps current",
			current:     "https://old.example.com",
			flagValue:   "not-a-url",
			validate:    isURL,
			wantUse:     "https://old.example.com",
			wantPersist: false,
		},
		{
			name:        "invalid current, valid flag saves",
			current:     "not-a-url",
			flagValue:   "https://new.example.com",
			validate:    isURL,
			wantUse:     "https://new.example.com",
			wantPersist: true,
		},
		{
			name:        "invalid current, invalid flag keeps current",
			current:     "not-a-url",
			flagValue:   "still-not-a-url",
			validate:    isURL,
			wantUse:     "not-a-url",
			wantPersist: false,
		},
		{
			name:        "nil validator treats every flag value as valid",
			current:     "",
			flagValue:   "anything",
			validate:    nil,
			wantUse:     "anything",
			wantPersist: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			use, persist := SaveIfEmptyOrInvalid(tc.current, tc.flagValue, tc.validate)
			assert.Equal(t, tc.wantUse, use)
			assert.Equal(t, tc.wantPersist, persist)
		})
	}
}

// TestIsTruthy verifies the CLI's IsTruthy re-export matches the
// server's shared boolean parser, since the doc comment guarantees CLI
// flags and cli.yml booleans use the exact same rules.
func TestIsTruthy(t *testing.T) {
	tests := []struct {
		in   string
		want bool
	}{
		{"true", true},
		{"yes", true},
		{"1", true},
		{"  TRUE  ", true},
		{"false", false},
		{"no", false},
		{"0", false},
		{"", false},
		{"not-a-bool", false},
	}

	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			assert.Equal(t, tc.want, IsTruthy(tc.in))
		})
	}
}
