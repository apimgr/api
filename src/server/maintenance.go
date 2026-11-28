package server

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/apimgr/api/src/backup"
	"github.com/apimgr/api/src/config"
	"github.com/apimgr/api/src/database"
	"github.com/apimgr/api/src/email"
	"github.com/apimgr/api/src/paths"
	"github.com/apimgr/api/src/server/handler"
)

type DatabaseRecoveryFunc func(context.Context) error

type MaintenanceMonitor struct {
	cfg      *config.ServerConfig
	recover  DatabaseRecoveryFunc
	mu       sync.Mutex
	info     *handler.MaintenanceInfo
	stopOnce sync.Once
	cancel   context.CancelFunc
	done     chan struct{}
	wake     chan struct{}
}

var maintenanceMonitorMu sync.RWMutex
var activeMaintenanceMonitor *MaintenanceMonitor

// MaintenanceMiddleware keeps health and read-only routes available while
// rejecting all state-changing HTTP methods during critical recovery.
func MaintenanceMiddleware(cfg *config.ServerConfig) func(http.Handler) http.Handler {
	interval := maintenanceRetryInterval(cfg)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			info := handler.MaintenanceState()
			if info == nil || isReadOnlyMethod(r.Method) {
				next.ServeHTTP(w, r)
				return
			}
			w.Header().Set("Retry-After", fmt.Sprintf("%d", max(1, int(interval.Seconds()))))
			w.Header().Set("X-Maintenance-Mode", "true")
			w.Header().Set("X-Maintenance-Reason", info.Reason)
			message := fmt.Sprintf("Server is in maintenance mode due to: %s", info.Message)
			writeEnvelopeError(w, http.StatusServiceUnavailable, "MAINTENANCE", message, responseObject(responseField("reason", info.Reason), responseField("self_healing", info.SelfHealing.Enabled)))
		})
	}
}

func isReadOnlyMethod(method string) bool {
	switch strings.ToUpper(method) {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return true
	default:
		return false
	}
}

// StartMaintenanceMonitor starts the critical dependency probes and recovery
// loop. Database recovery is supplied by app so schema setup remains owned by
// the startup layer.
func StartMaintenanceMonitor(ctx context.Context, cfg *config.ServerConfig, recoverDatabase DatabaseRecoveryFunc) *MaintenanceMonitor {
	if ctx == nil {
		ctx = context.Background()
	}
	if cfg == nil {
		cfg = config.DefaultConfig()
	}
	monitorCtx, cancel := context.WithCancel(ctx)
	monitor := &MaintenanceMonitor{
		cfg: cfg, recover: recoverDatabase, cancel: cancel, done: make(chan struct{}), wake: make(chan struct{}, 1),
	}
	maintenanceMonitorMu.Lock()
	activeMaintenanceMonitor = monitor
	maintenanceMonitorMu.Unlock()
	go monitor.run(monitorCtx)
	return monitor
}

// Stop stops the monitor and waits for its recovery attempt to finish.
func (m *MaintenanceMonitor) Stop() {
	if m == nil {
		return
	}
	m.stopOnce.Do(m.cancel)
	<-m.done
	maintenanceMonitorMu.Lock()
	cleared := false
	if activeMaintenanceMonitor == m {
		activeMaintenanceMonitor = nil
		cleared = true
	}
	maintenanceMonitorMu.Unlock()
	if cleared {
		handler.SetMaintenanceState(nil)
	}
}

// TriggerMaintenance enters read-only mode for a critical write failure
// reported by a runtime subsystem.
func TriggerMaintenance(reason, message string) {
	maintenanceMonitorMu.RLock()
	monitor := activeMaintenanceMonitor
	maintenanceMonitorMu.RUnlock()
	if monitor != nil {
		monitor.enter(reason, message)
	}
}

