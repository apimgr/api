package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"
)

// RateLimitStore persists sliding-window counters in the canonical rate_limits table.
type RateLimitStore struct{}

// RateLimitCounter is the current and previous fixed-window state for one
// rate-limit key.
type RateLimitCounter struct {
	Key         string
	Count       int64
	PrevCount   int64
	WindowStart time.Time
	UpdatedAt   time.Time
}

// Close satisfies cache.Store; the store uses the process-wide database.
func (RateLimitStore) Close() error { return nil }

// EnsureRateLimitSchema creates and migrates the canonical aggregate counter schema.
func EnsureRateLimitSchema(ctx context.Context) error {
	if serverDB == nil {
		return errors.New("rate-limit database is unavailable")
	}
	table := TableName("rate_limits")
	index := TableName("idx_rate_limits_window")
	if strings.HasPrefix(table, "srv_") {
		if _, err := execContext(ctx, serverDB, fmt.Sprintf(`ALTER TABLE %s ADD COLUMN prev_count INTEGER NOT NULL DEFAULT 0`, table)); err != nil && !strings.Contains(strings.ToLower(err.Error()), "duplicate column") {
			return fmt.Errorf("migrate remote previous rate-limit counter: %w", err)
		}
	} else {
		rows, err := queryContext(ctx, serverDB, fmt.Sprintf(`PRAGMA table_info(%s)`, table))
		if err != nil {
			return fmt.Errorf("inspect rate-limit schema: %w", err)
		}
		hasPrevious := false
		for rows.Next() {
			var cid, notNull, primaryKey int
			var name, columnType string
			var defaultValue sql.NullString
			if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
				_ = rows.Close()
				return fmt.Errorf("read rate-limit schema: %w", err)
			}
			if name == "prev_count" {
				hasPrevious = true
			}
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return fmt.Errorf("iterate rate-limit schema: %w", err)
		}
		if err := rows.Close(); err != nil {
			return fmt.Errorf("close rate-limit schema: %w", err)
		}
		if !hasPrevious {
			if _, err := execContext(ctx, serverDB, fmt.Sprintf(`ALTER TABLE %s ADD COLUMN prev_count INTEGER NOT NULL DEFAULT 0`, table)); err != nil {
				return fmt.Errorf("add previous rate-limit counter: %w", err)
			}
		}
		if _, err := execContext(ctx, serverDB, fmt.Sprintf(`UPDATE %s SET window_start = CAST(strftime('%%s', window_start) AS INTEGER) WHERE typeof(window_start) = 'text'`, table)); err != nil {
			return fmt.Errorf("migrate rate-limit window timestamps: %w", err)
		}
		if _, err := execContext(ctx, serverDB, fmt.Sprintf(`UPDATE %s SET updated_at = CAST(strftime('%%s', updated_at) AS INTEGER) WHERE typeof(updated_at) = 'text'`, table)); err != nil {
			return fmt.Errorf("migrate rate-limit update timestamps: %w", err)
		}
	}
	if _, err := execContext(ctx, serverDB, fmt.Sprintf(`CREATE INDEX IF NOT EXISTS %s ON %s(window_start)`, index, table)); err != nil {
		return fmt.Errorf("create rate-limit window index: %w", err)
	}
	return nil
}

