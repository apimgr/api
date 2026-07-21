package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/apimgr/api/src/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// classLimiter.allow must allow up to limit requests within the window,
// block the next one with remaining=0 and the class limit echoed back,
// and allow again once the window has elapsed.
func TestClassLimiterAllow(t *testing.T) {
	cl := newClassLimiter(2, 1) // 2 requests per 1 second

	allowed, remaining, limit, _ := cl.allow("1.2.3.4")
	assert.True(t, allowed)
	assert.Equal(t, 1, remaining)
	assert.Equal(t, 2, limit)

	allowed, remaining, limit, _ = cl.allow("1.2.3.4")
	assert.True(t, allowed)
	assert.Equal(t, 0, remaining)
	assert.Equal(t, 2, limit)

	allowed, remaining, limit, resetTime := cl.allow("1.2.3.4")
	assert.False(t, allowed)
	assert.Equal(t, 0, remaining)
	assert.Equal(t, 2, limit)
	assert.False(t, resetTime.IsZero())

	time.Sleep(1100 * time.Millisecond)

	allowed, _, _, _ = cl.allow("1.2.3.4")
	assert.True(t, allowed, "request should be allowed again after window elapses")
}

// classLimiter.allow must track distinct clients independently - one
// client hitting its limit must not affect another client's quota.
func TestClassLimiterAllowPerClient(t *testing.T) {
	cl := newClassLimiter(1, 60)

	allowed, _, _, _ := cl.allow("1.1.1.1")
	assert.True(t, allowed)

	allowed, _, _, _ = cl.allow("1.1.1.1")
	assert.False(t, allowed, "same client should be blocked on 2nd request")

	allowed, _, _, _ = cl.allow("2.2.2.2")
	assert.True(t, allowed, "different client should have its own quota")
}

// classLimiter.allow must be safe for concurrent use by many goroutines
// hitting the same client key, and must never admit more than limit
// requests within the window regardless of concurrency.
func TestClassLimiterAllowConcurrent(t *testing.T) {
	limit := 10
	cl := newClassLimiter(limit, 60)

	var wg sync.WaitGroup
	var mu sync.Mutex
	allowedCount := 0

	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			allowed, _, _, _ := cl.allow("concurrent-client")
			if allowed {
				mu.Lock()
				allowedCount++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()

	assert.Equal(t, limit, allowedCount)
}

// intToString must convert zero, positive, negative, and multi-digit
// integers without using strconv.
func TestIntToString(t *testing.T) {
	tests := []struct {
		in   int
		want string
	}{
		{0, "0"},
		{1, "1"},
		{9, "9"},
		{10, "10"},
		{429, "429"},
		{-1, "-1"},
		{-42, "-42"},
		{1234567, "1234567"},
	}

	for _, tt := range tests {
		t.Run(strconv.Itoa(tt.in), func(t *testing.T) {
			assert.Equal(t, tt.want, intToString(tt.in))
		})
	}
}

// getClientIP must strip the port from RemoteAddr, keeping only the host
// portion (including for IPv6 addresses with a bracketed host).
func TestGetClientIP(t *testing.T) {
	tests := []struct {
		remoteAddr string
		want       string
	}{
		{"1.2.3.4:5678", "1.2.3.4"},
		{"1.2.3.4", "1.2.3.4"},
		{"[::1]:5678", "[::1]"},
	}

	for _, tt := range tests {
		t.Run(tt.remoteAddr, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.RemoteAddr = tt.remoteAddr
			assert.Equal(t, tt.want, getClientIP(req))
		})
	}
}

// shouldSkipRateLimit must match exact skip paths and prefix-based skip
// paths, and must not match unrelated paths.
func TestShouldSkipRateLimit(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{"/robots.txt", true},
		{"/security.txt", true},
		{"/favicon.ico", true},
		{"/.well-known/acme-challenge/token", true},
		{"/static/css/app.css", true},
		{"/api/v1/network/caller", false},
		{"/", false},
		{"/staticfoo", false},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			assert.Equal(t, tt.want, shouldSkipRateLimit(tt.path))
		})
	}
}

// writeRateLimitExceeded must write a 429 with Retry-After and
// X-RateLimit-Limit headers and the standard RATE_LIMITED JSON envelope.
// Per the Public Endpoint Safety Principle (AI.md PART 11), the body must
// not leak the specific rate-limit threshold or wait duration - only a
// generic 429 with the retry hint conveyed via the Retry-After header.
func TestWriteRateLimitExceeded(t *testing.T) {
	w := httptest.NewRecorder()
	resetTime := time.Now().Add(5 * time.Second)

	writeRateLimitExceeded(w, 10, resetTime)

	assert.Equal(t, http.StatusTooManyRequests, w.Code)
	assert.Equal(t, "application/json", w.Header().Get("Content-Type"))
	assert.Equal(t, "10", w.Header().Get("X-RateLimit-Limit"))
	require.NotEmpty(t, w.Header().Get("Retry-After"))

	var body map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Equal(t, false, body["ok"])
	assert.Equal(t, "RATE_LIMITED", body["error"])
	_, hasWait := body["wait"]
	assert.False(t, hasWait)
	_, hasRetryAfter := body["retry_after"]
	assert.False(t, hasRetryAfter)
}