func (m *MaintenanceMonitor) run(ctx context.Context) {
	defer close(m.done)
	if reason, message := m.checkCritical(ctx); reason != "" {
		m.enter(reason, message)
	}
	for {
		state := m.current()
		delay := maintenanceRetryInterval(m.cfg)
		if state != nil {
			if state.SelfHealing.Exhausted || state.SelfHealing.NextAttempt == nil {
				select {
				case <-ctx.Done():
					return
				case <-m.wake:
					continue
				}
			}
			delay = time.Until(*state.SelfHealing.NextAttempt)
			if delay < 0 {
				delay = 0
			}
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			return
		case <-m.wake:
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			continue
		case <-timer.C:
			if state == nil {
				if reason, message := m.checkCritical(ctx); reason != "" {
					m.enter(reason, message)
				}
				continue
			}
			if state.SelfHealing.Exhausted {
				continue
			}
			m.retry(ctx)
		}
	}
}

func (m *MaintenanceMonitor) current() *handler.MaintenanceInfo {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.info == nil {
		return nil
	}
	copyOfInfo := *m.info
	copyOfInfo.SelfHealing = cloneSelfHealingInfo(m.info.SelfHealing)
	return &copyOfInfo
}

func (m *MaintenanceMonitor) enter(reason, message string) {
	if reason == "" {
		reason = "file_write"
	}
	if message == "" {
		message = "A critical server dependency failed. Review the health endpoint guidance and server logs."
	}
	m.mu.Lock()
	if m.info != nil {
		m.mu.Unlock()
		return
	}
	now := time.Now().UTC()
	next := now.Add(maintenanceRetryInterval(m.cfg))
	m.info = &handler.MaintenanceInfo{
		Reason: reason, Message: message, Since: now,
		SelfHealing: handler.SelfHealingInfo{Enabled: true, NextAttempt: &next},
	}
	info := *m.info
	m.mu.Unlock()
	handler.SetMaintenanceState(&info)
	select {
	case m.wake <- struct{}{}:
	default:
	}
	logMaintenance("error", "server entered maintenance mode", map[string]interface{}{"reason": reason, "message": message})
	emitMaintenanceAudit("server.maintenance_entered", map[string]interface{}{"reason": reason})
	if m.cfg.Server.Maintenance.Notify.OnEnter {
		go notifyMaintenance("Server entered maintenance mode", reason, message)
	}
}

func (m *MaintenanceMonitor) retry(ctx context.Context) {
	m.mu.Lock()
	if m.info == nil || m.info.SelfHealing.Exhausted {
		m.mu.Unlock()
		return
	}
	info := m.info
	info.SelfHealing.Attempts++
	now := time.Now().UTC()
	info.SelfHealing.LastAttempt = &now
	next := now.Add(maintenanceRetryInterval(m.cfg))
	info.SelfHealing.NextAttempt = &next
	infoCopy := *info
	infoCopy.SelfHealing = cloneSelfHealingInfo(info.SelfHealing)
	m.mu.Unlock()
	handler.SetMaintenanceState(&infoCopy)

	if strings.HasPrefix(infoCopy.Reason, "database_") && m.recover != nil {
		if err := m.recoverDatabase(ctx); err != nil {
			logMaintenance("warn", "database self-healing attempt failed", map[string]interface{}{"attempt": infoCopy.SelfHealing.Attempts, "error": err.Error()})
		}
		if ctx.Err() != nil {
			return
		}
	}
	if err := m.cleanupIfNeeded(); err != nil {
		logMaintenance("warn", "maintenance cleanup attempt failed", map[string]interface{}{"error": err.Error()})
	}

	if reason, message := m.checkCritical(ctx); reason == "" {
		verifyCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
		select {
		case <-verifyCtx.Done():
			cancel()
			return
		case <-time.After(250 * time.Millisecond):
		}
		reason, message = m.checkCritical(verifyCtx)
		cancel()
		if reason == "" {
			m.exit()
			return
		}
		m.updateReason(reason, message)
	} else {
		m.updateReason(reason, message)
	}

	m.mu.Lock()
	if m.info != nil && m.cfg.Server.Maintenance.SelfHealing.MaxAttempts > 0 && m.info.SelfHealing.Attempts >= m.cfg.Server.Maintenance.SelfHealing.MaxAttempts {
		m.info.SelfHealing.Enabled = false
		m.info.SelfHealing.Exhausted = true
		m.info.SelfHealing.NextAttempt = nil
		exhausted := *m.info
		exhausted.SelfHealing = cloneSelfHealingInfo(m.info.SelfHealing)
		m.mu.Unlock()
		handler.SetMaintenanceState(&exhausted)
		logMaintenance("error", "self-healing attempts exhausted", map[string]interface{}{"reason": exhausted.Reason, "attempts": exhausted.SelfHealing.Attempts})
		emitMaintenanceAudit("server.self_healing_exhausted", map[string]interface{}{"reason": exhausted.Reason, "attempts": exhausted.SelfHealing.Attempts})
		go notifyMaintenance("Operator action required", exhausted.Reason, "Self-healing stopped after the configured attempt limit.")
		return
	}
	if m.info != nil {
		m.info.SelfHealing.NextAttempt = ptrTime(time.Now().UTC().Add(maintenanceRetryInterval(m.cfg)))
		infoCopy = *m.info
		infoCopy.SelfHealing = cloneSelfHealingInfo(m.info.SelfHealing)
	}
	m.mu.Unlock()
	if infoCopy.Reason != "" {
		handler.SetMaintenanceState(&infoCopy)
	}
	logMaintenance("warn", "server remains in maintenance mode", map[string]interface{}{"reason": infoCopy.Reason, "attempt": infoCopy.SelfHealing.Attempts})
}

