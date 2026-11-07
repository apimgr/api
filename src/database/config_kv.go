package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// GetConfigValue reads one key from the config key/value table. A missing key
// returns an empty string and a nil error, so callers can treat "never set"
// and "set to empty" identically without special-casing sql.ErrNoRows.
//
// The config table holds internal server-managed state only (AI.md PART 8);
// operator-editable settings live in server.yml, never here.
func GetConfigValue(ctx context.Context, key string) (string, error) {
	db := GetServerDB()
	if db == nil {
		return "", fmt.Errorf("config: database not initialized")
	}

	const selectQuery = `SELECT value FROM config WHERE key = ?`

	queryCtx, cancel := context.WithTimeout(ctx, timeoutSimpleSelect)
	defer cancel()

	var value sql.NullString
	if err := queryRowContext(queryCtx, db, selectQuery, key).Scan(&value); err != nil {
		recordQueryError(selectQuery, err)
		if errors.Is(err, sql.ErrNoRows) {
			return "", nil
		}
		return "", fmt.Errorf("config: read %s: %w", key, err)
	}
	return value.String, nil
}

// SetConfigValue writes one string key to the config key/value table,
// inserting it or replacing the existing value and refreshing updated_at.
func SetConfigValue(ctx context.Context, key, value string) error {
	db := GetServerDB()
	if db == nil {
		return fmt.Errorf("config: database not initialized")
	}

	queryCtx, cancel := context.WithTimeout(ctx, timeoutWrite)
	defer cancel()

	if _, err := execContext(queryCtx, db, `
		INSERT INTO config (key, value, type, updated_at)
		VALUES (?, ?, 'string', strftime('%s', 'now'))
		ON CONFLICT(key) DO UPDATE SET
			value = excluded.value,
			updated_at = excluded.updated_at
	`, key, value); err != nil {
		return fmt.Errorf("config: write %s: %w", key, err)
	}
	return nil
}

// PingServerDB verifies the server database connection is usable. It is the
// connectivity probe used by health checks, so callers never need a raw
// *sql.DB handle just to test liveness.
func PingServerDB(ctx context.Context) error {
	db := GetServerDB()
	if db == nil {
		return fmt.Errorf("database not initialized")
	}
	return db.PingContext(ctx)
}
