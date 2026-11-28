package server

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/apimgr/api/src/config"
	"github.com/google/uuid"
)

// TestRequestIDMiddleware_GeneratesID covers the case where no
// X-Request-ID is supplied by the client/proxy: a fresh UUIDv4
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
	parsed, err := uuid.Parse(headerID)
	require.NoError(t, err)
	assert.Equal(t, uuid.Version(4), parsed.Version())
	assert.Equal(t, uuid.RFC4122, parsed.Variant())
	assert.Equal(t, headerID, seenInContext, "context request ID must match response header")
	assert.Equal(t, headerID, req.Header.Get("X-Request-ID"), "downstream request header must carry the canonical ID")
}

// TestRequestIDMiddleware_ValidatesUpstreamID covers the proxy case: a
// valid UUIDv4 is reused while arbitrary text is replaced.
func TestRequestIDMiddleware_ValidatesUpstreamID(t *testing.T) {
	var seenInContext string
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenInContext = RequestIDFromContext(r.Context())
	})

	handler := requestIDMiddleware(next)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	upstreamID := "550e8400-e29b-41d4-a716-446655440000"
	req.Header.Set("X-Request-ID", upstreamID)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	assert.Equal(t, upstreamID, rec.Header().Get("X-Request-ID"))
	assert.Equal(t, upstreamID, seenInContext)

	req = httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Request-ID", "upstream-provided-id")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	parsed, err := uuid.Parse(rec.Header().Get("X-Request-ID"))
	require.NoError(t, err)
	assert.Equal(t, uuid.Version(4), parsed.Version())
	assert.NotEqual(t, "upstream-provided-id", seenInContext)
}

func TestRequestIDMiddlewareUsesCorrelationAndTraceFallback(t *testing.T) {
	for _, header := range []string{"X-Correlation-ID", "X-Trace-ID"} {
		t.Run(header, func(t *testing.T) {
			requestID := "550e8400-e29b-41d4-a716-446655440000"
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.Header.Set(header, requestID)
			rec := httptest.NewRecorder()
			requestIDMiddleware(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
				assert.Equal(t, requestID, RequestIDFromContext(r.Context()))
			})).ServeHTTP(rec, req)
			assert.Equal(t, requestID, rec.Header().Get("X-Request-ID"))
		})
	}
}

func TestRequestIDMiddlewareInvalidPrimaryDoesNotUseLowerPriorityID(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Request-ID", "invalid")
	req.Header.Set("X-Correlation-ID", "550e8400-e29b-41d4-a716-446655440000")
	rec := httptest.NewRecorder()
	requestIDMiddleware(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		assert.NotEqual(t, "550e8400-e29b-41d4-a716-446655440000", RequestIDFromContext(r.Context()))
	})).ServeHTTP(rec, req)
	parsed, err := uuid.Parse(rec.Header().Get("X-Request-ID"))
	require.NoError(t, err)
	assert.Equal(t, uuid.Version(4), parsed.Version())
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

// newSecurityHeadersConfig builds a production-default config (so CSP,
// HSTS, Permissions-Policy, and other Web.* header settings are populated
// as they would be at runtime) and overrides only the SSL flag under test.
func newSecurityHeadersConfig(sslEnabled bool) *config.ServerConfig {
	cfg := config.DefaultConfig()
	cfg.Server.SSL.Enabled = sslEnabled
	cfg.Server.Port = ""
	return cfg
}

// TestSecurityHeadersMiddleware_StaticHeaders covers headers that must
// always be present regardless of scheme/SSL state.
func TestSecurityHeadersMiddleware_StaticHeaders(t *testing.T) {
	cfg := newSecurityHeadersConfig(false)
	cfg.Server.APIVersion = "v7"
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
	permissionsPolicy := rec.Header().Get("Permissions-Policy")
	assert.NotEmpty(t, permissionsPolicy)
	assert.Contains(t, permissionsPolicy, "camera=()")
	assert.Contains(t, permissionsPolicy, "autoplay=(self)")
	assert.Contains(t, permissionsPolicy, "attribution-reporting=()")
	assert.NotContains(t, permissionsPolicy, "ambient-light-sensor")
	assert.NotContains(t, permissionsPolicy, "battery=")
	assert.NotContains(t, permissionsPolicy, "interest-cohort")
	assert.NotContains(t, permissionsPolicy, "web-share")
	assert.Equal(t, `default="http://example.com/api/v7/server/reports/default", nel="http://example.com/api/v7/server/reports/nel"`, rec.Header().Get("Reporting-Endpoints"))
	assert.Equal(t, `{"group":"default","max_age":10886400,"endpoints":[{"url":"http://example.com/api/v7/server/reports/default"}]}, {"group":"nel","max_age":10886400,"endpoints":[{"url":"http://example.com/api/v7/server/reports/nel"}]}`, rec.Header().Get("Report-To"))
	assert.Contains(t, rec.Header().Get("NEL"), `"report_to":"nel"`)
	assert.Contains(t, rec.Header().Get("NEL"), `"max_age":2592000`)

	csp := rec.Header().Get("Content-Security-Policy")
	assert.Contains(t, csp, "default-src 'self'")
	assert.Contains(t, csp, "report-uri http://example.com/api/v7/server/reports/csp")
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

// TestSecurityHeadersMiddleware_SchemeDetection verifies TLS and trusted
// proxy headers select HTTPS while untrusted forwarded headers are ignored.
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
			req.RemoteAddr = "127.0.0.1:1234"
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
		_, _ = w.Write([]byte("hello"))
	})
	handler := securityHeadersMiddleware(cfg)(next)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assert.True(t, called)
	assert.Equal(t, http.StatusTeapot, rec.Code)
	assert.Equal(t, "hello", rec.Body.String())
}

