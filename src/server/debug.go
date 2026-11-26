package server

import (
	"bytes"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"expvar"
	"io"
	"log/slog"
	"net/http"
	"net/http/pprof"
	"net/url"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/apimgr/api/src/common/i18n"
	"github.com/apimgr/api/src/common/sanitize"
	"github.com/apimgr/api/src/config"
	"github.com/apimgr/api/src/database"
	"github.com/apimgr/api/src/metrics"
	"github.com/apimgr/api/src/mode"
	"github.com/go-chi/chi/v5"
	"gopkg.in/yaml.v3"
)

var (
	debugStartTime       = time.Now()
	debugRequestsTotal   = expvar.NewInt("api_requests_total")
	debugErrorsTotal     = expvar.NewInt("api_errors_total")
	debugRequestsSeconds = expvar.NewFloat("api_requests_duration_seconds")
	debugSchedulerMu     sync.RWMutex
	debugSchedulerStatus func() any
)

func init() {
	expvar.Publish("api_uptime_seconds", expvar.Func(func() any {
		return time.Since(debugStartTime).Seconds()
	}))
	expvar.Publish("api_goroutines", expvar.Func(func() any {
		return runtime.NumGoroutine()
	}))
	expvar.Publish("api_memory", expvar.Func(func() any {
		var state runtime.MemStats
		runtime.ReadMemStats(&state)
		return map[string]uint64{
			"alloc": state.Alloc, "total_alloc": state.TotalAlloc,
			"sys": state.Sys, "heap_alloc": state.HeapAlloc, "heap_sys": state.HeapSys,
		}
	}))
}

// SetDebugSchedulerStatus supplies the live scheduler snapshot used by the
// authenticated debug endpoint.
func SetDebugSchedulerStatus(provider func() any) {
	debugSchedulerMu.Lock()
	debugSchedulerStatus = provider
	debugSchedulerMu.Unlock()
}

func registerDebugRoutes(router chi.Router, cfg *config.Config) {
	if !mode.ShouldShowDebugEndpoints() {
		return
	}
	router.Route("/debug", func(debugRouter chi.Router) {
		debugRouter.Use(requireDebugOperatorToken(cfg))
		if cfg.Server.Debug.Pprof {
			debugRouter.HandleFunc("/pprof", pprof.Index)
			debugRouter.HandleFunc("/pprof/cmdline", pprof.Cmdline)
			debugRouter.HandleFunc("/pprof/profile", pprof.Profile)
			debugRouter.HandleFunc("/pprof/symbol", pprof.Symbol)
			debugRouter.HandleFunc("/pprof/trace", pprof.Trace)
			debugRouter.Handle("/pprof/heap", pprof.Handler("heap"))
			debugRouter.Handle("/pprof/goroutine", pprof.Handler("goroutine"))
			debugRouter.Handle("/pprof/allocs", pprof.Handler("allocs"))
			debugRouter.Handle("/pprof/block", pprof.Handler("block"))
			debugRouter.Handle("/pprof/mutex", pprof.Handler("mutex"))
			debugRouter.Handle("/pprof/threadcreate", pprof.Handler("threadcreate"))
		}
		if !cfg.Server.Debug.RuntimeEndpoints {
			return
		}
		debugRouter.Handle("/vars", expvar.Handler())
		debugRouter.Get("/config", func(w http.ResponseWriter, r *http.Request) {
			writeDebugJSON(w, http.StatusOK, sanitizedDebugConfig(cfg, i18n.LangFromRequest(r)))
		})
		debugRouter.Get("/routes", debugRoutesHandler(router))
		debugRouter.Get("/cache", func(w http.ResponseWriter, _ *http.Request) {
			writeDebugJSON(w, http.StatusOK, metrics.Get().CacheDebugSnapshot())
		})
		debugRouter.Get("/db", debugDatabaseHandler)
		debugRouter.Get("/scheduler", debugSchedulerHandler)
		debugRouter.Get("/memory", debugMemoryHandler)
		debugRouter.Get("/goroutines", debugGoroutinesHandler)
		debugRouter.Get("/error500", debugError500Handler(cfg))
	})
}

