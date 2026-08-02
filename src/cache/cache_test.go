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
