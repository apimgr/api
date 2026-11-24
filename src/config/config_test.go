package config

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

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

func TestI2PConfigNormalization(t *testing.T) {
	cfg, warnings := (I2PConfig{SAMAddress: "", VirtualPort: 0, InboundLength: 8, OutboundLength: -1, InboundQuantity: 0, OutboundQuantity: 17, BootstrapTimeout: 29}).Normalized()
	assert.Equal(t, "127.0.0.1:7656", cfg.SAMAddress)
	assert.Equal(t, 80, cfg.VirtualPort)
	assert.Equal(t, 3, cfg.InboundLength)
	assert.Equal(t, 3, cfg.OutboundLength)
	assert.Equal(t, 5, cfg.InboundQuantity)
	assert.Equal(t, 5, cfg.OutboundQuantity)
	assert.Equal(t, 300, cfg.BootstrapTimeout)
	assert.Len(t, warnings, 6)
}

func TestI2PEnabledEnvironmentOverride(t *testing.T) {
	t.Setenv("I2P_ENABLED", "false")
	cfg := &Config{}
	cfg.Server.I2P.Enabled = true
	applyI2PEnvOverrides(cfg)
	assert.False(t, cfg.Server.I2P.Enabled)
	t.Setenv("I2P_ENABLED", "true")
	applyI2PEnvOverrides(cfg)
	assert.True(t, cfg.Server.I2P.Enabled)
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
	assert.Equal(t, "https://services.nvd.nist.gov/rest/json/cves/2.0", cfg.Data["cve"].(map[string]interface{})["source"])
	assert.Equal(t, false, cfg.Data["cve"].(map[string]interface{})["filter_by_cpe"])
	assert.Equal(t, false, cfg.Data["trivy"].(map[string]interface{})["enabled"])

	assert.Equal(t, "0.0.0.0", cfg.Server.Address)
	assert.Equal(t, "production", cfg.Server.Mode)
	assert.Equal(t, "v1", cfg.Server.APIVersion)
	assert.False(t, cfg.Server.SSL.Enabled)
	assert.Equal(t, RateLimitClassConfig{Requests: 10, Window: 900}, cfg.Server.RateLimit.Strict)
	assert.Equal(t, RateLimitClassConfig{Requests: 5, Window: 900}, cfg.Server.RateLimit.AuthFailures)
	assert.Equal(t, "sqlite", cfg.Server.Database.Driver)
	assert.Equal(t, DatabaseCacheConfig{
		Enabled: false, Path: filepath.Join(paths.DataDir(), "db", "cache.db"),
		SyncInterval: "30s", OfflineMode: true, MaxAge: "1h",
	}, cfg.Server.Database.Cache)
	assert.Equal(t, "dark", cfg.Web.UI.Theme)
	assert.Equal(t, []string{"*"}, cfg.Web.CORS.AllowedOrigins)
	assert.NotEmpty(t, cfg.Server.FQDN)
	assert.Equal(t, "en", cfg.Server.I18N.DefaultLanguage)
	assert.Equal(t, []string{"en", "es", "zh", "fr", "ar", "de", "it", "ja"}, cfg.Server.I18N.AvailableLanguages)
	assert.Equal(t, ServerTokenConfig{MaxLinked: 25, CookieMaxAge: "1y", IdlePurgeDays: 0}, cfg.Server.Tokens)
	assert.Equal(t, DefaultRestoreConfig(), cfg.Server.Restore)
	assert.Equal(t, DefaultServerLimitsConfig(), cfg.Server.Limits)
	assert.Equal(t, DefaultDiskLowConfig(), cfg.Server.Notifications.DiskLow)
	assert.Equal(t, DefaultDatabaseErrorConfig(), cfg.Server.Notifications.DatabaseError)
	assert.Equal(t, DebugConfig{
		Pprof:                true,
		LogQueries:           true,
		LogCache:             true,
		LogBodies:            false,
		MaxBodyLogSize:       "10KB",
		BlockProfileRate:     1,
		MutexProfileFraction: 1,
		RuntimeEndpoints:     true,
	}, cfg.Server.Debug)

	port, err := stringToInt(cfg.Server.Port)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, port, 64000)
	assert.LessOrEqual(t, port, 64999)
}

func TestDefaultConfigOmitsAccountAndPermissionsPolicySettings(t *testing.T) {
	cfg := defaultConfig()
	encoded, err := yaml.Marshal(cfg)
	require.NoError(t, err)
	assert.NotContains(t, string(encoded), "users:")
	assert.NotContains(t, string(encoded), "permissions_policy:")
	header := cfg.Web.PermissionsPolicy.Header()
	assert.Contains(t, header, "camera=()")
	assert.Contains(t, header, "autoplay=(self)")
	assert.Contains(t, header, "attribution-reporting=()")
	assert.NotContains(t, header, "ambient-light-sensor")
	assert.NotContains(t, header, "battery=")
	assert.NotContains(t, header, "interest-cohort")
	assert.NotContains(t, header, "web-share")
}

