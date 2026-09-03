package server

import (
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/apimgr/api/src/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// isTrustedPeer must always trust loopback/RFC1918/link-local addresses
// regardless of configuration, reject arbitrary public addresses, honor a
// configured additional allow-list entry, and reject an unparseable addr.
func TestIsTrustedPeer(t *testing.T) {
	tests := []struct {
		name       string
		addr       string
		additional []string
		want       bool
	}{
		{"loopback with port", "127.0.0.1:5555", nil, true},
		{"loopback ipv6", "::1", nil, true},
		{"rfc1918 10/8", "10.1.2.3:80", nil, true},
		{"rfc1918 172.16/12", "172.16.5.5:80", nil, true},
		{"rfc1918 192.168/16", "192.168.1.1:80", nil, true},
		{"link-local", "169.254.1.1:80", nil, true},
		{"public ip not trusted by default", "203.0.113.9:80", nil, false},
		{"public ip trusted via additional exact IP", "203.0.113.9:80", []string{"203.0.113.9"}, true},
		{"public ip trusted via additional CIDR", "203.0.113.9:80", []string{"203.0.113.0/24"}, true},
		{"public ip not covered by unrelated additional", "203.0.113.9:80", []string{"198.51.100.0/24"}, false},
		{"unparseable address", "not-an-ip:80", nil, false},
		{"empty address", "", nil, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{}
			cfg.Server.TrustedProxies.Additional = tt.additional
			assert.Equal(t, tt.want, isTrustedPeer(cfg, tt.addr))
		})
	}
}

// resolveForwardedClientIP must check headers in priority order, extract
// only the first hop of a comma-separated X-Forwarded-For chain, skip a
// header whose value fails to parse as an IP, and return "" when no
// recognized header carries a valid IP.
func TestResolveForwardedClientIP(t *testing.T) {
	t.Run("no headers", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		assert.Equal(t, "", resolveForwardedClientIP(req))
	})

	t.Run("X-Real-IP takes priority over X-Forwarded-For", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("X-Real-IP", "198.51.100.1")
		req.Header.Set("X-Forwarded-For", "203.0.113.2")
		assert.Equal(t, "198.51.100.1", resolveForwardedClientIP(req))
	})

	t.Run("X-Forwarded-For chain uses first hop, trimmed", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("X-Forwarded-For", " 203.0.113.2 , 10.0.0.1, 10.0.0.2")
		assert.Equal(t, "203.0.113.2", resolveForwardedClientIP(req))
	})

	t.Run("invalid value in higher-priority header is skipped", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("X-Real-IP", "not-an-ip")
		req.Header.Set("X-Forwarded-For", "203.0.113.2")
		assert.Equal(t, "203.0.113.2", resolveForwardedClientIP(req))
	})

	t.Run("falls through to CF-Connecting-IP", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("CF-Connecting-IP", "203.0.113.5")
		assert.Equal(t, "203.0.113.5", resolveForwardedClientIP(req))
	})

	t.Run("falls through to True-Client-IP", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("True-Client-IP", "203.0.113.6")
		assert.Equal(t, "203.0.113.6", resolveForwardedClientIP(req))
	})

	t.Run("falls through to X-Client-IP", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("X-Client-IP", "203.0.113.7")
		assert.Equal(t, "203.0.113.7", resolveForwardedClientIP(req))
	})

	t.Run("all headers invalid returns empty", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("X-Real-IP", "garbage")
		req.Header.Set("X-Forwarded-For", "garbage-too")
		assert.Equal(t, "", resolveForwardedClientIP(req))
	})
}

