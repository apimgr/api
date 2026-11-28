package cache

import (
	"context"
	"testing"
	"time"

	"github.com/apimgr/api/src/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMemoryValueStorePrefixCopyExpiryAndDelete(t *testing.T) {
	store := newMemoryStore(config.CacheConfig{Prefix: "test:", TTL: "1h"})
	t.Cleanup(func() { require.NoError(t, store.Close()) })
	ctx := context.Background()
	original := []byte("value")
	require.NoError(t, store.SetValue(ctx, "key", original, time.Minute))
	original[0] = 'X'

	value, found, err := store.GetValue(ctx, "key")
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, []byte("value"), value)
	value[0] = 'Y'
	value, found, err = store.GetValue(ctx, "key")
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, []byte("value"), value)

	store.mu.Lock()
	entry := store.values["test:key"]
	entry.expiresAt = time.Now().Add(-time.Second)
	store.values["test:key"] = entry
	store.mu.Unlock()
	value, found, err = store.GetValue(ctx, "key")
	require.NoError(t, err)
	assert.False(t, found)
	assert.Nil(t, value)

	require.NoError(t, store.SetValue(ctx, "key", []byte("again"), time.Minute))
	require.NoError(t, store.DeleteValue(ctx, "key"))
	_, found, err = store.GetValue(ctx, "key")
	require.NoError(t, err)
	assert.False(t, found)
}

func TestMemoryValueStoreRejectsOversizedEntryAndHonorsCancellation(t *testing.T) {
	store := newMemoryStore()
	t.Cleanup(func() { require.NoError(t, store.Close()) })
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	assert.ErrorIs(t, store.SetValue(ctx, "key", []byte("value"), time.Minute), context.Canceled)
	_, _, err := store.GetValue(ctx, "key")
	assert.ErrorIs(t, err, context.Canceled)
	assert.ErrorIs(t, store.DeleteValue(ctx, "key"), context.Canceled)
	assert.ErrorIs(t, store.SetValue(context.Background(), "large", make([]byte, maxValueCacheBytes+1), time.Minute), ErrValueTooLarge)
	assert.ErrorIs(t, store.SetValue(context.Background(), "", []byte("value"), time.Minute), ErrInvalidKey)
}

func TestNewValueStoreDisabledDoesNotRetainValues(t *testing.T) {
	store := NewValueStore(config.CacheConfig{Type: "none"})
	require.NoError(t, store.SetValue(context.Background(), "key", []byte("value"), time.Hour))
	value, found, err := store.GetValue(context.Background(), "key")
	require.NoError(t, err)
	assert.False(t, found)
	assert.Nil(t, value)
}
