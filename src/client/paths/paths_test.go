package paths

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestConfigDir covers the config directory resolution for both the
// Windows (APPDATA-based) and POSIX (~/.config-based) branches, since
// ConfigDir switches on runtime.GOOS.
func TestConfigDir(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Setenv("APPDATA", `C:\Users\tester\AppData\Roaming`)
		got := ConfigDir()
		assert.Equal(t, filepath.Join(`C:\Users\tester\AppData\Roaming`, OrgName, ProjectName), got)
		return
	}

	home := t.TempDir()
	t.Setenv("HOME", home)
	got := ConfigDir()
	assert.Equal(t, filepath.Join(home, ".config", OrgName, ProjectName), got)
}

// TestDataDir mirrors TestConfigDir for the data directory, which uses
// LOCALAPPDATA on Windows and ~/.local/share elsewhere.
func TestDataDir(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Setenv("LOCALAPPDATA", `C:\Users\tester\AppData\Local`)
		got := DataDir()
		assert.Equal(t, filepath.Join(`C:\Users\tester\AppData\Local`, OrgName, ProjectName, "data"), got)
		return
	}

	home := t.TempDir()
	t.Setenv("HOME", home)
	got := DataDir()
	assert.Equal(t, filepath.Join(home, ".local", "share", OrgName, ProjectName), got)
}

// TestCacheDir mirrors TestConfigDir for the cache directory.
func TestCacheDir(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Setenv("LOCALAPPDATA", `C:\Users\tester\AppData\Local`)
		got := CacheDir()
		assert.Equal(t, filepath.Join(`C:\Users\tester\AppData\Local`, OrgName, ProjectName, "cache"), got)
		return
	}

	home := t.TempDir()
	t.Setenv("HOME", home)
	got := CacheDir()
	assert.Equal(t, filepath.Join(home, ".cache", OrgName, ProjectName), got)
}

// TestLogDir mirrors TestConfigDir for the log directory. Per PART 4, the
// non-root Linux log dir lives under ~/.local/log, NOT ~/.local/share.
func TestLogDir(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Setenv("LOCALAPPDATA", `C:\Users\tester\AppData\Local`)
		got := LogDir()
		assert.Equal(t, filepath.Join(`C:\Users\tester\AppData\Local`, OrgName, ProjectName, "log"), got)
		return
	}

	home := t.TempDir()
	t.Setenv("HOME", home)
	got := LogDir()
	assert.Equal(t, filepath.Join(home, ".local", "log", OrgName, ProjectName), got)
}

// TestConfigFile verifies the default config file path is cli.yml inside
// ConfigDir().
func TestConfigFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if runtime.GOOS == "windows" {
		t.Setenv("APPDATA", home)
	}
	assert.Equal(t, filepath.Join(ConfigDir(), "cli.yml"), ConfigFile())
}

// TestNamedConfigFile covers the profile-name resolution rules: empty
// string and the literal "cli" both alias to the default cli.yml, while
// any other name resolves to "<name>.yml" in the same directory.
func TestNamedConfigFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if runtime.GOOS == "windows" {
		t.Setenv("APPDATA", home)
	}

	tests := []struct {
		name    string
		profile string
		want    string
	}{
		{"empty profile aliases default", "", ConfigFile()},
		{"cli profile aliases default", "cli", ConfigFile()},
		{"named profile", "dev", filepath.Join(ConfigDir(), "dev.yml")},
		{"another named profile", "staging", filepath.Join(ConfigDir(), "staging.yml")},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, NamedConfigFile(tc.profile))
		})
	}
}

// TestLogFile verifies the log file path is cli.log inside LogDir().
func TestLogFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if runtime.GOOS == "windows" {
		t.Setenv("LOCALAPPDATA", home)
	}
	assert.Equal(t, filepath.Join(LogDir(), "cli.log"), LogFile())
}

// TestEnsureDirs verifies all four directories are created with 0700
// permissions (user-only), and that calling it twice (idempotency) does
// not error.
func TestEnsureDirs(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if runtime.GOOS == "windows" {
		t.Setenv("APPDATA", home)
		t.Setenv("LOCALAPPDATA", home)
	}

	require.NoError(t, EnsureDirs())

	dirs := []string{ConfigDir(), DataDir(), CacheDir(), LogDir()}
	for _, dir := range dirs {
		info, err := os.Stat(dir)
		require.NoError(t, err)
		assert.True(t, info.IsDir())
		if runtime.GOOS != "windows" {
			assert.Equal(t, os.FileMode(0o700), info.Mode().Perm())
		}
	}

	// Idempotency: calling EnsureDirs again on already-existing
	// directories must not error.
	require.NoError(t, EnsureDirs())
}
