package server

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/apimgr/api/src/cache"
	"github.com/apimgr/api/src/config"
	"github.com/apimgr/api/src/database"
	"github.com/apimgr/api/src/metrics"
	"github.com/apimgr/api/src/paths"
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
	count, resetTime, err := cl.hit(clientIP)
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

func (cl *classLimiter) hit(identity string) (int, time.Time, error) {
	return cl.store.SlidingWindow(context.Background(), cl.prefix+identity, time.Now(), cl.window)
}

// RateLimiter enforces the per-class (read/write/health) sliding window
// limits plus an absolute global_burst ceiling, all per client IP
type RateLimiter struct {
	closeOnce    sync.Once
	closeErr     error
	read         *classLimiter
	write        *classLimiter
	health       *classLimiter
	global       *classLimiter
	strict       *classLimiter
	authFailures *classLimiter
}

var activeRateLimitersMu sync.Mutex
var activeRateLimiters []*RateLimiter

type authFailureRecorderKey struct{}

type authFailureRecorder func() (int, time.Time, error)

// globalBurstWindowSeconds is the fixed window used for the global_burst
// ceiling - the config only carries a flat request count (no window key),
// so it shares the same 60s window as the other rate limit classes
const globalBurstWindowSeconds = 60

// NewRateLimiter creates a new rate limiter from server.rate_limit config,
// backed by the server.cache.* store (in-process memory by default, or a
// shared valkey/redis instance when configured).
func NewRateLimiter(cfg *config.Config) *RateLimiter {
	rateLimits, _ := cfg.Server.RateLimit.Normalized()
	var store cache.Store
	switch strings.ToLower(cfg.Server.Cache.Type) {
	case "redis", "valkey":
		store = newResilientCacheStore(cache.New(cfg.Server.Cache), cache.New(config.CacheConfig{Type: "memory"}))
	default:
		if database.GetStore() != nil {
			store = newBufferedRateLimitStore(database.RateLimitStore{})
		} else if database.GetServerDB() != nil {
			store = database.RateLimitStore{}
		} else {
			store = cache.New(cfg.Server.Cache)
		}
	}

	limiter := &RateLimiter{
		read:         newClassLimiter(store, "read", rateLimits.Read.Requests, rateLimits.Read.Window),
		write:        newClassLimiter(store, "write", rateLimits.Write.Requests, rateLimits.Write.Window),
		health:       newClassLimiter(store, "health", rateLimits.Health.Requests, rateLimits.Health.Window),
		global:       newClassLimiter(store, "global", rateLimits.GlobalBurst, globalBurstWindowSeconds),
		strict:       newClassLimiter(store, "strict", rateLimits.Strict.Requests, rateLimits.Strict.Window),
		authFailures: newClassLimiter(store, "auth_failures", rateLimits.AuthFailures.Requests, rateLimits.AuthFailures.Window),
	}
	activeRateLimitersMu.Lock()
	activeRateLimiters = append(activeRateLimiters, limiter)
	activeRateLimitersMu.Unlock()
	return limiter
}

// Close flushes and closes the single shared cache store used by this limiter.
func (limiter *RateLimiter) Close() error {
	if limiter == nil || limiter.read == nil || limiter.read.store == nil {
		return nil
	}
	limiter.closeOnce.Do(func() { limiter.closeErr = limiter.read.store.Close() })
	return limiter.closeErr
}

// CloseRateLimiters flushes buffered SQLite counters after HTTP requests drain
// and before the database store closes.
func CloseRateLimiters() error {
	activeRateLimitersMu.Lock()
	limiters := activeRateLimiters
	activeRateLimiters = nil
	activeRateLimitersMu.Unlock()

	var closeErr error
	for _, limiter := range limiters {
		if err := limiter.Close(); err != nil {
			closeErr = errors.Join(closeErr, err)
		}
	}
	return closeErr
}

