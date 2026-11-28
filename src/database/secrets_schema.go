package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

type appSecretsDB interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func migrateAppSecretsSchema(ctx context.Context, db appSecretsDB, driver string) error {
	if starter, ok := db.(interface {
		BeginTx(context.Context, *sql.TxOptions) (*sql.Tx, error)
	}); ok {
		tx, err := starter.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		if err := migrateAppSecretsSchemaInTx(ctx, tx, driver); err != nil {
			_ = tx.Rollback()
			return err
		}
		return tx.Commit()
	}
	return migrateAppSecretsSchemaInTx(ctx, db, driver)
}

func migrateAppSecretsSchemaInTx(ctx context.Context, db appSecretsDB, driver string) error {
	table := tableNameForDriver(driver, "app_secrets")
	rows, err := db.QueryContext(ctx, "PRAGMA table_info("+table+")")
	if err != nil {
		return fmt.Errorf("inspect app_secrets schema: %w", err)
	}
	columns := make(map[string]bool)
	for rows.Next() {
		var cid, notNull, primaryKey int
		var name, dataType string
		var defaultValue sql.NullString
		if err := rows.Scan(&cid, &name, &dataType, &notNull, &defaultValue, &primaryKey); err != nil {
			_ = rows.Close()
			return err
		}
		columns[name] = true
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		return err
	}
	if len(columns) == 3 && columns["name"] && columns["value"] && columns["updated_at"] {
		values, err := db.QueryContext(ctx, "SELECT updated_at FROM "+table)
		if err != nil {
			return err
		}
		canonical := true
		for values.Next() {
			var updatedAt string
			if err := values.Scan(&updatedAt); err != nil {
				_ = values.Close()
				return err
			}
			if _, err := time.Parse(time.RFC3339Nano, updatedAt); err != nil {
				canonical = false
			}
		}
		if err := errors.Join(values.Err(), values.Close()); err != nil {
			return err
		}
		if canonical {
			return nil
		}
	}
	if len(columns) == 0 {
		return fmt.Errorf("app_secrets table %s is missing", table)
	}
	staging := tableNameForDriver(driver, "app_secrets_migrating")
	if _, err := db.ExecContext(ctx, "DROP TABLE IF EXISTS "+staging); err != nil {
		return err
	}
	if _, err := db.ExecContext(ctx, "CREATE TABLE "+staging+` (
		name TEXT PRIMARY KEY,
		value TEXT NOT NULL,
		updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
	)`); err != nil {
		return err
	}
	selectColumn := func(name, fallback string) string {
		if columns[name] {
			return "CAST(" + name + " AS TEXT)"
		}
		return fallback
	}
	query := "SELECT name, value, " + selectColumn("updated_at", "NULL") + ", " +
		selectColumn("created_at", "NULL") + ", " + selectColumn("previous_value", "NULL") + ", " +
		selectColumn("previous_expires_at", "NULL") + ", " + selectColumn("rotated_at", "NULL") +
		" FROM " + table + " ORDER BY name"
	legacy, err := db.QueryContext(ctx, query)
	if err != nil {
		return fmt.Errorf("read legacy app_secrets rows: %w", err)
	}
	for legacy.Next() {
		var name, value string
		var updated, created, previous, expires, rotated sql.NullString
		if err := legacy.Scan(&name, &value, &updated, &created, &previous, &expires, &rotated); err != nil {
			_ = legacy.Close()
			return err
		}
		updatedAt := migrationTimestamp(updated.String)
		if updatedAt == "" {
			updatedAt = migrationTimestamp(created.String)
		}
		if updatedAt == "" {
			updatedAt = time.Now().UTC().Format(time.RFC3339Nano)
		}
		if _, err := db.ExecContext(ctx, "INSERT INTO "+staging+" (name, value, updated_at) VALUES (?, ?, ?) ON CONFLICT(name) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at", name, value, updatedAt); err != nil {
			_ = legacy.Close()
			return err
		}
		if previous.Valid && previous.String != "" {
			if _, err := db.ExecContext(ctx, "INSERT INTO "+staging+" (name, value, updated_at) VALUES (?, ?, ?) ON CONFLICT(name) DO NOTHING", name+"_previous", previous.String, updatedAt); err != nil {
				_ = legacy.Close()
				return err
			}
		}
		if expires.Valid && expires.String != "" {
			expiresAt := migrationTimestamp(expires.String)
			if expiresAt == "" {
				_ = legacy.Close()
				return fmt.Errorf("legacy app_secrets expiry for %s is invalid", name)
			}
			if _, err := db.ExecContext(ctx, "INSERT INTO "+staging+" (name, value, updated_at) VALUES (?, ?, ?) ON CONFLICT(name) DO NOTHING", name+"_previous_until", expiresAt, updatedAt); err != nil {
				_ = legacy.Close()
				return err
			}
		}
		if rotated.Valid && rotated.String != "" {
			rotatedAt := migrationTimestamp(rotated.String)
			if rotatedAt == "" {
				_ = legacy.Close()
				return fmt.Errorf("legacy app_secrets rotation time for %s is invalid", name)
			}
			if _, err := db.ExecContext(ctx, "INSERT INTO "+staging+" (name, value, updated_at) VALUES (?, ?, ?) ON CONFLICT(name) DO NOTHING", name+"_at", rotatedAt, updatedAt); err != nil {
				_ = legacy.Close()
				return err
			}
		}
	}
	if err := errors.Join(legacy.Err(), legacy.Close()); err != nil {
		return err
	}
	if _, err := db.ExecContext(ctx, "DROP TABLE "+table); err != nil {
		return err
	}
	if _, err := db.ExecContext(ctx, "ALTER TABLE "+staging+" RENAME TO "+table); err != nil {
		return err
	}
	return nil
}

func migrationTimestamp(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if unix, err := strconv.ParseInt(value, 10, 64); err == nil {
		return time.Unix(unix, 0).UTC().Format(time.RFC3339Nano)
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02 15:04:05"} {
		parsed, err := time.Parse(layout, value)
		if err == nil {
			return parsed.UTC().Format(time.RFC3339Nano)
		}
	}
	return ""
}
