package server

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/apimgr/api/src/cache"
	"github.com/apimgr/api/src/config"
	"github.com/apimgr/api/src/database"
)

type memoryRateLimitBackend struct {
	mu       sync.Mutex
	counters map[string]database.RateLimitCounter
	failLoad bool
	failNext bool
	batches  int
	flushed  chan struct{}
}

type switchableRateLimitCache struct {
	cache.Store
	mu   sync.Mutex
	fail bool
}

func rateLimitTestTime() time.Time {
	return time.Now().UTC().Truncate(time.Second)
}

func (store *switchableRateLimitCache) SlidingWindow(ctx context.Context, key string, now time.Time, window time.Duration) (int, time.Time, error) {
	store.mu.Lock()
	fail := store.fail
	store.mu.Unlock()
	if fail {
		return 0, time.Time{}, errors.New("cache unavailable")
	}
	return store.Store.SlidingWindow(ctx, key, now, window)
}

func (store *switchableRateLimitCache) CountWindow(ctx context.Context, key string, now time.Time, window time.Duration) (int, time.Time, error) {
	store.mu.Lock()
	fail := store.fail
	store.mu.Unlock()
	if fail {
		return 0, time.Time{}, errors.New("cache unavailable")
	}
	return store.Store.CountWindow(ctx, key, now, window)
}

func (backend *memoryRateLimitBackend) LoadCounter(_ context.Context, key string) (database.RateLimitCounter, bool, error) {
	backend.mu.Lock()
	defer backend.mu.Unlock()
	if backend.failLoad {
		return database.RateLimitCounter{}, false, errors.New("temporary database error")
	}
	counter, ok := backend.counters[key]
	return counter, ok, nil
}

func (backend *memoryRateLimitBackend) RecordBatch(_ context.Context, counters []database.RateLimitCounter) error {
	backend.mu.Lock()
	defer backend.mu.Unlock()
	if backend.failNext {
		backend.failNext = false
		return errors.New("temporary database error")
	}
	if backend.counters == nil {
		backend.counters = make(map[string]database.RateLimitCounter)
	}
	backend.batches++
	for _, counter := range counters {
		backend.counters[counter.Key] = counter
	}
	if backend.flushed != nil {
		select {
		case backend.flushed <- struct{}{}:
		default:
		}
	}
	return nil
}

func TestBufferedRateLimitStoreFlushesSnapshotsOnClose(t *testing.T) {
	backend := &memoryRateLimitBackend{}
	store := newBufferedRateLimitStore(backend)
	now := rateLimitTestTime()
	for _, key := range []string{"read:ip:192.0.2.1", "write:ip:192.0.2.1"} {
		if _, _, err := store.SlidingWindow(context.Background(), key, now, time.Minute); err != nil {
			t.Fatalf("record in-memory hit: %v", err)
		}
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close buffered store: %v", err)
	}
	backend.mu.Lock()
	defer backend.mu.Unlock()
	if backend.batches != 1 {
		t.Fatalf("database batches = %d, want 1", backend.batches)
	}
	if len(backend.counters) != 2 {
		t.Fatalf("persisted counter snapshots = %d, want 2", len(backend.counters))
	}
}

func TestBufferedRateLimitStoreFlushesPeriodically(t *testing.T) {
	backend := &memoryRateLimitBackend{flushed: make(chan struct{}, 1)}
	store := newBufferedRateLimitStoreWithInterval(backend, 5*time.Millisecond)
	defer func() { _ = store.Close() }()
	if _, _, err := store.SlidingWindow(context.Background(), "read:ip:192.0.2.1", time.Now(), time.Minute); err != nil {
		t.Fatalf("record in-memory hit: %v", err)
	}
	select {
	case <-backend.flushed:
	case <-time.After(time.Second):
		t.Fatal("periodic counter batch was not flushed")
	}
}

