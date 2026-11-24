package database

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

var securityReportMu sync.Mutex

// LockSecurityReportKey excludes report submissions while a project PGP key
// is installed, replaced, or removed. The returned function releases the lock.
func LockSecurityReportKey() func() {
	securityReportMu.Lock()
	return securityReportMu.Unlock
}

var (
	ErrSecurityReportNotFound = errors.New("security report not found")
	ErrSecurityStatusToken    = errors.New("security report status token is invalid or unavailable")
)

// SecurityReportInput contains report metadata and already-encrypted content.
// Callers must encrypt both report and contact data before storing them.
type SecurityReportInput struct {
	TrackingID string
	IDWindow   int64
	Severity   string
	Component  string
	Body       []byte
	BodyAlg    string
	Contact    []byte
	CreditPref string
	CreditName string
	CVE        bool
	TokenHash  string
}

// SecurityReport is the operator-facing report metadata. Encrypted bodies are
// intentionally omitted so list and show operations cannot disclose content.
type SecurityReport struct {
	TrackingID        string
	IDWindow          int64
	Severity          string
	Component         string
	Status            string
	BodyAlg           string
	CreditPref        string
	CreditName        string
	ResearcherComment string
	DisclosureDate    string
	CVERequested      bool
	CreatedAt         string
	UpdatedAt         string
	ClosedAt          string
}

// SecurityReportStatus contains only information permitted on the researcher's
// status page. It never includes the submitted report or contact information.
type SecurityReportStatus struct {
	TrackingID        string
	Status            string
	Severity          string
	ResearcherComment string
	DisclosureDate    string
	CreatedAt         string
	UpdatedAt         string
}

// StoreSecurityReport persists encrypted disclosure material and its metadata.
func StoreSecurityReport(ctx context.Context, input SecurityReportInput) error {
	securityReportMu.Lock()
	defer securityReportMu.Unlock()
	return storeSecurityReportLocked(ctx, input)
}

// StoreSecurityReportPrepared runs body encryption and report insertion under
// the same lock used by key rotation, so a submitted PGP body cannot race a
// switch to a new project key.
func StoreSecurityReportPrepared(ctx context.Context, input SecurityReportInput, prepare func() ([]byte, string, error)) error {
	if prepare == nil {
		return fmt.Errorf("security reports: body preparer is required")
	}
	securityReportMu.Lock()
	defer securityReportMu.Unlock()
	body, algorithm, err := prepare()
	if err != nil {
		return err
	}
	input.Body = body
	input.BodyAlg = algorithm
	return storeSecurityReportLocked(ctx, input)
}

func storeSecurityReportLocked(ctx context.Context, input SecurityReportInput) error {
	if serverDB == nil {
		return fmt.Errorf("security reports: database is not initialized")
	}
	trackingSuffix := strings.TrimPrefix(input.TrackingID, "sec_")
	if len(input.TrackingID) != 20 || trackingSuffix == input.TrackingID || len(input.Body) == 0 || len(input.Contact) == 0 {
		return fmt.Errorf("security reports: required encrypted fields are missing")
	}
	if _, err := hex.DecodeString(trackingSuffix); err != nil || input.IDWindow < 0 {
		return fmt.Errorf("security reports: invalid tracking identifier or security-id window")
	}
	if len(input.TokenHash) != sha256.Size*2 {
		return fmt.Errorf("security reports: status token hash must be a SHA-256 hex digest")
	}
	if _, err := hex.DecodeString(input.TokenHash); err != nil {
		return fmt.Errorf("security reports: status token hash must be a SHA-256 hex digest")
	}
	if !validSecuritySeverity(input.Severity) || !validSecurityCredit(input.CreditPref) {
		return fmt.Errorf("security reports: invalid severity or credit preference")
	}
	if input.BodyAlg != "pgp" && input.BodyAlg != "aes-256-gcm" {
		return fmt.Errorf("security reports: unsupported body encryption algorithm")
	}
	input.Component = strings.TrimSpace(input.Component)
	input.CreditName = strings.TrimSpace(input.CreditName)
	if utf8.RuneCountInString(input.Component) > 255 || utf8.RuneCountInString(input.CreditName) > 100 {
		return fmt.Errorf("security reports: metadata exceeds its length limit")
	}
	if (input.CreditPref == "name" || input.CreditPref == "handle") && input.CreditName == "" {
		return fmt.Errorf("security reports: chosen credit requires a name")
	}
	if input.CreditPref == "anon" || input.CreditPref == "no" {
		input.CreditName = ""
	}
	_, err := execContext(ctx, serverDB, `INSERT INTO security_reports
		(tracking_id, id_window, severity, component, status, body_enc, body_alg, contact_enc, credit_pref, credit_name, cve_requested, token_hash)
		VALUES (?, ?, ?, ?, 'received', ?, ?, ?, ?, ?, ?, ?)`,
		input.TrackingID, input.IDWindow, strings.ToLower(input.Severity), input.Component, input.Body, input.BodyAlg,
		input.Contact, input.CreditPref, nullableSecurityText(input.CreditName), input.CVE, input.TokenHash)
	if err != nil {
		return fmt.Errorf("security reports: store encrypted report: %w", err)
	}
	return nil
}

