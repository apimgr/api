package metrics

import (
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
)

// responseWriter wraps http.ResponseWriter to capture the status code and
// response byte count for RecordRequest.
type responseWriter struct {
	http.ResponseWriter
	status int
	size   int
}

func (w *responseWriter) WriteHeader(status int) {
	if w.status != 0 {
		return
	}
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

func (w *responseWriter) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	n, err := w.ResponseWriter.Write(b)
	w.size += n
	return n, err
}

type requestBodyCounter struct {
	io.ReadCloser
	read int64
}

func (b *requestBodyCounter) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	b.read += int64(n)
	return n, err
}

// isOnionHost reports whether a request Host header names a Tor hidden
// service. The port is stripped first because a v3 onion is reached over
// the same listener as clearnet traffic and may carry an explicit port.
func isOnionHost(host string) bool {
	if host == "" {
		return false
	}
	if i := strings.LastIndex(host, ":"); i != -1 && !strings.Contains(host[i:], "]") {
		host = host[:i]
	}
	return strings.HasSuffix(strings.ToLower(strings.TrimSuffix(host, ".")), ".onion")
}

// Middleware returns HTTP middleware that records every request against
// the receiver's metric families: active-request tracking, request/response
// size, latency, and total count, keyed by the matched router template. A
// fixed label is used for unmatched requests to avoid attacker-controlled
// path cardinality.
func (m *Metrics) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		m.IncActiveRequests()
		defer m.DecActiveRequests()
		var requestBody *requestBodyCounter
		if r.Body != nil {
			requestBody = &requestBodyCounter{ReadCloser: r.Body}
			r.Body = requestBody
		}

		// A request whose Host is the hidden service arrived over the onion
		// listener; the counter is unlabelled, so the address itself never
		// becomes a metric label.
		if isOnionHost(r.Host) {
			m.IncTorRequests()
		}

		start := time.Now()
		wrapped := &responseWriter{ResponseWriter: w}
		next.ServeHTTP(wrapped, r)

		duration := time.Since(start)
		path := ""
		if routeContext := chi.RouteContext(r.Context()); routeContext != nil {
			path = routeContext.RoutePattern()
		}
		if path == "" {
			path = "__unmatched__"
		}
		status := wrapped.status
		if status == 0 {
			status = http.StatusOK
		}

		requestSize := 0
		if requestBody != nil {
			requestSize = int(requestBody.read)
		}

		m.RecordRequest(r.Method, path, strconv.Itoa(status), duration, requestSize, wrapped.size)
	})
}
