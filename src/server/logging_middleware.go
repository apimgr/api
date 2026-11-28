package server

import (
	"net/http"
	"time"
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

// Unwrap exposes the underlying http.ResponseWriter for http.ResponseController
// and any code that needs to walk down through a chain of wrapping writers
// (e.g. serverTimingMiddleware's writer lookup in server.go) to find a
// specific wrapper type further down the chain.
func (rw *responseWriter) Unwrap() http.ResponseWriter {
	return rw.ResponseWriter
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

		// Serve the request
		next.ServeHTTP(wrapped, r)

		// Calculate duration
		duration := time.Since(start)

		// Log the request
		if logger := GetLogger(); logger != nil {
			logger.LogAccess(r, wrapped.status, wrapped.size, duration)
		}

	})
}
