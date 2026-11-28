package cache

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/apimgr/api/src/config"
	"github.com/apimgr/api/src/metrics"
)

// memoryCacheName is the low-cardinality `cache` label value reported for the
// in-process store by the PART 20 cache metrics.
const memoryCacheName = "memory"

// timestampBytes is the in-memory footprint of a single time.Time entry, used
// for the cache_bytes estimate computed during the existing sweep walk.
const timestampBytes = 24

const (
	maxValueCacheEntries = 10000
	maxValueCacheBytes   = 64 * 1024 * 1024
	maxValueCacheKeySize = 1024
)

var (
	ErrValueTooLarge = errors.New("cache value exceeds the configured size limit")
	ErrInvalidKey    = errors.New("cache key must contain 1 to 1024 bytes")
)

// memoryStore keeps sliding-window timestamps and bounded TTL values in
// process memory. Both are lost on restart and not shared across instances,
// the documented tradeoff of server.cache.type: memory (or none) versus
// valkey/redis. Rate-limit counters remain in the database for memory mode.
type memoryStore struct {
	mu         sync.Mutex
	windows    map[string]*window
	values     map[string]memoryValue
	valueBytes int
	prefix     string
	defaultTTL time.Duration
	stop       chan struct{}
	done       chan struct{}
	closeOnce  sync.Once
}

type memoryValue struct {
	data      []byte
	expiresAt time.Time
	lastUsed  time.Time
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
func newMemoryStore(configs ...config.CacheConfig) *memoryStore {
	var cfg config.CacheConfig
	if len(configs) > 0 {
		cfg = configs[0]
	}
	m := &memoryStore{
		windows:    make(map[string]*window),
		values:     make(map[string]memoryValue),
		prefix:     cfg.Prefix,
		defaultTTL: parseDuration(cfg.TTL, time.Hour),
		stop:       make(chan struct{}),
		done:       make(chan struct{}),
	}
	go m.sweepLoop()
	return m
}

// sweepLoop periodically removes keys with no timestamps inside staleAfter,
// mirroring the pre-cache.Store cleanup behaviour of the in-process limiter.
func (m *memoryStore) sweepLoop() {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()
	defer close(m.done)

	for {
		select {
		case <-m.stop:
			return
		case now := <-ticker.C:
			m.sweep(now)
		}
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
	for key, value := range m.values {
		if !value.expiresAt.After(now) {
			m.valueBytes -= len(value.data)
			delete(m.values, key)
			evicted++
		}
	}
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
	size := len(m.windows) + len(m.values)
	bytes += int64(m.valueBytes)
	m.mu.Unlock()

	mx := metrics.GetMetrics()
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

// CountWindow reads and prunes a window without recording a hit.
func (m *memoryStore) CountWindow(_ context.Context, key string, now time.Time, dur time.Duration) (int, time.Time, error) {
	m.mu.Lock()
	w := m.windows[key]
	m.mu.Unlock()
	if w == nil {
		return 0, time.Time{}, nil
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	windowStart := now.Add(-dur)
	valid := w.timestamps[:0]
	for _, ts := range w.timestamps {
		if ts.After(windowStart) {
			valid = append(valid, ts)
		}
	}
	w.timestamps = valid
	if len(valid) == 0 {
		return 0, time.Time{}, nil
	}
	return len(valid), valid[0].Add(dur), nil
}

// recordLookup reports one sliding-window lookup to the PART 20 cache metrics.
func (m *memoryStore) recordLookup(hit bool, expired, size int) {
	debugCacheLog(memoryCacheName, hit, expired, size)
	mx := metrics.GetMetrics()
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

func (m *memoryStore) GetValue(ctx context.Context, key string) ([]byte, bool, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	if err := validateValueKey(key); err != nil {
		return nil, false, err
	}
	now := time.Now()
	fullKey := m.prefix + key
	m.mu.Lock()
	value, ok := m.values[fullKey]
	expired := ok && !value.expiresAt.After(now)
	if expired {
		m.valueBytes -= len(value.data)
		delete(m.values, fullKey)
		ok = false
	}
	if ok {
		value.lastUsed = now
		m.values[fullKey] = value
	}
	size := len(m.windows) + len(m.values)
	m.mu.Unlock()
	m.recordValueLookup(ok, size)
	if expired {
		metrics.GetMetrics().RecordCacheEviction(memoryCacheName)
	}
	if !ok {
		return nil, false, nil
	}
	return append([]byte(nil), value.data...), true, nil
}

func (m *memoryStore) SetValue(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := validateValueKey(key); err != nil {
		return err
	}
	if len(value) > maxValueCacheBytes {
		return ErrValueTooLarge
	}
	if ttl <= 0 {
		ttl = m.defaultTTL
	}
	now := time.Now()
	fullKey := m.prefix + key
	copyOfValue := append([]byte(nil), value...)
	m.mu.Lock()
	if old, ok := m.values[fullKey]; ok {
		m.valueBytes -= len(old.data)
	}
	m.values[fullKey] = memoryValue{data: copyOfValue, expiresAt: now.Add(ttl), lastUsed: now}
	m.valueBytes += len(copyOfValue)
	for len(m.values) > maxValueCacheEntries || m.valueBytes > maxValueCacheBytes {
		m.evictOldestValueLocked(fullKey)
	}
	size := len(m.windows) + len(m.values)
	bytes := m.valueBytes
	m.mu.Unlock()
	metrics.GetMetrics().SetCacheSize(memoryCacheName, size)
	metrics.GetMetrics().SetCacheBytes(memoryCacheName, int64(bytes))
	return nil
}

func (m *memoryStore) DeleteValue(ctx context.Context, key string) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := validateValueKey(key); err != nil {
		return err
	}
	fullKey := m.prefix + key
	m.mu.Lock()
	if value, ok := m.values[fullKey]; ok {
		m.valueBytes -= len(value.data)
		delete(m.values, fullKey)
	}
	size := len(m.windows) + len(m.values)
	bytes := m.valueBytes
	m.mu.Unlock()
	metrics.GetMetrics().SetCacheSize(memoryCacheName, size)
	metrics.GetMetrics().SetCacheBytes(memoryCacheName, int64(bytes))
	return nil
}

func (m *memoryStore) evictOldestValueLocked(except string) {
	oldestKey := ""
	var oldest time.Time
	for key, value := range m.values {
		if key == except {
			continue
		}
		if oldestKey == "" || value.lastUsed.Before(oldest) {
			oldestKey, oldest = key, value.lastUsed
		}
	}
	if oldestKey == "" {
		value := m.values[except]
		m.valueBytes -= len(value.data)
		delete(m.values, except)
		return
	}
	m.valueBytes -= len(m.values[oldestKey].data)
	delete(m.values, oldestKey)
	metrics.GetMetrics().RecordCacheEviction(memoryCacheName)
}

func (m *memoryStore) recordValueLookup(hit bool, size int) {
	mx := metrics.GetMetrics()
	if hit {
		mx.RecordCacheHit(memoryCacheName)
	} else {
		mx.RecordCacheMiss(memoryCacheName)
	}
	mx.SetCacheSize(memoryCacheName, size)
	m.mu.Lock()
	bytes := m.valueBytes
	m.mu.Unlock()
	mx.SetCacheBytes(memoryCacheName, int64(bytes))
}

// Close stops the in-process cleanup goroutine and waits for it to exit.
func (m *memoryStore) Close() error {
	m.closeOnce.Do(func() { close(m.stop) })
	<-m.done
	return nil
}
