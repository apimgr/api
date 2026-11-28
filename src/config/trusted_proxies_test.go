package config

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestTrustedProxiesClientIPHeaderNormalization(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		want        string
		wantWarning bool
	}{
		{name: "empty defaults to auto", input: "", want: "auto"},
		{name: "auto is case insensitive", input: "AUTO", want: "auto"},
		{name: "supported header is canonicalized", input: "x-forwarded-for", want: "X-Forwarded-For"},
		{name: "unsupported header falls back", input: "X-Forwarded-Host", want: "auto", wantWarning: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, warnings := (TrustedProxiesConfig{ClientIPHeader: test.input}).Normalized()
			assert.Equal(t, test.want, got.ClientIPHeader)
			assert.Equal(t, test.wantWarning, len(warnings) > 0)
		})
	}
}

func TestDefaultConfigTrustedProxyClientIPHeader(t *testing.T) {
	cfg := DefaultConfig()
	assert.Equal(t, "auto", cfg.Server.TrustedProxies.ClientIPHeader)

	loaded, err := cfg.Server.TrustedProxies.Normalized()
	require.Empty(t, err)
	assert.Equal(t, "auto", loaded.ClientIPHeader)
}

func TestTrustedProxiesClientIPHeaderYAMLRoundTrip(t *testing.T) {
	var cfg ServerConfig
	require.NoError(t, yaml.Unmarshal([]byte("server:\n  trusted_proxies:\n    client_ip_header: x-real-ip\n"), &cfg))
	normalized, warnings := cfg.Server.TrustedProxies.Normalized()
	require.Empty(t, warnings)
	assert.Equal(t, "X-Real-IP", normalized.ClientIPHeader)

	encoded, err := yaml.Marshal(TrustedProxiesConfig{Additional: []string{}, ClientIPHeader: normalized.ClientIPHeader})
	require.NoError(t, err)
	assert.True(t, strings.Contains(string(encoded), "client_ip_header: X-Real-IP"))
}
