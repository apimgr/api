package config

import (
	"crypto/rand"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

type failingRandomReader struct{}

func (failingRandomReader) Read([]byte) (int, error) {
	return 0, errors.New("random source unavailable")
}

func TestEnsureFirstRunSecretsFailsWhenMetricsTokenCannotBeGenerated(t *testing.T) {
	previousReader := rand.Reader
	rand.Reader = failingRandomReader{}
	t.Cleanup(func() { rand.Reader = previousReader })

	cfg := defaultConfig()
	cfg.Server.Security.EncryptionKey = "already-configured"
	cfg.Server.Token = "already-configured"

	changed, err := ensureFirstRunSecrets(cfg)
	require.Error(t, err)
	require.False(t, changed)
	require.ErrorContains(t, err, "generate metrics prometheus token")
	require.Empty(t, cfg.Server.Metrics.Auth.Tokens.Prometheus)
}
