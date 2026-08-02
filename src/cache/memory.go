package cache

import (
	"context"
	"sync"
	"time"
)

// memoryStore is the default in-process Store: a sliding window of
// timestamps per key, guarded by a per-key mutex. Lost on restart, not
// shared across instances - the documented tradeoff of server.cache.type:
// memory (or none) versus valkey/redis.
type memoryStore struct {
	mu      sync.Mutex
	windows map[string]*window
}

// window tracks the recent hit timestamps for a single key.
type window struct {
	mu         sync.Mutex
	timestamps []time.Time
}

// staleAfter bounds how long an idle key's window is kept before the
// background sweep reclaims it, preventing unbounded growth of the
// in-process map for clients that stop sending requests.
const staleAfter = 10 * time.Minute

// newMemoryStore creates an empty in-process store and starts its
// background sweep of stale, idle keys.
func newMemoryStore() *memoryStore {
	m := &memoryStore{windows: make(map[string]*window)}
	go m.sweepLoop()
	return m
}

// sweepLoop periodically removes keys with no timestamps inside staleAfter,
// mirroring the pre-cache.Store cleanup behaviour of the in-process limiter.
func (m *memoryStore) sweepLoop() {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
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
	}
}

// SlidingWindow implements Store.
func (m *memoryStore) SlidingWindow(_ context.Context, key string, now time.Time, dur time.Duration) (int, time.Time, error) {
	m.mu.Lock()
	w, ok := m.windows[key]
	if !ok {
		w = &window{timestamps: make([]time.Time, 0, 4)}
		m.windows[key] = w
	}
	m.mu.Unlock()

	w.mu.Lock()
	defer w.mu.Unlock()

	windowStart := now.Add(-dur)
	valid := w.timestamps[:0]
	for _, ts := range w.timestamps {
		if ts.After(windowStart) {
			valid = append(valid, ts)
		}
	}
	valid = append(valid, now)
	w.timestamps = valid

	resetAt := valid[0].Add(dur)
	return len(valid), resetAt, nil
}

// Close is a no-op for the in-process store.
func (m *memoryStore) Close() error {
	return nil
}
