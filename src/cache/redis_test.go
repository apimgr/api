package cache

import (
	"testing"
	"time"

	"github.com/apimgr/api/src/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// redisOptions must prefer cfg.URL over discrete host/port fields when
// both are set, per the documented "url takes precedence" rule.
func TestRedisOptionsURLPrecedence(t *testing.T) {
	opts, err := redisOptions(config.CacheConfig{
		URL:  "redis://user:pass@cache.example.com:6380/2",
		Host: "ignored.example.com",
		Port: 9999,
	}, 5*time.Second)
	require.NoError(t, err)
	assert.Equal(t, "cache.example.com:6380", opts.Addr)
	assert.Equal(t, "user", opts.Username)
	assert.Equal(t, "pass", opts.Password)
	assert.Equal(t, 2, opts.DB)
}

// redisOptions must reject a malformed URL rather than silently ignoring it.
func TestRedisOptionsInvalidURL(t *testing.T) {
	_, err := redisOptions(config.CacheConfig{URL: "not a url::"}, 5*time.Second)
	assert.Error(t, err)
}

// redisOptions must fall back to localhost:6379 when no host/port/url is
// configured, matching the documented server.cache.* defaults.
func TestRedisOptionsDefaults(t *testing.T) {
	opts, err := redisOptions(config.CacheConfig{}, 5*time.Second)
	require.NoError(t, err)
	assert.Equal(t, "localhost:6379", opts.Addr)
}

// redisOptions must build a TLS config only when cfg.TLS is set, honoring
// TLSSkipVerify as an explicit operator opt-in.
func TestRedisOptionsTLS(t *testing.T) {
	opts, err := redisOptions(config.CacheConfig{Host: "cache", TLS: true, TLSSkipVerify: true}, 5*time.Second)
	require.NoError(t, err)
	require.NotNil(t, opts.TLSConfig)
	assert.True(t, opts.TLSConfig.InsecureSkipVerify)

	opts, err = redisOptions(config.CacheConfig{Host: "cache"}, 5*time.Second)
	require.NoError(t, err)
	assert.Nil(t, opts.TLSConfig)
}

// newRedisStore must return an error (never panic) when the configured
// backend is unreachable, so callers can fall back to the memory store.
func TestNewRedisStoreUnreachable(t *testing.T) {
	_, err := newRedisStore(config.CacheConfig{Type: "redis", Host: "127.0.0.1", Port: 1, Timeout: "100ms"})
	assert.Error(t, err)
}
