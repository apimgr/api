package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/netip"
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
			cfg := &config.ServerConfig{}
			cfg.Server.TrustedProxies.Additional = tt.additional
			assert.Equal(t, tt.want, isTrustedPeer(cfg, tt.addr))
		})
	}
}

func TestIsTrustedPeerIncludesConfiguredListenAddressSubnet(t *testing.T) {
	cfg := &config.ServerConfig{}
	cfg.Server.Address = "203.0.113.11"
	assert.True(t, isTrustedPeer(cfg, "203.0.113.245:8080"))
	assert.False(t, isTrustedPeer(cfg, "203.0.114.11:8080"))
	cfg.Server.Address = "0.0.0.0"
	assert.False(t, isTrustedPeer(cfg, "198.51.100.21:8080"))
}

// resolveForwardedClientIP must honor configured source order and walk
// trusted proxy chains from right to left.
func TestResolveForwardedClientIP(t *testing.T) {
	cfg := config.DefaultConfig()
	t.Run("no headers", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		assert.Equal(t, "", resolveForwardedClientIP(req, cfg, "10.0.0.1:80"))
	})

	t.Run("automatic order prefers X-Forwarded-For", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("X-Real-IP", "198.51.100.1")
		req.Header.Set("X-Forwarded-For", "203.0.113.2")
		assert.Equal(t, "203.0.113.2", resolveForwardedClientIP(req, cfg, "10.0.0.1:80"))
	})

	t.Run("walks right to left and ignores forged left prefix", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("X-Forwarded-For", "1.2.3.4, 203.0.113.2")
		assert.Equal(t, "203.0.113.2", resolveForwardedClientIP(req, cfg, "10.0.0.1:80"))
	})

	t.Run("RFC Forwarded chain parses quoted IPv6 and ports", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("Forwarded", `for="[2001:DB8::5]:4711", for=10.0.0.2`)
		assert.Equal(t, "2001:db8::5", resolveForwardedClientIP(req, cfg, "10.0.0.1:80"))
	})

	t.Run("trusted chain hop is skipped", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("X-Forwarded-For", "203.0.113.5, 198.51.100.7")
		cfg.Server.TrustedProxies.Additional = []string{"198.51.100.0/24"}
		assert.Equal(t, "203.0.113.5", resolveForwardedClientIP(req, cfg, "10.0.0.1:80"))
		cfg.Server.TrustedProxies.Additional = nil
	})

	t.Run("all trusted chain resolves to leftmost hop", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("X-Forwarded-For", "10.1.1.1, 10.0.0.2")
		assert.Equal(t, "10.1.1.1", resolveForwardedClientIP(req, cfg, "10.0.0.1:80"))
	})

	t.Run("repeated XFF header instances form one chain", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Add("X-Forwarded-For", "203.0.113.9")
		req.Header.Add("X-Forwarded-For", "198.51.100.7")
		cfg.Server.TrustedProxies.Additional = []string{"198.51.100.0/24"}
		assert.Equal(t, "203.0.113.9", resolveForwardedClientIP(req, cfg, "10.0.0.1:80"))
		cfg.Server.TrustedProxies.Additional = nil
	})

	t.Run("invalid hop stops at nearest valid hop to its right", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("X-Forwarded-For", "203.0.113.9, unknown")
		assert.Equal(t, "10.0.0.1", resolveForwardedClientIP(req, cfg, "10.0.0.1:80"))
	})

	t.Run("pinned header ignores other sources", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("X-Forwarded-For", "203.0.113.2")
		req.Header.Set("X-Real-IP", "198.51.100.1")
		cfg.Server.TrustedProxies.ClientIPHeader = "X-Real-IP"
		assert.Equal(t, "198.51.100.1", resolveForwardedClientIP(req, cfg, "10.0.0.1:80"))
		cfg.Server.TrustedProxies.ClientIPHeader = "auto"
	})

	t.Run("invalid single address falls through", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("CF-Connecting-IP", "garbage")
		req.Header.Set("True-Client-IP", "203.0.113.6")
		assert.Equal(t, "203.0.113.6", resolveForwardedClientIP(req, cfg, "10.0.0.1:80"))
	})
}

func TestNormalizeIP(t *testing.T) {
	tests := []struct{ input, want string }{
		{"192.0.2.1", "192.0.2.1"},
		{"192.0.2.1:8080", "192.0.2.1"},
		{"::ffff:192.0.2.1", "192.0.2.1"},
		{"[2001:DB8::1]:443", "2001:db8::1"},
		{"2001:0db8:0000:0000:0000:0000:0000:0001", "2001:db8::1"},
		{"fe80::1%eth0", "fe80::1"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, ok := normalizeIP(tt.input)
			require.True(t, ok)
			assert.Equal(t, tt.want, got.String())
		})
	}
	if _, ok := normalizeIP("not-an-ip"); ok {
		t.Fatal("invalid IP accepted")
	}
}

func TestIsTrustedPeerUnmapsIPv4MappedIPv6(t *testing.T) {
	cfg := config.DefaultConfig()
	assert.True(t, isTrustedPeer(cfg, "[::ffff:127.0.0.1]:8080"))
	cfg.Server.TrustedProxies.Additional = []string{"2001:db8::6/128"}
	assert.True(t, isTrustedPeer(cfg, "[2001:0db8::6]:443"))
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
		cfg := &config.ServerConfig{}
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
		cfg := &config.ServerConfig{}
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
		cfg := &config.ServerConfig{}
		mw := realIPMiddleware(cfg)(next)

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.RemoteAddr = "127.0.0.1:9999"
		w := httptest.NewRecorder()

		mw.ServeHTTP(w, req)

		assert.Equal(t, "127.0.0.1:9999", capturedRemoteAddr)
	})
}

func TestRealIPMiddlewarePreservesTorCircuitIdentity(t *testing.T) {
	cfg := config.DefaultConfig()
	var remote, original string
	next := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		remote = r.RemoteAddr
		original, _ = r.Context().Value(originalPeerContextKey).(string)
		if !IsTorRequest(r) {
			t.Error("expected request to be identified as Tor")
		}
	})
	request := httptest.NewRequest(http.MethodGet, "http://example.onion/", nil)
	request.Header.Set("X-Forwarded-For", "198.51.100.8")
	request = request.WithContext(context.WithValue(request.Context(), torCircuitContextKey, "tor:0123456789abcdef"))
	recorder := httptest.NewRecorder()
	realIPMiddleware(cfg)(next).ServeHTTP(recorder, request)
	assert.Equal(t, "tor:0123456789abcdef", remote)
	assert.Equal(t, "tor:0123456789abcdef", original)
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

func mustParseIP(t *testing.T, s string) netip.Addr {
	t.Helper()
	ip, err := netip.ParseAddr(s)
	require.NoError(t, err)
	return ip
}
