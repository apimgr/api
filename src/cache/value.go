package cache

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/apimgr/api/src/config"
	"github.com/apimgr/api/src/metrics"
	"github.com/redis/go-redis/v9"
)

// ValueStore is the bounded TTL value cache used for derived runtime data.
// It is separate from Store so the in-process rate-limit fallback never
// stores rate-limit counters.
type ValueStore interface {
	GetValue(context.Context, string) ([]byte, bool, error)
	SetValue(context.Context, string, []byte, time.Duration) error
	DeleteValue(context.Context, string) error
	Close() error
}

// NewValueStore creates the TTL cache used for blocklist and GeoIP metadata.
// A disabled cache returns a no-op store; an unavailable external backend
// degrades to the bounded in-process value cache.
func NewValueStore(cfg config.CacheConfig) ValueStore {
	switch normalizeType(cfg.Type) {
	case "none":
		return disabledValueStore{}
	case "valkey", "redis":
		store, err := newRedisStore(cfg)
		if err != nil {
			slog.Warn("cache: falling back to in-process value cache", "type", cfg.Type, "error", err)
			SetHealthStatus("backend", "degraded")
			return newMemoryStore(cfg)
		}
		SetHealthStatus("backend", "ok")
		return store
	default:
		return newMemoryStore(cfg)
	}
}

type disabledValueStore struct{}

func (disabledValueStore) GetValue(context.Context, string) ([]byte, bool, error) {
	return nil, false, nil
}

func (disabledValueStore) SetValue(context.Context, string, []byte, time.Duration) error {
	return nil
}

func (disabledValueStore) DeleteValue(context.Context, string) error { return nil }
func (disabledValueStore) Close() error                              { return nil }

func (r *redisStore) GetValue(ctx context.Context, key string) ([]byte, bool, error) {
	if err := validateValueKey(key); err != nil {
		return nil, false, err
	}
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	value, err := r.client.Get(ctx, r.prefix+key).Bytes()
	if errors.Is(err, redis.Nil) {
		r.recordValueLookup(false)
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	r.recordValueLookup(true)
	return value, true, nil
}

func (r *redisStore) SetValue(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	if err := validateValueKey(key); err != nil {
		return err
	}
	if len(value) > maxValueCacheBytes {
		return ErrValueTooLarge
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if ttl <= 0 {
		ttl = r.defaultTTL
	}
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	return r.client.Set(ctx, r.prefix+key, value, ttl).Err()
}

func (r *redisStore) DeleteValue(ctx context.Context, key string) error {
	if err := validateValueKey(key); err != nil {
		return err
	}
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	return r.client.Del(ctx, r.prefix+key).Err()
}

func validateValueKey(key string) error {
	if len(key) == 0 || len(key) > maxValueCacheKeySize {
		return ErrInvalidKey
	}
	return nil
}

func (r *redisStore) recordValueLookup(hit bool) {
	mx := metrics.GetMetrics()
	if hit {
		mx.RecordCacheHit(r.name)
	} else {
		mx.RecordCacheMiss(r.name)
	}
}
