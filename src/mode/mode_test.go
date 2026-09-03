package mode

import (
	"errors"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// resetState restores package-level state to a known baseline before and
// after each test, since mode/debug/lang are package-global.
func resetState(t *testing.T) {
	t.Helper()
	mu.Lock()
	currentMode = Production
	debugEnabled = false
	currentLang = ""
	mu.Unlock()
	t.Cleanup(func() {
		mu.Lock()
		currentMode = Production
		debugEnabled = false
		currentLang = ""
		mu.Unlock()
	})
}

func TestParseMode(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    Mode
		wantErr bool
	}{
		{"development", "development", Development, false},
		{"dev", "dev", Development, false},
		{"devel", "devel", Development, false},
		{"debug", "debug", Debug, false},
		{"debug uppercase", "DEBUG", Debug, false},
		{"debug has no shortcut", "dbg", "", true},
		{"production", "production", Production, false},
		{"prod", "prod", Production, false},
		{"uppercase", "PRODUCTION", Production, false},
		{"mixed case with spaces", "  Dev  ", Development, false},
		{"invalid", "bogus", "", true},
		{"empty", "", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseMode(tt.input)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestSet(t *testing.T) {
	resetState(t)

	require.NoError(t, SetMode("development"))
	assert.Equal(t, Development, GetCurrentMode())
	assert.True(t, IsDevelopment())
	assert.False(t, IsProduction())

	require.NoError(t, SetMode("production"))
	assert.Equal(t, Production, GetCurrentMode())
	assert.True(t, IsProduction())
	assert.False(t, IsDevelopment())

	// An invalid mode must not be applied and must return an error.
	err := SetMode("nonsense")
	require.Error(t, err)
	assert.Equal(t, Production, GetCurrentMode())
}

func TestSetWithDebugDefault(t *testing.T) {
	resetState(t)

	// Debug mode is its own mode and defaults the debug flag on
	require.NoError(t, SetWithDebugDefault("debug"))
	assert.Equal(t, Debug, GetCurrentMode())
	assert.True(t, IsDebugMode())
	assert.False(t, IsDevelopment())
	assert.True(t, IsDebugEnabled())

	// Development never turns the debug flag on by itself
	resetState(t)
	require.NoError(t, SetWithDebugDefault("development"))
	assert.Equal(t, Development, GetCurrentMode())
	assert.False(t, IsDebugEnabled())

	resetState(t)
	require.NoError(t, SetWithDebugDefault("production"))
	assert.Equal(t, Production, GetCurrentMode())
	assert.False(t, IsDebugEnabled())

	resetState(t)
	err := SetWithDebugDefault("garbage")
	require.Error(t, err)
}

// TestSixOperationalStates walks the six (mode, debug) combinations in
// AI.md PART 6 "Six Operational States" and asserts the behavior each row
// promises: debug endpoints follow the flag only, sanitization is full
// everywhere except debug mode, and log level/panic recovery/cache
// defaults follow the mode.
func TestSixOperationalStates(t *testing.T) {
	tests := []struct {
		name              string
		mode              string
		debug             bool
		wantMode          Mode
		wantEndpoints     bool
		wantSanitization  SanitizationLevel
		wantLogLevel      string
		wantPanicRecovery string
		wantCacheDefault  bool
	}{
		{"production", "production", false, Production, false, SanitizationFull, "info", "graceful", true},
		{"production + debug", "production", true, Production, true, SanitizationFull, "info", "graceful", true},
		{"development", "development", false, Development, false, SanitizationFull, "debug", "verbose", false},
		{"development + debug", "development", true, Development, true, SanitizationFull, "debug", "verbose", false},
		{"debug, endpoints off", "debug", false, Debug, false, SanitizationMinimal, "debug", "verbose", false},
		{"debug + endpoints", "debug", true, Debug, true, SanitizationMinimal, "debug", "verbose", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resetState(t)

			require.NoError(t, SetMode(tt.mode))
			SetDebugEnabled(tt.debug)

			assert.Equal(t, tt.wantMode, GetCurrentMode())
			assert.Equal(t, tt.wantEndpoints, ShouldShowDebugEndpoints())
			assert.Equal(t, tt.wantEndpoints, ShouldEnableProfiling())
			assert.Equal(t, tt.wantSanitization, GetSanitizationLevel())
			assert.Equal(t, tt.wantSanitization == SanitizationMinimal, MayExposeInternals())
			assert.Equal(t, tt.wantLogLevel, GetLogLevel())
			assert.Equal(t, tt.wantPanicRecovery, GetPanicRecoveryMode())
			assert.Equal(t, tt.wantCacheDefault, DefaultCacheEnabled())
			assert.Equal(t, tt.wantCacheDefault, ShouldCacheTemplates(nil))
			assert.Equal(t, tt.wantCacheDefault, ShouldCacheStatic(nil))
		})
	}
}

// TestCredentialsAlwaysRedacted covers the PART 6 rule that keys, tokens,
// passwords, and secrets are redacted in EVERY mode, debug included.
func TestCredentialsAlwaysRedacted(t *testing.T) {
	for _, m := range []string{"production", "development", "debug"} {
		t.Run(m, func(t *testing.T) {
			resetState(t)
			require.NoError(t, SetMode(m))
			SetDebugEnabled(true)

			assert.NotContains(t, Sanitize("password=hunter2"), "hunter2")
			assert.NotContains(t, Sanitize("token: tok_abc123"), "tok_abc123")
			assert.NotContains(t, GetErrorDetail(errors.New("connect failed: password=hunter2")), "hunter2")
		})
	}
}

func TestDebugEnabled(t *testing.T) {
	resetState(t)

	assert.False(t, IsDebugEnabled())
	SetDebugEnabled(true)
	assert.True(t, IsDebugEnabled())
	SetDebugEnabled(false)
	assert.False(t, IsDebugEnabled())
}

func TestLang(t *testing.T) {
	resetState(t)

	assert.Equal(t, "", GetLang())
	SetLang("en")
	assert.Equal(t, "en", GetLang())
	SetLang("fr")
	assert.Equal(t, "fr", GetLang())
}

func TestInitialize_CLIModeWins(t *testing.T) {
	resetState(t)
	os.Unsetenv("MODE")
	os.Unsetenv("DEBUG")

	require.NoError(t, Initialize("development", false, false))
	assert.Equal(t, Development, GetCurrentMode())
}

func TestInitialize_EnvModeUsedWhenNoCLI(t *testing.T) {
	resetState(t)
	t.Setenv("MODE", "development")
	os.Unsetenv("DEBUG")

	require.NoError(t, Initialize("", false, false))
	assert.Equal(t, Development, GetCurrentMode())
}

func TestInitialize_DefaultProductionWhenNothingSet(t *testing.T) {
	resetState(t)
	os.Unsetenv("MODE")
	os.Unsetenv("DEBUG")

	require.NoError(t, Initialize("", false, false))
	assert.Equal(t, Production, GetCurrentMode())
	assert.False(t, IsDebugEnabled())
}

func TestInitialize_CLIDebugWinsOverEnv(t *testing.T) {
	resetState(t)
	os.Unsetenv("MODE")
	t.Setenv("DEBUG", "true")

	// cliDebugSet=true with cliDebug=false must win over DEBUG=true env var.
	require.NoError(t, Initialize("", false, true))
	assert.False(t, IsDebugEnabled())
}

func TestInitialize_EnvDebugUsedWhenCLINotSet(t *testing.T) {
	resetState(t)
	os.Unsetenv("MODE")
	t.Setenv("DEBUG", "true")

	require.NoError(t, Initialize("", false, false))
	assert.True(t, IsDebugEnabled())
}

func TestInitialize_DebugModeDefaultsDebugOnWhenNoExplicitDebug(t *testing.T) {
	resetState(t)
	os.Unsetenv("DEBUG")

	require.NoError(t, Initialize("debug", false, false))
	assert.Equal(t, Debug, GetCurrentMode())
	assert.True(t, IsDebugEnabled())
}

// TestInitialize_DebugModeWithFalsyDebugEnv covers the PART 6 precedence
// case: selecting debug mode while explicitly setting DEBUG to a falsy
// value runs debug mode with the /debug/* endpoints off.
func TestInitialize_DebugModeWithFalsyDebugEnv(t *testing.T) {
	resetState(t)
	t.Setenv("MODE", "debug")
	t.Setenv("DEBUG", "false")

	require.NoError(t, Initialize("", false, false))
	assert.Equal(t, Debug, GetCurrentMode())
	assert.True(t, IsDebugMode())
	assert.False(t, IsDebugEnabled())
	assert.False(t, ShouldShowDebugEndpoints())
	// Debug mode still relaxes sanitization even with the endpoints off
	assert.Equal(t, SanitizationMinimal, GetSanitizationLevel())
}

// TestInitialize_DebugModeWithFalsyDebugFlag is the CLI-flag twin of the
// env-var case: an explicit --debug=false wins over debug mode's default.
func TestInitialize_DebugModeWithFalsyDebugFlag(t *testing.T) {
	resetState(t)
	os.Unsetenv("MODE")
	os.Unsetenv("DEBUG")

	require.NoError(t, Initialize("debug", false, true))
	assert.Equal(t, Debug, GetCurrentMode())
	assert.False(t, IsDebugEnabled())
}

// TestInitialize_DebugModeIsNeverImplied guards the explicit-opt-in rule:
// nothing but an explicit debug mode selection may select it.
func TestInitialize_DebugModeIsNeverImplied(t *testing.T) {
	resetState(t)
	os.Unsetenv("MODE")
	t.Setenv("DEBUG", "true")

	require.NoError(t, Initialize("development", false, false))
	assert.Equal(t, Development, GetCurrentMode())
	assert.False(t, IsDebugMode())
	assert.True(t, IsDebugEnabled())
	assert.Equal(t, SanitizationFull, GetSanitizationLevel())
}

func TestInitialize_InvalidCLIModeReturnsError(t *testing.T) {
	resetState(t)

	err := Initialize("bogus-mode", false, false)
	require.Error(t, err)
}

func TestInitialize_InvalidEnvModeReturnsError(t *testing.T) {
	resetState(t)
	t.Setenv("MODE", "bogus-mode")

	err := Initialize("", false, false)
	require.Error(t, err)
}

func TestGetErrorDetail(t *testing.T) {
	resetState(t)

	assert.Equal(t, "", GetErrorDetail(nil))

	require.NoError(t, SetMode("development"))
	err := assert.AnError
	assert.Equal(t, err.Error(), GetErrorDetail(err))

	require.NoError(t, SetMode("production"))
	assert.Equal(t, "An internal error occurred. Please contact support if the problem persists.", GetErrorDetail(err))
}

func TestShouldShowDebugEndpoints(t *testing.T) {
	resetState(t)

	assert.False(t, ShouldShowDebugEndpoints())
	SetDebugEnabled(true)
	assert.True(t, ShouldShowDebugEndpoints())
}

func TestGetCacheHeaders(t *testing.T) {
	resetState(t)

	require.NoError(t, SetMode("development"))
	headers := GetCacheHeaders(nil)
	assert.Equal(t, "no-cache, no-store, must-revalidate", headers.CacheControl)
	assert.Equal(t, "no-cache", headers.Pragma)
	assert.Equal(t, "0", headers.Expires)

	require.NoError(t, SetMode("production"))
	headers = GetCacheHeaders(nil)
	assert.Equal(t, "public, max-age=31536000, immutable", headers.CacheControl)
	assert.Equal(t, "", headers.Pragma)
	assert.Equal(t, "", headers.Expires)

	// Config wins over the mode default in both directions
	enabled, disabled := true, false
	require.NoError(t, SetMode("development"))
	assert.Equal(t, "public, max-age=31536000, immutable", GetCacheHeaders(&enabled).CacheControl)
	require.NoError(t, SetMode("production"))
	assert.Equal(t, "no-cache, no-store, must-revalidate", GetCacheHeaders(&disabled).CacheControl)
}

func TestGetLogLevel(t *testing.T) {
	resetState(t)

	require.NoError(t, SetMode("development"))
	assert.Equal(t, "debug", GetLogLevel())

	require.NoError(t, SetMode("production"))
	assert.Equal(t, "info", GetLogLevel())
}

// TestShouldCacheTemplates checks the config-driven rule: an explicit
// server.cache.templates value is honored in EVERY mode, and the mode only
// supplies the default when the key is unset.
func TestShouldCacheTemplates(t *testing.T) {
	enabled, disabled := true, false

	tests := []struct {
		name       string
		mode       string
		configured *bool
		want       bool
	}{
		{"production default", "production", nil, true},
		{"development default", "development", nil, false},
		{"debug default", "debug", nil, false},
		{"production explicitly off", "production", &disabled, false},
		{"development explicitly on", "development", &enabled, true},
		{"debug explicitly on", "debug", &enabled, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resetState(t)
			require.NoError(t, SetMode(tt.mode))
			assert.Equal(t, tt.want, ShouldCacheTemplates(tt.configured))
			assert.Equal(t, tt.want, ShouldCacheStatic(tt.configured))
		})
	}
}

func TestShouldEnableAutoReload(t *testing.T) {
	resetState(t)

	require.NoError(t, SetMode("development"))
	assert.True(t, ShouldEnableAutoReload())

	require.NoError(t, SetMode("production"))
	assert.False(t, ShouldEnableAutoReload())
}

func TestShouldEnableProfiling(t *testing.T) {
	resetState(t)

	assert.False(t, ShouldEnableProfiling())
	SetDebugEnabled(true)
	assert.True(t, ShouldEnableProfiling())
}

func TestGetPanicRecoveryMode(t *testing.T) {
	resetState(t)

	require.NoError(t, SetMode("development"))
	assert.Equal(t, "verbose", GetPanicRecoveryMode())

	require.NoError(t, SetMode("production"))
	assert.Equal(t, "graceful", GetPanicRecoveryMode())
}

func TestModeString(t *testing.T) {
	assert.Equal(t, "production", Production.String())
	assert.Equal(t, "development", Development.String())
	assert.Equal(t, "debug", Debug.String())
}

func TestGetModeString(t *testing.T) {
	resetState(t)

	require.NoError(t, SetMode("production"))
	assert.Equal(t, "production", GetModeString())

	SetDebugEnabled(true)
	assert.Equal(t, "production [debugging]", GetModeString())

	require.NoError(t, SetMode("debug"))
	assert.Equal(t, "debug [debugging]", GetModeString())

	SetDebugEnabled(false)
	assert.Equal(t, "debug", GetModeString())
}

func TestModeValidate(t *testing.T) {
	assert.NoError(t, Production.Validate())
	assert.NoError(t, Development.Validate())
	assert.NoError(t, Debug.Validate())
	assert.Error(t, Mode("bogus").Validate())
	assert.Error(t, Mode("").Validate())
}
