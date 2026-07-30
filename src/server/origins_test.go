package server

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/apimgr/api/src/config"
)

// TestDomainEnvOrigins covers DOMAIN env parsing: comma-separated
// hostnames become https:// origins, whitespace is trimmed, empty entries
// are skipped, and an unset/empty DOMAIN yields no origins.
func TestDomainEnvOrigins(t *testing.T) {
	t.Run("unset returns nil", func(t *testing.T) {
		t.Setenv("DOMAIN", "")
		assert.Nil(t, domainEnvOrigins())
	})

	t.Run("single hostname", func(t *testing.T) {
		t.Setenv("DOMAIN", "example.com")
		assert.Equal(t, []string{"https://example.com"}, domainEnvOrigins())
	})

	t.Run("multiple hostnames trimmed, empties skipped", func(t *testing.T) {
		t.Setenv("DOMAIN", "example.com, api.example.com ,, other.example.com")
		assert.Equal(t, []string{
			"https://example.com",
			"https://api.example.com",
			"https://other.example.com",
		}, domainEnvOrigins())
	})
}

// TestLearnedOrigins covers steps 2+3 of the CORS Allow-list Resolution
// Order: DOMAIN env entries always included, X-Forwarded-Host only learned
// from a trusted peer, and an untrusted peer's forwarded host must never be
// recorded.
func TestLearnedOrigins(t *testing.T) {
	t.Run("DOMAIN env only, no request", func(t *testing.T) {
		resetLearnedOrigins()
		t.Setenv("DOMAIN", "example.com")
		cfg := &config.Config{}
		assert.Equal(t, []string{"https://example.com"}, learnedOrigins(cfg, nil))
	})

	t.Run("trusted proxy X-Forwarded-Host is learned", func(t *testing.T) {
		resetLearnedOrigins()
		t.Setenv("DOMAIN", "")
		cfg := &config.Config{}

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.RemoteAddr = "127.0.0.1:9999"
		req.Header.Set("X-Forwarded-Host", "proxy.example.com")
		req.Header.Set("X-Forwarded-Proto", "https")

		assert.Equal(t, []string{"https://proxy.example.com"}, learnedOrigins(cfg, req))
	})

	t.Run("untrusted proxy X-Forwarded-Host is never learned", func(t *testing.T) {
		resetLearnedOrigins()
		t.Setenv("DOMAIN", "")
		cfg := &config.Config{}

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.RemoteAddr = "203.0.113.9:9999"
		req.Header.Set("X-Forwarded-Host", "evil.example.com")

		assert.Empty(t, learnedOrigins(cfg, req))
	})

	t.Run("learned origin persists across subsequent calls", func(t *testing.T) {
		resetLearnedOrigins()
		t.Setenv("DOMAIN", "")
		cfg := &config.Config{}

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.RemoteAddr = "127.0.0.1:9999"
		req.Header.Set("X-Forwarded-Host", "proxy.example.com")
		learnedOrigins(cfg, req)

		// A second, unrelated request with no forwarded header should still
		// see the previously learned origin.
		plain := httptest.NewRequest(http.MethodGet, "/", nil)
		plain.RemoteAddr = "127.0.0.1:9999"
		assert.Equal(t, []string{"https://proxy.example.com"}, learnedOrigins(cfg, plain))
	})

	t.Run("missing X-Forwarded-Proto defaults to https", func(t *testing.T) {
		resetLearnedOrigins()
		t.Setenv("DOMAIN", "")
		cfg := &config.Config{}

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.RemoteAddr = "127.0.0.1:9999"
		req.Header.Set("X-Forwarded-Host", "proxy.example.com")

		assert.Equal(t, []string{"https://proxy.example.com"}, learnedOrigins(cfg, req))
	})
}

// TestResolveCORSOrigins covers the full 4-step CORS Allow-list Resolution
// Order: explicit config, "" disabling and stopping resolution, wildcard
// short-circuit, DOMAIN-env fallback, trusted-proxy-learned fallback, and
// the final "*" default.
func TestResolveCORSOrigins(t *testing.T) {
	t.Run("explicit config list is used verbatim", func(t *testing.T) {
		resetLearnedOrigins()
		t.Setenv("DOMAIN", "")
		cfg := &config.Config{}
		cfg.Web.CORS.AllowedOrigins = []string{"https://a.com", "https://b.com"}

		origins, disabled := resolveCORSOrigins(cfg, nil)
		assert.False(t, disabled)
		assert.Equal(t, []string{"https://a.com", "https://b.com"}, origins)
	})

	t.Run("single empty-string entry disables CORS and stops resolution", func(t *testing.T) {
		resetLearnedOrigins()
		t.Setenv("DOMAIN", "example.com")
		cfg := &config.Config{}
		cfg.Web.CORS.AllowedOrigins = []string{""}

		origins, disabled := resolveCORSOrigins(cfg, nil)
		assert.True(t, disabled)
		assert.Nil(t, origins)
	})

	t.Run("explicit wildcard short-circuits to pure wildcard mode", func(t *testing.T) {
		resetLearnedOrigins()
		t.Setenv("DOMAIN", "example.com")
		cfg := &config.Config{}
		cfg.Web.CORS.AllowedOrigins = []string{"*"}

		origins, disabled := resolveCORSOrigins(cfg, nil)
		assert.False(t, disabled)
		assert.Equal(t, []string{"*"}, origins)
	})

	t.Run("no explicit config falls back to DOMAIN env entries", func(t *testing.T) {
		resetLearnedOrigins()
		t.Setenv("DOMAIN", "example.com")
		cfg := &config.Config{}

		origins, disabled := resolveCORSOrigins(cfg, nil)
		assert.False(t, disabled)
		assert.Equal(t, []string{"https://example.com"}, origins)
	})

	t.Run("no explicit config, no DOMAIN, falls back to trusted-proxy-learned host", func(t *testing.T) {
		resetLearnedOrigins()
		t.Setenv("DOMAIN", "")
		cfg := &config.Config{}

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.RemoteAddr = "127.0.0.1:9999"
		req.Header.Set("X-Forwarded-Host", "proxy.example.com")
		req.Header.Set("X-Forwarded-Proto", "https")

		origins, disabled := resolveCORSOrigins(cfg, req)
		assert.False(t, disabled)
		assert.Equal(t, []string{"https://proxy.example.com"}, origins)
	})

	t.Run("nothing resolved falls back to * default", func(t *testing.T) {
		resetLearnedOrigins()
		t.Setenv("DOMAIN", "")
		cfg := &config.Config{}

		origins, disabled := resolveCORSOrigins(cfg, nil)
		assert.False(t, disabled)
		assert.Equal(t, []string{"*"}, origins)
	})

	t.Run("explicit config merged with DOMAIN env, deduplicated", func(t *testing.T) {
		resetLearnedOrigins()
		t.Setenv("DOMAIN", "example.com")
		cfg := &config.Config{}
		cfg.Web.CORS.AllowedOrigins = []string{"https://example.com", "https://custom.example.com"}

		origins, disabled := resolveCORSOrigins(cfg, nil)
		assert.False(t, disabled)
		assert.Equal(t, []string{"https://example.com", "https://custom.example.com"}, origins)
	})
}