// realIPMiddleware must only rewrite RemoteAddr when the immediate TCP
// peer is trusted, must always preserve the original peer in the request
// context, and must leave RemoteAddr untouched for an untrusted peer even
// when forwarded headers are present.
func TestRealIPMiddleware(t *testing.T) {
	var capturedRemoteAddr string
	var capturedOriginalPeer interface{}

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedRemoteAddr = r.RemoteAddr
		capturedOriginalPeer = r.Context().Value(originalPeerContextKey)
		w.WriteHeader(http.StatusOK)
	})

	t.Run("trusted peer: RemoteAddr rewritten from header", func(t *testing.T) {
		cfg := &config.Config{}
		mw := realIPMiddleware(cfg)(next)

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.RemoteAddr = "127.0.0.1:9999"
		req.Header.Set("X-Real-IP", "203.0.113.42")
		w := httptest.NewRecorder()

		mw.ServeHTTP(w, req)

		assert.Equal(t, "203.0.113.42", capturedRemoteAddr)
		require.NotNil(t, capturedOriginalPeer)
		assert.Equal(t, "127.0.0.1:9999", capturedOriginalPeer)
	})

	t.Run("untrusted peer: RemoteAddr left unmodified", func(t *testing.T) {
		cfg := &config.Config{}
		mw := realIPMiddleware(cfg)(next)

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.RemoteAddr = "203.0.113.1:9999"
		req.Header.Set("X-Real-IP", "198.51.100.1")
		w := httptest.NewRecorder()

		mw.ServeHTTP(w, req)

		assert.Equal(t, "203.0.113.1:9999", capturedRemoteAddr)
		assert.Equal(t, "203.0.113.1:9999", capturedOriginalPeer)
	})

	t.Run("trusted peer with no recognized forwarded header: RemoteAddr unchanged", func(t *testing.T) {
		cfg := &config.Config{}
		mw := realIPMiddleware(cfg)(next)

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.RemoteAddr = "127.0.0.1:9999"
		w := httptest.NewRecorder()

		mw.ServeHTTP(w, req)

		assert.Equal(t, "127.0.0.1:9999", capturedRemoteAddr)
	})
}

// trustedProxyResolver.resolve must accept CIDR entries, bare IP entries,
// and DNS-name entries (resolved to IPs), and trusts() must match against
// all three forms.
func TestTrustedProxyResolverResolveAndTrusts(t *testing.T) {
	t.Run("CIDR and bare IP entries", func(t *testing.T) {
		resolver := &trustedProxyResolver{}
		resolver.resolve([]string{"198.51.100.0/24", "203.0.113.9"})

		assert.True(t, resolver.trusts(mustParseIP(t, "198.51.100.5")))
		assert.True(t, resolver.trusts(mustParseIP(t, "203.0.113.9")))
		assert.False(t, resolver.trusts(mustParseIP(t, "8.8.8.8")))
	})

	t.Run("DNS name entry resolves via loopback hostname", func(t *testing.T) {
		resolver := &trustedProxyResolver{}
		resolver.resolve([]string{"localhost"})

		assert.True(t, resolver.trusts(mustParseIP(t, "127.0.0.1")))
	})

	t.Run("malformed CIDR entry is skipped, does not error", func(t *testing.T) {
		resolver := &trustedProxyResolver{}
		resolver.resolve([]string{"not/a/cidr"})

		assert.False(t, resolver.trusts(mustParseIP(t, "1.2.3.4")))
	})

	t.Run("cached result reused within 5 minutes for identical entries", func(t *testing.T) {
		resolver := &trustedProxyResolver{}
		entries := []string{"203.0.113.50"}
		resolver.resolve(entries)
		first := resolver.lastResolve

		resolver.resolve(entries)
		assert.Equal(t, first, resolver.lastResolve)
	})

	t.Run("changed entries force re-resolve", func(t *testing.T) {
		resolver := &trustedProxyResolver{}
		resolver.resolve([]string{"203.0.113.60"})
		assert.True(t, resolver.trusts(mustParseIP(t, "203.0.113.60")))

		resolver.resolve([]string{"203.0.113.61"})
		assert.False(t, resolver.trusts(mustParseIP(t, "203.0.113.60")))
		assert.True(t, resolver.trusts(mustParseIP(t, "203.0.113.61")))
	})
}

func mustParseIP(t *testing.T, s string) net.IP {
	t.Helper()
	ip := net.ParseIP(s)
	require.NotNil(t, ip)
	return ip
}