func TestBufferedRateLimitStoreRestoresAndContinuesPersistedWindow(t *testing.T) {
	backend := &memoryRateLimitBackend{}
	now := rateLimitTestTime()
	first := newBufferedRateLimitStore(backend)
	if count, _, err := first.SlidingWindow(context.Background(), "read:ip:192.0.2.1", now, time.Minute); err != nil || count != 1 {
		t.Fatalf("first hit = %d, %v; want 1, nil", count, err)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("close first store: %v", err)
	}

	second := newBufferedRateLimitStore(backend)
	defer func() { _ = second.Close() }()
	if count, _, err := second.CountWindow(context.Background(), "read:ip:192.0.2.1", now.Add(10*time.Second), time.Minute); err != nil || count != 1 {
		t.Fatalf("restored count = %d, %v; want 1, nil", count, err)
	}
	if count, _, err := second.SlidingWindow(context.Background(), "read:ip:192.0.2.1", now.Add(10*time.Second), time.Minute); err != nil || count != 2 {
		t.Fatalf("continued count = %d, %v; want 2, nil", count, err)
	}
}

func TestBufferedRateLimitStoreFallsBackToEnforcingInMemoryCounters(t *testing.T) {
	backend := &memoryRateLimitBackend{failLoad: true}
	store := newBufferedRateLimitStore(backend)
	defer func() { _ = store.Close() }()

	now := rateLimitTestTime()
	for want := 1; want <= 2; want++ {
		count, _, err := store.SlidingWindow(context.Background(), "read:ip:192.0.2.1", now.Add(time.Duration(want)*time.Second), time.Minute)
		if err != nil || count != want {
			t.Fatalf("in-memory fallback count = %d, %v; want %d, nil", count, err, want)
		}
	}
}

func TestBufferedRateLimitStoreRequeuesFailedBatch(t *testing.T) {
	backend := &memoryRateLimitBackend{failNext: true}
	store := newBufferedRateLimitStore(backend)
	defer func() { _ = store.Close() }()

	now := rateLimitTestTime()
	if _, _, err := store.SlidingWindow(context.Background(), "read:ip:192.0.2.1", now, time.Minute); err != nil {
		t.Fatalf("record in-memory hit: %v", err)
	}
	if err := store.flush(); err == nil {
		t.Fatal("first flush error = nil, want temporary failure")
	}
	store.mu.Lock()
	pending := len(store.pending)
	store.mu.Unlock()
	if pending != 1 {
		t.Fatalf("queued snapshots after failed flush = %d, want 1", pending)
	}
	if err := store.flush(); err != nil {
		t.Fatalf("retry snapshot flush: %v", err)
	}
	backend.mu.Lock()
	defer backend.mu.Unlock()
	if backend.batches != 1 || len(backend.counters) != 1 {
		t.Fatalf("successful persistence = %d batches, %d counters; want 1, 1", backend.batches, len(backend.counters))
	}
}

func TestResilientCacheStoreKeepsMirroredCountsOnOutage(t *testing.T) {
	cache.SetHealthStatus("ratelimit", "ok")
	t.Cleanup(func() { cache.SetHealthStatus("ratelimit", "ok") })
	primary := &switchableRateLimitCache{Store: cache.New(config.CacheConfig{Type: "memory"})}
	fallback := cache.New(config.CacheConfig{Type: "memory"})
	store := newResilientCacheStore(primary, fallback)
	defer func() { _ = store.Close() }()
	now := rateLimitTestTime()
	key := "auth_failures:ip:192.0.2.1"
	for want := 1; want <= 2; want++ {
		count, _, err := store.SlidingWindow(context.Background(), key, now.Add(time.Duration(want)*time.Second), time.Minute)
		if err != nil || count != want {
			t.Fatalf("external cache count = %d, %v; want %d, nil", count, err, want)
		}
	}
	primary.mu.Lock()
	primary.fail = true
	primary.mu.Unlock()
	count, _, err := store.SlidingWindow(context.Background(), key, now.Add(3*time.Second), time.Minute)
	if err != nil || count != 3 {
		t.Fatalf("fallback count = %d, %v; want 3, nil", count, err)
	}
	if status := cache.HealthStatus(); status != "degraded" {
		t.Fatalf("cache health = %q, want degraded", status)
	}
	count, _, err = store.CountWindow(context.Background(), key, now.Add(3*time.Second), time.Minute)
	if err != nil || count != 3 {
		t.Fatalf("fallback lockout count = %d, %v; want 3, nil", count, err)
	}
}
