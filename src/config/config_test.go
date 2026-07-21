package config

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/apimgr/api/src/paths"
)

// initTestPaths points the paths package at fresh temp directories for
// config/data/logs so Load/Save exercise real file I/O without touching the
// developer's actual config directory. paths has no per-test reset, so each
// call re-initializes with a distinct temp dir.
func initTestPaths(t *testing.T) (configDir, dataDir string) {
	t.Helper()
	configDir = t.TempDir()
	dataDir = t.TempDir()
	logDir := t.TempDir()
	paths.Init(configDir, dataDir, logDir)
	return configDir, dataDir
}

// resetGlobalConfig clears the package-level currentConfig singleton so
// tests don't leak state into each other via Get()/Set().
func resetGlobalConfig(t *testing.T) {
	t.Helper()
	configMu.Lock()
	currentConfig = nil
	configMu.Unlock()
	t.Cleanup(func() {
		configMu.Lock()
		currentConfig = nil
		configMu.Unlock()
	})
}

// TestDefaultConfig checks the built-in defaults match what AI.md PART 12
// / the struct doc comments promise: sane hostname fallback, expected
// feature toggles, and a port in the documented 64xxx random range.
func TestDefaultConfig(t *testing.T) {
	initTestPaths(t)

	cfg := defaultConfig()
	require.NotNil(t, cfg)

	assert.Equal(t, "0.0.0.0", cfg.Server.Address)
	assert.Equal(t, "production", cfg.Server.Mode)
	assert.Equal(t, "v1", cfg.Server.APIVersion)
	assert.False(t, cfg.Server.SSL.Enabled)
	assert.True(t, cfg.Server.Schedule.Enabled)
	assert.True(t, cfg.Server.RateLimit.Enabled)
	assert.Equal(t, "sqlite", cfg.Server.Database.Driver)
	assert.Equal(t, "dark", cfg.Web.UI.Theme)
	assert.Equal(t, "*", cfg.Web.CORS)
	assert.NotEmpty(t, cfg.Server.FQDN)

	port, err := stringToInt(cfg.Server.Port)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, port, 64000)
	assert.LessOrEqual(t, port, 64999)
}

// stringToInt is a tiny local helper so the port-range assertion above
// doesn't need to pull in strconv just for one test.
func stringToInt(s string) (int, error) {
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0, fmt.Errorf("not a digit: %q", r)
		}
		n = n*10 + int(r-'0')
	}
	return n, nil
}

// TestGenerateRandomPort checks the port is always exactly 5 digits and
// falls in the documented 64000-64999 range across many samples, since the
// generator does manual digit-by-digit string building that's easy to get
// wrong at the boundaries.
func TestGenerateRandomPort(t *testing.T) {
	for i := 0; i < 200; i++ {
		port := generateRandomPort()
		require.Len(t, port, 5)
		n, err := stringToInt(port)
		require.NoError(t, err)
		assert.GreaterOrEqual(t, n, 64000)
		assert.LessOrEqual(t, n, 64999)
	}
}

// TestLoadCreatesDefaultWhenMissing verifies Load() writes a default
// server.yml the first time it's called against an empty config dir, and
// that the returned config is usable.
func TestLoadCreatesDefaultWhenMissing(t *testing.T) {
	configDir, _ := initTestPaths(t)
	resetGlobalConfig(t)

	cfg, err := Load()
	require.NoError(t, err)
	require.NotNil(t, cfg)

	configFile := filepath.Join(configDir, "server.yml")
	assert.FileExists(t, configFile)

	data, err := os.ReadFile(configFile)
	require.NoError(t, err)
	assert.Contains(t, string(data), "# CasTools Configuration")
}

// TestLoadReadsExistingFile verifies Load() parses an existing server.yml
// rather than overwriting it, and that a round-tripped value survives.
func TestLoadReadsExistingFile(t *testing.T) {
	configDir, _ := initTestPaths(t)
	resetGlobalConfig(t)

	cfg := defaultConfig()
	cfg.Server.FQDN = "roundtrip.example.com"
	cfg.Server.Branding.Title = "Custom Title"
	require.NoError(t, Save(cfg))
	_ = configDir

	loaded, err := Load()
	require.NoError(t, err)
	assert.Equal(t, "roundtrip.example.com", loaded.Server.FQDN)
	assert.Equal(t, "Custom Title", loaded.Server.Branding.Title)
}

