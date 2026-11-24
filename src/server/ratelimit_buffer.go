package server

import (
	"context"
	"errors"
	"log/slog"
	"math"
	"sync"
	"time"

	"github.com/apimgr/api/src/cache"
	"github.com/apimgr/api/src/database"
)

const rateLimitFlushInterval = 30 * time.Second

type rateLimitCounterBackend interface {
	LoadCounter(context.Context, string) (database.RateLimitCounter, bool, error)
	RecordBatch(context.Context, []database.RateLimitCounter) error
}

// bufferedRateLimitStore keeps the SQLite sliding-window state in memory and
// persists aggregate snapshots in one write transaction every 30 seconds.
type bufferedRateLimitStore struct {
	backend  rateLimitCounterBackend
	interval time.Duration

	mu       sync.Mutex
	counters map[string]database.RateLimitCounter
	windows  map[string]time.Duration
	pending  map[string]database.RateLimitCounter

	stop     chan struct{}
	done     chan struct{}
	stopOnce sync.Once
	closeErr error
	warnOnce sync.Once
}

func newBufferedRateLimitStore(backend rateLimitCounterBackend) *bufferedRateLimitStore {
	return newBufferedRateLimitStoreWithInterval(backend, rateLimitFlushInterval)
}

func newBufferedRateLimitStoreWithInterval(backend rateLimitCounterBackend, interval time.Duration) *bufferedRateLimitStore {
	if interval <= 0 {
		interval = rateLimitFlushInterval
	}
	store := &bufferedRateLimitStore{
		backend:  backend,
		interval: interval,
		counters: make(map[string]database.RateLimitCounter),
		windows:  make(map[string]time.Duration),
		pending:  make(map[string]database.RateLimitCounter),
		stop:     make(chan struct{}),
		done:     make(chan struct{}),
	}
	go store.flushLoop()
	return store
}

func (store *bufferedRateLimitStore) SlidingWindow(ctx context.Context, key string, now time.Time, window time.Duration) (int, time.Time, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	counter, err := store.loadLocked(ctx, key, now, window)
	if err != nil {
		return 0, time.Time{}, err
	}
	advanceRateLimitCounter(&counter, now, window)
	counter.Count++
	counter.UpdatedAt = now.UTC()
	store.counters[key] = counter
	store.windows[key] = window
	store.pending[key] = counter
	return weightedRateLimitCount(counter, now, window)
}

func (store *bufferedRateLimitStore) CountWindow(ctx context.Context, key string, now time.Time, window time.Duration) (int, time.Time, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	counter, err := store.loadLocked(ctx, key, now, window)
	if err != nil {
		return 0, time.Time{}, err
	}
	return weightedRateLimitCount(counter, now, window)
}

func (store *bufferedRateLimitStore) loadLocked(ctx context.Context, key string, now time.Time, window time.Duration) (database.RateLimitCounter, error) {
	if key == "" {
		return database.RateLimitCounter{}, errors.New("rate-limit key is required")
	}
	if counter, ok := store.counters[key]; ok {
		store.windows[key] = window
		return counter, nil
	}
	counter, found, err := store.backend.LoadCounter(ctx, key)
	if err != nil {
		store.warnOnce.Do(func() {
			slog.Warn("ratelimit: SQLite counter backend unavailable; enforcing limits with in-memory counters", "error", err)
		})
		counter = database.RateLimitCounter{Key: key, WindowStart: now.UTC().Truncate(time.Second)}
		store.counters[key] = counter
		store.windows[key] = window
		return counter, nil
	}
	if !found {
		counter = database.RateLimitCounter{Key: key, WindowStart: now.UTC().Truncate(time.Second)}
	}
	store.counters[key] = counter
	store.windows[key] = window
	return counter, nil
}

func advanceRateLimitCounter(counter *database.RateLimitCounter, now time.Time, window time.Duration) {
	windowSeconds := int64(window / time.Second)
	if windowSeconds < 1 {
		windowSeconds = 1
	}
	start := counter.WindowStart.Unix()
	elapsed := now.Unix() - start
	if elapsed < windowSeconds {
		return
	}
	windowsPassed := elapsed / windowSeconds
	if windowsPassed == 1 {
		counter.PrevCount = counter.Count
	} else {
		counter.PrevCount = 0
	}
	counter.Count = 0
	counter.WindowStart = time.Unix(start+windowsPassed*windowSeconds, 0).UTC()
}

func weightedRateLimitCount(counter database.RateLimitCounter, now time.Time, window time.Duration) (int, time.Time, error) {
	windowSeconds := float64(window) / float64(time.Second)
	if windowSeconds <= 0 {
		return 0, time.Time{}, errors.New("rate-limit window must be positive")
	}
	elapsed := now.Sub(counter.WindowStart).Seconds()
	if elapsed < 0 {
		elapsed = 0
	}
	current, previous := float64(counter.Count), float64(counter.PrevCount)
	if elapsed >= windowSeconds {
		elapsed -= windowSeconds
		current, previous = 0, current
		if elapsed >= windowSeconds {
			return 0, time.Time{}, nil
		}
	}
	value := current + previous*(1-elapsed/windowSeconds)
	return int(math.Ceil(value)), counter.WindowStart.Add(2 * window), nil
}

