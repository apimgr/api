package server

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
)

// TestResponseWriter_WriteHeader_CapturesStatus verifies the wrapper
// records whatever status the handler sets, instead of the default.
func TestResponseWriter_WriteHeader_CapturesStatus(t *testing.T) {
	tests := []struct {
		name   string
		status int
	}{
		{"ok", http.StatusOK},
		{"not found", http.StatusNotFound},
		{"server error", http.StatusInternalServerError},
		{"teapot", http.StatusTeapot},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			rw := &responseWriter{ResponseWriter: rec, status: 200}

			rw.WriteHeader(tt.status)

			assert.Equal(t, tt.status, rw.status)
			assert.Equal(t, tt.status, rec.Code)
		})
	}
}

// TestResponseWriter_Write_AccumulatesSize covers single and multiple
// writes, including a zero-byte write, to confirm size accumulates
// correctly rather than being overwritten each call.
func TestResponseWriter_Write_AccumulatesSize(t *testing.T) {
	rec := httptest.NewRecorder()
	rw := &responseWriter{ResponseWriter: rec, status: 200}

	n1, err := rw.Write([]byte("hello"))
	assert.NoError(t, err)
	assert.Equal(t, 5, n1)
	assert.Equal(t, 5, rw.size)

	n2, err := rw.Write([]byte(" world"))
	assert.NoError(t, err)
	assert.Equal(t, 6, n2)
	assert.Equal(t, 11, rw.size)

	n3, err := rw.Write([]byte(""))
	assert.NoError(t, err)
	assert.Equal(t, 0, n3)
	assert.Equal(t, 11, rw.size)

	assert.Equal(t, "hello world", rec.Body.String())
}

// TestLoggingMiddleware_DefaultStatus200 covers a handler that never
// calls WriteHeader explicitly — the default status must be reported as
// 200, matching net/http's own implicit-200 behavior.
func TestLoggingMiddleware_DefaultStatus200(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ok"))
	})

	handler := loggingMiddleware(next)
	req := httptest.NewRequest(http.MethodGet, "/anything", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "ok", rec.Body.String())
}

// TestLoggingMiddleware_ExplicitStatus covers a handler that sets a
// non-default status, ensuring it propagates through the wrapper to the
// real ResponseWriter untouched.
func TestLoggingMiddleware_ExplicitStatus(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte("created"))
	})

	handler := loggingMiddleware(next)
	req := httptest.NewRequest(http.MethodPost, "/things", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusCreated, rec.Code)
	assert.Equal(t, "created", rec.Body.String())
}

// TestLoggingMiddleware_NoRouteContext ensures the middleware does not
// panic when there is no chi RouteContext on the request (e.g. handler
// invoked directly in a unit test rather than through a chi router) —
// it must fall back to treating the route as "unmatched" internally
// without erroring.
func TestLoggingMiddleware_NoRouteContext(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})
	handler := loggingMiddleware(next)

	req := httptest.NewRequest(http.MethodGet, "/no-route-context", nil)
	rec := httptest.NewRecorder()

	assert.NotPanics(t, func() {
		handler.ServeHTTP(rec, req)
	})
}

// TestLoggingMiddleware_WithChiRoutePattern drives the middleware through
// a real chi router so RoutePattern() resolves to the registered pattern
// (rather than the raw, high-cardinality request path) before metrics
// are recorded.
func TestLoggingMiddleware_WithChiRoutePattern(t *testing.T) {
	r := chi.NewRouter()
	var capturedPattern string
	r.Use(loggingMiddleware)
	r.Get("/items/{id}", func(w http.ResponseWriter, r *http.Request) {
		capturedPattern = chi.RouteContext(r.Context()).RoutePattern()
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/items/42", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "/items/{id}", capturedPattern)
}

// TestLoggingMiddleware_CallsNextExactlyOnce guards against the
// middleware accidentally invoking the wrapped handler more than once
// (e.g. via a retry/duplicate call bug).
func TestLoggingMiddleware_CallsNextExactlyOnce(t *testing.T) {
	callCount := 0
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
	})

	handler := loggingMiddleware(next)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assert.Equal(t, 1, callCount)
}