func requireDebugOperatorToken(cfg *config.Config) func(http.Handler) http.Handler {
	expected := sha256.Sum256([]byte(cfg.Server.Token))
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			authorization := r.Header.Get("Authorization")
			provided := ""
			if strings.HasPrefix(authorization, "Bearer ") {
				provided = strings.TrimPrefix(authorization, "Bearer ")
			}
			got := sha256.Sum256([]byte(provided))
			if cfg.Server.Token == "" || subtle.ConstantTimeCompare(got[:], expected[:]) != 1 {
				if credential, err := validateConfiguredOwnerToken(r.Context(), cfg, provided); err == nil && credential.Kind != "operator" {
					writeEnvelopeError(w, http.StatusForbidden, "FORBIDDEN", i18n.Translate(i18n.LangFromRequest(r), "errors.forbidden"), nil)
					return
				}
				count, reset, _ := recordAuthenticationFailure(r)
				if limit := cfg.Server.RateLimit.AuthFailures.Requests; limit > 0 && count >= limit && !reset.IsZero() {
					writeRateLimitExceeded(w, r, limit, reset)
					return
				}
				writeEnvelopeError(w, http.StatusUnauthorized, "UNAUTHORIZED", i18n.Translate(i18n.LangFromRequest(r), "errors.forbidden"), nil)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func debugRoutesHandler(router chi.Routes) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		routes := make([]map[string]string, 0)
		if err := chi.Walk(router, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
			routes = append(routes, map[string]string{"method": method, "route": route})
			return nil
		}); err != nil {
			writeEnvelopeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", i18n.Translate(i18n.LangFromRequest(r), "errors.server_error"), nil)
			return
		}
		writeDebugJSON(w, http.StatusOK, map[string]any{"count": len(routes), "routes": routes})
	}
}

func debugDatabaseHandler(w http.ResponseWriter, _ *http.Request) {
	db := database.GetServerDB()
	if db == nil {
		writeDebugJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "unavailable"})
		return
	}
	stats := db.Stats()
	writeDebugJSON(w, http.StatusOK, map[string]any{
		"open_connections": stats.OpenConnections, "in_use": stats.InUse,
		"idle": stats.Idle, "wait_count": stats.WaitCount,
		"wait_duration_ms": stats.WaitDuration.Milliseconds(),
		"max_idle_closed":  stats.MaxIdleClosed, "max_lifetime_closed": stats.MaxLifetimeClosed,
	})
}

func debugSchedulerHandler(w http.ResponseWriter, _ *http.Request) {
	debugSchedulerMu.RLock()
	provider := debugSchedulerStatus
	debugSchedulerMu.RUnlock()
	if provider == nil {
		writeDebugJSON(w, http.StatusOK, []any{})
		return
	}
	writeDebugJSON(w, http.StatusOK, provider())
}

func debugMemoryHandler(w http.ResponseWriter, _ *http.Request) {
	var state runtime.MemStats
	runtime.ReadMemStats(&state)
	writeDebugJSON(w, http.StatusOK, map[string]any{
		"alloc_mb":       state.Alloc / 1024 / 1024,
		"total_alloc_mb": state.TotalAlloc / 1024 / 1024,
		"sys_mb":         state.Sys / 1024 / 1024,
		"num_gc":         state.NumGC, "heap_objects": state.HeapObjects,
		"goroutines": runtime.NumGoroutine(),
	})
}

func debugGoroutinesHandler(w http.ResponseWriter, _ *http.Request) {
	buffer := make([]byte, 1<<20)
	length := runtime.Stack(buffer, true)
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	if _, err := io.WriteString(w, mode.Sanitize(string(buffer[:length]))); err != nil {
		slog.Debug("goroutine dump response failed", "error", mode.Sanitize(err.Error()))
	}
}

func debugError500Handler(cfg *config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		data := newPageData(cfg, r, "error")
		data.ErrorCode = "500"
		data.ErrorTitle = i18n.Translate(data.Lang, "errors.server_error")
		data.ErrorMessage = i18n.Translate(data.Lang, "errors.server_error")
		w.WriteHeader(http.StatusInternalServerError)
		renderPage(w, "error", data)
	}
}

func writeDebugJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		slog.Debug("debug response write failed", "error", mode.Sanitize(err.Error()))
	}
}

func sanitizedDebugConfig(cfg *config.Config, lang string) any {
	encoded, err := yaml.Marshal(cfg)
	if err != nil {
		return map[string]string{"error": i18n.Translate(lang, "errors.server_error")}
	}
	var value map[string]any
	if err := yaml.Unmarshal(encoded, &value); err != nil {
		return map[string]string{"error": i18n.Translate(lang, "errors.server_error")}
	}
	return redactDebugValue(value)
}

func redactDebugValue(value any) any {
	switch current := value.(type) {
	case map[string]any:
		for key, item := range current {
			lower := strings.ToLower(key)
			if strings.Contains(lower, "token") || strings.Contains(lower, "password") || strings.Contains(lower, "secret") || strings.Contains(lower, "private_key") || lower == "key" {
				current[key] = "[redacted]"
				continue
			}
			current[key] = redactDebugValue(item)
		}
		return current
	case []any:
		for index := range current {
			current[index] = redactDebugValue(current[index])
		}
		return current
	case string:
		return redactDebugURL(current)
	default:
		return value
	}
}

