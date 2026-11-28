package database

import (
	"context"
	"database/sql"
	"encoding/base64"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestLibSQLCacheSyncCopiesRemoteRows(t *testing.T) {
	ctx := context.Background()
	remote, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "remote.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = remote.Close() })
	_, err = remote.Exec(`CREATE TABLE srv_items (id INTEGER PRIMARY KEY, value TEXT NOT NULL); INSERT INTO srv_items (id, value) VALUES (1, 'remote')`)
	require.NoError(t, err)

	cache, err := openLibSQLCache(filepath.Join(t.TempDir(), "cache.db"), 2, time.Second, time.Minute, true)
	require.NoError(t, err)
	t.Cleanup(func() { _ = cache.store.Close() })
	cache.remote = remote
	_, err = cache.store.writeDB.Exec(`CREATE TABLE items (id INTEGER PRIMARY KEY, value TEXT NOT NULL)`)
	require.NoError(t, err)

	require.NoError(t, cache.syncAndMark(ctx))
	var value string
	require.NoError(t, cache.store.readDB.QueryRow(`SELECT value FROM items WHERE id = 1`).Scan(&value))
	require.Equal(t, "remote", value)
	var prefixedTables int
	require.NoError(t, cache.store.readDB.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'srv_items'`).Scan(&prefixedTables))
	require.Zero(t, prefixedTables)
	require.Equal(t, `SELECT * FROM items`, sqliteCacheSQL(`SELECT * FROM srv_items`))
	require.True(t, cache.fresh(time.Now().UTC()))
}

func TestSeedRestoredSQLiteOnlyIntoEmptyRemote(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	sourcePath := filepath.Join(dir, "server.db")
	source, err := sql.Open("sqlite", sourcePath)
	require.NoError(t, err)
	_, err = source.Exec(`CREATE TABLE owners (id TEXT PRIMARY KEY); INSERT INTO owners (id) VALUES ('restored')`)
	require.NoError(t, err)

	remote, err := sql.Open("sqlite", filepath.Join(dir, "remote.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = remote.Close() })
	_, err = remote.Exec(`CREATE TABLE srv_config_meta (id INTEGER PRIMARY KEY); INSERT INTO srv_config_meta (id) VALUES (1); CREATE TABLE srv_owners (id TEXT PRIMARY KEY)`)
	require.NoError(t, err)

	require.NoError(t, seedRestoredSQLite(ctx, remote, sourcePath))
	var owner string
	require.NoError(t, remote.QueryRow(`SELECT id FROM srv_owners`).Scan(&owner))
	require.Equal(t, "restored", owner)

	_, err = source.Exec(`INSERT INTO owners (id) VALUES ('later')`)
	require.NoError(t, err)
	require.NoError(t, seedRestoredSQLite(ctx, remote, sourcePath))
	var count int
	require.NoError(t, remote.QueryRow(`SELECT COUNT(*) FROM srv_owners`).Scan(&count))
	require.Equal(t, 1, count)
}

func TestInstallRestoredSQLiteCacheReplacesCacheFromBackup(t *testing.T) {
	dir := t.TempDir()
	sourcePath := filepath.Join(dir, "server.db")
	source, err := sql.Open("sqlite", sourcePath)
	require.NoError(t, err)
	_, err = source.Exec(`CREATE TABLE owners (id TEXT PRIMARY KEY); INSERT INTO owners (id) VALUES ('restored-owner')`)
	require.NoError(t, err)
	require.NoError(t, source.Close())

	cachePath := filepath.Join(dir, "cache.db")
	cache, err := sql.Open("sqlite", cachePath)
	require.NoError(t, err)
	_, err = cache.Exec(`CREATE TABLE old_cache (value TEXT)`)
	require.NoError(t, err)
	require.NoError(t, cache.Close())

	require.NoError(t, installRestoredSQLiteCache(sourcePath, cachePath))
	seeded, err := sql.Open("sqlite", cachePath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = seeded.Close() })
	var owner string
	require.NoError(t, seeded.QueryRow(`SELECT id FROM owners`).Scan(&owner))
	require.Equal(t, "restored-owner", owner)
	var oldTables int
	require.NoError(t, seeded.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'old_cache'`).Scan(&oldTables))
	require.Zero(t, oldTables)
}

func TestLibSQLCacheFreshnessUsesConfiguredMaximumAge(t *testing.T) {
	cache := &libSQLCache{maxAge: time.Minute, lastSync: time.Unix(100, 0).UTC()}
	require.True(t, cache.fresh(time.Unix(160, 0).UTC()))
	require.False(t, cache.fresh(time.Unix(161, 0).UTC()))
	cache.lastSync = time.Time{}
	require.False(t, cache.fresh(time.Unix(100, 0).UTC()))
}

