package database

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// CleanupExpiredTokens must delete expired and already-used password reset
// rows and expired/already-verified email verification rows, while leaving
// still-valid, unused/unverified rows in place.
func TestCleanupExpiredTokens(t *testing.T) {
	db := GetUsersDB()
	now := time.Now()
	future := now.Add(24 * time.Hour)
	past := now.Add(-24 * time.Hour)

	_, err := db.Exec(`INSERT INTO password_resets (token, email, expires_at, used) VALUES (?, ?, ?, ?)`,
		"pr-expired", "expired@example.com", past, 0)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO password_resets (token, email, expires_at, used) VALUES (?, ?, ?, ?)`,
		"pr-used", "used@example.com", future, 1)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO password_resets (token, email, expires_at, used) VALUES (?, ?, ?, ?)`,
		"pr-valid", "valid@example.com", future, 0)
	require.NoError(t, err)

	_, err = db.Exec(`INSERT INTO email_verifications (token, user_id, email, expires_at, verified) VALUES (?, ?, ?, ?, ?)`,
		"ev-expired", 1, "expired@example.com", past, 0)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO email_verifications (token, user_id, email, expires_at, verified) VALUES (?, ?, ?, ?, ?)`,
		"ev-valid", 2, "valid@example.com", future, 0)
	require.NoError(t, err)

	cleaned, err := CleanupExpiredTokens()
	require.NoError(t, err)
	assert.Equal(t, int64(3), cleaned)

	var prCount int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM password_resets`).Scan(&prCount))
	assert.Equal(t, 1, prCount)

	var remainingToken string
	require.NoError(t, db.QueryRow(`SELECT token FROM password_resets`).Scan(&remainingToken))
	assert.Equal(t, "pr-valid", remainingToken)

	var evCount int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM email_verifications`).Scan(&evCount))
	assert.Equal(t, 1, evCount)
}

// CleanupExpiredTokens with nothing to clean must return zero, not error.
func TestCleanupExpiredTokensNoop(t *testing.T) {
	db := GetUsersDB()
	_, err := db.Exec(`DELETE FROM password_resets`)
	require.NoError(t, err)
	_, err = db.Exec(`DELETE FROM email_verifications`)
	require.NoError(t, err)

	cleaned, err := CleanupExpiredTokens()
	require.NoError(t, err)
	assert.Equal(t, int64(0), cleaned)
}

// CleanupOldAuditLogs must remove only rows older than the retention
// window.
func TestCleanupOldAuditLogs(t *testing.T) {
	db := GetServerDB()
	_, err := db.Exec(`DELETE FROM audit_log`)
	require.NoError(t, err)

	old := time.Now().AddDate(0, 0, -100)
	recent := time.Now().AddDate(0, 0, -1)

	_, err = db.Exec(`INSERT INTO audit_log (timestamp, event) VALUES (?, ?)`, old, "old-event")
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO audit_log (timestamp, event) VALUES (?, ?)`, recent, "recent-event")
	require.NoError(t, err)

	count, err := CleanupOldAuditLogs(90)
	require.NoError(t, err)
	assert.Equal(t, int64(1), count)

	var remainingEvent string
	require.NoError(t, db.QueryRow(`SELECT event FROM audit_log`).Scan(&remainingEvent))
	assert.Equal(t, "recent-event", remainingEvent)
}

// CleanupOldSchedulerHistory must keep only the newest 100 rows per task,
// deleting the rest.
func TestCleanupOldSchedulerHistory(t *testing.T) {
	db := GetServerDB()
	_, err := db.Exec(`DELETE FROM scheduler_history`)
	require.NoError(t, err)

	taskID := "history-task"
	base := time.Now().Add(-24 * time.Hour)
	const total = 105
	for i := 0; i < total; i++ {
		started := base.Add(time.Duration(i) * time.Minute)
		_, err := db.Exec(`INSERT INTO scheduler_history (task_id, started_at, status) VALUES (?, ?, ?)`,
			taskID, started, "success")
		require.NoError(t, err)
	}

	deleted, err := CleanupOldSchedulerHistory()
	require.NoError(t, err)
	assert.Equal(t, int64(total-100), deleted)

	var remaining int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM scheduler_history WHERE task_id = ?`, taskID).Scan(&remaining))
	assert.Equal(t, 100, remaining)
}

// VacuumDatabases must run without error against live, initialized
// databases.
func TestVacuumDatabases(t *testing.T) {
	assert.NoError(t, VacuumDatabases())
}
