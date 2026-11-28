package database

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

func TestMigrateAppSecretsToCanonicalRowsPreservesRotationData(t *testing.T) {
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "legacy-secrets.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	_, err = db.Exec(`CREATE TABLE app_secrets (
		name TEXT PRIMARY KEY,
		value TEXT NOT NULL,
		previous_value TEXT,
		previous_expires_at INTEGER,
		created_at INTEGER NOT NULL,
		rotated_at INTEGER
	)`)
	require.NoError(t, err)
	created := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	rotated := created.Add(24 * time.Hour)
	expires := rotated.Add(7 * 24 * time.Hour)
	_, err = db.Exec(`INSERT INTO app_secrets (name, value, previous_value, previous_expires_at, created_at, rotated_at) VALUES (?, ?, ?, ?, ?, ?)`, "cookie_signing_key", "current-b64", "previous-b64", expires.Unix(), created.Unix(), rotated.Unix())
	require.NoError(t, err)
	require.NoError(t, migrateAppSecretsSchema(context.Background(), db, "sqlite"))
	rows, err := db.Query(`PRAGMA table_info(app_secrets)`)
	require.NoError(t, err)
	columns := make([]string, 0, 3)
	for rows.Next() {
		var cid, notNull, primaryKey int
		var name, dataType string
		var defaultValue sql.NullString
		require.NoError(t, rows.Scan(&cid, &name, &dataType, &notNull, &defaultValue, &primaryKey))
		columns = append(columns, name)
	}
	require.NoError(t, errors.Join(rows.Err(), rows.Close()))
	require.Equal(t, []string{"name", "value", "updated_at"}, columns)
	values := map[string]string{}
	rows, err = db.Query(`SELECT name, value FROM app_secrets`)
	require.NoError(t, err)
	for rows.Next() {
		var name, value string
		require.NoError(t, rows.Scan(&name, &value))
		values[name] = value
	}
	require.NoError(t, errors.Join(rows.Err(), rows.Close()))
	require.Equal(t, "current-b64", values["cookie_signing_key"])
	require.Equal(t, "previous-b64", values["cookie_signing_key_previous"])
	require.Equal(t, expires.Format(time.RFC3339Nano), values["cookie_signing_key_previous_until"])
	require.Equal(t, rotated.Format(time.RFC3339Nano), values["cookie_signing_key_at"])
}

func TestAppSecretsBackupRoundTripPreservesCanonicalRotationRows(t *testing.T) {
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "app-secrets.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	_, err = db.Exec(`CREATE TABLE app_secrets (name TEXT PRIMARY KEY, value TEXT NOT NULL, updated_at TEXT NOT NULL)`)
	require.NoError(t, err)
	now := time.Date(2026, 5, 6, 7, 8, 9, 0, time.UTC).Format(time.RFC3339Nano)
	for _, row := range [][3]string{
		{SecretInstallationSecret, "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=", now},
		{SecretCookieSigningKey, "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=", now},
		{SecretCSRFTokenSecret, "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=", now},
		{SecretCookieSigningKey + "_previous", "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=", now},
		{SecretCookieSigningKey + "_previous_until", now, now},
	} {
		_, err := db.Exec(`INSERT INTO app_secrets (name, value, updated_at) VALUES (?, ?, ?)`, row[0], row[1], row[2])
		require.NoError(t, err)
	}
	mu.Lock()
	previousDB, previousStore, previousDriver, previousCache := serverDB, serverStore, serverDriver, serverCache
	serverDB, serverStore, serverDriver, serverCache = db, nil, "sqlite", nil
	mu.Unlock()
	t.Cleanup(func() {
		mu.Lock()
		serverDB, serverStore, serverDriver, serverCache = previousDB, previousStore, previousDriver, previousCache
		mu.Unlock()
	})
	invalidPayload, err := json.Marshal([]backupSecretRow{
		{Name: SecretInstallationSecret, Value: "invalid-key", UpdatedAt: now},
		{Name: SecretCookieSigningKey, Value: "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=", UpdatedAt: now},
		{Name: SecretCSRFTokenSecret, Value: "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=", UpdatedAt: now},
	})
	require.NoError(t, err)
	require.Error(t, RestoreAppSecretsSnapshot(context.Background(), invalidPayload, false))
	var rowCount int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM app_secrets`).Scan(&rowCount))
	require.Equal(t, 5, rowCount)
	payload, err := ExportAppSecretsSnapshot(context.Background())
	require.NoError(t, err)
	_, err = db.Exec(`DELETE FROM app_secrets`)
	require.NoError(t, err)
	require.NoError(t, RestoreAppSecretsSnapshot(context.Background(), payload, false))
	var restored string
	require.NoError(t, db.QueryRow(`SELECT value FROM app_secrets WHERE name = ?`, SecretCookieSigningKey+"_previous").Scan(&restored))
	require.Equal(t, "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=", restored)
	_, err = db.Exec(`UPDATE app_secrets SET value = 'existing' WHERE name = ?`, SecretCookieSigningKey)
	require.NoError(t, err)
	require.NoError(t, RestoreAppSecretsSnapshot(context.Background(), payload, true))
	require.NoError(t, db.QueryRow(`SELECT value FROM app_secrets WHERE name = ?`, SecretCookieSigningKey).Scan(&restored))
	require.Equal(t, "existing", restored)
}

func TestExportAppSecretsFromSQLiteSnapshotReadsSnapshotFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "snapshot.db")
	db, err := sql.Open("sqlite", path)
	require.NoError(t, err)
	_, err = db.Exec(`CREATE TABLE app_secrets (name TEXT PRIMARY KEY, value TEXT NOT NULL, updated_at TEXT NOT NULL)`)
	require.NoError(t, err)
	updatedAt := time.Date(2026, 11, 27, 12, 0, 0, 0, time.UTC).Format(time.RFC3339Nano)
	_, err = db.Exec(`INSERT INTO app_secrets (name, value, updated_at) VALUES (?, ?, ?)`, SecretInstallationSecret, "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=", updatedAt)
	require.NoError(t, err)
	require.NoError(t, db.Close())
	payload, err := ExportAppSecretsFromSQLiteSnapshot(context.Background(), path)
	require.NoError(t, err)
	var rows []backupSecretRow
	require.NoError(t, json.Unmarshal(payload, &rows))
	require.Len(t, rows, 1)
	require.Equal(t, SecretInstallationSecret, rows[0].Name)
	require.Equal(t, updatedAt, rows[0].UpdatedAt)
}
