package scheduler

import (
	"context"
	"log"
	"os"
	"os/exec"
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

// RegisterDefaultTasks registers the built-in scheduled tasks defined by
// AI.md PART 18. Skippable reflects the spec's "Skippable" column: an
// operator may disable a skippable task, but ssl_renewal, token_cleanup,
// log_rotation and healthcheck_self always run.
func (s *Scheduler) RegisterDefaultTasks() {
	s.register(&Task{
		Name:      "ssl_renewal",
		Title:     "Check and renew SSL certificates",
		Schedule:  "0 3 * * *",
		Func:      sslRenewalTask,
		Enabled:   true,
		Skippable: false,
		failure:   failureSSLRenewal,
	})

	s.register(&Task{
		Name:      "geoip_update",
		Title:     "Download latest GeoIP databases",
		Schedule:  "0 3 * * 0",
		Func:      geoipUpdateTask,
		Enabled:   true,
		Skippable: true,
	})

	s.register(&Task{
		Name:      "blocklist_update",
		Title:     "Update IP and domain blocklists",
		Schedule:  "0 4 * * *",
		Func:      blocklistUpdateTask,
		Enabled:   true,
		Skippable: true,
	})

	s.register(&Task{
		Name:      "cve_update",
		Title:     "Update CVE database",
		Schedule:  "0 5 * * *",
		Func:      cveUpdateTask,
		Enabled:   true,
		Skippable: true,
	})

	s.register(&Task{
		Name:      "update_check",
		Title:     "Check for application updates",
		Schedule:  "0 6 * * *",
		Func:      s.updateCheckTask,
		Enabled:   true,
		Skippable: true,
	})

	s.register(&Task{
		Name:      "token_cleanup",
		Title:     "Remove expired tokens and sessions",
		Schedule:  "@every 15m",
		Func:      tokenCleanupTask,
		Enabled:   true,
		Skippable: false,
	})

	s.register(&Task{
		Name:      "log_rotation",
		Title:     "Rotate and compress log files",
		Schedule:  "0 0 * * *",
		Func:      logRotationTask,
		Enabled:   true,
		Skippable: false,
	})

	s.register(&Task{
		Name:      "backup_daily",
		Title:     "Create daily backup archive",
		Schedule:  "0 2 * * *",
		Func:      s.dailyBackupTask,
		Enabled:   true,
		Skippable: true,
		failure:   failureBackup,
	})

	s.register(&Task{
		Name:      "backup_hourly",
		Title:     "Create hourly incremental backup",
		Schedule:  "@hourly",
		Func:      s.hourlyBackupTask,
		Enabled:   false,
		Skippable: true,
		failure:   failureBackup,
	})

	s.register(&Task{
		Name:      "healthcheck_self",
		Title:     "Self health check",
		Schedule:  "@every 5m",
		Func:      healthCheckTask,
		Enabled:   true,
		Skippable: false,
	})

	// tor_health is always registered, but is only mandatory ("Skippable:
	// No") when a Tor binary is actually present - without Tor the task is a
	// no-op an operator may switch off. The task itself is a soft no-op
	// whenever no Tor process is running.
	s.register(&Task{
		Name:      "tor_health",
		Title:     "Check Tor hidden service health",
		Schedule:  "@every 10m",
		Func:      s.torHealthTask,
		Enabled:   true,
		Skippable: !torInstalled(),
	})

	// i2p_health is deliberately NOT registered. AI.md PART 18 lists it in the
	// built-in task table ("only when I2P opt-in enabled") but omits it from
	// the server.scheduler.tasks YAML block, which is the authoritative list of
	// configurable built-in tasks - and this project ships no I2P manager at
	// all (src/server/handler/health.go reports the eepsite permanently
	// disabled with provider "none"). Registering a task whose subsystem does
	// not exist would fabricate a capability, so the eleven tasks above are the
	// complete built-in set; i2p_health is added when an I2P provider lands.
	log.Println("Scheduler: Registered default tasks")
}

// torInstalled reports whether a Tor executable is reachable on PATH. It is
// used only to decide whether tor_health is mandatory; the Tor manager does
// its own richer per-OS lookup when actually starting Tor.
func torInstalled() bool {
	if _, err := exec.LookPath("tor"); err == nil {
		return true
	}
	return false
}

// dailyBackupTask runs the PART 21 backup_daily flow: retention sweep, disk
// check, full backup, verification, daily incremental, verification, then
// retention - all owned by backup.Manager so the operator notifications and
// audit events fire for scheduled runs exactly as they do for manual ones.
func (s *Scheduler) dailyBackupTask() error {
	manager, err := s.backupManager()
	if err != nil {
		return err
	}
	log.Println("Scheduler: Running daily backup task...")
	return manager.RunDaily(time.Now())
}

// hourlyBackupTask refreshes the rolling hourly incremental archive
// (api-hourly.tar.gz[.enc]) - always exactly one file, replaced each run.
func (s *Scheduler) hourlyBackupTask() error {
	manager, err := s.backupManager()
	if err != nil {
		return err
	}
	log.Println("Scheduler: Running hourly backup task...")
	return manager.RunHourly(time.Now())
}

// backupManager builds the backup.Manager a scheduled run drives. The backup
// directory is resolved once here and cached by the Manager, so retention can
// never target a different path than the archive just written (AI.md PART 21).
func (s *Scheduler) backupManager() (*backup.Manager, error) {
	cfg, err := config.Load()
	if err != nil {
		log.Printf("Scheduler: Backup skipped, failed to load config: %v", err)
		return nil, err
	}

	// Password is read from server.backup.encryption_password (AI.md PART
	// 21). API_BACKUP_PASSWORD overrides it for this unattended run only -
	// the scheduler cannot prompt interactively the way the CLI/WebUI can.
	password := cfg.Server.Backup.EncryptionPassword
	if envPassword := os.Getenv("API_BACKUP_PASSWORD"); envPassword != "" {
		password = envPassword
	}

	// Sources: databases first, then the operator-editable config tree.
	// AI.md PART 21 always includes server.yml and server.db, and includes the
	// custom template/ and theme/ trees only when they exist, so an operator
	// who never overrode a template does not fail the scheduled backup.
	sources := []string{
		filepath.Join(paths.DataDir(), "db"),
		filepath.Join(paths.ConfigDir(), "server.yml"),
	}
	for _, optional := range []string{"template", "theme"} {
		path := filepath.Join(paths.ConfigDir(), optional)
		if _, err := os.Stat(path); err == nil {
			sources = append(sources, path)
		}
	}

	backupDir := paths.BackupDir()
	if err := os.MkdirAll(backupDir, 0o750); err != nil {
		log.Printf("Scheduler: Backup failed, cannot create %s: %v", backupDir, err)
		return nil, err
	}

	return backup.NewManager(backup.Options{
		Dir:               backupDir,
		Sources:           sources,
		Password:          password,
		Retention:         cfg.Server.Backup.Retention,
		ComplianceEnabled: cfg.Server.Compliance.Enabled,
		AppVersion:        s.opts.Version,
		Audit:             s.opts.Audit,
		// The scheduler owns the execution id PART 17's suppression rule
		// keys on, so it sends backup_failed itself from notifyFailure.
		SuppressFailureNotify: true,
	}), nil
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

	// The download honours server.geoip so the weekly refresh respects the
	// operator's directory, enabled flag, and per-database selection.
	cfg, err := config.Load()
	if err != nil {
		log.Printf("Scheduler: GeoIP update failed to load config: %v", err)
		return err
	}

	if err := geoip.DownloadFromConfig(cfg.Server.GeoIP, paths.DataDir()); err != nil {
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
// and restarts it if unresponsive. The restart is gated on
// server.scheduler.tasks.tor_health.restart_on_fail (AI.md PART 18), which
// defaults to true. It is a soft no-op whenever Tor was never started (no
// binary found, or Tor disabled) - Tor is always optional and this task must
// never fail the scheduler.
func (s *Scheduler) torHealthTask() error {
	mgr := tor.Get()
	if mgr == nil || !mgr.Running() {
		return nil
	}

	if err := mgr.Ping(); err != nil {
		if !s.opts.restartOnFail("tor_health") {
			log.Printf("Scheduler: Tor process unresponsive, restart_on_fail is disabled: %v", err)
			return nil
		}
		log.Printf("Scheduler: Tor process unresponsive, restarting: %v", err)
		if err := mgr.Restart(context.Background()); err != nil {
			log.Printf("Scheduler: Tor restart failed: %v", err)
		}
	}

	return nil
}
