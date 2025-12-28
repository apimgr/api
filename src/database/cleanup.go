package database

import (
	"log"
	"time"
)

// CleanupExpiredTokens removes expired tokens from the database
// This includes API keys, password reset tokens, and email verification tokens
func CleanupExpiredTokens() (int64, error) {
	db := GetUsersDB()
	if db == nil {
		return 0, nil
	}

	now := time.Now()
	var totalCleaned int64

	// Clean expired API keys
	result, err := db.Exec(`
		DELETE FROM api_keys
		WHERE expires_at IS NOT NULL AND expires_at < ?
	`, now)
	if err != nil {
		return 0, err
	}
	count, _ := result.RowsAffected()
	totalCleaned += count
	if count > 0 {
		log.Printf("Database: Cleaned %d expired API keys", count)
	}

	// Clean expired password reset tokens
	result, err = db.Exec(`
		DELETE FROM password_resets
		WHERE expires_at < ? OR used = 1
	`, now)
	if err != nil {
		return totalCleaned, err
	}
	count, _ = result.RowsAffected()
	totalCleaned += count
	if count > 0 {
		log.Printf("Database: Cleaned %d expired password reset tokens", count)
	}

	// Clean expired email verification tokens
	result, err = db.Exec(`
		DELETE FROM email_verifications
		WHERE expires_at < ? OR verified = 1
	`, now)
	if err != nil {
		return totalCleaned, err
	}
	count, _ = result.RowsAffected()
	totalCleaned += count
	if count > 0 {
		log.Printf("Database: Cleaned %d expired email verification tokens", count)
	}

	if totalCleaned > 0 {
		log.Printf("Database: Total tokens cleaned: %d", totalCleaned)
	}

	return totalCleaned, nil
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

	// Vacuum users.db
	if usersDB != nil {
		if _, err := usersDB.Exec("VACUUM"); err != nil {
			log.Printf("Database: Failed to vacuum users.db: %v", err)
		} else {
			log.Println("Database: Vacuumed users.db")
		}
	}

	return nil
}
