package database

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// CleanupExpiredRateLimits must delete rate_limits rows whose window_start
// is older than the 1-hour staleness cutoff, while leaving recent rows in
// place. This project has no accounts/sessions/API tokens (IDEA.md
// non-goals), so rate_limits is the real expiring state PART 18's
// "token_cleanup" task acts on here.
func TestCleanupExpiredRateLimits(t *testing.T) {
	db := GetServerDB()
	execTestWrite(t, `DELETE FROM rate_limits`)

	now := time.Now()
	stale := now.Add(-2 * time.Hour)
	recent := now.Add(-1 * time.Minute)

	execTestWrite(t, `INSERT INTO rate_limits (key, count, window_start) VALUES (?, ?, ?)`,
		"stale-key", 5, stale.Unix())
	execTestWrite(t, `INSERT INTO rate_limits (key, count, window_start) VALUES (?, ?, ?)`,
		"recent-key", 3, recent.Unix())

	cleaned, err := CleanupExpiredRateLimits()
	require.NoError(t, err)
	assert.Equal(t, int64(1), cleaned)

	var remainingKey string
	require.NoError(t, db.QueryRow(`SELECT key FROM rate_limits`).Scan(&remainingKey))
	assert.Equal(t, "recent-key", remainingKey)
}

// CleanupExpiredRateLimits with nothing stale to clean must return zero,
// not error.
func TestCleanupExpiredRateLimitsNoop(t *testing.T) {
	execTestWrite(t, `DELETE FROM rate_limits`)

	cleaned, err := CleanupExpiredRateLimits()
	require.NoError(t, err)
	assert.Equal(t, int64(0), cleaned)
}

// CleanupOldAuditLogs must remove only rows older than the retention
// window.
func TestCleanupOldAuditLogs(t *testing.T) {
	db := GetServerDB()
	execTestWrite(t, `DELETE FROM audit_log`)

	old := time.Now().AddDate(0, 0, -100)
	recent := time.Now().AddDate(0, 0, -1)

	execTestWrite(t, `INSERT INTO audit_log (timestamp, event) VALUES (?, ?)`, old, "old-event")
	execTestWrite(t, `INSERT INTO audit_log (timestamp, event) VALUES (?, ?)`, recent, "recent-event")

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
	execTestWrite(t, `DELETE FROM scheduler_history`)

	taskID := "history-task"
	base := time.Now().Add(-24 * time.Hour)
	const total = 105
	for i := 0; i < total; i++ {
		started := base.Add(time.Duration(i) * time.Minute)
		execTestWrite(t, `INSERT INTO scheduler_history (task_id, started_at, status) VALUES (?, ?, ?)`,
			taskID, started.Unix(), "success")
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
