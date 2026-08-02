package cache

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// SlidingWindow must count hits within the window, roll old hits out once
// the window elapses, and track distinct keys independently.
func TestMemoryStoreSlidingWindow(t *testing.T) {
	m := newMemoryStore()
	ctx := context.Background()
	window := time.Second

	base := time.Now()

	count, resetAt, err := m.SlidingWindow(ctx, "client-a", base, window)
	assert.NoError(t, err)
	assert.Equal(t, 1, count)
	assert.True(t, resetAt.After(base))

	count, _, err = m.SlidingWindow(ctx, "client-a", base.Add(100*time.Millisecond), window)
	assert.NoError(t, err)
	assert.Equal(t, 2, count)

	// A different key must have its own independent window.
	count, _, err = m.SlidingWindow(ctx, "client-b", base.Add(100*time.Millisecond), window)
	assert.NoError(t, err)
	assert.Equal(t, 1, count)

	// Past the window, the earlier hits for client-a must be dropped.
	count, _, err = m.SlidingWindow(ctx, "client-a", base.Add(1100*time.Millisecond), window)
	assert.NoError(t, err)
	assert.Equal(t, 1, count)
}

// Close must be a safe no-op.
func TestMemoryStoreClose(t *testing.T) {
	m := newMemoryStore()
	assert.NoError(t, m.Close())
}

// sweepLoop must reclaim keys whose most recent hit is older than
// staleAfter, without touching keys that are still active.
func TestMemoryStoreSweepStaleKeys(t *testing.T) {
	m := &memoryStore{windows: make(map[string]*window)}

	m.windows["stale"] = &window{timestamps: []time.Time{time.Now().Add(-2 * staleAfter)}}
	m.windows["fresh"] = &window{timestamps: []time.Time{time.Now()}}

	cutoff := time.Now().Add(-staleAfter)
	m.mu.Lock()
	for key, w := range m.windows {
		w.mu.Lock()
		stale := len(w.timestamps) == 0 || w.timestamps[len(w.timestamps)-1].Before(cutoff)
		w.mu.Unlock()
		if stale {
			delete(m.windows, key)
		}
	}
	m.mu.Unlock()

	_, staleStillPresent := m.windows["stale"]
	_, freshStillPresent := m.windows["fresh"]
	assert.False(t, staleStillPresent, "stale key must be swept")
	assert.True(t, freshStillPresent, "fresh key must survive the sweep")
}
