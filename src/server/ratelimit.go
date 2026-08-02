package server

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/apimgr/api/src/cache"
	"github.com/apimgr/api/src/config"
)

// classLimiter implements a sliding window rate limiter for a single
// request class (read, write, health, or the absolute global burst
// ceiling), backed by a cache.Store - in-process by default, or a shared
// valkey/redis store when server.cache.type is configured, per AI.md
// PART 12 "Cache Usage in Application" (rate limiting uses the cache).
type classLimiter struct {
	store  cache.Store
	prefix string
	limit  int
	window time.Duration
}

// newClassLimiter creates a sliding-window limiter for one rate limit
// class, keying its counters in store under a class-specific prefix so
// read/write/health/global don't collide on a shared cache instance.
func newClassLimiter(store cache.Store, class string, limit int, windowSeconds int) *classLimiter {
	return &classLimiter{
		store:  store,
		prefix: class + ":",
		limit:  limit,
		window: time.Duration(windowSeconds) * time.Second,
	}
}

// allow checks if a request is allowed for the given client IP under this class
func (cl *classLimiter) allow(clientIP string) (bool, int, int, time.Time) {
	count, resetTime, err := cl.store.SlidingWindow(context.Background(), cl.prefix+clientIP, time.Now(), cl.window)
	if err != nil {
		// Fail open on a cache backend error - never let a transient cache
		// outage take the whole server down; the error is logged so an
		// operator sees a degraded shared cache.
		slog.Warn("ratelimit: cache store error, allowing request", "class", cl.prefix, "error", err)
		return true, cl.limit, cl.limit, time.Time{}
	}

	remaining := cl.limit - count
	if remaining < 0 {
		remaining = 0
	}
	return count <= cl.limit, remaining, cl.limit, resetTime
}

// RateLimiter enforces the per-class (read/write/health) sliding window
// limits plus an absolute global_burst ceiling, all per client IP
type RateLimiter struct {
	enabled bool
	read    *classLimiter
	write   *classLimiter
	health  *classLimiter
	global  *classLimiter
}

// globalBurstWindowSeconds is the fixed window used for the global_burst
// ceiling - the config only carries a flat request count (no window key),
// so it shares the same 60s window as the other rate limit classes
const globalBurstWindowSeconds = 60

// NewRateLimiter creates a new rate limiter from server.rate_limit config,
// backed by the server.cache.* store (in-process memory by default, or a
// shared valkey/redis instance when configured).
func NewRateLimiter(cfg *config.Config) *RateLimiter {
	store := cache.New(cfg.Server.Cache)

	return &RateLimiter{
		enabled: cfg.Server.RateLimit.Enabled,
		read:    newClassLimiter(store, "read", cfg.Server.RateLimit.Read.Requests, cfg.Server.RateLimit.Read.Window),
		write:   newClassLimiter(store, "write", cfg.Server.RateLimit.Write.Requests, cfg.Server.RateLimit.Write.Window),
		health:  newClassLimiter(store, "health", cfg.Server.RateLimit.Health.Requests, cfg.Server.RateLimit.Health.Window),
		global:  newClassLimiter(store, "global", cfg.Server.RateLimit.GlobalBurst, globalBurstWindowSeconds),
	}
}

// RateLimitMiddleware creates a rate limiting middleware
func RateLimitMiddleware(cfg *config.Config) func(http.Handler) http.Handler {
	limiter := NewRateLimiter(cfg)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Skip rate limiting for certain paths
			if !limiter.enabled || shouldSkipRateLimit(r.URL.Path) {
				next.ServeHTTP(w, r)
				return
			}

			clientIP := getClientIP(r)

			// Absolute ceiling across all endpoint types, checked first
			if allowed, _, limit, resetTime := limiter.global.allow(clientIP); !allowed {
				writeRateLimitExceeded(w, limit, resetTime)
				return
			}

			var class *classLimiter
			switch {
			case isHealthCheckPath(r.URL.Path):
				class = limiter.health
			case r.Method == http.MethodGet || r.Method == http.MethodHead:
				class = limiter.read
			default:
				class = limiter.write
			}

			allowed, remaining, limit, resetTime := class.allow(clientIP)

			// Set rate limit headers (always)
			w.Header().Set("X-RateLimit-Limit", intToString(limit))
			w.Header().Set("X-RateLimit-Remaining", intToString(remaining))
			if !resetTime.IsZero() {
				w.Header().Set("X-RateLimit-Reset", intToString(int(resetTime.Unix())))
			}

			if !allowed {
				writeRateLimitExceeded(w, limit, resetTime)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// writeRateLimitExceeded writes the standard 429 response - the wait time
// is conveyed only via the Retry-After header, never as a body field
func writeRateLimitExceeded(w http.ResponseWriter, limit int, resetTime time.Time) {
	retryAfter := int(time.Until(resetTime).Seconds()) + 1
	w.Header().Set("Retry-After", intToString(retryAfter))
	w.Header().Set("X-RateLimit-Limit", intToString(limit))
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusTooManyRequests)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"ok":      false,
		"error":   "RATE_LIMITED",
		"message": "Too many requests",
	})
}

// shouldSkipRateLimit returns true for paths that should bypass rate limiting
// entirely (static/well-known files, not endpoints)
func shouldSkipRateLimit(path string) bool {
	skipPaths := []string{
		"/robots.txt",
		"/security.txt",
		"/.well-known/",
		"/favicon.ico",
		"/static/",
	}

	for _, skip := range skipPaths {
		if path == skip || strings.HasPrefix(path, skip) {
			return true
		}
	}

	return false
}

// getClientIP returns the client IP for rate-limit keying. Trust for any
// proxy-supplied header has already been evaluated upstream by
// realIPMiddleware, so RemoteAddr is used as-is here
func getClientIP(r *http.Request) string {
	ip := r.RemoteAddr
	if idx := strings.LastIndex(ip, ":"); idx != -1 {
		ip = ip[:idx]
	}
	return ip
}

// intToString converts an integer to a string without using strconv
func intToString(n int) string {
	if n == 0 {
		return "0"
	}

	negative := n < 0
	if negative {
		n = -n
	}

	// Build string in reverse
	digits := make([]byte, 0, 20)
	for n > 0 {
		digits = append(digits, byte('0'+n%10))
		n /= 10
	}

	if negative {
		digits = append(digits, '-')
	}

	// Reverse the string
	for i, j := 0, len(digits)-1; i < j; i, j = i+1, j-1 {
		digits[i], digits[j] = digits[j], digits[i]
	}

	return string(digits)
}
