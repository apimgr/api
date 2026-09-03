// Package cache implements the server.cache.* backend documented in AI.md
// PART 12 "Cache Configuration". It provides a single Store abstraction
// used for sliding-window rate-limit counters (and, in future, general
// response/session caching): "memory" (default, in-process, lost on
// restart) or "valkey"/"redis" (shared, persists across restarts).
package cache

import (
	"context"
	"log/slog"
	"time"

	"github.com/apimgr/api/src/config"
)

// Store is a sliding-window counter backend.
type Store interface {
	// SlidingWindow records a hit for key at now and returns the number of
	// hits still inside the window (now-window, now], plus the time at
	// which the oldest hit currently counted will fall out of the window
	// (used for the Retry-After / X-RateLimit-Reset calculation).
	SlidingWindow(ctx context.Context, key string, now time.Time, window time.Duration) (count int, resetAt time.Time, err error)

	// Close releases any underlying connections. Safe to call on a store
	// that was never connected.
	Close() error
}

// defaultTimeout bounds every cache backend operation when the configured
// Timeout is empty or fails to parse.
const defaultTimeout = 5 * time.Second

// New builds a Store from server.cache.* config. Type "none"/"memory"/""
// all return the in-process memory store; "valkey"/"redis" connect to the
// configured external cache. An unrecognized type, or a redis/valkey
// connection that cannot be established, warns and falls back to the
// memory store rather than failing startup, per AI.md PART 5 "never fail
// startup on invalid config".
func New(cfg config.CacheConfig) Store {
	switch normalizeType(cfg.Type) {
	case "valkey", "redis":
		store, err := newRedisStore(cfg)
		if err != nil {
			slog.Warn("cache: falling back to in-process memory store", "type", cfg.Type, "error", err)
			return newMemoryStore()
		}
		return store
	case "none", "memory", "":
		return newMemoryStore()
	default:
		slog.Warn("cache: unknown server.cache.type, using memory", "type", cfg.Type)
		return newMemoryStore()
	}
}

// normalizeType lowercases and trims the configured cache type.
func normalizeType(t string) string {
	switch t {
	case "":
		return ""
	default:
		return lowerASCII(t)
	}
}

// lowerASCII avoids pulling in strings.ToLower's full Unicode table for a
// value that is always one of a handful of ASCII config keywords.
func lowerASCII(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[i] = c + ('a' - 'A')
		}
	}
	return string(b)
}

// parseDuration parses a server.cache.* duration string, falling back to
// def (never failing) on an empty or invalid value.
func parseDuration(s string, def time.Duration) time.Duration {
	if s == "" {
		return def
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		slog.Warn("cache: invalid duration, using default", "value", s, "default", def, "error", err)
		return def
	}
	return d
}
