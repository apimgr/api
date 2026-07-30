package server

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/apimgr/api/src/config"
)

// newCORSTestHandler wraps corsMiddleware around a trivial 200-OK handler.
func newCORSTestHandler(cfg *config.Config) http.Handler {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	return corsMiddleware(cfg)(next)
}

// TestCORSMiddleware_Wildcard covers the default "*" resolution: a literal
// "*" is written (never the reflected Origin), and credentials are never
// sent per AI.md PART 16 ("Credentials ... are sent only when the resolved
// list is explicit — never with *").
func TestCORSMiddleware_Wildcard(t *testing.T) {
	resetLearnedOrigins()
	t.Setenv("DOMAIN", "")
	cfg := &config.Config{}
	handler := newCORSTestHandler(cfg)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Origin", "https://anywhere.example.com")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	assert.Equal(t, "*", rec.Header().Get("Access-Control-Allow-Origin"))
	assert.Empty(t, rec.Header().Get("Access-Control-Allow-Credentials"))
	assert.Empty(t, rec.Header().Get("Vary"))
}

// TestCORSMiddleware_ExplicitOriginAllowed covers the explicit-list path:
// the matching request Origin is reflected back (not a literal "*"),
// Vary: Origin is set, and credentials ARE allowed since the list is explicit.
func TestCORSMiddleware_ExplicitOriginAllowed(t *testing.T) {
	resetLearnedOrigins()
	t.Setenv("DOMAIN", "")
	cfg := &config.Config{}
	cfg.Web.CORS.AllowedOrigins = []string{"https://allowed.example.com"}
	handler := newCORSTestHandler(cfg)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Origin", "https://allowed.example.com")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	assert.Equal(t, "https://allowed.example.com", rec.Header().Get("Access-Control-Allow-Origin"))
	assert.Equal(t, "true", rec.Header().Get("Access-Control-Allow-Credentials"))
	assert.Equal(t, "Origin", rec.Header().Get("Vary"))
}

// TestCORSMiddleware_ExplicitOriginRejected covers a request Origin that is
// not in the explicit allow-list: no CORS headers should be written.
func TestCORSMiddleware_ExplicitOriginRejected(t *testing.T) {
	resetLearnedOrigins()
	t.Setenv("DOMAIN", "")
	cfg := &config.Config{}
	cfg.Web.CORS.AllowedOrigins = []string{"https://allowed.example.com"}
	handler := newCORSTestHandler(cfg)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Origin", "https://not-allowed.example.com")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	assert.Empty(t, rec.Header().Get("Access-Control-Allow-Origin"))
	assert.Empty(t, rec.Header().Get("Access-Control-Allow-Credentials"))
}

// TestCORSMiddleware_Disabled covers a sole "" entry: CORS is disabled
// entirely and no CORS headers are written, regardless of Origin.
func TestCORSMiddleware_Disabled(t *testing.T) {
	resetLearnedOrigins()
	t.Setenv("DOMAIN", "example.com")
	cfg := &config.Config{}
	cfg.Web.CORS.AllowedOrigins = []string{""}
	handler := newCORSTestHandler(cfg)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Origin", "https://example.com")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	assert.Empty(t, rec.Header().Get("Access-Control-Allow-Origin"))
	assert.Equal(t, http.StatusOK, rec.Code)
}

// TestCORSMiddleware_PreflightAllowed covers an OPTIONS preflight for an
// allowed origin: 204 No Content plus the full preflight header set.
func TestCORSMiddleware_PreflightAllowed(t *testing.T) {
	resetLearnedOrigins()
	t.Setenv("DOMAIN", "")
	cfg := &config.Config{}
	cfg.Web.CORS.AllowedOrigins = []string{"https://allowed.example.com"}
	handler := newCORSTestHandler(cfg)

	req := httptest.NewRequest(http.MethodOptions, "/", nil)
	req.Header.Set("Origin", "https://allowed.example.com")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusNoContent, rec.Code)
	assert.Equal(t, corsAllowedMethods, rec.Header().Get("Access-Control-Allow-Methods"))
	assert.Equal(t, corsAllowedHeaders, rec.Header().Get("Access-Control-Allow-Headers"))
	assert.Equal(t, "86400", rec.Header().Get("Access-Control-Max-Age"))
}

// TestCORSMiddleware_PreflightRejected covers an OPTIONS preflight for a
// disallowed origin: still 204 No Content (no leaking of allow-list
// existence via status code), but no preflight headers.
func TestCORSMiddleware_PreflightRejected(t *testing.T) {
	resetLearnedOrigins()
	t.Setenv("DOMAIN", "")
	cfg := &config.Config{}
	cfg.Web.CORS.AllowedOrigins = []string{"https://allowed.example.com"}
	handler := newCORSTestHandler(cfg)

	req := httptest.NewRequest(http.MethodOptions, "/", nil)
	req.Header.Set("Origin", "https://not-allowed.example.com")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusNoContent, rec.Code)
	assert.Empty(t, rec.Header().Get("Access-Control-Allow-Methods"))
	assert.Empty(t, rec.Header().Get("Access-Control-Allow-Headers"))
}

// TestCORSMiddleware_NonPreflightOptionsWithoutOrigin covers a plain OPTIONS
// request with no Origin header (not a CORS preflight): the request should
// simply pass through to the next handler.
func TestCORSMiddleware_NonPreflightOptionsWithoutOrigin(t *testing.T) {
	resetLearnedOrigins()
	t.Setenv("DOMAIN", "")
	cfg := &config.Config{}
	handler := newCORSTestHandler(cfg)

	req := httptest.NewRequest(http.MethodOptions, "/", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
}