func (m *MaintenanceMonitor) recoverDatabase(ctx context.Context) error {
	var err error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			delay := time.Duration(1<<(attempt-1)) * time.Second
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(delay):
			}
		}
		if err = m.recover(ctx); err == nil {
			return nil
		}
	}
	return err
}

func (m *MaintenanceMonitor) updateReason(reason, message string) {
	m.mu.Lock()
	if m.info == nil {
		m.mu.Unlock()
		return
	}
	m.info.Reason = reason
	m.info.Message = message
	copyOfInfo := *m.info
	copyOfInfo.SelfHealing = cloneSelfHealingInfo(m.info.SelfHealing)
	m.mu.Unlock()
	handler.SetMaintenanceState(&copyOfInfo)
}

func (m *MaintenanceMonitor) exit() {
	m.mu.Lock()
	if m.info == nil {
		m.mu.Unlock()
		return
	}
	info := *m.info
	duration := time.Since(info.Since).Round(time.Second).String()
	m.info = nil
	m.mu.Unlock()
	handler.SetMaintenanceState(nil)
	logMaintenance("info", "issue resolved; exiting maintenance mode", map[string]interface{}{"reason": info.Reason, "duration": duration})
	emitMaintenanceAudit("server.maintenance_exited", map[string]interface{}{"reason": info.Reason, "duration": duration})
	if m.cfg.Server.Maintenance.Notify.OnExit {
		go notifyMaintenance("Server recovered from maintenance mode", info.Reason, "Recovery duration: "+duration)
	}
}