func TestRewriteSQLTablesPrefixesRemoteObjectsOnly(t *testing.T) {
	mu.Lock()
	previousDriver := serverDriver
	serverDriver = "libsql"
	mu.Unlock()
	t.Cleanup(func() {
		mu.Lock()
		serverDriver = previousDriver
		mu.Unlock()
	})

	query := `INSERT INTO api_tokens (owner_id) VALUES (?)`
	require.Equal(t, `INSERT INTO srv_api_tokens (owner_id) VALUES (?)`, rewriteSQLTables(query))
	require.Equal(t, `SELECT r.tracking_id FROM srv_security_reports r JOIN srv_owners o ON o.id = r.tracking_id`, rewriteSQLTables(`SELECT r.tracking_id FROM security_reports r JOIN owners o ON o.id = r.tracking_id`))
	require.Equal(t, `CREATE INDEX IF NOT EXISTS srv_idx_captcha ON srv_captcha_challenges(expires_at)`, rewriteSQLTables(`CREATE INDEX IF NOT EXISTS idx_captcha ON captcha_challenges(expires_at)`))
	require.Equal(t, `SELECT 'api_tokens' FROM srv_owners`, rewriteSQLTables(`SELECT 'api_tokens' FROM owners`))
	require.Equal(t, `SELECT 'srv_owner', 'it''s srv_owner', "owners", [api_tokens], `+"`audit_log`"+` FROM owners -- srv_tokens
/* srv_config */`, sqliteCacheSQL(`SELECT 'srv_owner', 'it''s srv_owner', "srv_owners", [srv_api_tokens], `+"`srv_audit_log`"+` FROM srv_owners -- srv_tokens
/* srv_config */`))

	mu.Lock()
	serverDriver = "sqlite"
	mu.Unlock()
	require.Equal(t, query, rewriteSQLTables(query))
}

func TestTimedRowCacheNoRowsDoesNotFallThroughToRemote(t *testing.T) {
	dir := t.TempDir()
	cache, err := sql.Open("sqlite", filepath.Join(dir, "cache.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = cache.Close() })
	_, err = cache.Exec(`CREATE TABLE items (value TEXT)`)
	require.NoError(t, err)

	remote, err := sql.Open("sqlite", filepath.Join(dir, "remote.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = remote.Close() })
	_, err = remote.Exec(`CREATE TABLE items (value TEXT); INSERT INTO items (value) VALUES ('remote')`)
	require.NoError(t, err)

	row := timedRow{
		row:      cache.QueryRow(`SELECT value FROM items`),
		fallback: remote,
		query:    `SELECT value FROM items`,
		ctx:      context.Background(),
		cancel:   func() {},
	}
	var value string
	require.ErrorIs(t, row.Scan(&value), sql.ErrNoRows)
	require.Empty(t, value)
}

func TestLibSQLSchemaUsesPrefixedTablesAndMigrations(t *testing.T) {
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "remote-schema.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, createSchema(db, "libsql"))
	assertTablesExist(t, db, []string{
		"srv_config", "srv_config_meta", "srv_app_secrets", "srv_owners", "srv_api_tokens",
		"srv_audit_log", "srv_scheduler_tasks", "srv_scheduler_history", "srv_backups",
		"srv_security_reports", "srv_security_thanks", "srv_rate_limits", "srv_server_stats", "srv_server_request_hours",
	})
	var count int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM srv_config_meta`).Scan(&count))
	require.Equal(t, 1, count)
	columns, err := db.Query(`PRAGMA table_info(srv_app_secrets)`)
	require.NoError(t, err)
	columnNames := make([]string, 0, 3)
	for columns.Next() {
		var cid, notNull, primaryKey int
		var name, dataType string
		var defaultValue sql.NullString
		require.NoError(t, columns.Scan(&cid, &name, &dataType, &notNull, &defaultValue, &primaryKey))
		columnNames = append(columnNames, name)
	}
	require.NoError(t, errors.Join(columns.Err(), columns.Close()))
	require.Equal(t, []string{"name", "value", "updated_at"}, columnNames)
}

func TestEnsureAppSecretsUsesFreshOfflineCacheWithoutWriting(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	remote, err := sql.Open("sqlite", filepath.Join(dir, "remote.db"))
	require.NoError(t, err)
	require.NoError(t, createSchema(remote, "libsql"))
	cacheStore, err := OpenSQLite(filepath.Join(dir, "cache.db"), 2)
	require.NoError(t, err)
	require.NoError(t, createSchema(cacheStore.writeDB, "sqlite"))
	value := base64.StdEncoding.EncodeToString(make([]byte, 32))
	old := time.Now().UTC().AddDate(0, -7, 0).Unix()
	require.NoError(t, cacheStore.Write(ctx, func(tx *sql.Tx) error {
		for _, name := range []string{SecretInstallationSecret, SecretCookieSigningKey, SecretCSRFTokenSecret} {
			if _, err := tx.ExecContext(ctx, `INSERT INTO app_secrets (name, value, updated_at) VALUES (?, ?, ?)`, name, value, time.Unix(old, 0).UTC().Format(time.RFC3339Nano)); err != nil {
				return err
			}
		}
		return nil
	}))

	mu.Lock()
	previousDB, previousStore, previousDriver, previousCache := serverDB, serverStore, serverDriver, serverCache
	serverDB, serverStore, serverDriver = remote, nil, "libsql"
	serverCache = &libSQLCache{store: cacheStore, remote: remote, offlineMode: true, offlineOnly: true, maxAge: time.Hour, lastSync: time.Now().UTC()}
	mu.Unlock()
	t.Cleanup(func() {
		mu.Lock()
		serverDB, serverStore, serverDriver, serverCache = previousDB, previousStore, previousDriver, previousCache
		mu.Unlock()
		_ = cacheStore.Close()
		_ = remote.Close()
	})

	require.True(t, CacheReadOnlyOffline())
	require.NoError(t, EnsureAppSecrets(ctx))
	var remoteCount int
	require.NoError(t, remote.QueryRow(`SELECT COUNT(*) FROM srv_app_secrets`).Scan(&remoteCount))
	require.Zero(t, remoteCount)
	_, err = cacheStore.writeDB.Exec(`DELETE FROM app_secrets WHERE name = ?`, SecretCookieSigningKey)
	require.NoError(t, err)
	err = EnsureAppSecrets(ctx)
	require.ErrorContains(t, err, "missing from the fresh read-only libsql cache")
}