// ListSecurityReports returns metadata only, optionally filtered by status.
func ListSecurityReports(ctx context.Context, status string) ([]SecurityReport, error) {
	if serverDB == nil {
		return nil, fmt.Errorf("security reports: database is not initialized")
	}
	query := `SELECT tracking_id, id_window, severity, component, status, body_alg, credit_pref,
		COALESCE(credit_name, ''), researcher_comment, COALESCE(disclosure_date, ''), cve_requested,
		created_at, updated_at, COALESCE(closed_at, '') FROM security_reports`
	args := []any{}
	if status != "" {
		if !validSecurityReportStatus(status) {
			return nil, fmt.Errorf("security reports: invalid status")
		}
		query += " WHERE status = ?"
		args = append(args, status)
	}
	query += " ORDER BY created_at DESC, tracking_id"
	rows, err := queryContext(ctx, serverDB, query, args...)
	if err != nil {
		return nil, fmt.Errorf("security reports: list metadata: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var reports []SecurityReport
	for rows.Next() {
		var report SecurityReport
		if err := rows.Scan(&report.TrackingID, &report.IDWindow, &report.Severity, &report.Component,
			&report.Status, &report.BodyAlg, &report.CreditPref, &report.CreditName, &report.ResearcherComment,
			&report.DisclosureDate, &report.CVERequested, &report.CreatedAt, &report.UpdatedAt, &report.ClosedAt); err != nil {
			return nil, fmt.Errorf("security reports: scan metadata: %w", err)
		}
		reports = append(reports, report)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("security reports: iterate metadata: %w", err)
	}
	return reports, nil
}

// GetSecurityReport returns one report's operator-visible metadata without
// loading or exposing its encrypted report or contact material.
func GetSecurityReport(ctx context.Context, trackingID string) (SecurityReport, error) {
	if serverDB == nil {
		return SecurityReport{}, fmt.Errorf("security reports: database is not initialized")
	}
	var report SecurityReport
	err := queryRowContext(ctx, serverDB, `SELECT tracking_id, id_window, severity, component, status, body_alg,
		credit_pref, COALESCE(credit_name, ''), researcher_comment, COALESCE(disclosure_date, ''), cve_requested,
		created_at, updated_at, COALESCE(closed_at, '') FROM security_reports WHERE tracking_id = ?`, trackingID).Scan(
		&report.TrackingID, &report.IDWindow, &report.Severity, &report.Component, &report.Status, &report.BodyAlg,
		&report.CreditPref, &report.CreditName, &report.ResearcherComment, &report.DisclosureDate,
		&report.CVERequested, &report.CreatedAt, &report.UpdatedAt, &report.ClosedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return SecurityReport{}, ErrSecurityReportNotFound
	}
	if err != nil {
		return SecurityReport{}, fmt.Errorf("security reports: load metadata: %w", err)
	}
	return report, nil
}

// GetSecurityReportContactCiphertext returns only the encrypted researcher
// contact blob needed for outbound status notices. It never returns report
// content or plaintext contact details.
func GetSecurityReportContactCiphertext(ctx context.Context, trackingID string) ([]byte, error) {
	if serverDB == nil {
		return nil, fmt.Errorf("security reports: database is not initialized")
	}
	var ciphertext []byte
	err := queryRowContext(ctx, serverDB, `SELECT contact_enc FROM security_reports WHERE tracking_id = ?`, trackingID).Scan(&ciphertext)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrSecurityReportNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("security reports: load encrypted contact: %w", err)
	}
	if len(ciphertext) == 0 {
		return nil, ErrSecurityReportNotFound
	}
	return ciphertext, nil
}

// GetSecurityReportEncryptedBody returns only the already-encrypted report
// bytes and algorithm label for the operator export workflow.
func GetSecurityReportEncryptedBody(ctx context.Context, trackingID string) ([]byte, string, error) {
	if serverDB == nil {
		return nil, "", fmt.Errorf("security reports: database is not initialized")
	}
	var body []byte
	var algorithm string
	err := queryRowContext(ctx, serverDB, `SELECT body_enc, body_alg FROM security_reports WHERE tracking_id = ?`, trackingID).Scan(&body, &algorithm)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, "", ErrSecurityReportNotFound
	}
	if err != nil {
		return nil, "", fmt.Errorf("security reports: load encrypted body: %w", err)
	}
	if len(body) == 0 {
		return nil, "", ErrSecurityReportNotFound
	}
	return body, algorithm, nil
}

