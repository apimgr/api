package database

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/apimgr/api/src/config"
)

func TestOpenSQLiteUsesSeparatePoolsAndWriteTransactions(t *testing.T) {
	store, err := OpenSQLite(filepath.Join(t.TempDir(), "store.db"), 4)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.Write(context.Background(), func(tx *sql.Tx) error {
		_, err := tx.Exec(`CREATE TABLE items (id INTEGER PRIMARY KEY, value TEXT NOT NULL)`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.Write(context.Background(), func(tx *sql.Tx) error {
		_, err := tx.Exec(`INSERT INTO items (value) VALUES ('ok')`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.Read(context.Background(), func(ctx context.Context, reader Reader) error {
		var count int
		if err := reader.QueryRowContext(ctx, `SELECT COUNT(*) FROM items`).Scan(&count); err != nil {
			return err
		}
		if count != 1 {
			return fmt.Errorf("expected 1 row, got %d", count)
		}
		if _, ok := any(reader).(interface {
			ExecContext(context.Context, string, ...any) (sql.Result, error)
		}); ok {
			return fmt.Errorf("read interface exposes write access")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestResolveDatabasePool(t *testing.T) {
	pool, err := resolveDatabasePool(config.DatabasePoolConfig{})
	if err != nil || pool.maxOpen != 25 || pool.maxIdle != 5 || pool.maxLifetime != 5*time.Minute || pool.maxIdleTime != time.Minute {
		t.Fatalf("unexpected defaults: %#v, %v", pool, err)
	}
	_, err = resolveDatabasePool(config.DatabasePoolConfig{MaxOpen: 2, MaxIdle: 3, MaxLifetime: "5m", MaxIdleTime: "1m"})
	if err == nil {
		t.Fatal("expected invalid idle pool size to fail")
	}
}

func TestSQLiteSnapshotIntoUsesConsistentStoreSnapshot(t *testing.T) {
	dir := t.TempDir()
	store, err := OpenSQLite(filepath.Join(dir, "source.db"), 2)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.Write(context.Background(), func(tx *sql.Tx) error {
		if _, err := tx.Exec(`CREATE TABLE snapshot_probe (value TEXT NOT NULL)`); err != nil {
			return err
		}
		_, err := tx.Exec(`INSERT INTO snapshot_probe (value) VALUES ('consistent')`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	snapshot := filepath.Join(dir, "snapshot.db")
	if err := store.SnapshotInto(context.Background(), snapshot); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("snapshot permissions = %o, want 600", info.Mode().Perm())
	}
	db, err := sql.Open("sqlite", "file:"+snapshot+"?_pragma=query_only(1)")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	var value string
	if err := db.QueryRow(`SELECT value FROM snapshot_probe`).Scan(&value); err != nil {
		t.Fatal(err)
	}
	if value != "consistent" {
		t.Fatalf("snapshot value = %q, want consistent", value)
	}
}

func TestSQLiteConcurrentReadersAndWriters(t *testing.T) {
	store, err := OpenSQLite(filepath.Join(t.TempDir(), "concurrent.db"), 25)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.Write(context.Background(), func(tx *sql.Tx) error {
		_, err := tx.Exec(`CREATE TABLE counters (id INTEGER PRIMARY KEY, value INTEGER NOT NULL)`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	var failures atomic.Int64
	var workers sync.WaitGroup
	until := time.Now().Add(10 * time.Second)
	for i := 0; i < 40; i++ {
		workers.Add(1)
		go func(writer bool) {
			defer workers.Done()
			for time.Now().Before(until) {
				if writer {
					err := store.Write(context.Background(), func(tx *sql.Tx) error {
						_, err := tx.Exec(`INSERT INTO counters (value) VALUES (1)`)
						return err
					})
					if err != nil && isSQLiteBusy(err) {
						failures.Add(1)
					}
				} else {
					err := store.Read(context.Background(), func(ctx context.Context, reader Reader) error {
						var count int
						return reader.QueryRowContext(ctx, `SELECT COUNT(*) FROM counters`).Scan(&count)
					})
					if err != nil && isSQLiteBusy(err) {
						failures.Add(1)
					}
				}
			}
		}(i < 20)
	}
	workers.Wait()
	if count := failures.Load(); count != 0 {
		t.Fatalf("observed %d database locked errors", count)
	}
}
