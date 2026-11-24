package server

import (
	"context"
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

func TestGetFQDNUsesFirstDomainOverride(t *testing.T) {
	t.Setenv("DOMAIN", " api.example.com:8443, www.example.com")
	assert.Equal(t, "api.example.com", GetFQDN("api"))
}

func TestGetDisplayURLUsesResolvedDomainAndStripsDefaultPorts(t *testing.T) {
	t.Setenv("DOMAIN", "api.example.com")
	assert.Equal(t, "http://api.example.com", GetDisplayURL("api", 80, false))
	assert.Equal(t, "https://api.example.com", GetDisplayURL("api", 443, true))
	assert.Equal(t, "https://api.example.com:8443", GetDisplayURL("api", 8443, true))
}

func TestIsDevTLD(t *testing.T) {
	for _, host := range []string{"localhost", "api", "dev.api", "example.test", "node.home.arpa", "node.localhost"} {
		assert.True(t, isDevTLD(host, "api"), host)
	}
	assert.False(t, isDevTLD("api.example.com", "api"))
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

func TestBuildURLTrustAndDomainResolution(t *testing.T) {
	t.Run("trusted proxy headers determine origin and default port is stripped", func(t *testing.T) {
		t.Setenv("DOMAIN", "")
		cfg := &config.Config{}
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.RemoteAddr = "127.0.0.1:8080"
		req.Header.Set("X-Forwarded-Host", "public.example:443")
		req.Header.Set("X-Forwarded-Proto", "https")
		req.Header.Set("X-Forwarded-Prefix", "/edge")
		ctx := req.Context()
		ctx = context.WithValue(ctx, serverConfigKey, cfg)
		ctx = context.WithValue(ctx, originalPeerContextKey, req.RemoteAddr)
		req = req.WithContext(ctx)

		assert.Equal(t, "https://public.example/edge/path", BuildURL(req, "path"))
	})

	t.Run("untrusted forwarded headers are ignored", func(t *testing.T) {
		t.Setenv("DOMAIN", "")
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Host = "public.example"
		req.RemoteAddr = "203.0.113.10:8080"
		req.Header.Set("X-Forwarded-Host", "attacker.example")
		req.Header.Set("X-Forwarded-Proto", "https")

		assert.Equal(t, "http://public.example/path", BuildURL(req, "/path"))
	})

	t.Run("host outside DOMAIN uses configured primary domain", func(t *testing.T) {
		t.Setenv("DOMAIN", "primary.example,other.example")
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Host = "attacker.example"

		assert.Equal(t, "http://primary.example/path", BuildURL(req, "/path"))
	})

	t.Run("configured base URL path is applied to absolute links", func(t *testing.T) {
		t.Setenv("DOMAIN", "")
		cfg := &config.Config{}
		cfg.Server.BaseURL = "/configured"
		req := httptest.NewRequest(http.MethodGet, "http://site.example/", nil)
		req = requestWithServerConfig(req, cfg)

		assert.Equal(t, "http://site.example/configured/path", BuildURL(req, "/path"))
	})
}

func TestCSRFCookieSecureTrustsForwardedProto(t *testing.T) {
	t.Setenv("DOMAIN", "")
	cfg := &config.Config{}
	makeRequest := func(peer string) *http.Request {
		req := httptest.NewRequest(http.MethodGet, "http://site.example/", nil)
		req.RemoteAddr = peer
		req.Header.Set("X-Forwarded-Proto", "https")
		ctx := context.WithValue(req.Context(), serverConfigKey, cfg)
		ctx = context.WithValue(ctx, originalPeerContextKey, peer)
		return req.WithContext(ctx)
	}

	assert.False(t, csrfCookieSecure(makeRequest("203.0.113.10:1234"), "auto"))
	assert.True(t, csrfCookieSecure(makeRequest("127.0.0.1:1234"), "auto"))
}