// UpdateSecurityReport changes only operator-managed metadata and advances
// status monotonically. Moving a credited report to disclosed also records its
// public acknowledgment in the same transaction.
func UpdateSecurityReport(ctx context.Context, trackingID string, update SecurityReportUpdate, now time.Time) (SecurityReport, error) {
	if serverDB == nil {
		return SecurityReport{}, fmt.Errorf("security reports: database is not initialized")
	}
	if update.Status != "" && !validSecurityReportStatus(update.Status) {
		return SecurityReport{}, fmt.Errorf("security reports: invalid status")
	}
	if update.Severity != "" && !validSecuritySeverity(update.Severity) {
		return SecurityReport{}, fmt.Errorf("security reports: invalid severity")
	}
	if utf8.RuneCountInString(update.Comment) > 2000 {
		return SecurityReport{}, fmt.Errorf("security reports: researcher comment exceeds 2000 characters")
	}
	if update.DisclosureDate != "" {
		if _, err := time.Parse("2006-01-02", update.DisclosureDate); err != nil {
			return SecurityReport{}, fmt.Errorf("security reports: disclosure date must use YYYY-MM-DD")
		}
	}
	var current SecurityReport
	var oldStatus string
	var creditPref, creditName string
	var newStatus, newSeverity, newComment, newDisclosureDate string
	var closedAt any
	err := withWriteTransaction(ctx, serverDB, func(tx *sql.Tx) error {
		err := queryRowTxContext(ctx, tx, `SELECT tracking_id, id_window, severity, component, status, body_alg,
		credit_pref, COALESCE(credit_name, ''), researcher_comment, COALESCE(disclosure_date, ''), cve_requested,
		created_at, updated_at, COALESCE(closed_at, '') FROM security_reports WHERE tracking_id = ?`, trackingID).Scan(
			&current.TrackingID, &current.IDWindow, &current.Severity, &current.Component, &oldStatus, &current.BodyAlg,
			&creditPref, &creditName, &current.ResearcherComment, &current.DisclosureDate, &current.CVERequested,
			&current.CreatedAt, &current.UpdatedAt, &current.ClosedAt)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrSecurityReportNotFound
		}
		if err != nil {
			return fmt.Errorf("security reports: load report metadata: %w", err)
		}
		newStatus = oldStatus
		if update.Status != "" {
			newStatus = update.Status
			if !validStatusTransition(oldStatus, newStatus) {
				return fmt.Errorf("security reports: status cannot move from %s to %s", oldStatus, newStatus)
			}
		}
		newSeverity = current.Severity
		if update.Severity != "" {
			newSeverity = strings.ToLower(update.Severity)
		}
		newComment = current.ResearcherComment
		if update.Comment != "" {
			newComment = strings.TrimSpace(update.Comment)
		}
		newDisclosureDate = current.DisclosureDate
		if update.DisclosureDate != "" {
			newDisclosureDate = update.DisclosureDate
		}
		closedAt = nil
		if newStatus == "disclosed" || newStatus == "wont_fix" {
			closedAt = now.UTC().Format(time.RFC3339)
		}
		_, err = execTxContext(ctx, tx, `UPDATE security_reports SET status = ?, severity = ?, researcher_comment = ?,
		disclosure_date = ?, updated_at = ?, closed_at = COALESCE(closed_at, ?) WHERE tracking_id = ?`,
			newStatus, newSeverity, newComment, nullableSecurityText(newDisclosureDate), now.UTC().Format(time.RFC3339), closedAt, trackingID)
		if err != nil {
			return fmt.Errorf("security reports: update metadata: %w", err)
		}
		if oldStatus != "disclosed" && newStatus == "disclosed" && creditPref != "no" {
			credit := creditName
			if creditPref == "anon" {
				var count int
				if err := queryRowTxContext(ctx, tx, `SELECT COUNT(*) FROM security_thanks WHERE credit LIKE ?`, "Anonymous Researcher #%").Scan(&count); err != nil {
					return fmt.Errorf("security reports: count anonymous credits: %w", err)
				}
				credit = fmt.Sprintf("Anonymous Researcher #%d", count+1)
			}
			_, err = execTxContext(ctx, tx, `INSERT INTO security_thanks (tracking_id, credit, severity, disclosed_on)
			VALUES (?, ?, ?, ?) ON CONFLICT(tracking_id) DO NOTHING`, trackingID, credit, newSeverity, now.UTC().Format("2006-01-02"))
			if err != nil {
				return fmt.Errorf("security reports: record acknowledgment: %w", err)
			}
		}
		return nil
	})
	if err != nil {
		return SecurityReport{}, err
	}
	current.Status = newStatus
	current.Severity = newSeverity
	current.ResearcherComment = newComment
	current.DisclosureDate = newDisclosureDate
	current.UpdatedAt = now.UTC().Format(time.RFC3339)
	if closedAt != nil {
		current.ClosedAt = now.UTC().Format(time.RFC3339)
	}
	current.CreditPref = creditPref
	current.CreditName = creditName
	return current, nil
}