// RateLimitMiddleware creates a rate limiting middleware
func RateLimitMiddleware(cfg *config.Config) func(http.Handler) http.Handler {
	limiter := NewRateLimiter(cfg)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// The public MCP request is charged by this outer middleware. Its
			// in-process tool dispatch re-enters the router with this marker and
			// must not consume a second request from the same token budget.
			if isMCPDispatch(r) {
				next.ServeHTTP(w, r)
				return
			}
			if isAllowlistedRequest(r) {
				next.ServeHTTP(w, r)
				return
			}
			// Skip rate limiting for certain paths
			if shouldSkipRateLimit(r.URL.Path) {
				next.ServeHTTP(w, r)
				return
			}

			clientIP := getClientIP(r)
			now := time.Now()
			if count, reset := lockoutCount(r.Context(), limiter.authFailures.store, limiter.authFailures.prefix+clientIP, now, limiter.authFailures.window); count >= limiter.authFailures.limit {
				writeRateLimitExceeded(w, r, limiter.authFailures.limit, reset)
				return
			}
			identityKey := "ip:" + clientIP
			r = r.WithContext(context.WithValue(r.Context(), authFailureRecorderKey{}, authFailureRecorder(func() (int, time.Time, error) {
				return limiter.authFailures.hit(clientIP)
			})))
			var authenticatedCredential database.OwnerCredential
			authenticated := false
			if authHeader := strings.TrimSpace(r.Header.Get("Authorization")); authHeader != "" {
				credential, err := validateConfiguredOwnerToken(r.Context(), cfg, bearerToken(r))
				if err != nil {
					count, reset, failureErr := recordAuthenticationFailure(r)
					if failureErr != nil {
						slog.Warn("ratelimit: failed to record authentication failure", "error", failureErr)
					}
					if count >= limiter.authFailures.limit {
						writeRateLimitExceeded(w, r, limiter.authFailures.limit, reset)
						return
					}
				} else {
					authenticatedCredential = credential
					authenticated = true
					identityKey = "token:" + credential.OwnerID + ":" + strconv.FormatInt(credential.TokenID, 10)
					if credential.Kind == "operator" {
						identityKey = "token:operator"
					}
				}
			}
			if isStrictRateLimitRequest(r) {
				if authenticated && authenticatedCredential.OwnerID != "" {
					identityKey = "owner:" + authenticatedCredential.OwnerID
				} else if authenticated && authenticatedCredential.Kind == "operator" {
					identityKey = "ip:" + clientIP
				} else if credential, ok := ownerCookieCredential(r, cfg); ok {
					identityKey = "owner:" + credential.OwnerID
				}
				if allowed, _, limit, reset := limiter.strict.allow(identityKey); !allowed {
					mx := metrics.Get()
					mx.RecordRateLimitRequest("strict", "limited")
					mx.RecordRateLimitBlocked("strict")
					writeRateLimitExceeded(w, r, limit, reset)
					return
				}
				metrics.Get().RecordRateLimitRequest("strict", "allowed")
			}
			// The limit label carries only the fixed category ("global" or
			// "per_ip") - the client IP itself is unbounded cardinality and
			// belongs in the structured logs, never in a metric label.
			mx := metrics.Get()

			// Absolute ceiling across all endpoint types, checked first
			if allowed, _, limit, resetTime := limiter.global.allow(identityKey); !allowed {
				mx.RecordRateLimitRequest("global", "limited")
				mx.RecordRateLimitBlocked("global")
				writeRateLimitExceeded(w, r, limit, resetTime)
				return
			}
			mx.RecordRateLimitRequest("global", "allowed")

			var class *classLimiter
			switch {
			case isHealthCheckPath(r.URL.Path):
				class = limiter.health
			case r.Method == http.MethodGet || r.Method == http.MethodHead:
				class = limiter.read
			default:
				class = limiter.write
			}

			allowed, remaining, limit, resetTime := class.allow(identityKey)

			// Set rate limit headers (always)
			w.Header().Set("X-RateLimit-Limit", intToString(limit))
			w.Header().Set("X-RateLimit-Remaining", intToString(remaining))
			if !resetTime.IsZero() {
				w.Header().Set("X-RateLimit-Reset", intToString(int(resetTime.Unix())))
			}

			if !allowed {
				mx.RecordRateLimitRequest("per_ip", "limited")
				mx.RecordRateLimitBlocked("per_ip")
				writeRateLimitExceeded(w, r, limit, resetTime)
				return
			}
			mx.RecordRateLimitRequest("per_ip", "allowed")

			next.ServeHTTP(w, r)
		})
	}
}

