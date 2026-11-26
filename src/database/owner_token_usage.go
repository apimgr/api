package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

const (
	ownerTokenUsageFlushInterval = 30 * time.Second
	ownerTokenUsageMinInterval   = time.Minute
	ownerTokenUsageMaxEntries    = 8192
)

type ownerTokenUsageItem struct {
	ownerID string
	usedAt  time.Time
}

type ownerTokenUsageCache struct {
	mu       sync.Mutex
	flushMu  sync.Mutex
	last     map[string]time.Time
	pending  map[string]ownerTokenUsageItem
	db       *sql.DB
	progress chan struct{}
	stop     chan struct{}
	done     chan struct{}
	started  bool
	lastErr  error
}

var ownerTokenUsage = ownerTokenUsageCache{
	last:     make(map[string]time.Time),
	pending:  make(map[string]ownerTokenUsageItem),
	progress: make(chan struct{}, 1),
}

// startOwnerTokenUsageFlusher starts the single bounded hot-counter flush loop.
func startOwnerTokenUsageFlusher(db *sql.DB) {
	ownerTokenUsage.mu.Lock()
	if ownerTokenUsage.started {
		ownerTokenUsage.mu.Unlock()
		return
	}
	ownerTokenUsage.db = db
	if ownerTokenUsage.progress == nil {
		ownerTokenUsage.progress = make(chan struct{}, 1)
	}
	ownerTokenUsage.stop = make(chan struct{})
	ownerTokenUsage.done = make(chan struct{})
	ownerTokenUsage.started = true
	stop, done := ownerTokenUsage.stop, ownerTokenUsage.done
	ownerTokenUsage.mu.Unlock()

	go func() {
		defer close(done)
		ticker := time.NewTicker(ownerTokenUsageFlushInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				if err := ownerTokenUsage.flush(context.Background()); err != nil {
					slog.Warn("database: failed to flush owner token usage counters", "error", err)
				}
			case <-stop:
				if err := ownerTokenUsage.flush(context.Background()); err != nil {
					slog.Error("database: failed to flush owner token usage counters on shutdown", "error", err)
					ownerTokenUsage.mu.Lock()
					ownerTokenUsage.lastErr = err
					ownerTokenUsage.mu.Unlock()
				}
				return
			}
		}
	}()
}

// stopOwnerTokenUsageFlusher flushes pending counters before the database closes.
func stopOwnerTokenUsageFlusher() error {
	ownerTokenUsage.mu.Lock()
	if !ownerTokenUsage.started {
		ownerTokenUsage.mu.Unlock()
		return ownerTokenUsage.flush(context.Background())
	}
	stop, done := ownerTokenUsage.stop, ownerTokenUsage.done
	ownerTokenUsage.started = false
	close(stop)
	ownerTokenUsage.mu.Unlock()
	<-done
	ownerTokenUsage.mu.Lock()
	err := ownerTokenUsage.lastErr
	ownerTokenUsage.lastErr = nil
	ownerTokenUsage.db = nil
	ownerTokenUsage.mu.Unlock()
	return err
}

// recordOwnerTokenUsage coalesces successful validations without writing per request.
func recordOwnerTokenUsage(ctx context.Context, db *sql.DB, tokenID, ownerID string, now time.Time, lastUsed sql.NullInt64) error {
	if tokenID == "" || ownerID == "" {
		return errors.New("invalid owner token usage identity")
	}
	ownerTokenUsage.mu.Lock()
	started := ownerTokenUsage.started
	ownerTokenUsage.mu.Unlock()
	if !started {
		return nil
	}
	if lastUsed.Valid {
		stored := time.Unix(lastUsed.Int64, 0)
		if now.Sub(stored) < ownerTokenUsageMinInterval {
			ownerTokenUsage.mu.Lock()
			ownerTokenUsage.last[tokenID] = stored
			ownerTokenUsage.pruneLocked(now)
			ownerTokenUsage.mu.Unlock()
			return nil
		}
	}

	for {
		ownerTokenUsage.mu.Lock()
		if last, ok := ownerTokenUsage.last[tokenID]; ok && now.Sub(last) < ownerTokenUsageMinInterval {
			ownerTokenUsage.mu.Unlock()
			return nil
		}
		if _, alreadyPending := ownerTokenUsage.pending[tokenID]; !alreadyPending && len(ownerTokenUsage.pending) >= ownerTokenUsageMaxEntries {
			progress := ownerTokenUsage.progress
			ownerTokenUsage.mu.Unlock()
			select {
			case <-ctx.Done():
				return fmt.Errorf("wait for owner token usage flush: %w", ctx.Err())
			case <-progress:
				continue
			}
		}
		ownerTokenUsage.last[tokenID] = now
		ownerTokenUsage.pending[tokenID] = ownerTokenUsageItem{ownerID: ownerID, usedAt: now}
		ownerTokenUsage.pruneLocked(now)
		ownerTokenUsage.mu.Unlock()
		return nil
	}
}

func (usage *ownerTokenUsageCache) pruneLocked(now time.Time) {
	cutoff := now.Add(-ownerTokenUsageMinInterval)
	for id, last := range usage.last {
		if last.Before(cutoff) {
			delete(usage.last, id)
		}
	}
	for len(usage.last) > ownerTokenUsageMaxEntries {
		var oldestID string
		var oldest time.Time
		for id, last := range usage.last {
			if oldest.IsZero() || last.Before(oldest) {
				oldestID, oldest = id, last
			}
		}
		delete(usage.last, oldestID)
	}
}

func (usage *ownerTokenUsageCache) flush(ctx context.Context) error {
	usage.flushMu.Lock()
	defer usage.flushMu.Unlock()

	usage.mu.Lock()
	db := usage.db
	batch := make(map[string]ownerTokenUsageItem, len(usage.pending))
	for id, item := range usage.pending {
		batch[id] = item
	}
	usage.mu.Unlock()
	if len(batch) == 0 {
		return nil
	}
	if db == nil {
		return errors.New("server database is unavailable")
	}

	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	owners := make(map[string]time.Time)
	err := withWriteTransaction(ctx, db, func(tx *sql.Tx) error {
		for tokenID, item := range batch {
			if _, err := execTxContext(ctx, tx, `UPDATE api_tokens SET last_used_at = ? WHERE id = ?`, item.usedAt.Unix(), tokenID); err != nil {
				return fmt.Errorf("update owner token usage: %w", err)
			}
			if item.usedAt.After(owners[item.ownerID]) {
				owners[item.ownerID] = item.usedAt
			}
		}
		for ownerID, seenAt := range owners {
			if _, err := execTxContext(ctx, tx, `UPDATE owners SET last_seen_at = ? WHERE id = ?`, seenAt.Unix(), ownerID); err != nil {
				return fmt.Errorf("update owner last-seen time: %w", err)
			}
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("flush owner token usage: %w", err)
	}

	usage.mu.Lock()
	for tokenID, item := range batch {
		if pending, ok := usage.pending[tokenID]; ok && pending == item {
			delete(usage.pending, tokenID)
		}
	}
	progress := usage.progress
	usage.mu.Unlock()
	if progress != nil {
		select {
		case progress <- struct{}{}:
		default:
		}
	}
	return nil
}
