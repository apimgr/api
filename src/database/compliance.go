package database

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

var ErrOwnerPrefixNotFound = errors.New("owner token prefix not found")
var ErrOwnerPrefixAmbiguous = errors.New("owner token prefix is ambiguous")

type OwnerExportToken struct {
	Prefix    string     `json:"prefix"`
	Kind      string     `json:"kind"`
	Name      string     `json:"name"`
	CreatedAt time.Time  `json:"created_at"`
	LastUsed  *time.Time `json:"last_used_at"`
	ExpiresAt *time.Time `json:"expires_at"`
	Status    string     `json:"status"`
}

type OwnerDataExport struct {
	Format    int                `json:"format"`
	Owner     OwnerExportRecord  `json:"owner"`
	Tokens    []OwnerExportToken `json:"tokens"`
	Resources []any              `json:"resources"`
}

type OwnerExportRecord struct {
	ID         string    `json:"id"`
	CreatedAt  time.Time `json:"created_at"`
	LastSeenAt time.Time `json:"last_seen_at"`
}

type ComplianceSummary struct {
	Owners           int64
	ActiveTokens     int64
	OpenReports      int64
	Resources        int64
	EventsLast30Days int64
}

// GetComplianceSummary returns aggregate counts only; it never reads or
// returns owner, report, IP, or audit-event details.
func GetComplianceSummary(ctx context.Context, db *sql.DB, now time.Time) (ComplianceSummary, error) {
	if db == nil {
		return ComplianceSummary{}, errors.New("compliance database is unavailable")
	}
	var summary ComplianceSummary
	queries := []struct {
		query string
		dest  *int64
		args  []any
	}{
		{`SELECT COUNT(*) FROM ` + TableName("owners"), &summary.Owners, nil},
		{`SELECT COUNT(*) FROM ` + TableName("api_tokens") + ` WHERE revoked_at IS NULL AND (expires_at IS NULL OR expires_at > ?)`, &summary.ActiveTokens, []any{now.UTC().Unix()}},
		{`SELECT COUNT(*) FROM ` + TableName("security_reports") + ` WHERE closed_at IS NULL`, &summary.OpenReports, nil},
		{`SELECT COUNT(*) FROM ` + TableName("audit_log") + ` WHERE event LIKE 'compliance.%' AND timestamp >= ?`, &summary.EventsLast30Days, []any{now.UTC().Add(-30 * 24 * time.Hour).Format("2006-01-02T15:04:05.000Z")}},
	}
	for _, item := range queries {
		if err := queryRowContext(ctx, db, item.query, item.args...).Scan(item.dest); err != nil {
			return ComplianceSummary{}, err
		}
	}
	return summary, nil
}

type ComplianceStandardChange struct {
	Name    string
	Enabled bool
}

// ChangedComplianceStandards returns only transitions from a known stored
// value. A missing row is treated as disabled for the first observation.
func ChangedComplianceStandards(previous, current map[string]bool) []ComplianceStandardChange {
	names := make([]string, 0, len(current))
	for name := range current {
		names = append(names, name)
	}
	sort.Strings(names)
	changes := make([]ComplianceStandardChange, 0)
	for _, name := range names {
		if previous[name] != current[name] {
			changes = append(changes, ComplianceStandardChange{Name: name, Enabled: current[name]})
		}
	}
	return changes
}