// TestSecurityHeadersMiddleware_CSPLearnedOrigins covers AI.md PART 11 →
// "Content Security Policy" → "Auto-detection": connect-src, frame-
// ancestors, and form-action must automatically pick up DOMAIN env entries
// and reverse-proxy-detected hosts, using the same resolution order as the
// CORS allow-list (PART 16).
func TestSecurityHeadersMiddleware_CSPLearnedOrigins(t *testing.T) {
	resetLearnedOrigins()
	t.Setenv("DOMAIN", "app.example.com")
	cfg := newSecurityHeadersConfig(false)
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})
	handler := securityHeadersMiddleware(cfg)(next)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Host = "example.com"
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	csp := rec.Header().Get("Content-Security-Policy")
	assert.Contains(t, csp, "connect-src 'self' https://app.example.com")
	assert.Contains(t, csp, "frame-ancestors 'self' https://app.example.com")
	assert.Contains(t, csp, "form-action 'self' https://app.example.com")
}

// TestSecurityHeadersMiddleware_CSPNoLearnedOrigins ensures the directives
// fall back to a bare 'self' when nothing has been learned (no DOMAIN env,
// no trusted-proxy-forwarded host).
func TestSecurityHeadersMiddleware_CSPNoLearnedOrigins(t *testing.T) {
	resetLearnedOrigins()
	t.Setenv("DOMAIN", "")
	cfg := newSecurityHeadersConfig(false)
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})
	handler := securityHeadersMiddleware(cfg)(next)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Host = "example.com"
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	csp := rec.Header().Get("Content-Security-Policy")
	assert.Contains(t, csp, "connect-src 'self'")
	assert.NotContains(t, csp, "connect-src 'self' https://")
	assert.Contains(t, csp, "frame-ancestors 'self'")
	assert.NotContains(t, csp, "frame-ancestors 'self' https://")
}

// newSecFetchConfig builds a production-default config with
// SecFetchValidation forced to the given state, so tests don't depend on
// whatever config.DefaultConfig() happens to default it to.
func newSecFetchConfig(enabled bool) *config.ServerConfig {
	cfg := config.DefaultConfig()
	cfg.Web.Headers.SecFetchValidation = enabled
	return cfg
}

// TestSecFetchValidationMiddleware_Disabled ensures the middleware is a
// pure pass-through when cfg.Web.Headers.SecFetchValidation is off.
func TestSecFetchValidationMiddleware_Disabled(t *testing.T) {
	cfg := newSecFetchConfig(false)
	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true })
	handler := secFetchValidationMiddleware(cfg)(next)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/text/hash", nil)
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assert.True(t, called)
	assert.Equal(t, http.StatusOK, rec.Code)
}

// TestSecFetchValidationMiddleware_AbsentHeadersPassThrough covers the
// legacy-browser fallback: no Sec-Fetch-* headers at all must never be
// rejected, on any method.
func TestSecFetchValidationMiddleware_AbsentHeadersPassThrough(t *testing.T) {
	cfg := newSecFetchConfig(true)
	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true })
	handler := secFetchValidationMiddleware(cfg)(next)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/text/hash", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assert.True(t, called)
	assert.Equal(t, http.StatusOK, rec.Code)
}

// TestSecFetchValidationMiddleware_CrossSiteRejected covers the
// Sec-Fetch-Site rule: a cross-site state-changing request with no Bearer
// token must be rejected with 403.
func TestSecFetchValidationMiddleware_CrossSiteFrontendRejected(t *testing.T) {
	cfg := newSecFetchConfig(true)
	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true })
	handler := secFetchValidationMiddleware(cfg)(next)

	req := httptest.NewRequest(http.MethodPost, "/server/preferences", nil)
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assert.False(t, called)
	assert.Equal(t, http.StatusForbidden, rec.Code)
	assert.Contains(t, rec.Body.String(), "SEC_FETCH_REJECTED")
}

