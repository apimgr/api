package database

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"time"
)

// CleanupExpiredRateLimits removes stale rate-limit sliding-window rows.
// A window row is stale once it is more than one hour past window_start.
func CleanupExpiredRateLimits() (int64, error) {
	db := GetServerDB()
	if db == nil {
		return 0, nil
	}

	cutoff := time.Now().Add(-time.Hour).Unix()

	// A sweep may touch every window row accumulated since the last run, so
	// it is bounded as a bulk operation rather than a single write.
	ctx, cancel := context.WithTimeout(context.Background(), timeoutBulk)
	defer cancel()

	table := TableName("rate_limits")
	count, err := deleteBatches(ctx, db, fmt.Sprintf(`DELETE FROM %s WHERE rowid IN (SELECT rowid FROM %s WHERE window_start < ? LIMIT 500)`, table, table), cutoff)
	if err != nil {
		return count, err
	}
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

	// Retention sweeps span an entire retention window and are bulk work.
	ctx, cancel := context.WithTimeout(context.Background(), timeoutBulk)
	defer cancel()

	count, err := deleteBatches(ctx, db, `DELETE FROM audit_log WHERE id IN (SELECT id FROM audit_log WHERE timestamp < ? LIMIT 500)`, cutoff)
	if err != nil {
		return count, err
	}
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
	ctx, cancel := context.WithTimeout(context.Background(), timeoutBulk)
	defer cancel()

	count, err := deleteBatches(ctx, db, `DELETE FROM scheduler_history WHERE id IN (
		SELECT id FROM (
			SELECT id, row_number() OVER (PARTITION BY task_id ORDER BY started_at DESC, id DESC) AS position
			FROM scheduler_history
		) WHERE position > 100 LIMIT 500
	)`)
	if err != nil {
		return count, err
	}
	if count > 0 {
		log.Printf("Database: Cleaned %d old scheduler history entries", count)
	}

	return count, nil
}

// VacuumDatabases runs VACUUM on both databases to reclaim space
// Should be run periodically (weekly or monthly)
func VacuumDatabases() error {
	log.Println("Database: Running VACUUM on databases...")

	// VACUUM rewrites the whole file, so it is given the schema-change
	// allowance rather than the bulk-query one.
	ctx, cancel := context.WithTimeout(context.Background(), timeoutSchema)
	defer cancel()

	var vacuumErr error
	if store := GetStore(); store != nil {
		if err := store.Vacuum(ctx); err != nil {
			log.Printf("Database: Failed to vacuum server.db: %v", err)
			vacuumErr = err
		} else {
			log.Println("Database: Vacuumed server.db")
		}
	} else if db := GetServerDB(); db != nil {
		if _, err := execContext(ctx, db, "VACUUM"); err != nil {
			log.Printf("Database: Failed to vacuum server.db: %v", err)
			vacuumErr = err
		} else {
			log.Println("Database: Vacuumed server.db")
		}
	}

	return vacuumErr
}

func deleteBatches(ctx context.Context, db *sql.DB, statement string, args ...any) (int64, error) {
	var total int64
	for {
		if err := ctx.Err(); err != nil {
			return total, err
		}
		result, err := execContext(ctx, db, statement, args...)
		if err != nil {
			return total, err
		}
		removed, err := result.RowsAffected()
		if err != nil {
			return total, err
		}
		total += removed
		if removed < 500 {
			return total, nil
		}
		time.Sleep(10 * time.Millisecond)
	}
}
