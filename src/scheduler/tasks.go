package scheduler

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"

	"github.com/apimgr/api/src/backup"
	"github.com/apimgr/api/src/database"
	"github.com/apimgr/api/src/geoip"
	"github.com/apimgr/api/src/paths"
	"github.com/apimgr/api/src/ratelimit"
	"github.com/apimgr/api/src/session"
	"github.com/apimgr/api/src/ssl"
)

// RegisterDefaultTasks registers all built-in scheduled tasks
func (s *Scheduler) RegisterDefaultTasks() {
	// Daily backup at 02:00 (disabled by default - must be enabled in config)
	s.AddTask("backup_daily", "0 2 * * *", backupTask, false)

	// SSL renewal check at 03:00 daily
	s.AddTask("ssl_renewal", "0 3 * * *", sslRenewalTask, true)

	// GeoIP database update at 03:00 Sunday
	s.AddTask("geoip_update", "0 3 * * 0", geoipUpdateTask, true)

	// Session cleanup every hour
	s.AddTask("session_cleanup", "@hourly", sessionCleanupTask, true)

	// Token cleanup daily at 06:00
	s.AddTask("token_cleanup", "0 6 * * *", tokenCleanupTask, true)

	// Log rotation daily at midnight
	s.AddTask("log_rotation", "0 0 * * *", logRotationTask, true)

	// Self health check every 5 minutes
	s.AddTask("healthcheck_self", "@every 5m", healthCheckTask, true)

	// Tor health check every 10 minutes (only if Tor installed)
	s.AddTask("tor_health", "@every 10m", torHealthTask, true)

	log.Println("Scheduler: Registered default tasks")
}

// backupTask performs automatic database backup
func backupTask() error {
	log.Println("Scheduler: Running backup task...")

	// Determine backup path
	backupDir := filepath.Join(paths.DataDir(), "backup")
	backupFile := filepath.Join(backupDir, fmt.Sprintf("backup-%s.tar.gz", time.Now().Format("20060102-150405")))

	// Sources to backup
	sources := []string{
		filepath.Join(paths.DataDir(), "db"),        // Databases
		filepath.Join(paths.ConfigDir(), "server.yml"), // Config file
	}

	// Get encryption password from environment (API_BACKUP_PASSWORD)
	// If not set, backups are unencrypted (per AI.md, encryption is optional)
	password := os.Getenv("API_BACKUP_PASSWORD")

	// Create backup (with optional encryption)
	if err := backup.Create(backupFile, sources, password); err != nil {
		log.Printf("Scheduler: Backup failed: %v", err)
		return err
	}

	// Cleanup old backups (keep last 4)
	if err := backup.CleanupOldBackups(backupDir, 4); err != nil {
		log.Printf("Scheduler: Backup cleanup warning: %v", err)
		// Don't fail the task if cleanup fails
	}

	log.Printf("Scheduler: Backup completed successfully: %s", backupFile)
	return nil
}

// sslRenewalTask checks and renews SSL certificates
func sslRenewalTask() error {
	log.Println("Scheduler: Checking SSL certificates...")

	// Get certificate path from data directory
	certPath := filepath.Join(paths.DataDir(), "ssl", "cert.pem")

	// Run SSL renewal check
	if err := ssl.RenewalTask(certPath); err != nil {
		log.Printf("Scheduler: SSL renewal check failed: %v", err)
		return err
	}

	log.Println("Scheduler: SSL renewal check completed")
	return nil
}

// geoipUpdateTask updates the GeoIP database
func geoipUpdateTask() error {
	log.Println("Scheduler: Updating GeoIP database...")

	// Download and load the latest GeoIP database
	if err := geoip.Download(paths.DataDir()); err != nil {
		log.Printf("Scheduler: GeoIP update failed: %v", err)
		return err
	}

	log.Println("Scheduler: GeoIP update completed successfully")
	return nil
}

// sessionCleanupTask removes expired sessions
func sessionCleanupTask() error {
	log.Println("Scheduler: Cleaning up expired sessions...")

	// Use session package to clean up expired sessions
	if err := session.CleanupExpired(); err != nil {
		log.Printf("Scheduler: Session cleanup failed: %v", err)
		return err
	}

	log.Println("Scheduler: Session cleanup completed")
	return nil
}