// SecurityReportUpdate contains the fields maintainers may change through the
// maintenance workflow. Empty values leave the existing field unchanged.
type SecurityReportUpdate struct {
	Status         string
	Severity       string
	Comment        string
	DisclosureDate string
}

// SecurityReportStatusForResearcher checks and consumes the daily-use status
// token, returning only the state permitted on the public status page.
func SecurityReportStatusForResearcher(ctx context.Context, trackingID, token string, now time.Time) (SecurityReportStatus, error) {
	if serverDB == nil {
		return SecurityReportStatus{}, fmt.Errorf("security reports: database is not initialized")
	}
	if token == "" {
		return SecurityReportStatus{}, ErrSecurityStatusToken
	}
	var storedHash, tokenLastUsed, closedAt string
	var status SecurityReportStatus
	err := withWriteTransaction(ctx, serverDB, func(tx *sql.Tx) error {
		err := queryRowTxContext(ctx, tx, `SELECT token_hash, COALESCE(token_last_used, ''), COALESCE(closed_at, ''),
		status, severity, researcher_comment, COALESCE(disclosure_date, ''), created_at, updated_at
		FROM security_reports WHERE tracking_id = ?`, trackingID).Scan(
			&storedHash, &tokenLastUsed, &closedAt, &status.Status, &status.Severity,
			&status.ResearcherComment, &status.DisclosureDate, &status.CreatedAt, &status.UpdatedAt)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrSecurityReportNotFound
		}
		if err != nil {
			return fmt.Errorf("security reports: load status: %w", err)
		}
		providedHash := securityStatusTokenHash(token)
		if subtle.ConstantTimeCompare([]byte(storedHash), []byte(providedHash)) != 1 {
			return ErrSecurityStatusToken
		}
		utcNow := now.UTC()
		if tokenLastUsed == utcNow.Format("2006-01-02") {
			return ErrSecurityStatusToken
		}
		if closedAt != "" {
			closed, parseErr := time.Parse(time.RFC3339, closedAt)
			if parseErr != nil || !utcNow.Before(closed.Add(30*24*time.Hour)) {
				return ErrSecurityReportNotFound
			}
		}
		result, err := execTxContext(ctx, tx, `UPDATE security_reports SET token_last_used = ?
		WHERE tracking_id = ? AND (token_last_used IS NULL OR token_last_used <> ?)`,
			utcNow.Format("2006-01-02"), trackingID, utcNow.Format("2006-01-02"))
		if err != nil {
			return fmt.Errorf("security reports: consume status token: %w", err)
		}
		consumed, err := result.RowsAffected()
		if err != nil {
			return fmt.Errorf("security reports: confirm status token use: %w", err)
		}
		if consumed != 1 {
			return ErrSecurityStatusToken
		}
		return nil
	})
	if err != nil {
		return SecurityReportStatus{}, err
	}
	status.TrackingID = trackingID
	return status, nil
}

