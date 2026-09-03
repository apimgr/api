package cache

import (
	"context"
	"sync"
	"time"

	"github.com/apimgr/api/src/metrics"
)

// memoryCacheName is the low-cardinality `cache` label value reported for the
// in-process store by the PART 20 cache metrics.
const memoryCacheName = "memory"

// timestampBytes is the in-memory footprint of a single time.Time entry, used
// for the cache_bytes estimate computed during the existing sweep walk.
const timestampBytes = 24

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
		m.sweep(time.Now())
	}
}

// sweep drops stale keys and refreshes the cache_size/cache_bytes gauges. The
// byte figure is an estimate accumulated during the walk the sweep already
// performs, so no extra pass over the map is needed.
func (m *memoryStore) sweep(now time.Time) {
	cutoff := now.Add(-staleAfter)
	evicted := 0
	bytes := int64(0)

	m.mu.Lock()
	for key, w := range m.windows {
		w.mu.Lock()
		held := len(w.timestamps)
		stale := held == 0 || w.timestamps[held-1].Before(cutoff)
		w.mu.Unlock()
		if stale {
			evicted += held
			delete(m.windows, key)
			continue
		}
		bytes += int64(len(key) + held*timestampBytes)
	}
	size := len(m.windows)
	m.mu.Unlock()

	mx := metrics.Get()
	for i := 0; i < evicted; i++ {
		mx.RecordCacheEviction(memoryCacheName)
	}
	mx.SetCacheSize(memoryCacheName, size)
	mx.SetCacheBytes(memoryCacheName, bytes)
}

// SlidingWindow implements Store.
func (m *memoryStore) SlidingWindow(_ context.Context, key string, now time.Time, dur time.Duration) (int, time.Time, error) {
	m.mu.Lock()
	w, ok := m.windows[key]
	if !ok {
		w = &window{timestamps: make([]time.Time, 0, 4)}
		m.windows[key] = w
	}
	size := len(m.windows)
	m.mu.Unlock()

	w.mu.Lock()
	defer w.mu.Unlock()

	windowStart := now.Add(-dur)
	held := len(w.timestamps)
	valid := w.timestamps[:0]
	for _, ts := range w.timestamps {
		if ts.After(windowStart) {
			valid = append(valid, ts)
		}
	}
	// Timestamps that fell outside the window are expired entries; entries
	// still inside it make this lookup a hit, an empty window a miss.
	expired := held - len(valid)
	hit := len(valid) > 0

	valid = append(valid, now)
	w.timestamps = valid

	m.recordLookup(hit, expired, size)

	resetAt := valid[0].Add(dur)
	return len(valid), resetAt, nil
}

// recordLookup reports one sliding-window lookup to the PART 20 cache metrics.
func (m *memoryStore) recordLookup(hit bool, expired, size int) {
	mx := metrics.Get()
	if hit {
		mx.RecordCacheHit(memoryCacheName)
	} else {
		mx.RecordCacheMiss(memoryCacheName)
	}
	for i := 0; i < expired; i++ {
		mx.RecordCacheEviction(memoryCacheName)
	}
	mx.SetCacheSize(memoryCacheName, size)
}

// Close is a no-op for the in-process store.
func (m *memoryStore) Close() error {
	return nil
}
