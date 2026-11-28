package database

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
)

type backupSecretRow struct {
	Name      string `json:"name"`
	Value     string `json:"value"`
	UpdatedAt string `json:"updated_at"`
}

// ExportAppSecretsSnapshot returns every canonical generated-secret row,
// including previous values and expiry timestamps, for PART 21 backups.
func ExportAppSecretsSnapshot(ctx context.Context) (data []byte, returnErr error) {
	if serverDB == nil {
		return nil, errors.New("database handle is unavailable")
	}
	const query = `SELECT name, value, updated_at FROM app_secrets ORDER BY name`
	rows, err := queryContext(ctx, serverDB, query)
	if err != nil {
		return nil, fmt.Errorf("read app_secrets for backup: %w", err)
	}
	defer func() { returnErr = errors.Join(returnErr, rows.Close()) }()
	secrets := make([]backupSecretRow, 0)
	for rows.Next() {
		var row backupSecretRow
		if err := rows.Scan(&row.Name, &row.Value, &row.UpdatedAt); err != nil {
			return nil, err
		}
		secrets = append(secrets, row)
	}
	if err := rows.Err(); err != nil {
		recordQueryError(query, err)
		return nil, err
	}
	return json.Marshal(secrets)
}

// ExportAppSecretsFromSQLiteSnapshot reads app_secrets from an already
// consistent SQLite database snapshot, keeping generated secrets aligned
// with the server.db file in the same backup.
func ExportAppSecretsFromSQLiteSnapshot(ctx context.Context, path string) (data []byte, returnErr error) {
	if path == "" {
		return nil, errors.New("SQLite snapshot path is empty")
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open SQLite snapshot: %w", err)
	}
	defer func() { returnErr = errors.Join(returnErr, db.Close()) }()
	rows, err := db.QueryContext(ctx, `SELECT name, value, updated_at FROM app_secrets ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("read app_secrets from SQLite snapshot: %w", err)
	}
	secrets := make([]backupSecretRow, 0)
	for rows.Next() {
		var row backupSecretRow
		if err := rows.Scan(&row.Name, &row.Value, &row.UpdatedAt); err != nil {
			return nil, errors.Join(err, rows.Close())
		}
		secrets = append(secrets, row)
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		return nil, err
	}
	return json.Marshal(secrets)
}

// RestoreAppSecretsSnapshot replaces canonical app_secrets rows atomically.
// onlyWhenEmpty implements the remote-database restore rule.
func RestoreAppSecretsSnapshot(ctx context.Context, data []byte, onlyWhenEmpty bool) error {
	if serverDB == nil {
		return errors.New("database handle is unavailable")
	}
	if len(data) > 4<<20 {
		return errors.New("app_secrets backup exceeds 4 MiB")
	}
	var secrets []backupSecretRow
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&secrets); err != nil {
		return fmt.Errorf("decode app_secrets backup: %w", err)
	}
	if secrets == nil {
		return errors.New("app_secrets backup must be a JSON array")
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return errors.New("app_secrets backup contains trailing JSON data")
	}
	seen := make(map[string]struct{}, len(secrets))
	for _, row := range secrets {
		if row.Name == "" || row.Value == "" || row.UpdatedAt == "" {
			return errors.New("app_secrets backup contains an incomplete row")
		}
		if _, err := time.Parse(time.RFC3339Nano, row.UpdatedAt); err != nil {
			return errors.New("app_secrets backup contains an invalid updated_at timestamp")
		}
		if _, exists := seen[row.Name]; exists {
			return errors.New("app_secrets backup contains duplicate row names")
		}
		seen[row.Name] = struct{}{}
		if strings.HasSuffix(row.Name, "_until") || strings.HasSuffix(row.Name, "_at") {
			if _, err := time.Parse(time.RFC3339Nano, row.Value); err != nil {
				return errors.New("app_secrets backup contains an invalid secret timestamp")
			}
		}
		secretName := strings.TrimSuffix(row.Name, "_previous")
		if secretName == SecretInstallationSecret || secretName == SecretCookieSigningKey || secretName == SecretCSRFTokenSecret {
			decoded, err := base64.StdEncoding.DecodeString(row.Value)
			if err != nil || len(decoded) != secretByteLength {
				return fmt.Errorf("app_secrets backup contains invalid key material for %s", secretName)
			}
		}
		if row.Name == SecretEncryptionKeyPrevious || row.Name == SecretEncryptionKeyPending {
			wrapped, err := base64.StdEncoding.DecodeString(row.Value)
			if err != nil || len(wrapped) != 12+secretByteLength+16 {
				return errors.New("app_secrets backup contains invalid wrapped encryption-key material")
			}
		}
	}
	for _, required := range []string{SecretInstallationSecret, SecretCookieSigningKey, SecretCSRFTokenSecret} {
		if _, exists := seen[required]; !exists {
			return fmt.Errorf("app_secrets backup is missing required row %s", required)
		}
	}
	for name := range seen {
		if strings.HasSuffix(name, "_previous") {
			if _, exists := seen[name+"_until"]; !exists {
				return errors.New("app_secrets backup contains a previous value without its expiry row")
			}
		}
		if strings.HasSuffix(name, "_previous_until") {
			if _, exists := seen[strings.TrimSuffix(name, "_until")]; !exists {
				return errors.New("app_secrets backup contains a previous expiry without its value row")
			}
		}
	}
	return withWriteTransaction(ctx, serverDB, func(tx *sql.Tx) error {
		if onlyWhenEmpty {
			var count int
			if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM app_secrets`).Scan(&count); err != nil {
				return err
			}
			if count != 0 {
				return nil
			}
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM app_secrets`); err != nil {
			return err
		}
		for _, row := range secrets {
			if _, err := tx.ExecContext(ctx, `INSERT INTO app_secrets (name, value, updated_at) VALUES (?, ?, ?)`, row.Name, row.Value, row.UpdatedAt); err != nil {
				return err
			}
		}
		return nil
	})
}