func TestRateLimitConfigNormalization(t *testing.T) {
	got, warnings := (RateLimitConfig{}).Normalized()
	assert.Equal(t, RateLimitClassConfig{Requests: 120, Window: 60}, got.Read)
	assert.Equal(t, RateLimitClassConfig{Requests: 10, Window: 900}, got.Strict)
	assert.Equal(t, RateLimitClassConfig{Requests: 5, Window: 900}, got.AuthFailures)
	assert.Equal(t, 240, got.GlobalBurst)
	assert.Len(t, warnings, 6)
}

func TestServerTokenConfigNormalization(t *testing.T) {
	cfg, warnings := (ServerTokenConfig{
		MaxLinked:     -1,
		CookieMaxAge:  "0s",
		IdlePurgeDays: -1,
	}).Normalized()
	assert.Equal(t, ServerTokenConfig{MaxLinked: 25, CookieMaxAge: "1y", IdlePurgeDays: 0}, cfg)
	assert.Len(t, warnings, 3)

	duration, err := parseTokenCookieMaxAge("1y")
	require.NoError(t, err)
	assert.Equal(t, int64(365*24*time.Hour), int64(duration))
	duration, err = parseTokenCookieMaxAge("24h")
	require.NoError(t, err)
	assert.Equal(t, int64(24*time.Hour), int64(duration))
}

func TestDatabaseCacheConfigNormalization(t *testing.T) {
	initTestPaths(t)
	cfg, warnings := (DatabaseCacheConfig{SyncInterval: "invalid", MaxAge: "0s"}).Normalized()
	assert.Equal(t, filepath.Join(paths.DataDir(), "db", "cache.db"), cfg.Path)
	assert.Equal(t, "30s", cfg.SyncInterval)
	assert.Equal(t, "1h", cfg.MaxAge)
	assert.Len(t, warnings, 2)

	cfg, warnings = (DatabaseCacheConfig{Enabled: true, Path: "/var/lib/api/cache.db", SyncInterval: "10s", OfflineMode: false, MaxAge: "5m"}).Normalized()
	assert.True(t, cfg.Enabled)
	assert.Equal(t, "/var/lib/api/cache.db", cfg.Path)
	assert.Equal(t, "10s", cfg.SyncInterval)
	assert.False(t, cfg.OfflineMode)
	assert.Equal(t, "5m", cfg.MaxAge)
	assert.Empty(t, warnings)
}

func TestDebugConfigNormalization(t *testing.T) {
	cfg, warnings := (DebugConfig{BlockProfileRate: -1, MutexProfileFraction: -2}).Normalized()
	assert.Equal(t, 1, cfg.BlockProfileRate)
	assert.Equal(t, 1, cfg.MutexProfileFraction)
	assert.Len(t, warnings, 2)
}

func TestRestoreConfigNormalizationAndSizeParsing(t *testing.T) {
	cfg, warnings := (RestoreConfig{MaxArchiveSize: "bad", MaxExpandedSize: "8G", MaxFiles: 0, MaxFileSize: "1G", MaxRatio: 0}).Normalized()
	assert.Equal(t, "1G", cfg.MaxArchiveSize)
	assert.Equal(t, "8G", cfg.MaxExpandedSize)
	assert.Equal(t, 10000, cfg.MaxFiles)
	assert.Equal(t, "1G", cfg.MaxFileSize)
	assert.Equal(t, int64(100), cfg.MaxRatio)
	assert.Len(t, warnings, 3)

	for input, want := range map[string]int64{"1G": 1 << 30, "512M": 512 << 20, "2KiB": 2 << 10, "64B": 64} {
		got, err := ParseByteSize(input)
		require.NoError(t, err)
		assert.Equal(t, want, got)
	}
	for _, input := range []string{"", "0", "-1M", "9XB", "999999999999999999999G"} {
		_, err := ParseByteSize(input)
		assert.Error(t, err, input)
	}
}

func TestServerLimitsNormalization(t *testing.T) {
	got, warnings := (ServerLimitsConfig{MaxBodySize: "0", ReadTimeout: "bad", WriteTimeout: "0s", IdleTimeout: "-1m"}).Normalized()
	assert.Equal(t, DefaultServerLimitsConfig(), got)
	assert.Len(t, warnings, 4)
	assert.Equal(t, int64(10<<20), got.MaxBodySizeBytes())
	assert.Equal(t, "security.yml", areaFileForServerKey("limits"))
}