func (store *bufferedRateLimitStore) flushLoop() {
	ticker := time.NewTicker(store.interval)
	defer ticker.Stop()
	defer close(store.done)
	for {
		select {
		case <-ticker.C:
			if err := store.flush(); err != nil {
				store.warnOnce.Do(func() {
					slog.Warn("ratelimit: SQLite counter backend unavailable; enforcing limits with in-memory counters", "error", err)
				})
			}
		case <-store.stop:
			store.closeErr = store.flush()
			if store.closeErr != nil {
				store.warnOnce.Do(func() {
					slog.Warn("ratelimit: final SQLite counter flush failed; buffered snapshots were retained in memory", "error", store.closeErr)
				})
			}
			return
		}
	}
}

func (store *bufferedRateLimitStore) flush() error {
	store.mu.Lock()
	now := time.Now()
	for key, counter := range store.counters {
		window := store.windows[key]
		if window > 0 && !now.Before(counter.WindowStart.Add(2*window)) {
			delete(store.counters, key)
			delete(store.windows, key)
			delete(store.pending, key)
		}
	}
	counters := make([]database.RateLimitCounter, 0, len(store.pending))
	for _, counter := range store.pending {
		counters = append(counters, counter)
	}
	store.pending = make(map[string]database.RateLimitCounter)
	store.mu.Unlock()
	if len(counters) == 0 {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	err := store.backend.RecordBatch(ctx, counters)
	cancel()
	if err == nil {
		return nil
	}
	store.mu.Lock()
	for _, counter := range counters {
		current, ok := store.pending[counter.Key]
		if !ok || current.UpdatedAt.Before(counter.UpdatedAt) {
			store.pending[counter.Key] = counter
		}
	}
	store.mu.Unlock()
	return err
}

func (store *bufferedRateLimitStore) Close() error {
	if store == nil {
		return nil
	}
	store.stopOnce.Do(func() { close(store.stop) })
	<-store.done
	return store.closeErr
}

// resilientCacheStore mirrors successful external-cache hits into memory so a
// runtime cache outage does not clear counters or lift an auth lockout.
type resilientCacheStore struct {
	primary  cache.Store
	fallback cache.Store

	mu       sync.RWMutex
	degraded bool
	warnOnce sync.Once
}

func newResilientCacheStore(primary, fallback cache.Store) *resilientCacheStore {
	return &resilientCacheStore{primary: primary, fallback: fallback}
}

func (store *resilientCacheStore) SlidingWindow(ctx context.Context, key string, now time.Time, window time.Duration) (int, time.Time, error) {
	store.mu.RLock()
	if store.degraded {
		fallback := store.fallback
		store.mu.RUnlock()
		return fallback.SlidingWindow(ctx, key, now, window)
	}
	count, resetAt, err := store.primary.SlidingWindow(ctx, key, now, window)
	if err == nil {
		_, _, mirrorErr := store.fallback.SlidingWindow(ctx, key, now, window)
		store.mu.RUnlock()
		if mirrorErr != nil {
			store.degrade(mirrorErr)
			return store.fallback.SlidingWindow(ctx, key, now, window)
		}
		return count, resetAt, nil
	}
	store.mu.RUnlock()
	store.degrade(err)
	return store.fallback.SlidingWindow(ctx, key, now, window)
}

func (store *resilientCacheStore) CountWindow(ctx context.Context, key string, now time.Time, window time.Duration) (int, time.Time, error) {
	store.mu.RLock()
	if store.degraded {
		fallback := store.fallback
		store.mu.RUnlock()
		return fallback.CountWindow(ctx, key, now, window)
	}
	count, resetAt, err := store.primary.CountWindow(ctx, key, now, window)
	store.mu.RUnlock()
	if err == nil {
		return count, resetAt, nil
	}
	store.degrade(err)
	return store.fallback.CountWindow(ctx, key, now, window)
}

func (store *resilientCacheStore) degrade(err error) {
	store.mu.Lock()
	store.degraded = true
	store.mu.Unlock()
	cache.SetHealthStatus("ratelimit", "degraded")
	store.warnOnce.Do(func() {
		slog.Warn("ratelimit: external cache unavailable; enforcing limits with mirrored in-memory counters", "error", err)
	})
}

func (store *resilientCacheStore) Close() error {
	return errors.Join(store.primary.Close(), store.fallback.Close())
}

var _ cache.Store = (*bufferedRateLimitStore)(nil)
var _ cache.Store = (*resilientCacheStore)(nil)
