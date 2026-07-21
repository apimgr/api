package server

import (
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/apimgr/api/src/config"
)

// classLimiter implements a sliding window rate limiter for a single
// request class (read, write, health, or the absolute global burst ceiling)
type classLimiter struct {
	mu       sync.RWMutex
	requests map[string]*clientRequests
	limit    int
	window   time.Duration
}

// clientRequests tracks requests for a single client
type clientRequests struct {
	timestamps []time.Time
	mu         sync.Mutex
}

// newClassLimiter creates a sliding-window limiter for one rate limit class
func newClassLimiter(limit int, windowSeconds int) *classLimiter {
	return &classLimiter{
		requests: make(map[string]*clientRequests),
		limit:    limit,
		window:   time.Duration(windowSeconds) * time.Second,
	}
}

// allow checks if a request is allowed for the given client IP under this class
func (cl *classLimiter) allow(clientIP string) (bool, int, int, time.Time) {
	cl.mu.Lock()
	client, exists := cl.requests[clientIP]
	if !exists {
		client = &clientRequests{
			timestamps: make([]time.Time, 0, cl.limit),
		}
		cl.requests[clientIP] = client
	}
	cl.mu.Unlock()

	client.mu.Lock()
	defer client.mu.Unlock()

	now := time.Now()
	windowStart := now.Add(-cl.window)

	// Remove expired timestamps
	validTimestamps := make([]time.Time, 0, len(client.timestamps))
	for _, ts := range client.timestamps {
		if ts.After(windowStart) {
			validTimestamps = append(validTimestamps, ts)
		}
	}
	client.timestamps = validTimestamps

	// Check if limit exceeded
	remaining := cl.limit - len(client.timestamps)
	if remaining <= 0 {
		// Calculate reset time (oldest timestamp + window)
		resetTime := client.timestamps[0].Add(cl.window)
		return false, 0, cl.limit, resetTime
	}

	// Add current request
	client.timestamps = append(client.timestamps, now)
	remaining--

	// Calculate reset time
	resetTime := now.Add(cl.window)
	if len(client.timestamps) > 0 {
		resetTime = client.timestamps[0].Add(cl.window)
	}

	return true, remaining, cl.limit, resetTime
}

// cleanup periodically removes stale entries
func (cl *classLimiter) cleanup() {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		cl.mu.Lock()
		now := time.Now()
		windowStart := now.Add(-cl.window)

		for ip, client := range cl.requests {
			client.mu.Lock()
			// Remove expired timestamps
			validTimestamps := make([]time.Time, 0, len(client.timestamps))
			for _, ts := range client.timestamps {
				if ts.After(windowStart) {
					validTimestamps = append(validTimestamps, ts)
				}
			}
			client.timestamps = validTimestamps

			// Remove client if no recent requests
			if len(client.timestamps) == 0 {
				delete(cl.requests, ip)
			}
			client.mu.Unlock()
		}
		cl.mu.Unlock()
	}
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

// NewRateLimiter creates a new rate limiter from server.rate_limit config
func NewRateLimiter(cfg *config.Config) *RateLimiter {
	rl := &RateLimiter{
		enabled: cfg.Server.RateLimit.Enabled,
		read:    newClassLimiter(cfg.Server.RateLimit.Read.Requests, cfg.Server.RateLimit.Read.Window),
		write:   newClassLimiter(cfg.Server.RateLimit.Write.Requests, cfg.Server.RateLimit.Write.Window),
		health:  newClassLimiter(cfg.Server.RateLimit.Health.Requests, cfg.Server.RateLimit.Health.Window),
		global:  newClassLimiter(cfg.Server.RateLimit.GlobalBurst, globalBurstWindowSeconds),
	}

	// Start cleanup goroutines
	go rl.read.cleanup()
	go rl.write.cleanup()
	go rl.health.cleanup()
	go rl.global.cleanup()

	return rl
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