func TestPrivacyDefaultsMatchProjectDataHandling(t *testing.T) {
	privacy := defaultPrivacyConfig()

	assert.False(t, privacy.Retention.ExportAvailable)
	assert.False(t, privacy.Retention.DeletionAvailable)
	assert.Contains(t, privacy.Retention.Period, "not retained as user records")
	assert.Contains(t, privacy.DataCollectionContent(), "not retained as user records")
	assert.Contains(t, privacy.DataUsageContent(), "does not create user accounts")

	privacy.Data.Sold = true
	assert.Contains(t, privacy.DataUsageContent(), "may be shared or sold")
}

func TestI18NConfigKeepsRequiredLanguagesAndAllowsOptionalNarrowing(t *testing.T) {
	cfg, warnings := (I18NConfig{
		DefaultLanguage:    "it",
		AvailableLanguages: []string{"en", "es", "fr", "de", "it", "ar", "ja", "xx"},
	}).Normalized()
	assert.Equal(t, []string{"en", "es", "fr", "ar", "de", "it", "ja"}, cfg.AvailableLanguages)
	assert.Equal(t, "it", cfg.DefaultLanguage)
	assert.Len(t, warnings, 1)

	cfg, warnings = (I18NConfig{AvailableLanguages: []string{"en", "ja"}}).Normalized()
	assert.Equal(t, []string{"en", "es", "fr", "ar", "de", "it", "ja"}, cfg.AvailableLanguages)
	assert.Equal(t, "en", cfg.DefaultLanguage)
	assert.Len(t, warnings, 5)
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
	assert.Contains(t, string(data), "# This node only: listen, identity, database, cache, ssl.")
	assert.FileExists(t, filepath.Join(configDir, "secrets.yml"))
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

// TestLoadInvalidYAMLUsesDefaults verifies malformed YAML is ignored at
// startup when no last-good in-memory copy exists.
func TestLoadInvalidYAMLUsesDefaults(t *testing.T) {
	configDir, _ := initTestPaths(t)
	resetGlobalConfig(t)

	require.NoError(t, os.MkdirAll(configDir, 0755))
	badFile := filepath.Join(configDir, "server.yml")
	require.NoError(t, os.WriteFile(badFile, []byte("server: [unterminated"), 0644))

	cfg, err := Load()
	require.NoError(t, err)
	assert.Equal(t, defaultConfig().Server.Address, cfg.Server.Address)
	data, err := os.ReadFile(badFile)
	require.NoError(t, err)
	assert.NotContains(t, string(data), "unterminated")
}

// TestLoadMigratesLegacyBareStringCORS verifies that an old-format
// server.yml with `web.cors` as a bare string (the pre-migration format)
// is auto-migrated on load into the nested
// `web.cors.allowed_origins: [<value>]` struct form, per AI.md PART 16 →
// "CORS Allow-list Resolution Order".
func TestLoadMigratesLegacyBareStringCORS(t *testing.T) {
	configDir, _ := initTestPaths(t)
	resetGlobalConfig(t)

	require.NoError(t, os.MkdirAll(configDir, 0755))
	legacyFile := filepath.Join(configDir, "server.yml")
	legacyYAML := "web:\n  cors: https://legacy.example.com\n"
	require.NoError(t, os.WriteFile(legacyFile, []byte(legacyYAML), 0644))

	cfg, err := Load()
	require.NoError(t, err)
	assert.Equal(t, []string{"https://legacy.example.com"}, cfg.Web.CORS.AllowedOrigins)
}

func TestWebLLMSDefaultsAndConfig(t *testing.T) {
	configDir, _ := initTestPaths(t)
	resetGlobalConfig(t)
	defaults := defaultConfig().Web.LLMS
	assert.True(t, defaults.IncludeEndpoints)
	assert.False(t, defaults.IncludeSchemas)
	assert.Empty(t, defaults.CustomSections)

	require.NoError(t, os.MkdirAll(configDir, 0700))
	content := "web:\n  llms:\n    include_endpoints: false\n    include_schemas: true\n"
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "server.yml"), []byte(content), 0600))
	cfg, err := Load()
	require.NoError(t, err)
	assert.False(t, cfg.Web.LLMS.IncludeEndpoints)
	assert.True(t, cfg.Web.LLMS.IncludeSchemas)
	assert.Empty(t, cfg.Web.LLMS.CustomSections)
}

