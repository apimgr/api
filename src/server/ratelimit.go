package server

import (
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/apimgr/api/src/config"
)

// RateLimiter implements a sliding window rate limiter
type RateLimiter struct {
	mu       sync.RWMutex
	requests map[string]*clientRequests
	limit    int
	window   time.Duration
	enabled  bool
}

// clientRequests tracks requests for a single client
type clientRequests struct {
	timestamps []time.Time
	mu         sync.Mutex
}

// NewRateLimiter creates a new rate limiter
func NewRateLimiter(cfg *config.Config) *RateLimiter {
	rl := &RateLimiter{
		requests: make(map[string]*clientRequests),
		limit:    cfg.Server.RateLimit.Requests,
		window:   time.Duration(cfg.Server.RateLimit.Window) * time.Second,
		enabled:  cfg.Server.RateLimit.Enabled,
	}

	// Start cleanup goroutine
	go rl.cleanup()

	return rl
}

// Allow checks if a request is allowed for the given client IP
func (rl *RateLimiter) Allow(clientIP string) (bool, int, int, time.Time) {
	if !rl.enabled {
		return true, 0, rl.limit, time.Time{}
	}

	rl.mu.Lock()
	client, exists := rl.requests[clientIP]
	if !exists {
		client = &clientRequests{
			timestamps: make([]time.Time, 0, rl.limit),
		}
		rl.requests[clientIP] = client
	}
	rl.mu.Unlock()

	client.mu.Lock()
	defer client.mu.Unlock()

	now := time.Now()
	windowStart := now.Add(-rl.window)

	// Remove expired timestamps
	validTimestamps := make([]time.Time, 0, len(client.timestamps))
	for _, ts := range client.timestamps {
		if ts.After(windowStart) {
			validTimestamps = append(validTimestamps, ts)
		}
	}
	client.timestamps = validTimestamps

	// Check if limit exceeded
	remaining := rl.limit - len(client.timestamps)
	if remaining <= 0 {
		// Calculate reset time (oldest timestamp + window)
		resetTime := client.timestamps[0].Add(rl.window)
		return false, 0, rl.limit, resetTime
	}

	// Add current request
	client.timestamps = append(client.timestamps, now)
	remaining--

	// Calculate reset time
	resetTime := now.Add(rl.window)
	if len(client.timestamps) > 0 {
		resetTime = client.timestamps[0].Add(rl.window)
	}

	return true, remaining, rl.limit, resetTime
}

// cleanup periodically removes stale entries
func (rl *RateLimiter) cleanup() {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		rl.mu.Lock()
		now := time.Now()
		windowStart := now.Add(-rl.window)

		for ip, client := range rl.requests {
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
				delete(rl.requests, ip)
			}
			client.mu.Unlock()
		}
		rl.mu.Unlock()
	}
}

// RateLimitMiddleware creates a rate limiting middleware
func RateLimitMiddleware(cfg *config.Config) func(http.Handler) http.Handler {
	limiter := NewRateLimiter(cfg)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Skip rate limiting for certain paths
			if shouldSkipRateLimit(r.URL.Path) {
				next.ServeHTTP(w, r)
				return
			}

			// Get client IP
			clientIP := getClientIP(r)

			// Check rate limit
			allowed, remaining, limit, resetTime := limiter.Allow(clientIP)

			// Set rate limit headers (always)
			w.Header().Set("X-RateLimit-Limit", intToString(limit))
			w.Header().Set("X-RateLimit-Remaining", intToString(remaining))
			if !resetTime.IsZero() {
				w.Header().Set("X-RateLimit-Reset", intToString(int(resetTime.Unix())))
			}

			if !allowed {
				w.Header().Set("Retry-After", intToString(int(time.Until(resetTime).Seconds())+1))
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusTooManyRequests)
				json.NewEncoder(w).Encode(map[string]interface{}{
					"error":       "Rate limit exceeded",
					"limit":       limit,
					"retry_after": int(time.Until(resetTime).Seconds()) + 1,
				})
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// shouldSkipRateLimit returns true for paths that should bypass rate limiting
func shouldSkipRateLimit(path string) bool {
	// Skip rate limiting for health checks and well-known files
	skipPaths := []string{
		"/healthz",
		"/api/v1/healthz",
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

// getClientIP extracts the client IP from the request
func getClientIP(r *http.Request) string {
	// Check X-Forwarded-For first (common for proxies)
	xff := r.Header.Get("X-Forwarded-For")
	if xff != "" {
		// Take the first IP in the list
		ips := strings.Split(xff, ",")
		return strings.TrimSpace(ips[0])
	}

	// Check X-Real-IP (nginx proxy)
	xri := r.Header.Get("X-Real-IP")
	if xri != "" {
		return xri
	}

	// Fall back to RemoteAddr
	ip := r.RemoteAddr
	// Remove port if present
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