// ListSecurityThanks returns the public acknowledgments newest first.
func ListSecurityThanks(ctx context.Context) ([]SecurityThanks, error) {
	if serverDB == nil {
		return nil, fmt.Errorf("security reports: database is not initialized")
	}
	rows, err := queryContext(ctx, serverDB, `SELECT tracking_id, credit, severity, disclosed_on FROM security_thanks ORDER BY disclosed_on DESC, tracking_id`)
	if err != nil {
		return nil, fmt.Errorf("security reports: list acknowledgments: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var entries []SecurityThanks
	for rows.Next() {
		var entry SecurityThanks
		if err := rows.Scan(&entry.TrackingID, &entry.Credit, &entry.Severity, &entry.DisclosedOn); err != nil {
			return nil, fmt.Errorf("security reports: scan acknowledgment: %w", err)
		}
		entries = append(entries, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("security reports: iterate acknowledgments: %w", err)
	}
	return entries, nil
}

// SecurityThanks is the public credit record for a disclosed report.
type SecurityThanks struct {
	TrackingID  string
	Credit      string
	Severity    string
	DisclosedOn string
}

// PurgeClosedSecurityReports deletes only terminal reports whose retention
// period has elapsed. Open reports and the independent acknowledgment remain.
func PurgeClosedSecurityReports(ctx context.Context, now time.Time, retentionDays int) (int64, error) {
	if serverDB == nil {
		return 0, fmt.Errorf("security reports: database is not initialized")
	}
	if retentionDays < 1 {
		return 0, fmt.Errorf("security reports: retention days must be positive")
	}
	cutoff := now.UTC().Add(-time.Duration(retentionDays) * 24 * time.Hour).Format(time.RFC3339)
	result, err := execContext(ctx, serverDB, `DELETE FROM security_reports WHERE closed_at IS NOT NULL AND closed_at <= ?`, cutoff)
	if err != nil {
		return 0, fmt.Errorf("security reports: purge expired reports: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("security reports: count purged reports: %w", err)
	}
	return count, nil
}

func validSecuritySeverity(severity string) bool {
	switch strings.ToLower(severity) {
	case "critical", "high", "medium", "low", "informational":
		return true
	default:
		return false
	}
}

func validSecurityCredit(preference string) bool {
	switch preference {
	case "name", "handle", "anon", "no":
		return true
	default:
		return false
	}
}

func validSecurityReportStatus(status string) bool {
	switch status {
	case "received", "triaged", "confirmed", "patching", "disclosed", "wont_fix":
		return true
	default:
		return false
	}
}

func validStatusTransition(current, next string) bool {
	if current == next {
		return true
	}
	if current == "disclosed" || current == "wont_fix" {
		return false
	}
	if next == "wont_fix" {
		return true
	}
	rank := map[string]int{"received": 0, "triaged": 1, "confirmed": 2, "patching": 3, "disclosed": 4}
	return rank[next] >= rank[current]
}

func securityStatusTokenHash(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func nullableSecurityText(value string) any {
	if value == "" {
		return nil
	}
	return value
}
