package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// TestParseBool covers every truthy/falsy alias, case-insensitivity,
// whitespace trimming, the empty-string default, and the invalid-value
// error path.
func TestParseBool(t *testing.T) {
	tests := []struct {
		name       string
		in         string
		defaultVal bool
		want       bool
		wantErr    bool
	}{
		{"canonical true", "true", false, true, false},
		{"canonical false", "false", true, false, false},
		{"numeric 1", "1", false, true, false},
		{"numeric 0", "0", true, false, false},
		{"yes", "yes", false, true, false},
		{"no", "no", true, false, false},
		{"on", "on", false, true, false},
		{"off", "off", true, false, false},
		{"uppercase TRUE", "TRUE", false, true, false},
		{"mixed case YeS", "YeS", false, true, false},
		{"whitespace padded", "  true  ", false, true, false},
		{"enable alias", "enable", false, true, false},
		{"disabled alias", "disabled", true, false, false},
		{"empty uses default true", "", true, true, false},
		{"empty uses default false", "", false, false, false},
		{"invalid value", "maybe", false, false, true},
		{"invalid numeric", "2", false, false, true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseBool(tc.in, tc.defaultVal)
			if tc.wantErr {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

// TestMustParseBool covers the compatibility wrapper's default behavior.
func TestMustParseBool(t *testing.T) {
	assert.True(t, MustParseBool("true", false))
	assert.False(t, MustParseBool("", false))
	assert.True(t, MustParseBool("not-a-bool", true))
	assert.False(t, MustParseBool("not-a-bool", false))
}

func TestParseBoolOrDefault(t *testing.T) {
	assert.True(t, ParseBoolOrDefault("test.enabled", "yes", false))
	assert.False(t, ParseBoolOrDefault("test.disabled", "invalid", false))
	assert.True(t, ParseBoolOrDefault("test.enabled-default", "invalid", true))
}

func TestUnmarshalConfigResilientDefaultsInvalidBooleans(t *testing.T) {
	merged := map[string]interface{}{
		"server": map[string]interface{}{
			"debug":    map[string]interface{}{"pprof": "not-a-bool"},
			"ssl":      map[string]interface{}{"enabled": "perhaps"},
			"database": map[string]interface{}{"pool": map[string]interface{}{"max_open": "not-a-number"}},
		},
	}
	data, err := yaml.Marshal(merged)
	require.NoError(t, err)
	cfg := defaultConfig()
	require.NoError(t, unmarshalConfigResilient(data, cfg))
	assert.True(t, cfg.Server.Debug.Pprof)
	assert.False(t, cfg.Server.SSL.Enabled)
	assert.Equal(t, defaultConfig().Server.Database.Pool.MaxOpen, cfg.Server.Database.Pool.MaxOpen)
}

func TestLoadMergedConfigDefaultsInvalidBooleansWithoutMaskingOtherMalformedValues(t *testing.T) {
	configDir, _ := initTestPaths(t)
	resetGlobalConfig(t)
	contents := "server:\n  debug:\n    pprof: not-a-bool\n  ssl:\n    enabled: perhaps\n  database:\n    pool:\n      max_open: not-a-number\n"
	require.NoError(t, os.MkdirAll(configDir, 0700))
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "server.yml"), []byte(contents), 0600))

	cfg, err := Load(nil)
	require.NoError(t, err)
	assert.Equal(t, defaultConfig().Server.Debug.Pprof, cfg.Server.Debug.Pprof)
	assert.Equal(t, defaultConfig().Server.SSL.Enabled, cfg.Server.SSL.Enabled)
	assert.Equal(t, defaultConfig().Server.Database.Pool.MaxOpen, cfg.Server.Database.Pool.MaxOpen)
}

// TestIsTruthyIsFalsy covers both helpers across truthy, falsy, empty, and
// invalid input - each must return false (no error) for anything outside
// its own set.
func TestIsTruthyIsFalsy(t *testing.T) {
	assert.True(t, IsTruthy("yes"))
	assert.True(t, IsTruthy("  TRUE  "))
	assert.False(t, IsTruthy("no"))
	assert.False(t, IsTruthy(""))
	assert.False(t, IsTruthy("garbage"))

	assert.True(t, IsFalsy("no"))
	assert.True(t, IsFalsy("  FALSE  "))
	assert.False(t, IsFalsy("yes"))
	assert.False(t, IsFalsy(""))
	assert.False(t, IsFalsy("garbage"))
}
