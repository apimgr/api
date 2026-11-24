package cache

import (
	"testing"
	"time"

	"github.com/apimgr/api/src/config"
	"github.com/stretchr/testify/assert"
)

// New must return an in-process memory store for "memory", "none", and the
// empty string, and must fall back to memory (never panic or return nil)
// for an unrecognized type, per AI.md PART 5 "never fail startup on
// invalid config".
func TestNewStoreType(t *testing.T) {
	tests := []string{"memory", "Memory", "none", "", "bogus"}
	for _, typ := range tests {
		t.Run(typ, func(t *testing.T) {
			store := New(config.CacheConfig{Type: typ})
			assert.NotNil(t, store)
			_, ok := store.(*memoryStore)
			assert.True(t, ok, "expected a memory store fallback for type %q", typ)
		})
	}
}

// New must fall back to the memory store when a valkey/redis connection
// cannot be established, rather than failing startup.
func TestNewStoreRedisUnreachableFallsBackToMemory(t *testing.T) {
	store := New(config.CacheConfig{Type: "redis", Host: "127.0.0.1", Port: 1, Timeout: "100ms"})
	assert.NotNil(t, store)
	_, ok := store.(*memoryStore)
	assert.True(t, ok, "unreachable redis must fall back to memory store")
}

// normalizeType must lowercase and pass through the configured type.
func TestNormalizeType(t *testing.T) {
	assert.Equal(t, "valkey", normalizeType("Valkey"))
	assert.Equal(t, "redis", normalizeType("REDIS"))
	assert.Equal(t, "", normalizeType(""))
}

// parseDuration must fall back to the default on empty or invalid input,
// and parse valid duration strings otherwise.
func TestParseDuration(t *testing.T) {
	assert.Equal(t, 5*time.Second, parseDuration("", 5*time.Second))
	assert.Equal(t, 5*time.Second, parseDuration("not-a-duration", 5*time.Second))
	assert.Equal(t, 30*time.Second, parseDuration("30s", 5*time.Second))
}

func TestHealthStatusAggregatesIndependentComponents(t *testing.T) {
	cacheHealth.RLock()
	original := make(map[string]string, len(cacheHealth.components))
	for key, status := range cacheHealth.components {
		original[key] = status
	}
	cacheHealth.RUnlock()
	cacheHealth.Lock()
	cacheHealth.components = make(map[string]string)
	cacheHealth.Unlock()
	t.Cleanup(func() {
		cacheHealth.Lock()
		cacheHealth.components = original
		cacheHealth.Unlock()
	})

	SetHealthStatus("test-backend", "ok")
	t.Cleanup(func() {
		SetHealthStatus("test-backend", "ok")
		SetHealthStatus("test-ratelimit", "ok")
	})
	SetHealthStatus("test-ratelimit", "degraded")
	assert.Equal(t, "degraded", HealthStatus())
	SetHealthStatus("test-backend", "error")
	assert.Equal(t, "error", HealthStatus())
	SetHealthStatus("test-backend", "ok")
	assert.Equal(t, "degraded", HealthStatus())
	SetHealthStatus("test-ratelimit", "ok")
	assert.Equal(t, "ok", HealthStatus())
}

func TestInitialExternalCacheFailureMarksHealthDegraded(t *testing.T) {
	SetHealthStatus("backend", "ok")
	t.Cleanup(func() { SetHealthStatus("backend", "ok") })
	store := New(config.CacheConfig{Type: "redis", Host: "127.0.0.1", Port: 1, Timeout: "100ms"})
	assert.NotNil(t, store)
	assert.Equal(t, "degraded", HealthStatus())
}