// TestCORSConfigUnmarshalYAML_StructForm verifies the current nested-struct
// form still parses correctly (not just the legacy scalar fallback).
func TestCORSConfigUnmarshalYAML_StructForm(t *testing.T) {
	configDir, _ := initTestPaths(t)
	resetGlobalConfig(t)

	require.NoError(t, os.MkdirAll(configDir, 0755))
	structFile := filepath.Join(configDir, "server.yml")
	structYAML := "web:\n  cors:\n    allowed_origins:\n      - https://a.example.com\n      - https://b.example.com\n"
	require.NoError(t, os.WriteFile(structFile, []byte(structYAML), 0644))

	cfg, err := Load()
	require.NoError(t, err)
	assert.Equal(t, []string{"https://a.example.com", "https://b.example.com"}, cfg.Web.CORS.AllowedOrigins)
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

	// Prime the tree snapshot the same way watch() does.
	watcher.lastSnapshot = configTreeSnapshot()

	// No change yet: checkForChanges must not invoke the callback.
	watcher.checkForChanges()
	assert.Nil(t, received)

	// Rewrite the file so the tree snapshot changes.
	modified := defaultConfig()
	modified.Server.Branding.Title = "Watcher Detected Me"
	require.NoError(t, Save(modified))

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

func TestLoadReadsTermsFromBrandingDropIn(t *testing.T) {
	configDir, _ := initTestPaths(t)
	resetGlobalConfig(t)
	require.NoError(t, os.MkdirAll(filepath.Join(configDir, "yaml.d"), 0700))
	require.NoError(t, Save(defaultConfig()))
	branding := `server:
  branding:
    content:
      terms:
        format: md
        body:
          default: "Using {app_name} is governed by Example terms."
          fr: "Les conditions Example s'appliquent."
`
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "yaml.d", "branding.yml"), []byte(branding), 0600))

	loaded, err := Load()
	require.NoError(t, err)
	entry, ok := loaded.Server.Branding.Content["terms"]
	require.True(t, ok)
	assert.Equal(t, "md", entry.Format)
	body, ok := entry.Body.(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "Using {app_name} is governed by Example terms.", body["default"])

	serverYAML, err := os.ReadFile(filepath.Join(configDir, "server.yml"))
	require.NoError(t, err)
	assert.NotContains(t, string(serverYAML), "content:")
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

// TestServerModeRoundTrip verifies every mode value AI.md PART 6 defines —
// including the third mode, debug — survives a save/load round trip
// unchanged. The config layer stores the operator's choice verbatim; the
// mode package owns parsing and the shortcut forms.
func TestServerModeRoundTrip(t *testing.T) {
	tests := []struct {
		name string
		mode string
	}{
		{"production", "production"},
		{"development", "development"},
		{"debug", "debug"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			initTestPaths(t)
			resetGlobalConfig(t)

			cfg := defaultConfig()
			cfg.Server.Mode = tt.mode
			require.NoError(t, Save(cfg))

			loaded, err := Load()
			require.NoError(t, err)
			assert.Equal(t, tt.mode, loaded.Server.Mode)
		})
	}
}

// TestCacheModeKeysDefaultToUnset verifies server.cache.templates and
// server.cache.static ship unset. Nil means "use the current mode's
// default"; any explicit value is honored in every mode, since caching is
// config-driven rather than mode-driven (AI.md PART 6).
func TestCacheModeKeysDefaultToUnset(t *testing.T) {
	initTestPaths(t)

	cfg := defaultConfig()
	require.NotNil(t, cfg)

	assert.Nil(t, cfg.Server.Cache.Templates)
	assert.Nil(t, cfg.Server.Cache.Static)
}

// TestCacheModeKeysRoundTrip verifies the tri-state caching keys survive a
// save/load cycle in all three states, so an operator who turns caching on
// in development (or off in production) keeps that choice across restarts.
func TestCacheModeKeysRoundTrip(t *testing.T) {
	enabled := true
	disabled := false

	tests := []struct {
		name      string
		templates *bool
		static    *bool
	}{
		{"unset uses mode default", nil, nil},
		{"explicitly enabled", &enabled, &enabled},
		{"explicitly disabled", &disabled, &disabled},
		{"mixed", &enabled, &disabled},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			initTestPaths(t)
			resetGlobalConfig(t)

			cfg := defaultConfig()
			cfg.Server.Cache.Templates = tt.templates
			cfg.Server.Cache.Static = tt.static
			require.NoError(t, Save(cfg))

			loaded, err := Load()
			require.NoError(t, err)

			if tt.templates == nil {
				assert.Nil(t, loaded.Server.Cache.Templates)
			} else {
				require.NotNil(t, loaded.Server.Cache.Templates)
				assert.Equal(t, *tt.templates, *loaded.Server.Cache.Templates)
			}

			if tt.static == nil {
				assert.Nil(t, loaded.Server.Cache.Static)
			} else {
				require.NotNil(t, loaded.Server.Cache.Static)
				assert.Equal(t, *tt.static, *loaded.Server.Cache.Static)
			}
		})
	}
}
