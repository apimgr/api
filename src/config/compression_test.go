package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultCompressionConfig(t *testing.T) {
	config := DefaultCompressionConfig()
	assert.True(t, config.Enabled)
	assert.Equal(t, 5, config.Level)
	assert.Equal(t, []string{"text/html", "text/css", "text/javascript", "application/json", "application/xml"}, config.Types)
}

func TestCompressionConfigNormalization(t *testing.T) {
	tests := []struct {
		name         string
		input        CompressionConfig
		wantLevel    int
		wantTypes    []string
		wantWarnings int
	}{
		{
			name:      "valid settings are preserved and MIME values normalized",
			input:     CompressionConfig{Enabled: false, Level: 8, Types: []string{" Text/HTML ", "application/json", "text/html"}},
			wantLevel: 8, wantTypes: []string{"text/html", "application/json"},
		},
		{
			name:      "invalid level and MIME value use defaults",
			input:     CompressionConfig{Enabled: true, Level: 10, Types: []string{"invalid"}},
			wantLevel: 5, wantTypes: DefaultCompressionConfig().Types, wantWarnings: 2,
		},
		{
			name:      "empty MIME list uses defaults",
			input:     CompressionConfig{Enabled: true, Level: 1},
			wantLevel: 1, wantTypes: DefaultCompressionConfig().Types, wantWarnings: 1,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, warnings := tc.input.Normalized()
			assert.Equal(t, tc.input.Enabled, got.Enabled)
			assert.Equal(t, tc.wantLevel, got.Level)
			assert.Equal(t, tc.wantTypes, got.Types)
			assert.Len(t, warnings, tc.wantWarnings)
		})
	}
}

func TestServerConfigIncludesCompressionDefaults(t *testing.T) {
	cfg := DefaultConfig()
	normalized, warnings := cfg.Server.Compression.Normalized()
	require.Empty(t, warnings)
	assert.Equal(t, DefaultCompressionConfig(), normalized)
}