func redactDebugURL(value string) string {
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return value
	}
	if parsed.User != nil {
		parsed.User = url.User("[redacted]")
	}
	query := parsed.Query()
	for key := range query {
		lower := strings.ToLower(key)
		if strings.Contains(lower, "token") || strings.Contains(lower, "password") || strings.Contains(lower, "secret") || lower == "key" || lower == "auth" || lower == "api_key" {
			query.Set(key, "[redacted]")
		}
	}
	parsed.RawQuery = query.Encode()
	return parsed.String()
}

func debugMiddleware(cfg *config.Config) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		if !mode.IsDebugEnabled() {
			return next
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			started := time.Now()
			var requestPreview []byte
			if cfg.Server.Debug.LogBodies && r.Body != nil {
				maxBytes := debugBodyLimit(cfg.Server.Debug.MaxBodyLogSize)
				body, err := io.ReadAll(io.LimitReader(r.Body, int64(maxBytes)+1))
				requestPreview = body
				if len(requestPreview) > maxBytes {
					requestPreview = requestPreview[:maxBytes]
				}
				if err != nil {
					slog.Debug("request body preview incomplete", "error", mode.Sanitize(err.Error()))
				}
				r.Body = io.NopCloser(io.MultiReader(bytes.NewReader(body), r.Body))
			}
			bodyLimit := 0
			if cfg.Server.Debug.LogBodies {
				bodyLimit = debugBodyLimit(cfg.Server.Debug.MaxBodyLogSize)
			}
			wrapped := &debugCaptureWriter{ResponseWriter: w, status: http.StatusOK, max: bodyLimit}
			next.ServeHTTP(wrapped, r)
			duration := time.Since(started)
			debugRequestsTotal.Add(1)
			debugRequestsSeconds.Add(duration.Seconds())
			if wrapped.status >= 500 {
				debugErrorsTotal.Add(1)
			}
			attributes := []any{
				"method", r.Method, "path", r.URL.Path, "query", redactQuery(r.URL.RawQuery),
				"status", wrapped.status, "duration_ms", duration.Milliseconds(), "size", wrapped.size,
				"user_agent", mode.Sanitize(r.UserAgent()), "request_id", mode.Sanitize(r.Header.Get("X-Request-ID")),
				"remote_addr", r.RemoteAddr, "request_headers", sanitizedDebugHeaders(r.Header),
				"response_headers", sanitizedDebugHeaders(wrapped.Header()),
			}
			if cfg.Server.Debug.LogBodies {
				attributes = append(attributes, "request_body", safeDebugBody(requestPreview), "response_body", safeDebugBody(wrapped.preview.Bytes()))
			}
			slog.Debug("request details", attributes...)
		})
	}
}

func sanitizedDebugHeaders(headers http.Header) map[string][]string {
	result := make(map[string][]string, len(headers))
	for name, values := range headers {
		safeValues := make([]string, 0, len(values))
		normalizedName := strings.ReplaceAll(strings.ToLower(name), "-", "_")
		for _, value := range values {
			if sanitize.IsCredentialKey(normalizedName) {
				safeValues = append(safeValues, sanitize.RedactedPlaceholder)
				continue
			}
			safeValues = append(safeValues, mode.Sanitize(value))
		}
		result[name] = safeValues
	}
	return result
}

type debugCaptureWriter struct {
	http.ResponseWriter
	status  int
	size    int
	max     int
	preview bytes.Buffer
}

func (w *debugCaptureWriter) WriteHeader(status int) {
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

func (w *debugCaptureWriter) Write(body []byte) (int, error) {
	n, err := w.ResponseWriter.Write(body)
	w.size += n
	if w.preview.Len() < w.max {
		remaining := w.max - w.preview.Len()
		if n < remaining {
			remaining = n
		}
		_, _ = w.preview.Write(body[:remaining])
	}
	return n, err
}

func (w *debugCaptureWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}

func safeDebugBody(body []byte) string {
	var decoded any
	if json.Unmarshal(body, &decoded) == nil {
		encoded, err := json.Marshal(redactDebugValue(decoded))
		if err == nil {
			return string(encoded)
		}
	}
	return "[omitted: non-JSON body]"
}

func debugBodyLimit(value string) int {
	value = strings.ToUpper(strings.TrimSpace(value))
	if value == "" {
		return 10 * 1024
	}
	multiplier := 1
	for _, suffix := range []struct {
		name string
		size int
	}{{"GB", 1024 * 1024 * 1024}, {"MB", 1024 * 1024}, {"KB", 1024}, {"B", 1}} {
		if strings.HasSuffix(value, suffix.name) {
			value = strings.TrimSpace(strings.TrimSuffix(value, suffix.name))
			multiplier = suffix.size
			break
		}
	}
	count, err := strconv.Atoi(value)
	if err != nil || count <= 0 || count > int(^uint(0)>>1)/multiplier {
		return 10 * 1024
	}
	return count * multiplier
}