func (m *MaintenanceMonitor) checkCritical(ctx context.Context) (string, string) {
	if err := database.ProbeServerDB(ctx); err != nil {
		return "database_connection", "Database connection failed. Verify database availability, network access, and credentials."
	}
	if err := database.ProbeServerDBWrite(ctx); err != nil {
		return "database_write", "The database rejected a required write. Check database availability, disk space, and write permissions."
	}
	seenPaths := make(map[string]bool)
	for _, dir := range []string{paths.ConfigDir(), paths.DataDir(), paths.LogDir(), paths.BackupDir()} {
		cleanPath := filepath.Clean(dir)
		if cleanPath == "." || seenPaths[cleanPath] {
			continue
		}
		seenPaths[cleanPath] = true
		if percent, err := diskUsagePercent(cleanPath); err == nil && percent >= m.cfg.Server.Maintenance.Cleanup.DiskThreshold {
			if cleanupErr := m.cleanupIfNeeded(); cleanupErr != nil {
				logMaintenance("warn", "disk-pressure cleanup failed", map[string]interface{}{"error": cleanupErr.Error()})
			}
			break
		}
	}
	if err := checkDirectoryWritable(paths.ConfigDir()); err != nil {
		if isDiskFull(err) {
			return "disk_full", "The server cannot write because the configuration disk is full. Free disk space and check permissions."
		}
		return "file_write", "The server cannot write to its configuration directory. Check disk space and permissions."
	}
	if err := checkDirectoryWritable(paths.DataDir()); err != nil {
		if isDiskFull(err) {
			return "disk_full", "The server cannot write because the data disk is full. Free disk space and check permissions."
		}
		return "file_write", "The server cannot write to its data directory. Check disk space and permissions."
	}
	if err := checkDirectoryWritable(paths.LogDir()); err != nil {
		if isDiskFull(err) {
			return "disk_full", "The server cannot write because the log disk is full. Free disk space and check permissions."
		}
		return "file_write", "The server cannot write to its log directory. Check disk space and permissions."
	}
	if err := checkDirectoryWritable(paths.BackupDir()); err != nil {
		if isDiskFull(err) {
			return "disk_full", "The server cannot write because the backup disk is full. Free disk space and check permissions."
		}
		return "file_write", "The server cannot write to its backup directory. Check disk space and permissions."
	}
	if err := checkLogFilesWritable(maintenanceActiveLogFiles(m.cfg)); err != nil {
		if isDiskFull(err) {
			return "disk_full", "The server cannot write because a log file's disk is full. Free disk space and check permissions."
		}
		return "file_write", "The server cannot write to an active log file. Check file ownership and permissions."
	}
	return "", ""
}

func (m *MaintenanceMonitor) cleanupIfNeeded() error {
	var failures []error
	now := time.Now()
	removedBytes := int64(0)
	cutoff := now.Add(-time.Duration(m.cfg.Server.Maintenance.Cleanup.LogRetentionDays) * 24 * time.Hour)
	entries, err := os.ReadDir(paths.LogDir())
	if err != nil && !os.IsNotExist(err) {
		failures = append(failures, err)
	}
	activeLogs := maintenanceActiveLogFiles(m.cfg)
	auditLogBase := filepath.Base(m.cfg.Server.Logs.Audit.Filename)
	for _, entry := range entries {
		// Audit files are append-only and their retention is controlled only by
		// the audit rotation policy. The general log age policy must not remove
		// rotated audit records or their compressed copies.
		if entry.IsDir() || activeLogs[entry.Name()] || isAuditLogFile(entry.Name(), auditLogBase) {
			continue
		}
		path := filepath.Join(paths.LogDir(), entry.Name())
		info, statErr := entry.Info()
		if statErr != nil || info.ModTime().After(cutoff) {
			continue
		}
		if removeErr := os.Remove(path); removeErr != nil {
			failures = append(failures, removeErr)
		} else {
			removedBytes += info.Size()
		}
	}
	tempCutoff := now.Add(-24 * time.Hour)
	removedBytes += cleanupOwnedTempFiles(paths.ConfigDir(), ".config-*.tmp", tempCutoff)
	removedBytes += cleanupOwnedTempFiles(paths.ConfigDir(), ".pgp-*.tmp", tempCutoff)
	removedBytes += cleanupOwnedTempFiles(paths.ConfigDir(), ".i2p-*.tmp", tempCutoff)
	removedBytes += cleanupOwnedTempFiles(paths.ConfigDir(), ".acme-*", tempCutoff)
	removedBytes += cleanupOwnedTempFiles(paths.ConfigDir(), "*.tmp", tempCutoff)
	removedBytes += cleanupOwnedTempFiles(paths.DataDir(), ".geoip-*.tmp", tempCutoff)
	removedBytes += cleanupOwnedTempFiles(paths.DataDir(), ".cache-restore-*.db", tempCutoff)
	removedBytes += cleanupOwnedTempFiles(paths.DataDir(), ".maintenance-write-*", tempCutoff)
	removedBytes += cleanupOwnedTempFiles(paths.DataDir(), "*.tmp", tempCutoff)
	removedBytes += cleanupOwnedTempFiles(paths.CacheDir(), ".cache-restore-*.db", tempCutoff)
	removedBytes += cleanupOwnedTempFiles(paths.CacheDir(), ".maintenance-write-*", tempCutoff)
	removedBytes += cleanupOwnedTempFiles(paths.CacheDir(), "*.tmp", tempCutoff)
	removedBytes += cleanupOwnedTempFiles(paths.LogDir(), ".audit.anchor-*", tempCutoff)
	removedBytes += cleanupOwnedTempFiles(paths.LogDir(), ".audit-gzip-*", tempCutoff)
	removedBytes += cleanupOwnedTempFiles(paths.LogDir(), ".maintenance-write-*", tempCutoff)
	removedBytes += cleanupOwnedTempFiles(paths.LogDir(), "*.tmp", tempCutoff)
	removedBytes += cleanupOwnedTempFiles(paths.BackupDir(), ".backup-incremental-*", tempCutoff)
	removedBytes += cleanupOwnedTempFiles(paths.BackupDir(), ".restore-*.tmp", tempCutoff)
	removedBytes += cleanupOwnedTempFiles(paths.BackupDir(), ".restore-backup-*.tmp", tempCutoff)
	removedBytes += cleanupOwnedTempFiles(paths.BackupDir(), ".maintenance-write-*", tempCutoff)
	removedBytes += cleanupOwnedTempFiles(paths.BackupDir(), "*.tmp", tempCutoff)
	result, backupErr := backup.ApplyRetention(paths.BackupDir(), m.cfg.Server.Backup.Retention)
	if backupErr != nil {
		failures = append(failures, backupErr)
	} else {
		for _, deletion := range result.Deleted {
			removedBytes += deletion.Size
		}
	}
	if removedBytes > 0 {
		logMaintenance("warn", "maintenance disk cleanup completed", map[string]interface{}{"freed_bytes": removedBytes})
	}
	return errors.Join(failures...)
}