// SyncComplianceStandards records enabled/disabled transitions on startup and
// persists the new snapshot only after the authoritative audit event succeeds.
func SyncComplianceStandards(ctx context.Context, standards map[string]bool) error {
	db := GetServerDB()
	if db == nil {
		return errors.New("compliance database is unavailable")
	}
	rows, err := queryContext(ctx, db, `SELECT name, enabled FROM `+TableName("compliance_standards"))
	if err != nil {
		return err
	}
	previous := make(map[string]bool)
	for rows.Next() {
		var name string
		var enabled bool
		if err := rows.Scan(&name, &enabled); err != nil {
			_ = rows.Close()
			return err
		}
		previous[name] = enabled
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for _, change := range ChangedComplianceStandards(previous, standards) {
		event := "compliance.standard_disabled"
		if change.Enabled {
			event = "compliance.standard_enabled"
		}
		if err := WriteAuditEventWithSource(ctx, event, "system", "", "", map[string]any{"standard": change.Name, "enabled_by": "configuration"}); err != nil {
			return err
		}
	}
	writeDB := serverWriteDB()
	if writeDB == nil {
		return errors.New("compliance database is unavailable")
	}
	tx, err := writeDB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	for name, enabled := range standards {
		if _, err := execTxContext(ctx, tx, `INSERT INTO `+TableName("compliance_standards")+` (name, enabled, updated_at)
			VALUES (?, ?, ?) ON CONFLICT(name) DO UPDATE SET enabled=excluded.enabled, updated_at=excluded.updated_at`, name, enabled, time.Now().UTC().Unix()); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// AuditIntegritySummary checks the active JSONL chain and reports only its
// status and line count, never entry content.
func AuditIntegritySummary() (string, int, error) {
	auditFile.mu.Lock()
	defer auditFile.mu.Unlock()
	path := auditFile.path
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		if _, anchorErr := os.Stat(filepath.Join(filepath.Dir(path), "audit.anchor")); anchorErr == nil {
			return fmt.Sprintf("broken at %s:0", filepath.Base(path)), 0, nil
		} else if !errors.Is(anchorErr, os.ErrNotExist) {
			return "", 0, anchorErr
		}
		return "intact", 0, nil
	}
	if err != nil {
		return "", 0, err
	}
	lines := strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
	if len(lines) == 1 && lines[0] == "" {
		lines = nil
	}
	previous := zeroAuditHash
	anchor, anchorErr := os.ReadFile(filepath.Join(filepath.Dir(path), "audit.anchor"))
	if anchorErr == nil {
		previous = strings.TrimSpace(string(anchor))
	} else if !errors.Is(anchorErr, os.ErrNotExist) {
		return "", len(lines), anchorErr
	}
	for index, line := range lines {
		var entry struct {
			Prev string `json:"prev"`
		}
		if err := json.Unmarshal([]byte(line), &entry); err != nil || entry.Prev != previous {
			return fmt.Sprintf("broken at %s:%d", filepath.Base(path), index+1), len(lines), nil
		}
		hash := sha256.Sum256([]byte(line))
		previous = hex.EncodeToString(hash[:])
	}
	return "intact", len(lines), nil
}

// ExportOwnerData returns only the selected owner's identity and token
// metadata. Token values, token hashes, other owners, and audit records are
// never included. This product currently has no owner-scoped resource tables.
func ExportOwnerData(ctx context.Context, db *sql.DB, prefix string) (OwnerDataExport, error) {
	ownerID, err := ownerIDByTokenPrefix(ctx, db, prefix)
	if err != nil {
		return OwnerDataExport{}, err
	}
	var createdAt, lastSeenAt int64
	if err := queryRowContext(ctx, db, `SELECT created_at, last_seen_at FROM `+TableName("owners")+` WHERE id = ?`, ownerID).Scan(&createdAt, &lastSeenAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return OwnerDataExport{}, ErrOwnerPrefixNotFound
		}
		return OwnerDataExport{}, fmt.Errorf("load owner record: %w", err)
	}
	rows, err := queryContext(ctx, db, `SELECT token_prefix, kind, COALESCE(name,''), created_at, last_used_at, expires_at, revoked_at
		FROM `+TableName("api_tokens")+` WHERE owner_id = ? ORDER BY kind, created_at`, ownerID)
	if err != nil {
		return OwnerDataExport{}, fmt.Errorf("load owner token metadata: %w", err)
	}
	tokens := make([]OwnerExportToken, 0)
	for rows.Next() {
		var token OwnerExportToken
		var created int64
		var lastUsed, expires, revoked sql.NullInt64
		if err := rows.Scan(&token.Prefix, &token.Kind, &token.Name, &created, &lastUsed, &expires, &revoked); err != nil {
			_ = rows.Close()
			return OwnerDataExport{}, err
		}
		token.CreatedAt = time.Unix(created, 0).UTC()
		token.LastUsed = nullableOwnerTime(lastUsed)
		token.ExpiresAt = nullableOwnerTime(expires)
		switch {
		case revoked.Valid:
			token.Status = "revoked"
		case token.ExpiresAt != nil && !token.ExpiresAt.After(time.Now().UTC()):
			token.Status = "expired"
		default:
			token.Status = "active"
		}
		tokens = append(tokens, token)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return OwnerDataExport{}, err
	}
	if err := rows.Close(); err != nil {
		return OwnerDataExport{}, err
	}
	return OwnerDataExport{
		Format: 1,
		Owner:  OwnerExportRecord{ID: ownerID, CreatedAt: time.Unix(createdAt, 0).UTC(), LastSeenAt: time.Unix(lastSeenAt, 0).UTC()},
		Tokens: tokens, Resources: []any{},
	}, nil
}

// DeleteOwnerByTokenPrefix removes one owner's records and every primary and
// linked token in one write transaction. The product has no owner resource
// tables, so the resource count is currently zero.
func DeleteOwnerByTokenPrefix(ctx context.Context, db *sql.DB, prefix string) (string, int64, int64, error) {
	if db == nil {
		return "", 0, 0, errors.New("owner database is unavailable")
	}
	var ownerID string
	var tokenCount, resourceCount int64
	err := withWriteTransaction(ctx, db, func(tx *sql.Tx) error {
		var err error
		ownerID, err = ownerIDByTokenPrefixTx(ctx, tx, prefix)
		if err != nil {
			return err
		}
		if err := queryRowTxContext(ctx, tx, `SELECT COUNT(*) FROM `+TableName("api_tokens")+` WHERE owner_id = ?`, ownerID).Scan(&tokenCount); err != nil {
			return err
		}
		if _, err := execTxContext(ctx, tx, `DELETE FROM `+TableName("api_tokens")+` WHERE owner_id = ?`, ownerID); err != nil {
			return err
		}
		result, err := execTxContext(ctx, tx, `DELETE FROM `+TableName("owners")+` WHERE id = ?`, ownerID)
		if err != nil {
			return err
		}
		deleted, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if deleted != 1 {
			return ErrOwnerPrefixNotFound
		}
		return nil
	})
	if err != nil {
		return "", 0, 0, err
	}
	return ownerID, tokenCount, resourceCount, nil
}

func ownerIDByTokenPrefix(ctx context.Context, db *sql.DB, prefix string) (string, error) {
	if db == nil {
		return "", errors.New("owner database is unavailable")
	}
	return ownerIDByTokenPrefixQuery(ctx, db, prefix)
}

func ownerIDByTokenPrefixTx(ctx context.Context, tx *sql.Tx, prefix string) (string, error) {
	return ownerIDByTokenPrefixQuery(ctx, tx, prefix)
}

type complianceQueryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func ownerIDByTokenPrefixQuery(ctx context.Context, queryer complianceQueryer, prefix string) (string, error) {
	if prefix == "" {
		return "", ErrOwnerPrefixNotFound
	}
	rows, err := queryer.QueryContext(ctx, `SELECT DISTINCT owner_id FROM `+TableName("api_tokens")+` WHERE token_prefix = ?`, prefix)
	if err != nil {
		return "", err
	}
	defer func() { _ = rows.Close() }()
	var ownerID string
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return "", err
		}
		return "", ErrOwnerPrefixNotFound
	}
	if err := rows.Scan(&ownerID); err != nil {
		return "", err
	}
	if rows.Next() {
		return "", ErrOwnerPrefixAmbiguous
	}
	if err := rows.Err(); err != nil {
		return "", err
	}
	return ownerID, nil
}