func isStrictRateLimitRequest(r *http.Request) bool {
	path := strings.TrimSuffix(r.URL.Path, "/")
	method := r.Method
	if method == http.MethodPost {
		if path == "/server/preferences/token/create" || path == "/server/preferences/token/restore" {
			return true
		}
		if strings.HasSuffix(path, "/server/preferences/tokens/primary") ||
			strings.HasSuffix(path, "/server/preferences/tokens/primary/reveal") ||
			strings.HasSuffix(path, "/server/preferences/tokens/primary/rotate") ||
			strings.HasSuffix(path, "/server/preferences/token/reveal") ||
			strings.HasSuffix(path, "/server/preferences/token/rotate") ||
			(strings.Contains(path, "/server/preferences/tokens/") && (strings.HasSuffix(path, "/reveal") || strings.HasSuffix(path, "/rotate"))) {
			return true
		}
	}
	if method == http.MethodDelete && strings.HasSuffix(path, "/server/preferences/tokens/primary") {
		return true
	}
	if method == http.MethodPost && strings.Contains(path, "/server/backup/restore") {
		return true
	}
	return false
}

func ownerCookieCredential(r *http.Request, cfg *config.Config) (database.OwnerCredential, bool) {
	prefix := paths.ProjectName + "_owner_token_"
	for _, cookie := range r.Cookies() {
		if !strings.HasPrefix(cookie.Name, prefix) || cookie.Value == "" {
			continue
		}
		credential, err := validateConfiguredOwnerToken(r.Context(), cfg, cookie.Value)
		if err == nil && credential.Kind == "primary" {
			return credential, true
		}
	}
	return database.OwnerCredential{}, false
}

func lockoutCount(ctx context.Context, store cache.Store, key string, now time.Time, window time.Duration) (int, time.Time) {
	count, reset, err := store.CountWindow(ctx, key, now, window)
	if err != nil {
		slog.Warn("ratelimit: failed to read auth-failure lockout state", "error", err)
		return 0, time.Time{}
	}
	return count, reset
}

func recordAuthenticationFailure(r *http.Request) (int, time.Time, error) {
	recorder, ok := r.Context().Value(authFailureRecorderKey{}).(authFailureRecorder)
	if !ok {
		return 0, time.Time{}, nil
	}
	count, reset, err := recorder()
	if err != nil {
		slog.Warn("ratelimit: failed to record authentication failure", "error", err)
	}
	return count, reset, err
}

// writeRateLimitExceeded writes the standard 429 response - the wait time
// is conveyed only via the Retry-After header, never as a body field
func writeRateLimitExceeded(w http.ResponseWriter, r *http.Request, limit int, resetTime time.Time) {
	retryAfter := int(time.Until(resetTime).Seconds()) + 1
	w.Header().Set("Retry-After", intToString(retryAfter))
	w.Header().Set("X-RateLimit-Limit", intToString(limit))
	writeLocalizedAPIError(w, r, http.StatusTooManyRequests, "RATE_LIMITED")
}

// shouldSkipRateLimit returns true for paths that should bypass rate limiting
// entirely (static/well-known files, not endpoints)
func shouldSkipRateLimit(path string) bool {
	skipPaths := []string{
		"/robots.txt",
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
// realIPMiddleware. Requests from the dedicated I2P backend share one synthetic
// identity so they never collide with clearnet loopback traffic.
func getClientIP(r *http.Request) string {
	if isI2PBackendRequest(r) {
		return "i2p"
	}
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
