package database

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// ErrSecurityPGPMetadataNotFound means no project key metadata is recorded.
var ErrSecurityPGPMetadataNotFound = errors.New("security PGP metadata not found")

// SecurityPGPMetadata stores public key metadata without storing either key.
type SecurityPGPMetadata struct {
	Fingerprint         string            `json:"fingerprint"`
	CreatedAt           time.Time         `json:"created_at"`
	ExpiresAt           time.Time         `json:"expires_at"`
	LastRotatedAt       *time.Time        `json:"last_rotated_at,omitempty"`
	KeyserversPublished map[string]string `json:"keyservers_published"`
	Revoked             bool              `json:"revoked"`
}

func ensureSecurityPGPTable(ctx context.Context) error {
	if serverDB == nil {
		return fmt.Errorf("security PGP: database is not initialized")
	}
	_, err := execContext(ctx, serverDB, `CREATE TABLE IF NOT EXISTS security_pgp_metadata (
		singleton INTEGER PRIMARY KEY CHECK (singleton = 1),
		fingerprint TEXT NOT NULL,
		created_at TEXT NOT NULL,
		expires_at TEXT NOT NULL,
		last_rotated_at TEXT,
		keyservers_published TEXT NOT NULL DEFAULT '{}',
		revoked BOOLEAN NOT NULL DEFAULT FALSE
	)`)
	if err != nil {
		return fmt.Errorf("security PGP: create metadata table: %w", err)
	}
	_, err = execContext(ctx, serverDB, `CREATE TABLE IF NOT EXISTS security_pgp_exports (
		actor TEXT PRIMARY KEY,
		last_export_at TEXT NOT NULL
	)`)
	if err != nil {
		return fmt.Errorf("security PGP: create export limiter: %w", err)
	}
	return nil
}

