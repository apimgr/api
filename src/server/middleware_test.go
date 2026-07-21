package server

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/apimgr/api/src/config"
)

// TestRequestIDMiddleware_GeneratesID covers the case where no
// X-Request-ID is supplied by the client/proxy: a fresh 32-hex-char ID
// must be generated, set on the response header, and made available to
// downstream handlers through the request context.
func TestRequestIDMiddleware_GeneratesID(t *testing.T) {
	var seenInContext string
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenInContext = RequestIDFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	})

	handler := requestIDMiddleware(next)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	headerID := rec.Header().Get("X-Request-ID")
	require.NotEmpty(t, headerID)
	assert.Len(t, headerID, 32, "expected 16 random bytes hex-encoded to 32 chars")
	assert.Equal(t, headerID, seenInContext, "context request ID must match response header")
}

// TestRequestIDMiddleware_PreservesUpstreamID covers the load-balancer/
// proxy case: an incoming X-Request-ID must be reused verbatim, not
// replaced with a freshly generated one.
func TestRequestIDMiddleware_PreservesUpstreamID(t *testing.T) {
	var seenInContext string
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenInContext = RequestIDFromContext(r.Context())
	})

	handler := requestIDMiddleware(next)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Request-ID", "upstream-provided-id")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	assert.Equal(t, "upstream-provided-id", rec.Header().Get("X-Request-ID"))
	assert.Equal(t, "upstream-provided-id", seenInContext)
}

// TestRequestIDMiddleware_UniquePerRequest ensures two requests without an
// incoming ID get distinct generated IDs (guards against a broken/static
// "random" source).
func TestRequestIDMiddleware_UniquePerRequest(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})
	handler := requestIDMiddleware(next)

	rec1 := httptest.NewRecorder()
	handler.ServeHTTP(rec1, httptest.NewRequest(http.MethodGet, "/", nil))

	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, httptest.NewRequest(http.MethodGet, "/", nil))

	id1 := rec1.Header().Get("X-Request-ID")
	id2 := rec2.Header().Get("X-Request-ID")
	assert.NotEqual(t, id1, id2)
}

// newSecurityHeadersConfig builds the minimal config needed to exercise
// securityHeadersMiddleware's SSL-dependent branches.
func newSecurityHeadersConfig(sslEnabled bool) *config.Config {
	cfg := &config.Config{}
	cfg.Server.SSL.Enabled = sslEnabled
	return cfg
}

// TestSecurityHeadersMiddleware_StaticHeaders covers headers that must
// always be present regardless of scheme/SSL state.
func TestSecurityHeadersMiddleware_StaticHeaders(t *testing.T) {
	cfg := newSecurityHeadersConfig(false)
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	handler := securityHeadersMiddleware(cfg)(next)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Host = "example.com"
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	assert.Equal(t, "SAMEORIGIN", rec.Header().Get("X-Frame-Options"))
	assert.Equal(t, "nosniff", rec.Header().Get("X-Content-Type-Options"))
	assert.Equal(t, "1; mode=block", rec.Header().Get("X-XSS-Protection"))
	assert.Equal(t, "strict-origin-when-cross-origin", rec.Header().Get("Referrer-Policy"))
	assert.Equal(t, "none", rec.Header().Get("X-Permitted-Cross-Domain-Policies"))
	assert.Equal(t, "?1", rec.Header().Get("Origin-Agent-Cluster"))
	assert.NotEmpty(t, rec.Header().Get("Permissions-Policy"))
	assert.NotEmpty(t, rec.Header().Get("Reporting-Endpoints"))
	assert.NotEmpty(t, rec.Header().Get("Report-To"))
	assert.NotEmpty(t, rec.Header().Get("NEL"))

	csp := rec.Header().Get("Content-Security-Policy")
	assert.Contains(t, csp, "default-src 'self'")
	assert.Contains(t, csp, "report-uri http://example.com/api/v1/server/reports/csp")
	assert.NotContains(t, csp, "upgrade-insecure-requests")
}

// TestSecurityHeadersMiddleware_SSLDisabled_NoHSTS ensures HSTS is
// omitted entirely when SSL is not enabled — sending it over plain HTTP
// would be actively wrong (browsers would then force HTTPS incorrectly).
func TestSecurityHeadersMiddleware_SSLDisabled_NoHSTS(t *testing.T) {
	cfg := newSecurityHeadersConfig(false)
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})
	handler := securityHeadersMiddleware(cfg)(next)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assert.Empty(t, rec.Header().Get("Strict-Transport-Security"))
}

// TestSecurityHeadersMiddleware_SSLEnabled covers the HSTS and CSP
// upgrade-insecure-requests directive that only appear when SSL is on.
func TestSecurityHeadersMiddleware_SSLEnabled(t *testing.T) {
	cfg := newSecurityHeadersConfig(true)
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})
	handler := securityHeadersMiddleware(cfg)(next)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assert.Equal(t, "max-age=63072000; includeSubDomains; preload", rec.Header().Get("Strict-Transport-Security"))
	assert.Contains(t, rec.Header().Get("Content-Security-Policy"), "upgrade-insecure-requests")
}

// TestSecurityHeadersMiddleware_SchemeDetection covers the http-vs-https
// branch used to build the reports/CSP report-uri: TLS connection state
// and the X-Forwarded-Proto header (reverse-proxy case) must both select
// "https", while an absent/other value defaults to "http".
func TestSecurityHeadersMiddleware_SchemeDetection(t *testing.T) {
	cfg := newSecurityHeadersConfig(false)
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})
	handler := securityHeadersMiddleware(cfg)(next)

	tests := []struct {
		name           string
		forwardedProto string
		wantScheme     string
	}{
		{"no forwarded proto defaults to http", "", "http"},
		{"forwarded https", "https", "https"},
		{"forwarded HTTPS mixed case", "HTTPS", "https"},
		{"forwarded http stays http", "http", "http"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.Host = "example.com"
			if tt.forwardedProto != "" {
				req.Header.Set("X-Forwarded-Proto", tt.forwardedProto)
			}
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			csp := rec.Header().Get("Content-Security-Policy")
			assert.Contains(t, csp, "report-uri "+tt.wantScheme+"://example.com/api/v1/server/reports/csp")
		})
	}
}

// TestSecurityHeadersMiddleware_CallsNext ensures the wrapped handler is
// actually invoked (not swallowed) and its response body/status pass
// through unmodified.
func TestSecurityHeadersMiddleware_CallsNext(t *testing.T) {
	cfg := newSecurityHeadersConfig(false)
	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusTeapot)
		w.Write([]byte("hello"))
	})
	handler := securityHeadersMiddleware(cfg)(next)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assert.True(t, called)
	assert.Equal(t, http.StatusTeapot, rec.Code)
	assert.Equal(t, "hello", rec.Body.String())
}