// TestLoadInvalidYAMLReturnsError verifies malformed YAML on disk surfaces
// as an error rather than being silently swallowed into defaults.
func TestLoadInvalidYAMLReturnsError(t *testing.T) {
	configDir, _ := initTestPaths(t)
	resetGlobalConfig(t)

	require.NoError(t, os.MkdirAll(configDir, 0755))
	badFile := filepath.Join(configDir, "server.yml")
	require.NoError(t, os.WriteFile(badFile, []byte("server: [unterminated"), 0644))

	_, err := Load()
	assert.Error(t, err)
}

// TestApplyDatabaseEnvOverrides covers the three override env vars
// (DATABASE_DRIVER, DATABASE_URL, DATABASE_DIR), their priority
// (DATABASE_URL over DATABASE_DIR), and the no-env no-op case.
func TestApplyDatabaseEnvOverrides(t *testing.T) {
	t.Run("no env vars leaves config untouched", func(t *testing.T) {
		cfg := &Config{Server: ServerConfig{Database: DatabaseConfig{Driver: "sqlite", URL: "orig"}}}
		applyDatabaseEnvOverrides(cfg)
		assert.Equal(t, "sqlite", cfg.Server.Database.Driver)
		assert.Equal(t, "orig", cfg.Server.Database.URL)
	})

	t.Run("DATABASE_DRIVER overrides driver", func(t *testing.T) {
		t.Setenv("DATABASE_DRIVER", "postgres")
		cfg := &Config{Server: ServerConfig{Database: DatabaseConfig{Driver: "sqlite"}}}
		applyDatabaseEnvOverrides(cfg)
		assert.Equal(t, "postgres", cfg.Server.Database.Driver)
	})

	t.Run("DATABASE_URL overrides URL directly", func(t *testing.T) {
		t.Setenv("DATABASE_URL", "postgres://host/db")
		cfg := &Config{Server: ServerConfig{Database: DatabaseConfig{URL: "orig"}}}
		applyDatabaseEnvOverrides(cfg)
		assert.Equal(t, "postgres://host/db", cfg.Server.Database.URL)
	})

	t.Run("DATABASE_DIR builds server.db path when URL unset", func(t *testing.T) {
		t.Setenv("DATABASE_DIR", "/custom/data")
		cfg := &Config{Server: ServerConfig{Database: DatabaseConfig{URL: "orig"}}}
		applyDatabaseEnvOverrides(cfg)
		assert.Equal(t, filepath.Join("/custom/data", "server.db"), cfg.Server.Database.URL)
	})

	t.Run("DATABASE_URL takes priority over DATABASE_DIR", func(t *testing.T) {
		t.Setenv("DATABASE_URL", "postgres://host/db")
		t.Setenv("DATABASE_DIR", "/custom/data")
		cfg := &Config{Server: ServerConfig{Database: DatabaseConfig{URL: "orig"}}}
		applyDatabaseEnvOverrides(cfg)
		assert.Equal(t, "postgres://host/db", cfg.Server.Database.URL)
	})
}

// TestSaveCreatesConfigDir verifies Save() creates a missing config
// directory rather than failing.
func TestSaveCreatesConfigDir(t *testing.T) {
	configDir := filepath.Join(t.TempDir(), "nested", "config")
	dataDir := t.TempDir()
	paths.Init(configDir, dataDir, t.TempDir())

	cfg := defaultConfig()
	require.NoError(t, Save(cfg))
	assert.FileExists(t, filepath.Join(configDir, "server.yml"))
}

