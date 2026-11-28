package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"time"

	"github.com/apimgr/api/src/config"
	"github.com/apimgr/api/src/critical"
	"github.com/apimgr/api/src/metrics"
)

// Store owns the distinct read and write pools for one SQLite file.
type Store struct {
	writeDB *sql.DB
	readDB  *sql.DB
}

var maintenanceProbeSequence atomic.Uint64

// ProbeServerDBWrite verifies a transactional write and rollback-safe cleanup
// against the server database without retaining a row after the transaction.
func ProbeServerDBWrite(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	db := GetServerDB()
	if db == nil {
		return errors.New("database handle is unavailable")
	}
	probeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	key := fmt.Sprintf("__maintenance_probe_%d_%d", time.Now().UnixNano(), maintenanceProbeSequence.Add(1))
	return withWriteTransaction(probeCtx, db, func(tx *sql.Tx) error {
		if _, err := execTxContext(probeCtx, tx, `INSERT INTO app_secrets (name, value) VALUES (?, 'maintenance-probe')`, key); err != nil {
			return err
		}
		_, err := execTxContext(probeCtx, tx, `DELETE FROM app_secrets WHERE name = ?`, key)
		return err
	})
}

// SnapshotServerSQLite writes a transactionally consistent snapshot of the
// local server database or the enabled libSQL SQLite cache to destination.
func SnapshotServerSQLite(ctx context.Context, destination string) error {
	store := GetStore()
	if store == nil {
		if cache := getServerCache(); cache != nil {
			store = cache.store
		}
	}
	if store == nil {
		return errors.New("local server database or libsql cache is unavailable")
	}
	return store.SnapshotInto(ctx, destination)
}

// Reader exposes read-only queries without exposing a writable SQL handle.
type Reader struct{ db *sql.DB }

// QueryContext executes a bounded multi-row query through the read-only pool.
func (reader Reader) QueryContext(ctx context.Context, query string, args ...any) (*timedRows, error) {
	return queryContext(ctx, reader.db, query, args...)
}

// QueryRowContext executes a bounded single-row query through the read-only pool.
func (reader Reader) QueryRowContext(ctx context.Context, query string, args ...any) Row {
	return queryRowContext(ctx, reader.db, query, args...)
}

type databasePoolOptions struct {
	maxOpen     int
	maxIdle     int
	maxLifetime time.Duration
	maxIdleTime time.Duration
}

func resolveDatabasePool(cfg config.DatabasePoolConfig) (databasePoolOptions, error) {
	if cfg == (config.DatabasePoolConfig{}) {
		cfg = config.DatabasePoolConfig{MaxOpen: 25, MaxIdle: 5, MaxLifetime: "5m", MaxIdleTime: "1m"}
	}
	if cfg.MaxOpen < 1 || cfg.MaxIdle < 0 || cfg.MaxIdle > cfg.MaxOpen {
		return databasePoolOptions{}, errors.New("invalid database pool: require max_open > 0 and 0 <= max_idle <= max_open")
	}
	lifetime, err := time.ParseDuration(cfg.MaxLifetime)
	if err != nil || lifetime < 0 {
		return databasePoolOptions{}, errors.New("invalid database pool max_lifetime")
	}
	idleTime, err := time.ParseDuration(cfg.MaxIdleTime)
	if err != nil || idleTime < 0 {
		return databasePoolOptions{}, errors.New("invalid database pool max_idle_time")
	}
	return databasePoolOptions{maxOpen: cfg.MaxOpen, maxIdle: cfg.MaxIdle, maxLifetime: lifetime, maxIdleTime: idleTime}, nil
}