// ClaimSecurityPGPExport atomically enforces one private-key export per
// operator per hour across concurrent processes and database connections.
func ClaimSecurityPGPExport(ctx context.Context, actor string, now time.Time) (bool, error) {
	if strings.TrimSpace(actor) == "" {
		return false, fmt.Errorf("security PGP: export actor is required")
	}
	if err := ensureSecurityPGPTable(ctx); err != nil {
		return false, err
	}
	cutoff := now.UTC().Add(-time.Hour).Format(time.RFC3339Nano)
	result, err := execContext(ctx, serverDB, `INSERT INTO security_pgp_exports (actor, last_export_at)
		VALUES (?, ?)
		ON CONFLICT(actor) DO UPDATE SET last_export_at = excluded.last_export_at
		WHERE security_pgp_exports.last_export_at <= ?`, actor, now.UTC().Format(time.RFC3339Nano), cutoff)
	if err != nil {
		return false, fmt.Errorf("security PGP: claim private-key export: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("security PGP: inspect private-key export claim: %w", err)
	}
	return rows == 1, nil
}

// HasPGPSecurityReports reports whether retained disclosure bodies depend on
// an installed PGP private key.
func HasPGPSecurityReports(ctx context.Context) (bool, error) {
	if serverDB == nil {
		return false, fmt.Errorf("security PGP: database is not initialized")
	}
	var count int
	if err := queryRowContext(ctx, serverDB, `SELECT COUNT(*) FROM security_reports WHERE body_alg = 'pgp'`).Scan(&count); err != nil {
		return false, fmt.Errorf("security PGP: count PGP-encrypted reports: %w", err)
	}
	return count > 0, nil
}

// GetSecurityPGPMetadata returns the active or last-revoked key metadata.
func GetSecurityPGPMetadata(ctx context.Context) (SecurityPGPMetadata, error) {
	if err := ensureSecurityPGPTable(ctx); err != nil {
		return SecurityPGPMetadata{}, err
	}
	var result SecurityPGPMetadata
	var created, expires, rotated, published string
	err := queryRowContext(ctx, serverDB, `SELECT fingerprint, created_at, expires_at,
		COALESCE(last_rotated_at, ''), keyservers_published, revoked
		FROM security_pgp_metadata WHERE singleton = 1`).Scan(
		&result.Fingerprint, &created, &expires, &rotated, &published, &result.Revoked)
	if err != nil {
		if err == sql.ErrNoRows {
			return SecurityPGPMetadata{}, ErrSecurityPGPMetadataNotFound
		}
		return SecurityPGPMetadata{}, fmt.Errorf("security PGP: load metadata: %w", err)
	}
	var parseErr error
	if result.CreatedAt, parseErr = time.Parse(time.RFC3339Nano, created); parseErr != nil {
		return SecurityPGPMetadata{}, fmt.Errorf("security PGP: invalid creation time")
	}
	if result.ExpiresAt, parseErr = time.Parse(time.RFC3339Nano, expires); parseErr != nil {
		return SecurityPGPMetadata{}, fmt.Errorf("security PGP: invalid expiration time")
	}
	if rotated != "" {
		parsed, err := time.Parse(time.RFC3339Nano, rotated)
		if err != nil {
			return SecurityPGPMetadata{}, fmt.Errorf("security PGP: invalid rotation time")
		}
		result.LastRotatedAt = &parsed
	}
	result.KeyserversPublished = map[string]string{}
	if err := json.Unmarshal([]byte(published), &result.KeyserversPublished); err != nil {
		return SecurityPGPMetadata{}, fmt.Errorf("security PGP: decode keyserver metadata: %w", err)
	}
	return result, nil
}

// SaveSecurityPGPMetadata atomically replaces the singleton key metadata.
func SaveSecurityPGPMetadata(ctx context.Context, metadata SecurityPGPMetadata) error {
	if len(metadata.Fingerprint) != 40 {
		return fmt.Errorf("security PGP: fingerprint must be a 40-character OpenPGP v4 fingerprint")
	}
	if err := ensureSecurityPGPTable(ctx); err != nil {
		return err
	}
	published, err := json.Marshal(metadata.KeyserversPublished)
	if err != nil {
		return fmt.Errorf("security PGP: encode keyserver metadata: %w", err)
	}
	var rotated any
	if metadata.LastRotatedAt != nil {
		rotated = metadata.LastRotatedAt.UTC().Format(time.RFC3339Nano)
	}
	_, err = execContext(ctx, serverDB, `INSERT INTO security_pgp_metadata
		(singleton, fingerprint, created_at, expires_at, last_rotated_at, keyservers_published, revoked)
		VALUES (1, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(singleton) DO UPDATE SET fingerprint = excluded.fingerprint,
		created_at = excluded.created_at, expires_at = excluded.expires_at,
		last_rotated_at = excluded.last_rotated_at, keyservers_published = excluded.keyservers_published,
		revoked = excluded.revoked`, metadata.Fingerprint, metadata.CreatedAt.UTC().Format(time.RFC3339Nano),
		metadata.ExpiresAt.UTC().Format(time.RFC3339Nano), rotated, string(published), metadata.Revoked)
	if err != nil {
		return fmt.Errorf("security PGP: save metadata: %w", err)
	}
	return nil
}

// ReencryptPGPSecurityReports updates all PGP-encrypted report bodies while
// excluding concurrent submissions. It reads and transforms rows before the
// short write transaction begins.
func ReencryptPGPSecurityReports(ctx context.Context, transform func([]byte) ([]byte, error)) error {
	if serverDB == nil {
		return fmt.Errorf("security PGP: database is not initialized")
	}
	if transform == nil {
		return fmt.Errorf("security PGP: report transform is required")
	}
	securityReportMu.Lock()
	defer securityReportMu.Unlock()
	rows, err := queryContext(ctx, serverDB, `SELECT tracking_id, body_enc FROM security_reports WHERE body_alg = 'pgp'`)
	if err != nil {
		return fmt.Errorf("security PGP: list reports for rotation: %w", err)
	}
	type reportBody struct {
		id   string
		body []byte
	}
	var reports []reportBody
	for rows.Next() {
		var report reportBody
		if err := rows.Scan(&report.id, &report.body); err != nil {
			_ = rows.Close()
			return fmt.Errorf("security PGP: read report for rotation: %w", err)
		}
		reports = append(reports, report)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return fmt.Errorf("security PGP: iterate reports for rotation: %w", err)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("security PGP: close reports for rotation: %w", err)
	}
	type transformedBody struct {
		id   string
		body []byte
	}
	updated := make([]transformedBody, 0, len(reports))
	for _, report := range reports {
		body, err := transform(report.body)
		if err != nil {
			return fmt.Errorf("security PGP: reencrypt report %s: %w", report.id, err)
		}
		updated = append(updated, transformedBody{id: report.id, body: body})
	}
	if err := withWriteTransaction(ctx, serverDB, func(tx *sql.Tx) error {
		updatedAt := time.Now().UTC().Format(time.RFC3339)
		for _, report := range updated {
			if _, err := execTxContext(ctx, tx, `UPDATE security_reports SET body_enc = ?, updated_at = ? WHERE tracking_id = ?`, report.body, updatedAt, report.id); err != nil {
				return fmt.Errorf("update report %s: %w", report.id, err)
			}
		}
		return nil
	}); err != nil {
		return fmt.Errorf("security PGP: commit report rotation: %w", err)
	}
	return nil
}

// RotatePGPSecurityReports keeps report submissions excluded while report
// ciphertext and key metadata are changed, then activates staged key files
// after the transaction commits. No file I/O runs inside the transaction.
func RotatePGPSecurityReports(ctx context.Context, metadata SecurityPGPMetadata, transform func([]byte) ([]byte, error), activate func() error, rollbackFiles func() error) error {
	if serverDB == nil {
		return fmt.Errorf("security PGP: database is not initialized")
	}
	if transform == nil || activate == nil || rollbackFiles == nil {
		return fmt.Errorf("security PGP: rotation callbacks are required")
	}
	if len(metadata.Fingerprint) != 40 {
		return fmt.Errorf("security PGP: fingerprint must be a 40-character OpenPGP v4 fingerprint")
	}
	if err := ensureSecurityPGPTable(ctx); err != nil {
		return err
	}
	published, err := json.Marshal(metadata.KeyserversPublished)
	if err != nil {
		return fmt.Errorf("security PGP: encode keyserver metadata: %w", err)
	}
	var rotated any
	if metadata.LastRotatedAt != nil {
		rotated = metadata.LastRotatedAt.UTC().Format(time.RFC3339Nano)
	}
	securityReportMu.Lock()
	defer securityReportMu.Unlock()
	tx, err := serverDB.BeginTx(ctx, nil)
	if err != nil {
		return errors.Join(fmt.Errorf("security PGP: begin report rotation: %w", err), rollbackFiles())
	}
	defer func() { _ = tx.Rollback() }()
	failBeforeCommit := func(cause error) error {
		_ = tx.Rollback()
		return errors.Join(cause, rollbackFiles())
	}
	rows, err := queryTxContext(ctx, tx, `SELECT tracking_id, body_enc FROM security_reports WHERE body_alg = 'pgp'`)
	if err != nil {
		return failBeforeCommit(fmt.Errorf("security PGP: list reports for rotation: %w", err))
	}
	type reportBody struct {
		id   string
		body []byte
	}
	var reports []reportBody
	for rows.Next() {
		var report reportBody
		if err := rows.Scan(&report.id, &report.body); err != nil {
			_ = rows.Close()
			return failBeforeCommit(fmt.Errorf("security PGP: read report for rotation: %w", err))
		}
		reports = append(reports, report)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return failBeforeCommit(fmt.Errorf("security PGP: iterate reports for rotation: %w", err))
	}
	if err := rows.Close(); err != nil {
		return failBeforeCommit(fmt.Errorf("security PGP: close reports for rotation: %w", err))
	}
	for _, report := range reports {
		body, err := transform(report.body)
		if err != nil {
			return failBeforeCommit(fmt.Errorf("security PGP: reencrypt report %s: %w", report.id, err))
		}
		if _, err := execTxContext(ctx, tx, `UPDATE security_reports SET body_enc = ?, updated_at = ? WHERE tracking_id = ?`, body, time.Now().UTC().Format(time.RFC3339), report.id); err != nil {
			return failBeforeCommit(fmt.Errorf("security PGP: update report %s: %w", report.id, err))
		}
	}
	if _, err := execTxContext(ctx, tx, `INSERT INTO security_pgp_metadata
		(singleton, fingerprint, created_at, expires_at, last_rotated_at, keyservers_published, revoked)
		VALUES (1, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(singleton) DO UPDATE SET fingerprint = excluded.fingerprint,
		created_at = excluded.created_at, expires_at = excluded.expires_at,
		last_rotated_at = excluded.last_rotated_at, keyservers_published = excluded.keyservers_published,
		revoked = excluded.revoked`, metadata.Fingerprint, metadata.CreatedAt.UTC().Format(time.RFC3339Nano),
		metadata.ExpiresAt.UTC().Format(time.RFC3339Nano), rotated, string(published), metadata.Revoked); err != nil {
		return failBeforeCommit(fmt.Errorf("security PGP: update rotation metadata: %w", err))
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("security PGP: commit rotation: %w", err)
	}
	refreshServerCacheAfterWrite(ctx, serverDB)
	if err := activate(); err != nil {
		return fmt.Errorf("security PGP: database rotation committed; staged key activation needs recovery: %w", err)
	}
	return nil
}

// SecurityReportPGPBodies returns copies of retained PGP-encrypted report
// bodies for staged-key crash recovery.
func SecurityReportPGPBodies(ctx context.Context) ([][]byte, error) {
	if serverDB == nil {
		return nil, fmt.Errorf("security PGP: database is not initialized")
	}
	securityReportMu.Lock()
	defer securityReportMu.Unlock()
	rows, err := queryContext(ctx, serverDB, `SELECT body_enc FROM security_reports WHERE body_alg = 'pgp'`)
	if err != nil {
		return nil, fmt.Errorf("security PGP: list encrypted report bodies: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var bodies [][]byte
	for rows.Next() {
		var body []byte
		if err := rows.Scan(&body); err != nil {
			return nil, fmt.Errorf("security PGP: read encrypted report body: %w", err)
		}
		bodies = append(bodies, append([]byte(nil), body...))
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("security PGP: iterate encrypted report bodies: %w", err)
	}
	return bodies, nil
}
