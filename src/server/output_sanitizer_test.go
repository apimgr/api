package server

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/apimgr/api/src/config"
	"github.com/stretchr/testify/require"
)

func TestOutputSanitizationMiddlewareSanitizesJSONAndHTML(t *testing.T) {
	cfg := &config.ServerConfig{}
	cfg.Server.Mode = "production"
	handler := outputSanitizationMiddleware(cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/json" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"url":"https://example.test/?token=secret&page=1","message":"internal 10.1.2.3 at /root/private","_debug":{"path":"/root/debug"}}`))
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`<html><body><p>internal 192.168.1.4 at /root/private</p><a href="https://example.test/?password=secret&page=1">continue</a></body></html>`))
	}))

	jsonRecorder := httptest.NewRecorder()
	handler.ServeHTTP(jsonRecorder, httptest.NewRequest(http.MethodGet, "/json", nil))
	require.NotContains(t, jsonRecorder.Body.String(), "secret")
	require.NotContains(t, jsonRecorder.Body.String(), "10.1.2.3")
	require.NotContains(t, jsonRecorder.Body.String(), "/root/private")
	require.NotContains(t, jsonRecorder.Body.String(), "_debug")
	require.Contains(t, jsonRecorder.Body.String(), "page=1")

	htmlRecorder := httptest.NewRecorder()
	handler.ServeHTTP(htmlRecorder, httptest.NewRequest(http.MethodGet, "/html", nil))
	require.Contains(t, htmlRecorder.Body.String(), "<p>")
	require.Contains(t, htmlRecorder.Body.String(), "<a ")
	require.NotContains(t, htmlRecorder.Body.String(), "192.168.1.4")
	require.NotContains(t, htmlRecorder.Body.String(), "/root/private")
	require.NotContains(t, htmlRecorder.Body.String(), "password=secret")
	require.Contains(t, htmlRecorder.Body.String(), "page=1")
}

func TestOutputSanitizationKeepsBuildStampETagForHTML(t *testing.T) {
	cfg := &config.ServerConfig{}
	cfg.Server.Mode = "production"
	handler := outputSanitizationMiddleware(cfg)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("ETag", `"stale-body-tag"`)
		_, _ = w.Write([]byte(`<p>internal 10.1.2.3</p>`))
	}))

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))
	require.Equal(t, buildETag(), recorder.Header().Get("ETag"))
	require.NotContains(t, recorder.Body.String(), "10.1.2.3")
}

func TestOutputSanitizationFailsClosedOnMalformedJSON(t *testing.T) {
	cfg := &config.ServerConfig{}
	cfg.Server.Mode = "production"
	handler := outputSanitizationMiddleware(cfg)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"token":"secret", malformed`))
	}))

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/example", nil))
	require.Equal(t, http.StatusInternalServerError, recorder.Code)
	require.Equal(t, "private, no-store", recorder.Header().Get("Cache-Control"))
	require.NotContains(t, recorder.Body.String(), "secret")
	require.Contains(t, recorder.Body.String(), "INTERNAL_ERROR")
}

func TestOutputSanitizationMiddlewarePreservesBinaryAndStreaming(t *testing.T) {
	cfg := &config.ServerConfig{}
	cfg.Server.Mode = "production"
	handler := outputSanitizationMiddleware(cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/stream" {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = w.Write([]byte("data: /root/private 10.0.0.1\n\n"))
			w.(http.Flusher).Flush()
			return
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write([]byte("/root/private 10.0.0.1"))
	}))

	binaryRecorder := httptest.NewRecorder()
	handler.ServeHTTP(binaryRecorder, httptest.NewRequest(http.MethodGet, "/binary", nil))
	require.Equal(t, "/root/private 10.0.0.1", binaryRecorder.Body.String())

	streamRecorder := httptest.NewRecorder()
	handler.ServeHTTP(streamRecorder, httptest.NewRequest(http.MethodGet, "/stream", nil))
	require.Containsf(t, streamRecorder.Body.String(), "data: [redacted] [redacted]", "sanitized event stream: %q", streamRecorder.Body.String())
}
