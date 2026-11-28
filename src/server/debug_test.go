package server

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/apimgr/api/src/config"
	"github.com/apimgr/api/src/database"
	"github.com/apimgr/api/src/mode"
	"github.com/apimgr/api/src/paths"
	"github.com/apimgr/api/src/scheduler"
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
	srv := NewHTTPServer(cfg)

	recorder := httptest.NewRecorder()
	srv.Handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/debug/memory", nil))
	require.Equal(t, http.StatusNotFound, recorder.Code)

	setDebugModeForTest(t, true)
	srv = NewHTTPServer(cfg)
	recorder = httptest.NewRecorder()
	srv.Handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/debug/memory", nil))
	require.Equal(t, http.StatusUnauthorized, recorder.Code)

	request := httptest.NewRequest(http.MethodGet, "/debug/memory", nil)
	request.Header.Set("Authorization", "Bearer operator-secret")
	recorder = httptest.NewRecorder()
	srv.Handler.ServeHTTP(recorder, request)
	require.Equal(t, http.StatusOK, recorder.Code)

	request = httptest.NewRequest(http.MethodGet, "/debug/pprof", nil)
	request.Header.Set("Authorization", "Bearer operator-secret")
	recorder = httptest.NewRecorder()
	srv.Handler.ServeHTTP(recorder, request)
	require.Equal(t, http.StatusOK, recorder.Code)
	require.Contains(t, recorder.Body.String(), "goroutine")
}

func TestDebugOperatorRoutesRejectOwnerTokensAsForbidden(t *testing.T) {
	cfg := newTestConfig(t)
	cfg.Server.Token = "operator-secret"
	require.NoError(t, database.InitDatabase(cfg.Server.Database, paths.DataDir()))
	t.Cleanup(func() { _ = database.Close() })

	key := make([]byte, 32)
	primary, owner, err := database.IssueOwnerToken(t.Context(), database.GetServerDB(), key)
	require.NoError(t, err)
	linked, _, err := database.CreateLinkedOwnerToken(t.Context(), database.GetServerDB(), key, owner.OwnerID, "debug-test", nil, 10)
	require.NoError(t, err)

	handler := requireDebugOperatorToken(cfg)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	for name, token := range map[string]string{"primary owner": primary, "linked owner": linked} {
		t.Run(name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "/debug/config", nil)
			request.Header.Set("Authorization", "Bearer "+token)
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, request)
			require.Equal(t, http.StatusForbidden, recorder.Code)
		})
	}
}

func TestDebugEndpointConfigGatesBothGroups(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Server.Token = "operator-secret"
	cfg.Server.Debug.Pprof = false
	cfg.Server.Debug.RuntimeEndpoints = false
	setDebugModeForTest(t, true)
	srv := NewHTTPServer(cfg)

	for _, path := range []string{"/debug/pprof", "/debug/memory"} {
		request := httptest.NewRequest(http.MethodGet, path, nil)
		request.Header.Set("Authorization", "Bearer operator-secret")
		recorder := httptest.NewRecorder()
		srv.Handler.ServeHTTP(recorder, request)
		require.Equal(t, http.StatusNotFound, recorder.Code, path)
	}
}

func TestDebugSchedulerReturnsSerializableStatus(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Server.Token = "operator-secret"
	cfg.Server.Debug.RuntimeEndpoints = true
	SetDebugSchedulerStatus(func() any {
		return []scheduler.TaskStatus{{Name: "backup_daily", Schedule: "0 2 * * *", Enabled: true}}
	})
	t.Cleanup(func() { SetDebugSchedulerStatus(nil) })
	setDebugModeForTest(t, true)
	srv := NewHTTPServer(cfg)
	request := httptest.NewRequest(http.MethodGet, "/debug/scheduler", nil)
	request.Header.Set("Authorization", "Bearer operator-secret")
	recorder := httptest.NewRecorder()
	srv.Handler.ServeHTTP(recorder, request)
	require.Equal(t, http.StatusOK, recorder.Code)
	var tasks []scheduler.TaskStatus
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &tasks))
	require.Len(t, tasks, 1)
	require.Equal(t, "backup_daily", tasks[0].Name)
}

func TestDebugCmdlineEndpointsExposeOnlyBinaryName(t *testing.T) {
	previousArgs := os.Args
	os.Args = []string{"/opt/api/bin/api", "--token=credential-value", "--config=/private/path"}
	t.Cleanup(func() { os.Args = previousArgs })

	pprofRecorder := httptest.NewRecorder()
	debugPprofCmdlineHandler(pprofRecorder, httptest.NewRequest(http.MethodGet, "/debug/pprof/cmdline", nil))
	require.Equal(t, "api\x00", pprofRecorder.Body.String())
	require.NotContains(t, pprofRecorder.Body.String(), "credential-value")

	varsRecorder := httptest.NewRecorder()
	debugVarsHandler().ServeHTTP(varsRecorder, httptest.NewRequest(http.MethodGet, "/debug/vars", nil))
	var variables map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(varsRecorder.Body.Bytes(), &variables))
	var commandLine []string
	require.NoError(t, json.Unmarshal(variables["cmdline"], &commandLine))
	require.Equal(t, []string{"api"}, commandLine)
	require.NotContains(t, varsRecorder.Body.String(), "credential-value")
	require.NotContains(t, varsRecorder.Body.String(), "/private/path")
}
