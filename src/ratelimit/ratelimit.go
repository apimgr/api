package ratelimit

import (
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/apimgr/api/src/database"
)

// Limiter implements sliding window rate limiting
type Limiter struct {
	enabled bool
	limits  map[string]*Limit
	mu      sync.RWMutex
}

// Limit represents rate limit configuration
type Limit struct {
	Requests int           // Max requests per window
	Window   time.Duration // Time window
}

var (
	globalLimiter *Limiter
	limiterOnce   sync.Once
)

// Get returns the singleton rate limiter
func Get() *Limiter {
	limiterOnce.Do(func() {
		globalLimiter = &Limiter{
			enabled: true,
			limits:  make(map[string]*Limit),
		}
		// Set default limits per spec
		globalLimiter.SetLimit("authenticated", 100, time.Minute)
		globalLimiter.SetLimit("unauthenticated", 20, time.Minute)
		globalLimiter.SetLimit("login", 5, 15*time.Minute)
		globalLimiter.SetLimit("password_reset", 3, time.Hour)
		globalLimiter.SetLimit("registration", 5, time.Hour)
		globalLimiter.SetLimit("upload", 10, time.Hour)
	})
	return globalLimiter
}

// SetLimit sets a rate limit for a category
func (l *Limiter) SetLimit(category string, requests int, window time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.limits[category] = &Limit{
		Requests: requests,
		Window:   window,
	}
}

// Check checks if a request should be allowed
// Returns: allowed, remaining, resetTime, error
func (l *Limiter) Check(key string, category string) (bool, int, time.Time, error) {
	if !l.enabled {
		return true, 999, time.Time{}, nil
	}

	l.mu.RLock()
	limit, exists := l.limits[category]
	l.mu.RUnlock()

	if !exists {
		// No limit configured for this category - allow
		return true, 999, time.Time{}, nil
	}

	// Get current count from database
	db := database.GetServerDB()
	if db == nil {
		// Database not available - allow (fail open for availability)
		return true, 999, time.Time{}, nil
	}

	now := time.Now()
	windowStart := now.Add(-limit.Window)

	// Get or create rate limit entry
	var count int
	var dbWindowStart time.Time

	err := db.QueryRow(`
		SELECT count, window_start FROM rate_limits WHERE key = ?
	`, key).Scan(&count, &dbWindowStart)

	if err != nil {
		// No existing entry - create new one
		_, err = db.Exec(`
			INSERT INTO rate_limits (key, count, window_start, updated_at)
			VALUES (?, 1, ?, ?)
		`, key, now, now)

		if err != nil {
			log.Printf("RateLimit: Failed to create entry: %v", err)
			return true, 999, time.Time{}, err
		}

		return true, limit.Requests - 1, now.Add(limit.Window), nil
	}

	// Check if window has expired
	if dbWindowStart.Before(windowStart) {
		// Window expired - reset counter
		_, err = db.Exec(`
			UPDATE rate_limits
			SET count = 1, window_start = ?, updated_at = ?
			WHERE key = ?
		`, now, now, key)

		if err != nil {
			log.Printf("RateLimit: Failed to reset window: %v", err)
		}

		return true, limit.Requests - 1, now.Add(limit.Window), nil
	}

	// Check if limit exceeded
	if count >= limit.Requests {
		resetTime := dbWindowStart.Add(limit.Window)
		return false, 0, resetTime, nil
	}

	// Increment counter
	_, err = db.Exec(`
		UPDATE rate_limits
		SET count = count + 1, updated_at = ?
		WHERE key = ?
	`, now, key)

	if err != nil {
		log.Printf("RateLimit: Failed to increment counter: %v", err)
	}

	remaining := limit.Requests - (count + 1)
	resetTime := dbWindowStart.Add(limit.Window)

	return true, remaining, resetTime, nil
}

// Middleware is HTTP middleware that enforces rate limiting
func Middleware(category string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Use IP address as key
			key := r.RemoteAddr + ":" + category

			allowed, remaining, resetTime, err := Get().Check(key, category)

			// Add rate limit headers
			if !resetTime.IsZero() {
				w.Header().Set("X-RateLimit-Limit", fmt.Sprintf("%d", Get().GetLimit(category)))
				w.Header().Set("X-RateLimit-Remaining", fmt.Sprintf("%d", remaining))
				w.Header().Set("X-RateLimit-Reset", fmt.Sprintf("%d", resetTime.Unix()))
			}

			if err != nil {
				// Log error but allow request (fail open)
				log.Printf("RateLimit: Check failed: %v", err)
				next.ServeHTTP(w, r)
				return
			}

			if !allowed {
				// Rate limit exceeded
				w.Header().Set("Retry-After", fmt.Sprintf("%d", int(time.Until(resetTime).Seconds())))
				w.WriteHeader(http.StatusTooManyRequests)
				w.Write([]byte(fmt.Sprintf(`{"error":"Rate limit exceeded","status":429,"retry_after":%d}`, int(time.Until(resetTime).Seconds()))))
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// GetLimit returns the request limit for a category
func (l *Limiter) GetLimit(category string) int {
	l.mu.RLock()
	defer l.mu.RUnlock()

	if limit, exists := l.limits[category]; exists {
		return limit.Requests
	}
	return 100 // Default
}

// Enable enables rate limiting
func (l *Limiter) Enable() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.enabled = true
}

// Disable disables rate limiting
func (l *Limiter) Disable() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.enabled = false
}

// CleanupOldEntries removes old rate limit entries
// Should be called periodically to prevent table growth
func CleanupOldEntries() error {
	db := database.GetServerDB()
	if db == nil {
		return nil
	}

	// Delete entries older than 24 hours
	cutoff := time.Now().Add(-24 * time.Hour)

	result, err := db.Exec(`
		DELETE FROM rate_limits WHERE window_start < ?
	`, cutoff)

	if err != nil {
		return err
	}

	count, _ := result.RowsAffected()
	if count > 0 {
		log.Printf("RateLimit: Cleaned %d old entries", count)
	}

	return nil
}