// TestSecFetchValidationMiddleware_CrossSiteBearerBypasses covers the
// Bearer-token exemption: a cross-site state-changer that carries a Bearer
// credential must NOT be rejected by the Sec-Fetch-Site rule.
func TestSecFetchValidationMiddleware_CrossSiteBearerBypasses(t *testing.T) {
	cfg := newSecFetchConfig(true)
	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true })
	handler := secFetchValidationMiddleware(cfg)(next)

	req := httptest.NewRequest(http.MethodPost, "/server/preferences", nil)
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	req.Header.Set("Authorization", "Bearer sometoken")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assert.True(t, called)
	assert.Equal(t, http.StatusOK, rec.Code)
}

// API and compatibility routes bypass frontend Sec-Fetch validation.
func TestSecFetchValidationMiddleware_APIRoutesAreNotValidated(t *testing.T) {
	cfg := newSecFetchConfig(true)
	for _, route := range []string{"/api/v1/text/hash", "/mcp"} {
		t.Run(route, func(t *testing.T) {
			called := false
			next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true })
			handler := secFetchValidationMiddleware(cfg)(next)

			req := httptest.NewRequest(http.MethodPost, route, nil)
			req.Header.Set("Sec-Fetch-Site", "cross-site")
			req.Header.Set("Sec-Fetch-Mode", "navigate")
			req.Header.Set("Sec-Fetch-Dest", "iframe")
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			assert.True(t, called)
			assert.Equal(t, http.StatusOK, rec.Code)
		})
	}
}

// GET/HEAD API navigations also pass through because the API surface is
// outside frontend Sec-Fetch validation.
func TestSecFetchValidationMiddleware_GetNavigateOnAPIAllowed(t *testing.T) {
	cfg := newSecFetchConfig(true)
	for _, method := range []string{http.MethodGet, http.MethodHead} {
		t.Run(method, func(t *testing.T) {
			called := false
			next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true })
			handler := secFetchValidationMiddleware(cfg)(next)

			req := httptest.NewRequest(method, "/api/v1/text/hash", nil)
			req.Header.Set("Sec-Fetch-Mode", "navigate")
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			assert.True(t, called)
			assert.Equal(t, http.StatusOK, rec.Code)
		})
	}
}

// TestSecFetchValidationMiddleware_CrossSiteIframeRejected covers the
// Sec-Fetch-Dest rule when no allowed frame ancestor is identified.
func TestSecFetchValidationMiddleware_CrossSiteIframeRejected(t *testing.T) {
	cfg := newSecFetchConfig(true)
	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true })
	handler := secFetchValidationMiddleware(cfg)(next)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Sec-Fetch-Dest", "iframe")
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assert.False(t, called)
	assert.Equal(t, http.StatusForbidden, rec.Code)
}

// TestSecFetchValidationMiddleware_SameOriginIframeAllowed ensures the
// request's own origin is accepted by the Sec-Fetch-Dest rule.
func TestSecFetchValidationMiddleware_SameOriginIframeAllowed(t *testing.T) {
	cfg := newSecFetchConfig(true)
	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true })
	handler := secFetchValidationMiddleware(cfg)(next)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Sec-Fetch-Dest", "iframe")
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assert.True(t, called)
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestSecFetchValidationMiddleware_LearnedFrameAncestorAllowed(t *testing.T) {
	t.Setenv("DOMAIN", "parent.example.com")
	cfg := newSecFetchConfig(true)
	called := false
	handler := secFetchValidationMiddleware(cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true }))
	req := httptest.NewRequest(http.MethodGet, "http://app.example.test/", nil)
	req.Header.Set("Sec-Fetch-Dest", "iframe")
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	req.Header.Set("Referer", "https://parent.example.com/page")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assert.True(t, called)
	assert.Equal(t, http.StatusOK, rec.Code)
}

// Sec-Fetch-User is scoped to sensitive operator-token browser flows. This
// API project has no such frontend route, and owner-token navigation is not
// an operator flow.
func TestSecFetchValidationMiddleware_OwnerTokenNavigationIgnoresFetchUser(t *testing.T) {
	cfg := newSecFetchConfig(true)
	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true })
	handler := secFetchValidationMiddleware(cfg)(next)

	req := httptest.NewRequest(http.MethodPost, "/server/preferences/tokens", nil)
	req.Header.Set("Authorization", "Bearer sometoken")
	req.Header.Set("Sec-Fetch-Mode", "navigate")
	req.Header.Set("Sec-Fetch-User", "?0")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assert.True(t, called)
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestSecFetchValidationMiddleware_CrossSiteExemptPathPasses(t *testing.T) {
	cfg := newSecFetchConfig(true)
	cfg.Server.CSRF.ExemptPaths = []string{"/server/hooks/*"}
	called := false
	handler := secFetchValidationMiddleware(cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true }))
	req := httptest.NewRequest(http.MethodPost, "/server/hooks/provider", nil)
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assert.True(t, called)
	assert.Equal(t, http.StatusOK, rec.Code)
}