// OpenSQLite opens the single-writer and concurrent read pools for a local SQLite file.
func OpenSQLite(path string, readers int) (*Store, error) {
	if readers <= 0 {
		return nil, errors.New("sqlite reader pool size must be positive")
	}
	base := "file:" + path +
		"?_pragma=busy_timeout(10000)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)&_pragma=foreign_keys(1)"
	writeDB, err := sql.Open("sqlite", base+"&_txlock=immediate")
	if err != nil {
		return nil, fmt.Errorf("open sqlite writer: %w", err)
	}
	writeDB.SetMaxOpenConns(1)
	writeDB.SetMaxIdleConns(1)
	writeDB.SetConnMaxLifetime(0)
	writeDB.SetConnMaxIdleTime(0)

	readDB, err := sql.Open("sqlite", base+"&_pragma=query_only(1)")
	if err != nil {
		_ = writeDB.Close()
		return nil, fmt.Errorf("open sqlite readers: %w", err)
	}
	readDB.SetMaxOpenConns(readers)
	readDB.SetMaxIdleConns(readers)
	readDB.SetConnMaxLifetime(0)
	readDB.SetConnMaxIdleTime(0)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := writeDB.PingContext(ctx); err != nil {
		slog.Error("sqlite database unavailable", "path", path, "error", err)
		_ = readDB.Close()
		_ = writeDB.Close()
		return nil, fmt.Errorf("ping sqlite writer: %w", err)
	}
	var journal string
	if err := writeDB.QueryRowContext(ctx, "PRAGMA journal_mode").Scan(&journal); err != nil {
		slog.Error("sqlite journal mode check failed", "path", path, "error", err)
		_ = readDB.Close()
		_ = writeDB.Close()
		return nil, fmt.Errorf("read sqlite journal mode: %w", err)
	}
	if !strings.EqualFold(journal, "wal") {
		slog.Error("sqlite WAL mode unavailable", "path", path, "journal_mode", journal)
		_ = readDB.Close()
		_ = writeDB.Close()
		return nil, fmt.Errorf("sqlite WAL mode unavailable for %q (journal_mode=%q)", path, journal)
	}
	if err := readDB.PingContext(ctx); err != nil {
		_ = readDB.Close()
		_ = writeDB.Close()
		return nil, fmt.Errorf("ping sqlite readers: %w", err)
	}
	return &Store{writeDB: writeDB, readDB: readDB}, nil
}

// Read executes a read callback against the query-only pool.
func (s *Store) Read(ctx context.Context, fn func(context.Context, Reader) error) error {
	if s == nil || s.readDB == nil {
		return errors.New("database store is not initialized")
	}
	if fn == nil {
		return errors.New("database read callback is required")
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return fn(ctx, Reader{db: s.readDB})
}

// Write runs a short transaction on the single writer and retries SQLite lock errors.
func (s *Store) Write(ctx context.Context, fn func(*sql.Tx) error) error {
	if s == nil || s.writeDB == nil {
		return errors.New("database store is not initialized")
	}
	if fn == nil {
		return errors.New("database write callback is required")
	}
	parentCtx := ctx
	ctx, cancel := context.WithTimeout(parentCtx, 10*time.Second)
	defer cancel()
	caller := "unknown"
	if pc, _, _, ok := runtime.Caller(1); ok {
		if function := runtime.FuncForPC(pc); function != nil {
			caller = function.Name()
		}
	}
	var waited time.Duration
	defer func() {
		metrics.GetMetrics().ObserveDBWriteWait(waited)
		if waited > time.Second {
			slog.Warn("db.busy", "wait_ms", waited.Milliseconds(), "caller", caller)
		}
	}()
	var lastErr error
	for attempt := 0; attempt < 5; attempt++ {
		var attemptWait time.Duration
		attemptWait, lastErr = s.writeOnce(ctx, fn)
		waited += attemptWait
		if !isSQLiteBusy(lastErr) || ctx.Err() != nil {
			if ctx.Err() != nil {
				if errors.Is(ctx.Err(), context.DeadlineExceeded) && parentCtx.Err() == nil {
					critical.ReportWriteFailure("database_write")
				}
				return ctx.Err()
			}
			if isCriticalDatabaseWriteError(lastErr) {
				critical.ReportWriteFailure("database_write")
			}
			return lastErr
		}
		metrics.GetMetrics().RecordDBBusy()
		if attempt == 4 {
			break
		}
		backoff := time.Duration(25<<attempt)*time.Millisecond + time.Duration(rand.IntN(25))*time.Millisecond
		timer := time.NewTimer(backoff)
		select {
		case <-timer.C:
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			if errors.Is(ctx.Err(), context.DeadlineExceeded) && parentCtx.Err() == nil {
				critical.ReportWriteFailure("database_write")
			}
			return ctx.Err()
		}
	}
	if isCriticalDatabaseWriteError(lastErr) {
		critical.ReportWriteFailure("database_write")
	}
	return lastErr
}

// writeOnce reports time spent acquiring the immediate writer transaction.
func (s *Store) writeOnce(ctx context.Context, fn func(*sql.Tx) error) (time.Duration, error) {
	start := time.Now()
	tx, err := s.writeDB.BeginTx(ctx, nil)
	wait := time.Since(start)
	if err != nil {
		return wait, err
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		return wait, err
	}
	return wait, tx.Commit()
}

func withWriteTransaction(ctx context.Context, db *sql.DB, fn func(*sql.Tx) error) error {
	if db == nil {
		return errors.New("database handle is unavailable")
	}
	started := time.Now()
	defer recordRequestDatabaseTiming(ctx, started)
	var err error
	if isServerDatabaseHandle(db) {
		if store := GetStore(); store != nil {
			err = store.Write(ctx, fn)
		} else {
			err = writeDatabaseTransaction(ctx, db, fn)
			if isCriticalDatabaseWriteError(err) {
				critical.ReportWriteFailure("database_write")
			}
		}
	} else {
		err = writeDatabaseTransaction(ctx, db, fn)
		if isCriticalDatabaseWriteError(err) {
			critical.ReportWriteFailure("database_write")
		}
	}
	if err == nil {
		refreshServerCacheAfterWrite(ctx, db)
	}
	return err
}

func isCriticalDatabaseWriteError(err error) bool {
	if err == nil || errors.Is(err, context.Canceled) {
		return false
	}
	message := strings.ToLower(err.Error())
	for _, marker := range []string{
		"sqlite_full", "database is full", "disk is full", "disk quota", "no space left",
		"sqlite_ioerr", "disk i/o error", "i/o error", "sqlite_readonly", "readonly database",
		"read-only database", "unable to open database file", "database handle is unavailable", "database is closed",
		"permission denied", "access is denied", "no such file or directory", "connection refused", "connection reset", "connection closed", "connection is unavailable",
		"server closed", "failed to connect", "broken pipe", "i/o timeout", "database is locked",
		"database table is locked", "sqlite_busy", "sqlite_locked",
	} {
		if strings.Contains(message, marker) {
			return true
		}
	}
	return false
}

func writeDatabaseTransaction(ctx context.Context, db *sql.DB, fn func(*sql.Tx) error) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

func isSQLiteBusy(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "sqlite_busy") || strings.Contains(message, "sqlite_locked") || strings.Contains(message, "database is locked") || strings.Contains(message, "database table is locked")
}