// testRateLimitConfig builds a config.Config with small, deterministic
// rate limit classes suitable for exercising the middleware without long
// test sleeps.
func testRateLimitConfig(enabled bool, readLimit, writeLimit, healthLimit, globalBurst int) *config.Config {
	cfg := &config.Config{}
	cfg.Server.RateLimit = config.RateLimitConfig{
		Enabled:     enabled,
		Read:        config.RateLimitClassConfig{Requests: readLimit, Window: 60},
		Write:       config.RateLimitClassConfig{Requests: writeLimit, Window: 60},
		Health:      config.RateLimitClassConfig{Requests: healthLimit, Window: 60},
		GlobalBurst: globalBurst,
	}
	return cfg
}

// RateLimitMiddleware must pass every request through when disabled,
// must skip rate limiting entirely for well-known/static paths, must
// classify GET/HEAD as "read" and other methods as "write", must
// classify health-check paths separately, must set X-RateLimit-* headers
// on allowed requests, and must return 429 once a class's limit is
// exceeded.
func TestRateLimitMiddleware(t *testing.T) {
	okHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	t.Run("disabled: never limits", func(t *testing.T) {
		cfg := testRateLimitConfig(false, 1, 1, 1, 100)
		mw := RateLimitMiddleware(cfg)(okHandler)

		for i := 0; i < 5; i++ {
			req := httptest.NewRequest(http.MethodGet, "/x", nil)
			req.RemoteAddr = "9.9.9.9:1"
			w := httptest.NewRecorder()
			mw.ServeHTTP(w, req)
			assert.Equal(t, http.StatusOK, w.Code)
		}
	})

	t.Run("static path bypasses rate limit entirely", func(t *testing.T) {
		cfg := testRateLimitConfig(true, 1, 1, 1, 100)
		mw := RateLimitMiddleware(cfg)(okHandler)

		for i := 0; i < 5; i++ {
			req := httptest.NewRequest(http.MethodGet, "/static/app.css", nil)
			req.RemoteAddr = "9.9.9.8:1"
			w := httptest.NewRecorder()
			mw.ServeHTTP(w, req)
			assert.Equal(t, http.StatusOK, w.Code)
			assert.Empty(t, w.Header().Get("X-RateLimit-Limit"))
		}
	})

	t.Run("read class limited after N GET requests, headers set", func(t *testing.T) {
		cfg := testRateLimitConfig(true, 1, 1, 1, 100)
		mw := RateLimitMiddleware(cfg)(okHandler)

		req1 := httptest.NewRequest(http.MethodGet, "/api/v1/thing", nil)
		req1.RemoteAddr = "9.9.9.1:1"
		w1 := httptest.NewRecorder()
		mw.ServeHTTP(w1, req1)
		assert.Equal(t, http.StatusOK, w1.Code)
		assert.Equal(t, "1", w1.Header().Get("X-RateLimit-Limit"))
		assert.Equal(t, "0", w1.Header().Get("X-RateLimit-Remaining"))

		req2 := httptest.NewRequest(http.MethodGet, "/api/v1/thing", nil)
		req2.RemoteAddr = "9.9.9.1:1"
		w2 := httptest.NewRecorder()
		mw.ServeHTTP(w2, req2)
		assert.Equal(t, http.StatusTooManyRequests, w2.Code)

		var body map[string]interface{}
		require.NoError(t, json.Unmarshal(w2.Body.Bytes(), &body))
		assert.Equal(t, "RATE_LIMITED", body["error"])
	})

	t.Run("write class tracked independently from read class", func(t *testing.T) {
		cfg := testRateLimitConfig(true, 1, 1, 1, 100)
		mw := RateLimitMiddleware(cfg)(okHandler)

		getReq := httptest.NewRequest(http.MethodGet, "/api/v1/thing", nil)
		getReq.RemoteAddr = "9.9.9.2:1"
		mw.ServeHTTP(httptest.NewRecorder(), getReq)

		postReq := httptest.NewRequest(http.MethodPost, "/api/v1/thing", nil)
		postReq.RemoteAddr = "9.9.9.2:1"
		w := httptest.NewRecorder()
		mw.ServeHTTP(w, postReq)

		assert.Equal(t, http.StatusOK, w.Code, "write quota must be independent of read quota")
	})

	t.Run("health path uses its own class", func(t *testing.T) {
		cfg := testRateLimitConfig(true, 0, 0, 1, 100)
		mw := RateLimitMiddleware(cfg)(okHandler)

		req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
		req.RemoteAddr = "9.9.9.3:1"
		w := httptest.NewRecorder()
		mw.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code, "health class has its own quota, distinct from the (zero) read quota")
	})

	t.Run("global burst ceiling blocks even when class quota remains", func(t *testing.T) {
		cfg := testRateLimitConfig(true, 100, 100, 100, 1)
		mw := RateLimitMiddleware(cfg)(okHandler)

		req1 := httptest.NewRequest(http.MethodGet, "/api/v1/a", nil)
		req1.RemoteAddr = "9.9.9.4:1"
		mw.ServeHTTP(httptest.NewRecorder(), req1)

		req2 := httptest.NewRequest(http.MethodGet, "/api/v1/b", nil)
		req2.RemoteAddr = "9.9.9.4:1"
		w2 := httptest.NewRecorder()
		mw.ServeHTTP(w2, req2)

		assert.Equal(t, http.StatusTooManyRequests, w2.Code, "global_burst=1 must block the 2nd request even though read=100")
	})
}
