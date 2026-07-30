package server

import (
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/apimgr/api/src/config"
	"github.com/apimgr/api/src/mode"
)

// withDebugMode temporarily sets the package-global debug-mode flag and
// restores its prior value on test cleanup, to avoid cross-test pollution
// of the shared mode package state.
func withDebugMode(t *testing.T, enabled bool) {
	t.Helper()
	prev := mode.IsDebugEnabled()
	mode.SetDebugEnabled(enabled)
	t.Cleanup(func() {
		mode.SetDebugEnabled(prev)
	})
}

// serverTimingEntryPattern matches one comma-separated Server-Timing entry
// per the AI.md PART 11 spec format: `name;dur=N.N`.
var serverTimingEntryPattern = regexp.MustCompile(`^[a-zA-Z]+;dur=\d+\.\d$`)

func TestServerTimingMiddleware_DisabledInProduction(t *testing.T) {
	withDebugMode(t, false)
	cfg := config.DefaultConfig()

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	handler := serverTimingMiddleware(cfg)(next)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	assert.Empty(t, rec.Header().Get("Server-Timing"), "Server-Timing must never be emitted outside debug mode")
}

func TestServerTimingMiddleware_EmittedInDebugMode(t *testing.T) {
	withDebugMode(t, true)
	cfg := config.DefaultConfig()
	require.True(t, cfg.Web.Headers.ServerTimingInDebugOnly, "default config must keep the operator toggle on so this test exercises the real gate")

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	handler := serverTimingMiddleware(cfg)(next)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	header := rec.Header().Get("Server-Timing")
	require.NotEmpty(t, header, "Server-Timing must be emitted in debug mode")
	assert.Contains(t, header, "total;dur=")
}

func TestServerTimingMiddleware_HeaderFormat(t *testing.T) {
	withDebugMode(t, true)
	cfg := config.DefaultConfig()

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		recordServerTiming(w, "render", 0)
		w.WriteHeader(http.StatusOK)
	})

	handler := serverTimingMiddleware(cfg)(next)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	header := rec.Header().Get("Server-Timing")
	require.NotEmpty(t, header)

	entries := splitServerTimingEntries(header)
	require.Len(t, entries, 2, "expected exactly render and total entries, got %q", header)
	for _, entry := range entries {
		assert.Regexp(t, serverTimingEntryPattern, entry, "each entry must match `name;dur=N.N`")
	}
	assert.Equal(t, "render", entriesName(entries[0]))
	assert.Equal(t, "total", entriesName(entries[1]))
}

func TestServerTimingMiddleware_OperatorToggleSuppressesInDebugMode(t *testing.T) {
	withDebugMode(t, true)
	cfg := config.DefaultConfig()
	cfg.Web.Headers.ServerTimingInDebugOnly = false

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	handler := serverTimingMiddleware(cfg)(next)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	assert.Empty(t, rec.Header().Get("Server-Timing"), "operator toggle must be able to suppress the header even while debug mode is on")
}

// splitServerTimingEntries splits a Server-Timing header value on ", "
// into its individual entries.
func splitServerTimingEntries(header string) []string {
	return regexp.MustCompile(`, `).Split(header, -1)
}

// entriesName extracts the span name from one `name;dur=N.N` entry.
func entriesName(entry string) string {
	return regexp.MustCompile(`;.*$`).ReplaceAllString(entry, "")
}