func isAuditLogFile(name, auditLogBase string) bool {
	if auditLogBase == "" || auditLogBase == "." || auditLogBase == string(filepath.Separator) {
		return false
	}
	return name == auditLogBase || strings.HasPrefix(name, auditLogBase+".")
}

func cleanupOwnedTempFiles(dir, pattern string, olderThan time.Time) int64 {
	var removed int64
	err := filepath.WalkDir(dir, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			if os.IsNotExist(walkErr) {
				return nil
			}
			return nil
		}
		if entry.IsDir() || entry.Type()&os.ModeSymlink != 0 {
			return nil
		}
		matched, matchErr := filepath.Match(pattern, entry.Name())
		if matchErr != nil || !matched {
			return nil
		}
		info, infoErr := entry.Info()
		if infoErr != nil || !info.Mode().IsRegular() || info.ModTime().After(olderThan) {
			return nil
		}
		if os.Remove(path) == nil {
			removed += info.Size()
		}
		return nil
	})
	if err != nil {
		return 0
	}
	return removed
}

func maintenanceActiveLogFiles(cfg *config.ServerConfig) map[string]bool {
	active := make(map[string]bool)
	if cfg == nil {
		return active
	}
	for _, name := range []string{
		cfg.Server.Logs.Access.Filename,
		cfg.Server.Logs.Server.Filename,
		cfg.Server.Logs.Error.Filename,
		cfg.Server.Logs.App.Filename,
		cfg.Server.Logs.Auth.Filename,
		cfg.Server.Logs.Security.Filename,
		cfg.Server.Logs.Debug.Filename,
		cfg.Server.Logs.Audit.Filename,
	} {
		if name != "" {
			active[filepath.Base(name)] = true
		}
	}
	return active
}

