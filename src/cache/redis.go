package cache

import (
	"context"
	"crypto/tls"
	"fmt"
	"time"

	"github.com/apimgr/api/src/config"
	"github.com/apimgr/api/src/metrics"
	"github.com/redis/go-redis/v9"
)

// redisStore is the valkey/redis-backed Store. Both server.cache.type
// values use the same go-redis/v9 client (Valkey is wire-compatible with
// Redis), per AI.md PART 12 "Cache Configuration".
type redisStore struct {
	client  *redis.Client
	prefix  string
	timeout time.Duration
	// name is the low-cardinality `cache` label value reported by the PART 20
	// cache metrics: the driver name, never a key or any user-supplied value.
	name string
}

// redisCacheName maps the configured cache type onto the fixed set of driver
// label values, defaulting to "redis" for anything unexpected.
func redisCacheName(cfgType string) string {
	if normalizeType(cfgType) == "valkey" {
		return "valkey"
	}
	return "redis"
}

// newRedisStore connects to the configured valkey/redis instance. cfg.URL
// takes precedence over the discrete Host/Port/Username/Password/DB fields
// when set, matching the documented "url takes precedence if both are
// specified" rule.
func newRedisStore(cfg config.CacheConfig) (*redisStore, error) {
	timeout := parseDuration(cfg.Timeout, defaultTimeout)

	opts, err := redisOptions(cfg, timeout)
	if err != nil {
		return nil, err
	}

	client := redis.NewClient(opts)

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	if err := client.Ping(ctx).Err(); err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("connecting to %s cache: %w", cfg.Type, err)
	}

	return &redisStore{
		client:  client,
		prefix:  cfg.Prefix,
		timeout: timeout,
		name:    redisCacheName(cfg.Type),
	}, nil
}

// redisOptions builds go-redis client options from server.cache.* config.
func redisOptions(cfg config.CacheConfig, timeout time.Duration) (*redis.Options, error) {
	if cfg.URL != "" {
		opts, err := redis.ParseURL(cfg.URL)
		if err != nil {
			return nil, fmt.Errorf("parsing cache url: %w", err)
		}
		opts.DialTimeout = timeout
		opts.ReadTimeout = timeout
		opts.WriteTimeout = timeout
		if cfg.PoolSize > 0 {
			opts.PoolSize = cfg.PoolSize
		}
		if cfg.MinIdle > 0 {
			opts.MinIdleConns = cfg.MinIdle
		}
		return opts, nil
	}

	host := cfg.Host
	if host == "" {
		host = "localhost"
	}
	port := cfg.Port
	if port == 0 {
		port = 6379
	}

	opts := &redis.Options{
		Addr:         fmt.Sprintf("%s:%d", host, port),
		Username:     cfg.Username,
		Password:     cfg.Password,
		DB:           cfg.DB,
		PoolSize:     cfg.PoolSize,
		MinIdleConns: cfg.MinIdle,
		DialTimeout:  timeout,
		ReadTimeout:  timeout,
		WriteTimeout: timeout,
	}

	if cfg.TLS {
		// #nosec G402 -- TLSSkipVerify is an explicit, documented operator
		// opt-in for self-signed setups, never the default.
		opts.TLSConfig = &tls.Config{InsecureSkipVerify: cfg.TLSSkipVerify}
	}

	return opts, nil
}

// SlidingWindow implements Store using a per-key sorted set: each hit is
// added scored by its Unix-nanosecond timestamp, entries older than the
// window are trimmed, and the remaining cardinality is the count. TTL is
// refreshed to the window on every call so idle keys expire naturally.
func (r *redisStore) SlidingWindow(ctx context.Context, key string, now time.Time, window time.Duration) (int, time.Time, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	fullKey := r.prefix + key
	windowStart := now.Add(-window)
	member := fmt.Sprintf("%d.%d", now.UnixNano(), now.Nanosecond())

	pipe := r.client.TxPipeline()
	trimmed := pipe.ZRemRangeByScore(ctx, fullKey, "-inf", fmt.Sprintf("%d", windowStart.UnixNano()))
	pipe.ZAdd(ctx, fullKey, redis.Z{Score: float64(now.UnixNano()), Member: member})
	card := pipe.ZCard(ctx, fullKey)
	pipe.Expire(ctx, fullKey, window)
	oldest := pipe.ZRangeWithScores(ctx, fullKey, 0, 0)

	if _, err := pipe.Exec(ctx); err != nil {
		return 0, time.Time{}, fmt.Errorf("cache sliding window: %w", err)
	}

	resetAt := now.Add(window)
	if scores, err := oldest.Result(); err == nil && len(scores) > 0 {
		resetAt = time.Unix(0, int64(scores[0].Score)).Add(window)
	}

	r.recordLookup(card.Val(), trimmed.Val())

	return int(card.Val()), resetAt, nil
}

// recordLookup reports one sliding-window lookup to the PART 20 cache metrics.
// card counts the entries left in the window including the hit just added, so
// anything above one means the key was already populated; trimmed counts the
// entries the window trim expired out of the set.
func (r *redisStore) recordLookup(card, trimmed int64) {
	mx := metrics.Get()
	if card > 1 {
		mx.RecordCacheHit(r.name)
	} else {
		mx.RecordCacheMiss(r.name)
	}
	for i := int64(0); i < trimmed; i++ {
		mx.RecordCacheEviction(r.name)
	}
}

// Close closes the underlying redis client connection pool.
func (r *redisStore) Close() error {
	return r.client.Close()
}