// RecordBatch persists the in-memory counter snapshots in one database write.
func (RateLimitStore) RecordBatch(ctx context.Context, counters []RateLimitCounter) error {
	if len(counters) == 0 {
		return nil
	}
	if serverDB == nil {
		return errors.New("rate-limit database is unavailable")
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	table := TableName("rate_limits")
	return withWriteTransaction(ctx, serverDB, func(tx *sql.Tx) error {
		stmt, err := tx.PrepareContext(ctx, fmt.Sprintf(`INSERT INTO %s
			(key, count, prev_count, window_start, updated_at)
			VALUES (?, ?, ?, ?, ?)
			ON CONFLICT(key) DO UPDATE SET count = excluded.count,
			prev_count = excluded.prev_count, window_start = excluded.window_start,
			updated_at = excluded.updated_at`, table))
		if err != nil {
			return fmt.Errorf("prepare rate-limit counter batch: %w", err)
		}
		for _, counter := range counters {
			if counter.Key == "" || counter.Count < 0 || counter.PrevCount < 0 || counter.WindowStart.IsZero() || counter.UpdatedAt.IsZero() {
				return errors.Join(errors.New("invalid rate-limit counter snapshot"), stmt.Close())
			}
			if _, err := stmt.ExecContext(ctx, counter.Key, counter.Count, counter.PrevCount, counter.WindowStart.Unix(), counter.UpdatedAt.Unix()); err != nil {
				return errors.Join(fmt.Errorf("write rate-limit counter batch: %w", err), stmt.Close())
			}
		}
		if err := stmt.Close(); err != nil {
			return fmt.Errorf("close rate-limit counter batch: %w", err)
		}
		return nil
	})
}

// LoadCounter reads the raw persisted two-window state for lazy in-memory
// limiter initialization.
func (RateLimitStore) LoadCounter(ctx context.Context, key string) (RateLimitCounter, bool, error) {
	if serverDB == nil {
		return RateLimitCounter{}, false, errors.New("rate-limit database is unavailable")
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	query := fmt.Sprintf(`SELECT key, count, prev_count, window_start, updated_at FROM %s WHERE key = ?`, TableName("rate_limits"))
	var counter RateLimitCounter
	var start, updated int64
	err := queryRowContext(ctx, serverDB, query, key).Scan(&counter.Key, &counter.Count, &counter.PrevCount, &start, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return RateLimitCounter{}, false, nil
	}
	if err != nil {
		return RateLimitCounter{}, false, fmt.Errorf("load rate-limit counter: %w", err)
	}
	counter.WindowStart = time.Unix(start, 0).UTC()
	counter.UpdatedAt = time.Unix(updated, 0).UTC()
	return counter, true, nil
}

// SlidingWindow records one hit using the weighted current/previous-window
// counter required by AI.md PART 12.
func (RateLimitStore) SlidingWindow(ctx context.Context, key string, now time.Time, window time.Duration) (int, time.Time, error) {
	if serverDB == nil {
		return 0, time.Time{}, errors.New("rate-limit database is unavailable")
	}
	if key == "" || window < time.Second {
		return 0, time.Time{}, errors.New("invalid rate-limit key or window")
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	table := TableName("rate_limits")
	var counter RateLimitCounter
	err := withWriteTransaction(ctx, serverDB, func(tx *sql.Tx) error {
		var found bool
		var err error
		counter, found, err = loadCounterTx(ctx, tx, key)
		if err != nil {
			return err
		}
		if !found {
			counter = RateLimitCounter{Key: key, WindowStart: now.UTC().Truncate(time.Second)}
		}
		advanceCounter(&counter, now, window)
		counter.Count++
		counter.UpdatedAt = now.UTC()
		_, err = execTxContext(ctx, tx, fmt.Sprintf(`INSERT INTO %s
			(key, count, prev_count, window_start, updated_at) VALUES (?, ?, ?, ?, ?)
			ON CONFLICT(key) DO UPDATE SET count = excluded.count,
			prev_count = excluded.prev_count, window_start = excluded.window_start,
			updated_at = excluded.updated_at`, table), counter.Key, counter.Count, counter.PrevCount,
			counter.WindowStart.Unix(), counter.UpdatedAt.Unix())
		return err
	})
	if err != nil {
		return 0, time.Time{}, fmt.Errorf("rate-limit transaction: %w", err)
	}
	count, reset := weightedCount(counter, now, window)
	return count, reset, nil
}

// CountWindow reads and evaluates a counter without recording a request.
func (RateLimitStore) CountWindow(ctx context.Context, key string, now time.Time, window time.Duration) (int, time.Time, error) {
	counter, found, err := (RateLimitStore{}).LoadCounter(ctx, key)
	if err != nil || !found {
		return 0, time.Time{}, err
	}
	count, reset := weightedCount(counter, now, window)
	return count, reset, nil
}

func loadCounterTx(ctx context.Context, tx *sql.Tx, key string) (RateLimitCounter, bool, error) {
	var counter RateLimitCounter
	var start, updated int64
	query := fmt.Sprintf(`SELECT key, count, prev_count, window_start, updated_at FROM %s WHERE key = ?`, TableName("rate_limits"))
	err := queryRowTxContext(ctx, tx, query, key).
		Scan(&counter.Key, &counter.Count, &counter.PrevCount, &start, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return RateLimitCounter{}, false, nil
	}
	if err != nil {
		return RateLimitCounter{}, false, err
	}
	counter.WindowStart = time.Unix(start, 0).UTC()
	counter.UpdatedAt = time.Unix(updated, 0).UTC()
	return counter, true, nil
}

func advanceCounter(counter *RateLimitCounter, now time.Time, window time.Duration) {
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

func weightedCount(counter RateLimitCounter, now time.Time, window time.Duration) (int, time.Time) {
	windowSeconds := float64(window) / float64(time.Second)
	if windowSeconds <= 0 {
		return 0, time.Time{}
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
			return 0, time.Time{}
		}
	}
	weight := 1 - elapsed/windowSeconds
	value := current + previous*weight
	reset := counter.WindowStart.Add(2 * window)
	return int(math.Ceil(value)), reset
}