func checkLogFilesWritable(files map[string]bool) error {
	for name := range files {
		path := filepath.Join(paths.LogDir(), name)
		if err := probeLogFileWrite(path); os.IsPermission(err) {
			info, statErr := os.Stat(path)
			if statErr != nil {
				return errors.Join(err, statErr)
			}
			if chmodErr := os.Chmod(path, info.Mode().Perm()|0200); chmodErr != nil {
				return errors.Join(err, chmodErr)
			}
			err = probeLogFileWrite(path)
			if err != nil {
				return err
			}
		} else if err != nil {
			return err
		}
	}
	return nil
}

func probeLogFileWrite(path string) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0600)
	if err != nil {
		return err
	}
	syncErr := file.Sync()
	closeErr := file.Close()
	return errors.Join(syncErr, closeErr)
}

func checkDirectoryWritable(dir string) error {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	err := probeWrite(dir)
	if os.IsPermission(err) {
		if repairErr := repairDirectoryWritePermission(dir); repairErr == nil {
			err = probeWrite(dir)
		} else {
			err = errors.Join(err, repairErr)
		}
	}
	if err == nil {
		return nil
	}
	if alternateErr := probeAlternateWriteLocation(paths.CacheDir()); alternateErr != nil {
		return fmt.Errorf("%s: %w (alternate location unavailable: %v)", dir, err, alternateErr)
	}
	return fmt.Errorf("%s: %w (alternate cache location is writable)", dir, err)
}

func probeWrite(dir string) error {
	file, err := os.CreateTemp(dir, ".maintenance-write-*")
	if err != nil {
		return err
	}
	name := file.Name()
	_, writeErr := file.Write([]byte("ok"))
	syncErr := file.Sync()
	closeErr := file.Close()
	removeErr := os.Remove(name)
	return errors.Join(writeErr, syncErr, closeErr, removeErr)
}

func repairDirectoryWritePermission(dir string) error {
	info, err := os.Stat(dir)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("%s is not a directory", dir)
	}
	mode := info.Mode().Perm() | 0300
	if mode == info.Mode().Perm() {
		return nil
	}
	return os.Chmod(dir, mode)
}

func probeAlternateWriteLocation(dir string) error {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	file, err := os.CreateTemp(dir, ".maintenance-write-*")
	if err != nil {
		return err
	}
	name := file.Name()
	if _, err := file.Write([]byte("ok")); err != nil {
		_ = file.Close()
		_ = os.Remove(name)
		return err
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(name)
		return err
	}
	return os.Remove(name)
}

func maintenanceRetryInterval(cfg *config.ServerConfig) time.Duration {
	if cfg == nil {
		return 30 * time.Second
	}
	interval, err := time.ParseDuration(cfg.Server.Maintenance.SelfHealing.RetryInterval)
	if err != nil || interval <= 0 {
		return 30 * time.Second
	}
	return interval
}

func emitMaintenanceAudit(event string, details map[string]interface{}) {
	if err := database.WriteAuditEventWithSource(context.Background(), event, "system", "", "maintenance", details); err != nil {
		log.Printf("Maintenance: failed to record %s audit event: %v", event, err)
	}
}

func logMaintenance(level, message string, details map[string]interface{}) {
	if logger := GetLogger(); logger != nil {
		logger.LogApp(level, message, details)
		return
	}
	log.Printf("Maintenance: %s: %v", message, details)
}

func notifyMaintenance(event, reason, details string) {
	if notifier := email.DefaultNotifier(); notifier != nil && notifier.Enabled() {
		if err := notifier.NotifyTemplate("security_alert", map[string]string{
			"event":   event + ": " + reason,
			"ip":      "",
			"details": details,
		}, nil); err != nil {
			log.Printf("Maintenance: failed to send notification: %v", err)
		}
	}
}

func cloneSelfHealingInfo(info handler.SelfHealingInfo) handler.SelfHealingInfo {
	if info.LastAttempt != nil {
		last := *info.LastAttempt
		info.LastAttempt = &last
	}
	if info.NextAttempt != nil {
		next := *info.NextAttempt
		info.NextAttempt = &next
	}
	return info
}

func ptrTime(value time.Time) *time.Time { return &value }
