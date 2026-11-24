package scheduler

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/apimgr/api/src/backup"
	"github.com/apimgr/api/src/config"
	"github.com/apimgr/api/src/database"
	"github.com/apimgr/api/src/email"
	"github.com/apimgr/api/src/geoip"
	"github.com/apimgr/api/src/i2p"
	"github.com/apimgr/api/src/paths"
	"github.com/apimgr/api/src/pgp"
	"github.com/apimgr/api/src/ssl"
	"github.com/apimgr/api/src/tor"
)

var diskCheckState = struct {
	sync.Mutex
	alerts map[string]time.Time
}{alerts: make(map[string]time.Time)}

var databaseCheckState = struct {
	sync.Mutex
	failures    int
	since       time.Time
	lastAlert   time.Time
	lastMessage string
}{}

var databaseCredentialPattern = regexp.MustCompile(`(?i)(authToken|token|password|secret)(\s*[=:]\s*)([^&\s,;]+)`)

// RegisterDefaultTasks registers the built-in scheduled tasks defined by
// AI.md PART 18. Skippable reflects the spec's "Skippable" column: an
// operator may disable a skippable task, but ssl_renewal, token_cleanup,
// security_cleanup, keyserver_retry, log_rotation and healthcheck_self always run.
func (s *Scheduler) RegisterDefaultTasks() {
	notifications := config.Get().Server.Notifications
	diskInterval := strings.TrimSpace(notifications.DiskLow.CheckInterval)
	if _, err := time.ParseDuration(diskInterval); err != nil {
		diskInterval = "5m"
	}
	databaseInterval := strings.TrimSpace(notifications.DatabaseError.CheckInterval)
	if _, err := time.ParseDuration(databaseInterval); err != nil {
		databaseInterval = "1m"
	}
	s.register(&Task{
		Name:      "ssl_renewal",
		Title:     "Check and renew SSL certificates",
		Schedule:  "0 3 * * *",
		Func:      func() error { return sslRenewalTask(s.opts.SSLRenewer, s.opts.SSLCertificatePath, s.opts.SSLDomain) },
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
		Name:      "security_cleanup",
		Title:     "Remove expired security reports and retired keys",
		Schedule:  "30 3 * * *",
		Func:      securityCleanupTask,
		Enabled:   true,
		Skippable: false,
	})

	s.register(&Task{
		Name:      "keyserver_retry",
		Title:     "Retry security keyserver publishing",
		Schedule:  "@every 15m",
		Func:      keyserverRetryTask,
		Enabled:   true,
		Skippable: false,
	})

	s.register(&Task{
		Name:      "disk_check",
		Title:     "Check disk space and notify when low",
		Schedule:  "@every " + diskInterval,
		Func:      diskCheckTask,
		Enabled:   notifications.DiskLow.Enabled,
		Skippable: true,
	})

	s.register(&Task{
		Name:      "database_check",
		Title:     "Check database connections and notify on failure",
		Schedule:  "@every " + databaseInterval,
		Func:      databaseCheckTask,
		Enabled:   notifications.DatabaseError.Enabled,
		Skippable: true,
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

	if torInstalled() {
		s.register(&Task{
			Name:      "tor_health",
			Title:     "Check Tor hidden service health",
			Schedule:  "@every 10m",
			Func:      s.torHealthTask,
			Enabled:   true,
			Skippable: false,
		})
	}

	if config.Get().Server.I2P.Enabled {
		s.register(&Task{
			Name:      "i2p_health",
			Title:     "Check I2P eepsite health",
			Schedule:  "@every 10m",
			Func:      s.i2pHealthTask,
			Enabled:   true,
			Skippable: false,
		})
	}
	s.logRegisteredTasks()
}

func (s *Scheduler) logRegisteredTasks() {
	tasks := s.GetTasks()
	names := make([]string, 0, len(tasks))
	for _, task := range tasks {
		names = append(names, task.Name)
	}
	sort.Strings(names)
	log.Printf("Scheduler: Registered tasks: %s", strings.Join(names, ", "))
}

func (s *Scheduler) i2pHealthTask() error {
	mgr := i2p.Get()
	if mgr == nil {
		return nil
	}
	return mgr.HealthCheck()
}

// securityCleanupTask removes closed disclosure reports after the configured
// retention period and deletes a retired encrypted PGP key after 30 days.
func securityCleanupTask() error {
	cfg := config.Get()
	if err := database.CleanupPreviousEncryptionKey(context.Background(), time.Now().UTC()); err != nil {
		log.Printf("Scheduler: Previous encryption-key cleanup failed: %v", err)
		return err
	}
	retentionDays := cfg.Server.Security.Reports.RetentionDays
	if retentionDays <= 0 {
		retentionDays = 365
	}
	count, err := database.PurgeClosedSecurityReports(context.Background(), time.Now().UTC(), retentionDays)
	if err != nil {
		log.Printf("Scheduler: Security report cleanup failed: %v", err)
		return err
	}

	if err := pgp.DeleteRetiredPrivateKey(time.Now().UTC()); err != nil {
		log.Printf("Scheduler: Retired PGP key cleanup failed: %v", err)
		return err
	}
	log.Printf("Scheduler: Security cleanup completed (%d reports purged)", count)
	return nil
}

type diskVolumeUsage struct {
	path        string
	percentUsed float64
	freeBytes   uint64
}

func formatDiskBytes(bytes uint64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	value := float64(bytes)
	exponent := 0
	for value >= unit && exponent < 5 {
		value /= unit
		exponent++
	}
	units := [...]string{"KiB", "MiB", "GiB", "TiB", "PiB"}
	return fmt.Sprintf("%.1f %s", value, units[exponent-1])
}

// diskCheckTask checks each distinct filesystem containing the data, logs,
// and backup directories, then emits only threshold-crossing notifications.
func diskCheckTask() error {
	cfg := config.Get()
	settings := cfg.Server.Notifications.DiskLow
	if !settings.Enabled {
		return nil
	}
	threshold := settings.ThresholdPercent
	if threshold <= 0 || threshold > 100 {
		threshold = 85
	}
	repeat, err := time.ParseDuration(settings.Repeat)
	if err != nil || repeat <= 0 {
		repeat = 24 * time.Hour
	}
	pathsToCheck := []string{paths.DataDir(), paths.LogDir(), paths.BackupDir()}
	volumes := make(map[string]diskVolumeUsage, len(pathsToCheck))
	for _, path := range pathsToCheck {
		measurePath := nearestExistingDirectory(path)
		volume, freePercent, freeBytes, _, ok := diskSpaceAt(measurePath)
		if !ok {
			log.Printf("Scheduler: Disk check could not inspect %s", path)
			continue
		}
		if _, exists := volumes[volume]; !exists {
			volumes[volume] = diskVolumeUsage{path: path, percentUsed: 100 - freePercent, freeBytes: freeBytes}
		}
	}
	if len(volumes) == 0 {
		return fmt.Errorf("disk check could not inspect any configured volume")
	}
	now := time.Now().UTC()
	diskCheckState.Lock()
	defer diskCheckState.Unlock()
	for volume, usage := range volumes {
		lastAlert := diskCheckState.alerts[volume]
		if usage.percentUsed >= float64(threshold) {
			if lastAlert.IsZero() || now.Sub(lastAlert) >= repeat {
				details := fmt.Sprintf("path %s, used %.1f%%, free %s, threshold %d%%", usage.path, usage.percentUsed, formatDiskBytes(usage.freeBytes), threshold)
				log.Printf("WARN Disk space low: %s", details)
				if notifier := email.DefaultNotifier(); notifier != nil {
					if err := notifier.NotifyDiskLow(nil, usage.path, fmt.Sprintf("%.1f%%", usage.percentUsed), formatDiskBytes(usage.freeBytes), fmt.Sprintf("%d%%", threshold)); err != nil {
						log.Printf("Scheduler: Disk low notification failed: %v", err)
					}
				}
				diskCheckState.alerts[volume] = now
			}
			continue
		}
		if !lastAlert.IsZero() && usage.percentUsed < float64(threshold-5) {
			log.Printf("INFO Disk space recovered: %s (used %.1f%%)", usage.path, usage.percentUsed)
			delete(diskCheckState.alerts, volume)
		}
	}
	return nil
}

func nearestExistingDirectory(path string) string {
	for path != "" {
		if info, err := os.Stat(path); err == nil && info.IsDir() {
			return path
		}
		parent := filepath.Dir(path)
		if parent == path {
			break
		}
		path = parent
	}
	return paths.DataDir()
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

	// Scheduled backups use the configured secret; no password flag or
	// environment-variable override is exposed.
	password := cfg.Server.Backup.EncryptionPassword

	// Keep the scheduled backup contents aligned with the maintenance CLI.
	sources, supplementalSources, err := backup.SourcesWithConfig(cfg, paths.ConfigDir(), paths.DataDir(), false, false)
	if err != nil {
		log.Printf("Scheduler: Backup skipped, failed to resolve referenced TLS files: %v", err)
		return nil, err
	}
	var sqliteSourcePath string
	var snapshotSQLite func(context.Context, string) error
	if strings.EqualFold(cfg.Server.Database.Driver, "sqlite") || strings.EqualFold(cfg.Server.Database.Driver, "sqlite3") || strings.EqualFold(cfg.Server.Database.Driver, "sqlite2") {
		sqliteSourcePath = filepath.Join(paths.GetDatabaseDir(paths.DataDir()), "server.db")
		snapshotSQLite = database.SnapshotServerSQLite
	}

	backupDir := paths.BackupDir()
	if err := os.MkdirAll(backupDir, 0o750); err != nil {
		log.Printf("Scheduler: Backup failed, cannot create %s: %v", backupDir, err)
		return nil, err
	}

	return backup.NewManager(backup.Options{
		Dir:                 backupDir,
		Sources:             sources,
		SupplementalSources: supplementalSources,
		SQLiteSourcePath:    sqliteSourcePath,
		SnapshotSQLite:      snapshotSQLite,
		RestoreRoots:        backup.RestoreRoots(paths.ConfigDir(), paths.DataDir(), false, false),
		Password:            password,
		Retention:           cfg.Server.Backup.Retention,
		ComplianceEnabled:   cfg.Server.Compliance.Enabled,
		AppVersion:          s.opts.Version,
		Audit:               s.opts.Audit,
		// The scheduler owns the execution id PART 17's suppression rule
		// keys on, so it sends backup_failed itself from notifyFailure.
		SuppressFailureNotify: true,
	}), nil
}

// sslRenewalTask checks and renews SSL certificates. Per AI.md PART 15
// (Renewal Rules), only app-managed Let's Encrypt certificates under
// {config_dir}/ssl/letsencrypt/{fqdn}/ are auto-renewed; local/user-provided
// certificates under ssl/local/{fqdn}/ are never touched by this task.
func sslRenewalTask(renew func() error, managedCertificatePath, managedDomain string) error {
	log.Println("Scheduler: Checking SSL certificates...")

	cfg, err := config.Load()
	if err != nil {
		log.Printf("Scheduler: SSL renewal check skipped, failed to load config: %v", err)
		return err
	}

	sslCertPath := cfg.Server.SSL.CertPath
	if sslCertPath == "" {
		sslCertPath = filepath.Join(paths.ConfigDir(), "ssl")
	}
	if !cfg.Server.SSL.LetsEncrypt.Enabled {
		log.Println("Scheduler: Let's Encrypt is disabled, no ACME certificate renewal is needed")
		return nil
	}
	if managedCertificatePath == "" && renew == nil && ssl.ParseChallenge(cfg.Server.SSL.LetsEncrypt.Challenge) != "dns-01" {
		log.Println("Scheduler: No app-managed ACME certificate is active, nothing to renew")
		return nil
	}

	domain := cfg.Server.FQDN
	if domain == "" {
		domain = "localhost"
	}
	if managedDomain != "" {
		domain = managedDomain
	}
	certPath := filepath.Join(sslCertPath, "letsencrypt", domain, "fullchain.pem")
	if managedCertificatePath != "" {
		certPath = managedCertificatePath
	}
	if _, err := os.Stat(certPath); os.IsNotExist(err) {
		log.Println("Scheduler: No app-managed Let's Encrypt certificate found, nothing to renew")
		return nil
	}
	if ssl.ParseChallenge(cfg.Server.SSL.LetsEncrypt.Challenge) == "dns-01" {
		daysUntilExpiry, err := ssl.ReportCertificateExpiry(certPath)
		if err != nil {
			return err
		}
		if !ssl.ShouldRenew(daysUntilExpiry) {
			log.Println("Scheduler: DNS-01 certificate is valid, no renewal needed")
			return nil
		}
		if renew != nil {
			if err := renew(); err != nil {
				log.Printf("Scheduler: DNS-01 certificate renewal failed: %v", err)
				return err
			}
			log.Println("Scheduler: DNS-01 certificate renewal completed")
			return nil
		}
		manager := ssl.NewManager(ssl.Config{
			CertPath: sslCertPath,
			LetsEncrypt: ssl.LetsEncryptConfig{
				Enabled:        cfg.Server.SSL.LetsEncrypt.Enabled,
				Email:          cfg.Server.SSL.LetsEncrypt.Email,
				Challenge:      cfg.Server.SSL.LetsEncrypt.Challenge,
				DNSProvider:    cfg.Server.SSL.LetsEncrypt.DNSProvider,
				DNSCredentials: cfg.Server.SSL.LetsEncrypt.DNSCredentials,
			},
			OnDNSCertificateIssued: func(validatedAt time.Time) {
				if err := database.SetAppSecretValue(context.Background(), database.SecretDNSValidatedAt, validatedAt.UTC().Format(time.RFC3339)); err != nil {
					log.Printf("SSL: failed to record DNS provider validation time: %v", err)
				}
			},
		})
		if err := manager.RenewDNSCertificate([]string{domain}); err != nil {
			log.Printf("Scheduler: DNS-01 certificate renewal failed: %v", err)
			return err
		}
		log.Println("Scheduler: DNS-01 certificate renewal completed")
		return nil
	}

	daysUntilExpiry, err := ssl.ReportCertificateExpiry(certPath)
	if err != nil {
		return err
	}
	if !ssl.ShouldRenew(daysUntilExpiry) {
		log.Println("Scheduler: ACME certificate is valid, no renewal needed")
		return nil
	}
	if renew == nil {
		return fmt.Errorf("active ACME renewal manager is unavailable")
	}
	if err := renew(); err != nil {
		log.Printf("Scheduler: SSL renewal failed: %v", err)
		return err
	}

	log.Println("Scheduler: SSL renewal check completed")
	return nil
}

// geoipUpdateTask updates the GeoIP database
func geoipUpdateTask() error {
	log.Println("Scheduler: Updating GeoIP database...")

	// The download honours the GeoIP enabled flag and always refreshes the
	// complete fixed dataset set in the PART 19 directory.
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

// tokenCleanupTask removes stale rate-limit state and purges owners only
// when the configured idle-purge policy is enabled.
func tokenCleanupTask() error {
	log.Println("Scheduler: Cleaning up expired token and rate-limit state...")

	count, err := database.CleanupExpiredRateLimits()
	if err != nil {
		log.Printf("Scheduler: Token cleanup failed: %v", err)
		return err
	}
	challenges, err := database.DeleteExpiredCaptchaChallenges(context.Background(), time.Now().UTC())
	if err != nil {
		log.Printf("Scheduler: CAPTCHA cleanup failed: %v", err)
		return err
	}
	count += challenges
	policy, _ := config.Get().Server.Tokens.Normalized()
	owners, err := database.PurgeIdleOwners(context.Background(), database.GetServerDB(), policy.IdlePurgeDays)
	if err != nil {
		log.Printf("Scheduler: Owner token cleanup failed: %v", err)
		return err
	}
	count += owners

	if count > 0 {
		log.Printf("Scheduler: Token cleanup completed (%d entries removed, including %d idle owners)", count, owners)
	} else {
		log.Println("Scheduler: Token cleanup completed (no expired entries or idle owners)")
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
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := database.ProbeServerDB(ctx); err != nil {
		log.Printf("DEBUG healthcheck_self database probe failed; database_check owns notification thresholds: %s", sanitizeDatabaseError(err.Error(), config.Get()))
	}
	return nil
}

func databaseCheckTask() error {
	settings := config.Get().Server.Notifications.DatabaseError
	if !settings.Enabled {
		databaseCheckState.Lock()
		databaseCheckState.failures = 0
		databaseCheckState.since = time.Time{}
		databaseCheckState.lastAlert = time.Time{}
		databaseCheckState.lastMessage = ""
		databaseCheckState.Unlock()
		return nil
	}
	threshold := settings.Failures
	if threshold < 1 {
		threshold = 3
	}
	repeat, err := time.ParseDuration(settings.Repeat)
	if err != nil || repeat <= 0 {
		repeat = 24 * time.Hour
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	err = database.ProbeServerDB(ctx)
	cancel()
	now := time.Now().UTC()
	databaseCheckState.Lock()
	if err == nil {
		recovered := databaseCheckState.failures > 0
		databaseCheckState.failures = 0
		databaseCheckState.since = time.Time{}
		databaseCheckState.lastAlert = time.Time{}
		databaseCheckState.lastMessage = ""
		databaseCheckState.Unlock()
		if recovered {
			log.Printf("INFO Database connection recovered")
		}
		return nil
	}
	if databaseCheckState.failures == 0 {
		databaseCheckState.since = now
	}
	databaseCheckState.failures++
	databaseCheckState.lastMessage = sanitizeDatabaseError(err.Error(), config.Get())
	if databaseCheckState.failures < threshold || (!databaseCheckState.lastAlert.IsZero() && now.Sub(databaseCheckState.lastAlert) < repeat) {
		databaseCheckState.Unlock()
		return nil
	}
	databaseCheckState.lastAlert = now
	since := databaseCheckState.since
	message := databaseCheckState.lastMessage
	databaseCheckState.Unlock()
	log.Printf("WARN Database connection problem: since=%s error=%s", since.Format(time.RFC3339), message)
	if notifier := email.DefaultNotifier(); notifier != nil {
		if notifyErr := notifier.NotifyDatabaseError(nil, message, since.Format(time.RFC3339)); notifyErr != nil {
			log.Printf("Scheduler: Database error notification failed: %v", notifyErr)
		}
	}
	return nil
}

func sanitizeDatabaseError(message string, cfg *config.Config) string {
	if cfg != nil {
		if token := strings.TrimSpace(cfg.Server.Database.Token); token != "" {
			message = strings.ReplaceAll(message, token, "[redacted]")
		}
		if databaseURL := strings.TrimSpace(cfg.Server.Database.URL); databaseURL != "" {
			message = strings.ReplaceAll(message, databaseURL, "[database URL redacted]")
		}
	}
	return databaseCredentialPattern.ReplaceAllString(message, "$1$2[redacted]")
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