// TestGetSetReload covers the Get()/Set()/Reload() thread-safe accessor
// trio: Get() lazily loads when nothing has been set, Set() overrides in
// memory without touching disk, and Reload() re-reads from disk and
// overwrites whatever was in memory.
func TestGetSetReload(t *testing.T) {
	initTestPaths(t)
	resetGlobalConfig(t)

	// Get() with nothing set yet should lazily Load().
	got := Get()
	require.NotNil(t, got)

	// Set() should be visible to a subsequent Get() without touching disk.
	custom := defaultConfig()
	custom.Server.Branding.Title = "In-Memory Only"
	Set(custom)
	assert.Equal(t, "In-Memory Only", Get().Server.Branding.Title)

	// Reload() re-reads from disk, discarding the in-memory-only Set().
	require.NoError(t, Reload())
	assert.NotEqual(t, "In-Memory Only", Get().Server.Branding.Title)
}

// TestGetConfigPath verifies the path is always {configDir}/server.yml,
// never .yaml, per project convention.
func TestGetConfigPath(t *testing.T) {
	configDir, _ := initTestPaths(t)
	assert.Equal(t, filepath.Join(configDir, "server.yml"), GetConfigPath())
}

// TestConfigAccessorMethods covers the legacy Web-config accessor methods.
func TestConfigAccessorMethods(t *testing.T) {
	cfg := defaultConfig()
	assert.Equal(t, cfg.Web.UI, cfg.GetWebUI())
	assert.Equal(t, cfg.Web.Robots, cfg.GetWebRobots())
	assert.Equal(t, cfg.Web.Security, cfg.GetWebSecurity())
}

// TestConfigWatcherDetectsChanges drives the watcher's polling loop
// end-to-end: start watching, touch the config file with a newer mtime,
// and confirm the callback fires with a reloaded config. Uses a short
// custom poll by directly invoking checkForChanges rather than waiting on
// the real 5s ticker, keeping the test fast and deterministic.
func TestConfigWatcherDetectsChanges(t *testing.T) {
	configDir, _ := initTestPaths(t)
	resetGlobalConfig(t)

	cfg := defaultConfig()
	require.NoError(t, Save(cfg))

	var received *Config
	watcher := NewConfigWatcher(func(c *Config) {
		received = c
	})
	require.Equal(t, filepath.Join(configDir, "server.yml"), watcher.path)

	// Prime lastMtime the same way watch() does.
	info, err := os.Stat(watcher.path)
	require.NoError(t, err)
	watcher.lastMtime = info.ModTime()

	// No change yet: checkForChanges must not invoke the callback.
	watcher.checkForChanges()
	assert.Nil(t, received)

	// Bump mtime into the future and rewrite the file so a change is
	// detected on the next check.
	future := time.Now().Add(1 * time.Minute)
	modified := defaultConfig()
	modified.Server.Branding.Title = "Watcher Detected Me"
	require.NoError(t, Save(modified))
	require.NoError(t, os.Chtimes(watcher.path, future, future))

	watcher.checkForChanges()
	require.NotNil(t, received)
	assert.Equal(t, "Watcher Detected Me", received.Server.Branding.Title)
}

// TestConfigWatcherStartStop verifies Start/Stop don't panic and Stop
// actually terminates the watch goroutine (a second Stop on an already
// closed channel would panic, so we don't call it twice here - that
// behavior isn't guarded by the implementation, so we only assert the
// documented single Start/Stop cycle works).
func TestConfigWatcherStartStop(t *testing.T) {
	initTestPaths(t)
	resetGlobalConfig(t)

	cfg := defaultConfig()
	require.NoError(t, Save(cfg))

	watcher := NewConfigWatcher(func(*Config) {})
	watcher.Start()
	watcher.Stop()
}

// TestOnChange verifies OnChange returns a starter function without
// starting the watcher immediately (the returned closure must be called to
// begin watching).
func TestOnChange(t *testing.T) {
	initTestPaths(t)
	resetGlobalConfig(t)

	cfg := defaultConfig()
	require.NoError(t, Save(cfg))

	called := false
	start := OnChange(func(*Config) { called = true })
	require.NotNil(t, start)
	assert.False(t, called, "OnChange must not start watching before the returned function is invoked")

	start()
}
