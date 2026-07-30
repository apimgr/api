package scheduler

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/apimgr/api/src/backup"
	"github.com/apimgr/api/src/config"
	"github.com/apimgr/api/src/database"
	"github.com/apimgr/api/src/geoip"
	"github.com/apimgr/api/src/paths"
	"github.com/apimgr/api/src/ssl"
	"github.com/apimgr/api/src/tor"
)

// RegisterDefaultTasks registers all built-in scheduled tasks
func (s *Scheduler) RegisterDefaultTasks() {
	// Daily backup at 02:00 (disabled by default - must be enabled in config)
	s.AddTask("backup_daily", "0 2 * * *", backupTask, false)

	// SSL renewal check at 03:00 daily
	s.AddTask("ssl_renewal", "0 3 * * *", sslRenewalTask, true)

	// GeoIP database update at 03:00 Sunday
	s.AddTask("geoip_update", "0 3 * * 0", geoipUpdateTask, true)

	// Token cleanup every 15 minutes
	s.AddTask("token_cleanup", "@every 15m", tokenCleanupTask, true)

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
		filepath.Join(paths.DataDir(), "db"),           // Databases
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

// sslRenewalTask checks and renews SSL certificates. Per AI.md PART 15
// (Renewal Rules), only app-managed Let's Encrypt certificates under
// {config_dir}/ssl/letsencrypt/{fqdn}/ are auto-renewed; local/user-provided
// certificates under ssl/local/{fqdn}/ are never touched by this task.
func sslRenewalTask() error {
	log.Println("Scheduler: Checking SSL certificates...")

	cfg, err := config.Load()
	if err != nil {
		log.Printf("Scheduler: SSL renewal check skipped, failed to load config: %v", err)
		return err
	}

	domain := cfg.Server.FQDN
	if domain == "" {
		domain = "localhost"
	}

	sslCertPath := cfg.Server.SSL.CertPath
	if sslCertPath == "" {
		sslCertPath = filepath.Join(paths.ConfigDir(), "ssl")
	}

	certPath := filepath.Join(sslCertPath, "letsencrypt", domain, "fullchain.pem")
	if _, err := os.Stat(certPath); os.IsNotExist(err) {
		log.Println("Scheduler: No app-managed Let's Encrypt certificate found, nothing to renew")
		return nil
	}

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

// tokenCleanupTask removes expired ephemeral state.
// This project has no user accounts, sessions, or API tokens (IDEA.md
// non-goals) — the closest real expiring state to PART 18's spec purpose
// ("Remove expired API tokens and sessions") is the rate_limits
// sliding-window table, so that is what this task cleans.
func tokenCleanupTask() error {
	log.Println("Scheduler: Cleaning up expired rate-limit entries...")

	count, err := database.CleanupExpiredRateLimits()
	if err != nil {
		log.Printf("Scheduler: Token cleanup failed: %v", err)
		return err
	}

	if count > 0 {
		log.Printf("Scheduler: Token cleanup completed (%d entries removed)", count)
	} else {
		log.Println("Scheduler: Token cleanup completed (no expired entries)")
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
	if percentFree, ok := checkDiskSpace(); ok && percentFree < 10 {
		log.Printf("Scheduler: Health check - low disk space: %.1f%% free", percentFree)
	}

	return nil
}

// torHealthTask checks the running Tor hidden service's control connection
// and restarts it if unresponsive. It is a soft no-op whenever Tor was
// never started (no binary found, or Tor disabled) - Tor is always
// optional and this task must never fail the scheduler.
func torHealthTask() error {
	mgr := tor.Get()
	if mgr == nil || !mgr.Running() {
		return nil
	}

	if err := mgr.Ping(); err != nil {
		log.Printf("Scheduler: Tor process unresponsive, restarting: %v", err)
		if err := mgr.Restart(context.Background()); err != nil {
			log.Printf("Scheduler: Tor restart failed: %v", err)
		}
	}

	return nil
}
