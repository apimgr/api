package server

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/apimgr/api/src/config"
	"github.com/apimgr/api/src/mode"
	"github.com/stretchr/testify/require"
)

func setDebugModeForTest(t *testing.T, enabled bool) {
	t.Helper()
	previousMode := mode.GetCurrentMode().String()
	previousDebug := mode.IsDebugEnabled()
	if err := mode.Initialize(previousMode, enabled, true); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = mode.Initialize(previousMode, previousDebug, true)
	})
}

func TestSanitizedDebugConfigRedactsCredentials(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Server.Token = "operator-secret-value"
	cfg.Server.Cache.Password = "cache-secret-value"
	cfg.Server.Database.URL = "https://db.example.test/?authToken=database-secret-value"

	encoded, err := json.Marshal(sanitizedDebugConfig(cfg, "en"))
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "operator-secret-value")
	require.NotContains(t, string(encoded), "cache-secret-value")
	require.NotContains(t, string(encoded), "database-secret-value")
	require.Contains(t, string(encoded), "[redacted]")
}

func TestSafeDebugBodyOmitsNonJSON(t *testing.T) {
	require.Equal(t, "[omitted: non-JSON body]", safeDebugBody([]byte("password=cleartext")))
	require.Equal(t, `{"password":"[redacted]"}`, safeDebugBody([]byte(`{"password":"cleartext"}`)))
}

func TestDebugBodyLimitAndCaptureWriter(t *testing.T) {
	for input, expected := range map[string]int{"10KB": 10 * 1024, "2MB": 2 * 1024 * 1024, "bad": 10 * 1024} {
		require.Equal(t, expected, debugBodyLimit(input))
	}

	recorder := httptest.NewRecorder()
	wrapped := &debugCaptureWriter{ResponseWriter: recorder, status: http.StatusOK, max: 4}
	_, err := io.WriteString(wrapped, "abcdef")
	require.NoError(t, err)
	require.Equal(t, "abcdef", recorder.Body.String())
	require.Equal(t, "abcd", wrapped.preview.String())
	require.Equal(t, 6, wrapped.size)
}

func TestDebugMiddlewareReplaysRequestBody(t *testing.T) {
	setDebugModeForTest(t, true)
	cfg := config.DefaultConfig()
	cfg.Server.Debug.LogBodies = true
	cfg.Server.Debug.MaxBodyLogSize = "2B"
	var received string
	handler := debugMiddleware(cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		received = string(body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"ok":true}`)
	}))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/debug-test", strings.NewReader(`{"x":"long"}`)))
	require.Equal(t, `{"x":"long"}`, received)
	require.Equal(t, `{"ok":true}`, recorder.Body.String())
}

func TestDebugRoutesRequireFlagAndOperatorToken(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Server.Token = "operator-secret"
	setDebugModeForTest(t, false)
	srv := New(cfg)

	recorder := httptest.NewRecorder()
	srv.Handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/debug/memory", nil))
	require.Equal(t, http.StatusNotFound, recorder.Code)

	setDebugModeForTest(t, true)
	srv = New(cfg)
	recorder = httptest.NewRecorder()
	srv.Handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/debug/memory", nil))
	require.Equal(t, http.StatusUnauthorized, recorder.Code)

	request := httptest.NewRequest(http.MethodGet, "/debug/memory", nil)
	request.Header.Set("Authorization", "Bearer operator-secret")
	recorder = httptest.NewRecorder()
	srv.Handler.ServeHTTP(recorder, request)
	require.Equal(t, http.StatusOK, recorder.Code)
}

func TestDebugEndpointConfigGatesBothGroups(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Server.Token = "operator-secret"
	cfg.Server.Debug.Pprof = false
	cfg.Server.Debug.RuntimeEndpoints = false
	setDebugModeForTest(t, true)
	srv := New(cfg)

	for _, path := range []string{"/debug/pprof", "/debug/memory"} {
		request := httptest.NewRequest(http.MethodGet, path, nil)
		request.Header.Set("Authorization", "Bearer operator-secret")
		recorder := httptest.NewRecorder()
		srv.Handler.ServeHTTP(recorder, request)
		require.Equal(t, http.StatusNotFound, recorder.Code, path)
	}
}
