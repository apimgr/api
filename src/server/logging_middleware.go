package server

import (
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/apimgr/api/src/metrics"
)

// responseWriter wraps http.ResponseWriter to capture status code and size
type responseWriter struct {
	http.ResponseWriter
	status int
	size   int
}

// WriteHeader captures the status code
func (rw *responseWriter) WriteHeader(status int) {
	rw.status = status
	rw.ResponseWriter.WriteHeader(status)
}

// Write captures the response size
func (rw *responseWriter) Write(b []byte) (int, error) {
	size, err := rw.ResponseWriter.Write(b)
	rw.size += size
	return size, err
}

// loggingMiddleware logs all HTTP requests
func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		// Wrap response writer to capture status and size
		wrapped := &responseWriter{
			ResponseWriter: w,
			status:         200, // default status
			size:           0,
		}

		m := metrics.Get()
		m.IncActiveRequests()
		defer m.DecActiveRequests()

		// Serve the request
		next.ServeHTTP(wrapped, r)

		// Calculate duration
		duration := time.Since(start)

		// Log the request
		if logger := GetLogger(); logger != nil {
			logger.LogAccess(r, wrapped.status, wrapped.size, duration)
		}

		// Record metrics using the matched chi route pattern (never the
		// raw request path) to keep label cardinality bounded
		routePattern := chi.RouteContext(r.Context()).RoutePattern()
		if routePattern == "" {
			routePattern = "unmatched"
		}
		m.RecordRequest(r.Method, routePattern, strconv.Itoa(wrapped.status), duration, int(r.ContentLength), wrapped.size)
	})
}
