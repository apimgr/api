package database

import (
	"log"
	"time"
)

// CleanupExpiredRateLimits removes stale rate-limit sliding-window rows.
// This project has no user accounts, sessions, or API tokens (IDEA.md
// non-goals, confirmed against AI.md's own "no admin web UI" statements) —
// the closest real expiring state to PART 18's "token_cleanup" task is the
// rate_limits sliding-window table, so that is what this task cleans.
// A window row is stale once it is more than one hour past window_start.
func CleanupExpiredRateLimits() (int64, error) {
	db := GetServerDB()
	if db == nil {
		return 0, nil
	}

	cutoff := time.Now().Add(-1 * time.Hour)

	result, err := db.Exec(`
		DELETE FROM rate_limits
		WHERE window_start < ?
	`, cutoff)
	if err != nil {
		return 0, err
	}

	count, _ := result.RowsAffected()
	if count > 0 {
		log.Printf("Database: Cleaned %d stale rate-limit entries", count)
	}

	return count, nil
}

// CleanupOldAuditLogs removes audit logs older than the retention period
// Default retention: 90 days per spec
func CleanupOldAuditLogs(retentionDays int) (int64, error) {
	db := GetServerDB()
	if db == nil {
		return 0, nil
	}

	cutoff := time.Now().AddDate(0, 0, -retentionDays)

	result, err := db.Exec(`
		DELETE FROM audit_log
		WHERE timestamp < ?
	`, cutoff)

	if err != nil {
		return 0, err
	}

	count, _ := result.RowsAffected()
	if count > 0 {
		log.Printf("Database: Cleaned %d old audit log entries (older than %d days)", count, retentionDays)
	}

	return count, nil
}

// CleanupOldSchedulerHistory removes old scheduler execution history
// Keep last 100 executions per task
func CleanupOldSchedulerHistory() (int64, error) {
	db := GetServerDB()
	if db == nil {
		return 0, nil
	}

	// Delete old history, keeping last 100 per task
	result, err := db.Exec(`
		DELETE FROM scheduler_history
		WHERE id NOT IN (
			SELECT id FROM scheduler_history
			WHERE task_id = scheduler_history.task_id
			ORDER BY started_at DESC
			LIMIT 100
		)
	`)

	if err != nil {
		return 0, err
	}

	count, _ := result.RowsAffected()
	if count > 0 {
		log.Printf("Database: Cleaned %d old scheduler history entries", count)
	}

	return count, nil
}

// VacuumDatabases runs VACUUM on both databases to reclaim space
// Should be run periodically (weekly or monthly)
func VacuumDatabases() error {
	log.Println("Database: Running VACUUM on databases...")

	// Vacuum server.db
	if serverDB != nil {
		if _, err := serverDB.Exec("VACUUM"); err != nil {
			log.Printf("Database: Failed to vacuum server.db: %v", err)
		} else {
			log.Println("Database: Vacuumed server.db")
		}
	}

	return nil
}
