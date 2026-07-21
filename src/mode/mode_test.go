package mode

import (
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
		{"debug", "debug", Development, false},
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

	require.NoError(t, Set("development"))
	assert.Equal(t, Development, Get())
	assert.True(t, IsDevelopment())
	assert.False(t, IsProduction())

	require.NoError(t, Set("production"))
	assert.Equal(t, Production, Get())
	assert.True(t, IsProduction())
	assert.False(t, IsDevelopment())

	// An invalid mode must not be applied and must return an error.
	err := Set("nonsense")
	require.Error(t, err)
	assert.Equal(t, Production, Get())
}

func TestSetWithDebugAlias(t *testing.T) {
	resetState(t)

	require.NoError(t, SetWithDebugAlias("debug"))
	assert.Equal(t, Development, Get())
	assert.True(t, IsDebugEnabled())

	resetState(t)
	require.NoError(t, SetWithDebugAlias("production"))
	assert.Equal(t, Production, Get())
	assert.False(t, IsDebugEnabled())

	resetState(t)
	err := SetWithDebugAlias("garbage")
	require.Error(t, err)
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
	assert.Equal(t, Development, Get())
}

func TestInitialize_EnvModeUsedWhenNoCLI(t *testing.T) {
	resetState(t)
	t.Setenv("MODE", "development")
	os.Unsetenv("DEBUG")

	require.NoError(t, Initialize("", false, false))
	assert.Equal(t, Development, Get())
}

func TestInitialize_DefaultProductionWhenNothingSet(t *testing.T) {
	resetState(t)
	os.Unsetenv("MODE")
	os.Unsetenv("DEBUG")

	require.NoError(t, Initialize("", false, false))
	assert.Equal(t, Production, Get())
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

func TestInitialize_DebugAliasAppliesWhenNoExplicitDebug(t *testing.T) {
	resetState(t)
	os.Unsetenv("DEBUG")

	require.NoError(t, Initialize("debug", false, false))
	assert.Equal(t, Development, Get())
	assert.True(t, IsDebugEnabled())
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

	require.NoError(t, Set("development"))
	err := assert.AnError
	assert.Equal(t, err.Error(), GetErrorDetail(err))

	require.NoError(t, Set("production"))
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

	require.NoError(t, Set("development"))
	headers := GetCacheHeaders()
	assert.Equal(t, "no-cache, no-store, must-revalidate", headers.CacheControl)
	assert.Equal(t, "no-cache", headers.Pragma)
	assert.Equal(t, "0", headers.Expires)

	require.NoError(t, Set("production"))
	headers = GetCacheHeaders()
	assert.Equal(t, "public, max-age=31536000, immutable", headers.CacheControl)
	assert.Equal(t, "", headers.Pragma)
	assert.Equal(t, "", headers.Expires)
}

func TestGetLogLevel(t *testing.T) {
	resetState(t)

	require.NoError(t, Set("development"))
	assert.Equal(t, "debug", GetLogLevel())

	require.NoError(t, Set("production"))
	assert.Equal(t, "info", GetLogLevel())
}

func TestShouldCacheTemplates(t *testing.T) {
	resetState(t)

	require.NoError(t, Set("production"))
	assert.True(t, ShouldCacheTemplates())

	require.NoError(t, Set("development"))
	assert.False(t, ShouldCacheTemplates())
}

func TestShouldEnableAutoReload(t *testing.T) {
	resetState(t)

	require.NoError(t, Set("development"))
	assert.True(t, ShouldEnableAutoReload())

	require.NoError(t, Set("production"))
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

	require.NoError(t, Set("development"))
	assert.Equal(t, "verbose", GetPanicRecoveryMode())

	require.NoError(t, Set("production"))
	assert.Equal(t, "graceful", GetPanicRecoveryMode())
}

func TestModeString(t *testing.T) {
	assert.Equal(t, "production", Production.String())
	assert.Equal(t, "development", Development.String())
}

func TestModeValidate(t *testing.T) {
	assert.NoError(t, Production.Validate())
	assert.NoError(t, Development.Validate())
	assert.Error(t, Mode("bogus").Validate())
	assert.Error(t, Mode("").Validate())
}