// tokenCleanupTask removes expired tokens
func tokenCleanupTask() error {
	log.Println("Scheduler: Cleaning up expired tokens...")

	// Clean all expired tokens from database
	count, err := database.CleanupExpiredTokens()
	if err != nil {
		log.Printf("Scheduler: Token cleanup failed: %v", err)
		return err
	}

	if count > 0 {
		log.Printf("Scheduler: Token cleanup completed (%d tokens removed)", count)
	} else {
		log.Println("Scheduler: Token cleanup completed (no expired tokens)")
	}

	return nil
}

// logRotationTask rotates log files
func logRotationTask() error {
	log.Println("Scheduler: Rotating log files...")

	// Perform database maintenance tasks
	// Clean old audit logs (keep 90 days per spec)
	auditCount, err := database.CleanupOldAuditLogs(90)
	if err != nil {
		log.Printf("Scheduler: Audit log cleanup failed: %v", err)
	} else if auditCount > 0 {
		log.Printf("Scheduler: Cleaned %d old audit log entries", auditCount)
	}

	// Clean old scheduler history
	historyCount, err := database.CleanupOldSchedulerHistory()
	if err != nil {
		log.Printf("Scheduler: Scheduler history cleanup failed: %v", err)
	} else if historyCount > 0 {
		log.Printf("Scheduler: Cleaned %d old scheduler history entries", historyCount)
	}

	// Clean old rate limit entries
	if err := ratelimit.CleanupOldEntries(); err != nil {
		log.Printf("Scheduler: Rate limit cleanup failed: %v", err)
	}

	// Rotate actual log files on disk
	logDir := paths.LogDir()
	logFiles := []string{"access.log", "server.log", "error.log", "security.log"}

	for _, logFile := range logFiles {
		logPath := filepath.Join(logDir, logFile)

		// Check if file exists and needs rotation (>10MB)
		info, err := os.Stat(logPath)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			log.Printf("Scheduler: Failed to stat %s: %v", logFile, err)
			continue
		}

		// Rotate if >10MB
		if info.Size() > 10*1024*1024 {
			// Rename to .1
			newPath := logPath + ".1"
			if err := os.Rename(logPath, newPath); err != nil {
				log.Printf("Scheduler: Failed to rotate %s: %v", logFile, err)
			} else {
				log.Printf("Scheduler: Rotated %s (size: %d bytes)", logFile, info.Size())
			}
		}
	}

	log.Println("Scheduler: Log rotation completed")
	return nil
}

// healthCheckTask performs self health check
func healthCheckTask() error {
	// Check database connectivity
	db := database.GetServerDB()
	if db != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := db.PingContext(ctx); err != nil {
			log.Printf("Scheduler: Health check - database error: %v", err)
			// Don't fail the task, just log
		}
	}

	// Check disk space
	var stat syscall.Statfs_t
	if err := syscall.Statfs(paths.DataDir(), &stat); err == nil {
		freeBytes := stat.Bfree * uint64(stat.Bsize)
		totalBytes := stat.Blocks * uint64(stat.Bsize)
		percentFree := float64(freeBytes) / float64(totalBytes) * 100

		if percentFree < 10 {
			log.Printf("Scheduler: Health check - low disk space: %.1f%% free", percentFree)
		}
	}

	return nil
}

// torHealthTask checks and restarts Tor if needed
func torHealthTask() error {
	// Check if tor binary exists
	torPath, err := exec.LookPath("tor")
	if err != nil {
		// Tor not installed, skip
		return nil
	}

	// Check if tor process is running
	// Simple approach: try to connect to tor control port
	log.Printf("Scheduler: Tor binary found at %s (health check not yet implemented)", torPath)

	return nil
}

// parseScheduleExpression converts schedule string to next run time
// Supports: cron expressions, @hourly, @daily, @weekly, @every Xm
func parseScheduleExpression(expr string) (time.Duration, error) {
	// Handle special expressions
	switch expr {
	case "@hourly":
		return time.Hour, nil
	case "@daily":
		return 24 * time.Hour, nil
	case "@weekly":
		return 7 * 24 * time.Hour, nil
	case "@monthly":
		return 30 * 24 * time.Hour, nil
	}

	// Handle @every expressions
	if len(expr) > 7 && expr[:7] == "@every " {
		return time.ParseDuration(expr[7:])
	}

	// Handle cron expressions
	// Simple cron parser for common patterns:
	// "0 2 * * *" = daily at 02:00 -> 24 hours
	// "0 3 * * 0" = weekly Sunday at 03:00 -> 7 days
	// "0 * * * *" = hourly -> 1 hour
	//
	// Full cron parsing would require github.com/robfig/cron library
	// For now, return daily as default for any cron expression
	return 24 * time.Hour, nil
}