// Close checkpoints WAL and closes readers before the writer.
func (s *Store) Close() error {
	if s == nil {
		return nil
	}
	var closeErr error
	if s.writeDB != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		_, checkpointErr := s.writeDB.ExecContext(ctx, "PRAGMA wal_checkpoint(TRUNCATE)")
		cancel()
		if checkpointErr != nil {
			closeErr = fmt.Errorf("sqlite WAL checkpoint: %w", checkpointErr)
		}
	}
	if s.readDB != nil {
		if err := s.readDB.Close(); err != nil && closeErr == nil {
			closeErr = fmt.Errorf("close sqlite readers: %w", err)
		}
	}
	if s.writeDB != nil {
		if err := s.writeDB.Close(); err != nil && closeErr == nil {
			closeErr = fmt.Errorf("close sqlite writer: %w", err)
		}
	}
	return closeErr
}

// Vacuum runs the SQLite maintenance command outside a transaction on the writer pool.
func (s *Store) Vacuum(ctx context.Context) error {
	if s == nil || s.writeDB == nil {
		return errors.New("database store is not initialized")
	}
	ctx, cancel := context.WithTimeout(ctx, timeoutSchema)
	defer cancel()
	started := time.Now()
	_, err := s.writeDB.ExecContext(ctx, "VACUUM")
	observeQuery("VACUUM", started, err)
	return err
}

// SnapshotInto creates a consistent SQLite database snapshot at destination
// using VACUUM INTO on the serialized write handle.
func (s *Store) SnapshotInto(ctx context.Context, destination string) error {
	if s == nil || s.writeDB == nil {
		return errors.New("database store is not initialized")
	}
	abs, err := filepath.Abs(destination)
	if err != nil {
		return fmt.Errorf("resolve sqlite snapshot destination: %w", err)
	}
	if _, err := os.Lstat(abs); err == nil {
		return fmt.Errorf("sqlite snapshot destination already exists")
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("inspect sqlite snapshot destination: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0700); err != nil {
		return fmt.Errorf("create sqlite snapshot directory: %w", err)
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	started := time.Now()
	_, err = s.writeDB.ExecContext(ctx, "VACUUM INTO ?", abs)
	observeQuery("VACUUM INTO", started, err)
	if err != nil {
		return fmt.Errorf("create sqlite snapshot: %w", err)
	}
	if err := os.Chmod(abs, 0600); err != nil {
		removeErr := os.Remove(abs)
		return errors.Join(fmt.Errorf("protect sqlite snapshot: %w", err), removeErr)
	}
	return nil
}
