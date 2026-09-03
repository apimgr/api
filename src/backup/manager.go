package backup

import (
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/shirou/gopsutil/v3/disk"

	"github.com/apimgr/api/src/config"
	"github.com/apimgr/api/src/email"
)

// Audit event names from AI.md PART 21 "Audit Events".
const (
	EventCreated            = "backup.created"
	EventRestored           = "backup.restored"
	EventDeleted            = "backup.deleted"
	EventFailed             = "backup.failed"
	EventRetentionCleanup   = "backup.retention_cleanup"
	EventVerificationFailed = "backup.verification_failed"
	EventDailyUpdated       = "backup.daily_updated"
	EventSkippedDiskFull    = "backup.skipped_disk_full"
)

// DefaultDiskThreshold is the disk usage percentage above which PART 21
// aborts a scheduled backup.
const DefaultDiskThreshold = 90.0

// freeSpaceMultiplier is PART 21's "free < 2x size of most recent backup"
// guard.
const freeSpaceMultiplier = 2

// ErrEncryptionRequired is returned when compliance mode demands an encrypted
// backup but no password has been configured. PART 21 blocks backups entirely
// in that state.
var ErrEncryptionRequired = errors.New("compliance mode requires an encrypted backup but no backup password is set")

// ErrDiskFull is returned when the disk-space precondition fails and the
// scheduled backup is skipped.
var ErrDiskFull = errors.New("backup skipped: insufficient free space")

// AuditFunc records a backup audit event. The server supplies its audit
// logger; a nil hook makes every event a no-op.
type AuditFunc func(event string, details map[string]interface{})

// Options configures a Manager.
type Options struct {
	// Dir is the backup directory resolved at startup. PART 21 forbids
	// re-resolving it later, so the Manager caches exactly this value.
	Dir string
	// Sources are the files and directories every archive contains.
	Sources []string
	// Password enables encryption when non-empty.
	Password string
	// Retention is the configured policy; it is normalized once here.
	Retention config.BackupRetentionConfig
	// ComplianceEnabled mirrors server.compliance.enabled and forces
	// encryption when true.
	ComplianceEnabled bool
	// DiskThreshold is the disk usage percentage above which backups are
	// skipped. Zero uses DefaultDiskThreshold.
	DiskThreshold float64
	// AppVersion is stamped into every manifest.
	AppVersion string
	// Audit receives every PART 21 audit event.
	Audit AuditFunc
	// SuppressFailureNotify stops the Manager from raising the backup_failed
	// operator event itself. The scheduler sets it because it owns the
	// execution id PART 17's "backup_failed suppresses scheduler_error for
	// the same execution" rule keys on, and sends the event itself. The
	// structured ERROR log line is written either way.
	SuppressFailureNotify bool
}

// Manager owns the PART 21 backup lifecycle: creation, verification,
// retention and restore, all against a backup directory cached at startup.
type Manager struct {
	dir               string
	sources           []string
	password          string
	retention         config.BackupRetentionConfig
	complianceEnabled bool
	diskThreshold     float64
	appVersion        string
	audit             AuditFunc
	suppressFailure   bool
}

// NewManager caches the backup directory and normalizes the retention policy,
// logging any validation warnings rather than failing startup.
func NewManager(opts Options) *Manager {
	retention, warnings := opts.Retention.Normalized()
	for _, warning := range warnings {
		log.Printf("Backup: %s", warning)
	}

	threshold := opts.DiskThreshold
	if threshold <= 0 {
		threshold = DefaultDiskThreshold
	}

	return &Manager{
		dir:               opts.Dir,
		sources:           opts.Sources,
		password:          opts.Password,
		retention:         retention,
		complianceEnabled: opts.ComplianceEnabled,
		diskThreshold:     threshold,
		appVersion:        opts.AppVersion,
		audit:             opts.Audit,
		suppressFailure:   opts.SuppressFailureNotify,
	}
}

// Dir returns the backup directory cached at startup.
func (m *Manager) Dir() string {
	return m.dir
}

// Encrypted reports whether archives this Manager creates are encrypted.
func (m *Manager) Encrypted() bool {
	return m.password != ""
}

// emit sends an audit event when the server supplied a hook.
func (m *Manager) emit(event string, details map[string]interface{}) {
	if m.audit == nil {
		return
	}
	m.audit(event, details)
}

// checkEncryptionPolicy enforces the compliance-mode requirement that
// backups be encrypted.
func (m *Manager) checkEncryptionPolicy() error {
	if m.complianceEnabled && m.password == "" {
		return ErrEncryptionRequired
	}
	return nil
}

// RunDaily performs the PART 21 backup_daily flow: retention sweep, disk
// check, full backup, verification, daily incremental, verification, and
// finally retention — applied only when every verification passed.
func (m *Manager) RunDaily(now time.Time) error {
	if err := m.checkEncryptionPolicy(); err != nil {
		m.emit(EventFailed, map[string]interface{}{"error": err.Error()})
		return err
	}

	// Step 1: sweep before writing, so the disk check sees the real headroom.
	if _, err := m.Cleanup(); err != nil {
		log.Printf("Backup: pre-backup retention sweep failed: %v", err)
	}

	// Step 2: refuse to start a backup the volume cannot hold.
	if err := m.checkDiskSpace(); err != nil {
		return err
	}

	// Step 3 and 4: full backup, then verification.
	fullPath := filepath.Join(m.dir, FullBackupName(now, m.Encrypted()))
	fullManifest, err := m.createVerified(fullPath, CreateOptions{
		Sources:    m.sources,
		Password:   m.password,
		Kind:       KindFull,
		AppVersion: m.appVersion,
	})
	if err != nil {
		return err
	}

	m.emit(EventCreated, m.createdDetails(fullPath, fullManifest))

	// Steps 5 and 6: the incremental carries everything changed since the
	// full backup was taken, and is verified the same way.
	incrementalPath := filepath.Join(m.dir, DailyIncrementalName(m.Encrypted()))
	incrementalManifest, err := m.createVerified(incrementalPath, CreateOptions{
		Sources:       m.sources,
		Password:      m.password,
		Kind:          KindDailyIncremental,
		BaseBackup:    filepath.Base(fullPath),
		ModifiedSince: fullManifest.CreatedAt,
		AppVersion:    m.appVersion,
	})
	if err != nil {
		return err
	}

	details := m.createdDetails(incrementalPath, incrementalManifest)
	details["base_backup"] = filepath.Base(fullPath)
	details["changes_since"] = fullManifest.CreatedAt.UTC().Format(time.RFC3339)
	m.emit(EventDailyUpdated, details)

	// Step 7: both archives verified, so old backups may now be pruned.
	if _, err := m.Cleanup(); err != nil {
		return err
	}

	m.notifyBackupComplete(fullPath, fullManifest)
	return nil
}

// RunHourly refreshes the hourly incremental archive against the newest full
// backup. It is driven by the optional backup_hourly scheduler task.
func (m *Manager) RunHourly(now time.Time) error {
	if err := m.checkEncryptionPolicy(); err != nil {
		m.emit(EventFailed, map[string]interface{}{"error": err.Error()})
		return err
	}

	base, cutoff := m.latestFull()
	path := filepath.Join(m.dir, HourlyIncrementalName(m.Encrypted()))
	manifest, err := m.createVerified(path, CreateOptions{
		Sources:       m.sources,
		Password:      m.password,
		Kind:          KindHourlyIncremental,
		BaseBackup:    base,
		ModifiedSince: cutoff,
		AppVersion:    m.appVersion,
	})
	if err != nil {
		return err
	}

	details := m.createdDetails(path, manifest)
	if base != "" {
		details["base_backup"] = base
	}
	m.emit(EventCreated, details)

	m.notifyBackupComplete(path, manifest)
	return nil
}

// CreateManual writes a manual/timestamped full backup and verifies it. The
// caller may pass an explicit filename; an empty name uses the PART 21
// manual naming pattern.
func (m *Manager) CreateManual(name string, now time.Time) (string, error) {
	if err := m.checkEncryptionPolicy(); err != nil {
		m.emit(EventFailed, map[string]interface{}{"error": err.Error()})
		return "", err
	}

	if name == "" {
		name = ManualBackupName(now, m.Encrypted())
	}
	path := name
	if !filepath.IsAbs(path) {
		path = filepath.Join(m.dir, name)
	}

	manifest, err := m.createVerified(path, CreateOptions{
		Sources:    m.sources,
		Password:   m.password,
		Kind:       KindManual,
		AppVersion: m.appVersion,
	})
	if err != nil {
		return "", err
	}

	m.emit(EventCreated, m.createdDetails(path, manifest))

	m.notifyBackupComplete(path, manifest)
	return path, nil
}

// createVerified writes one archive and verifies it. A verification failure
// deletes only that archive: PART 21 keeps every existing valid backup and
// retries on the next scheduled run.
func (m *Manager) createVerified(path string, opts CreateOptions) (*Manifest, error) {
	if _, err := CreateWithOptions(path, opts); err != nil {
		m.emit(EventFailed, map[string]interface{}{
			"filename": filepath.Base(path),
			"error":    err.Error(),
		})
		m.notifyBackupFailed(path, "create", err)
		return nil, err
	}

	manifest, err := Verify(path, opts.Password)
	if err != nil {
		details := map[string]interface{}{
			"filename": filepath.Base(path),
			"error":    err.Error(),
		}
		var failure *VerificationError
		if errors.As(err, &failure) {
			details["check"] = failure.Check
		}
		m.emit(EventVerificationFailed, details)

		if removeErr := os.Remove(path); removeErr != nil && !os.IsNotExist(removeErr) {
			log.Printf("Backup: failed to delete unverified backup %s: %v", filepath.Base(path), removeErr)
		}
		m.notifyBackupFailed(path, "verify", err)
		return nil, err
	}

	return manifest, nil
}

// notifyBackupComplete logs the mandatory structured INFO line for a finished
// backup and raises the operator's backup_complete notification (AI.md
// PART 17 "Operator Notifications"). Email dispatch is best effort: a backup
// that succeeded stays succeeded regardless of SMTP.
func (m *Manager) notifyBackupComplete(path string, manifest *Manifest) {
	name := filepath.Base(path)
	size := formatSize(archiveSize(path))
	kind := ""
	if manifest != nil {
		kind = string(manifest.Kind)
	}

	log.Printf("Backup: [INFO] backup_complete filename=%s kind=%s size=%s encrypted=%t",
		name, kind, size, m.Encrypted())
	email.OperatorBackupComplete(name, size)
}

// notifyBackupFailed logs the mandatory structured ERROR line for a failed
// backup and raises the operator's backup_failed notification, which AI.md
// PART 17 marks critical and always emails when SMTP is available. The
// execution id is empty because a standalone Manager run is not tied to a
// scheduler execution; a scheduler-driven Manager sets SuppressFailureNotify
// and sends the event itself with the real execution id.
func (m *Manager) notifyBackupFailed(path, stage string, cause error) {
	name := filepath.Base(path)
	size := formatSize(archiveSize(path))

	log.Printf("Backup: [ERROR] backup_failed filename=%s stage=%s size=%s error=%v", name, stage, size, cause)
	if m.suppressFailure {
		return
	}
	email.OperatorBackupFailed("", name, size, cause.Error())
}

// archiveSize returns an archive's size on disk, or zero when it cannot be
// measured.
func archiveSize(path string) int64 {
	info, err := os.Stat(path)
	if err != nil {
		return 0
	}
	return info.Size()
}

// formatSize renders a byte count for operator-facing log lines and email
// templates.
func formatSize(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}

	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}

// createdDetails builds the audit payload shared by the creation events.
func (m *Manager) createdDetails(path string, manifest *Manifest) map[string]interface{} {
	details := map[string]interface{}{
		"filename":  filepath.Base(path),
		"encrypted": m.Encrypted(),
		"verified":  true,
	}
	if info, err := os.Stat(path); err == nil {
		details["size"] = info.Size()
	}
	if manifest != nil {
		details["checksum"] = manifest.Checksum
		details["kind"] = string(manifest.Kind)
	}
	return details
}

// Cleanup applies the retention policy to the cached backup directory and
// emits the retention audit events. It runs at startup and after every
// successful backup.
func (m *Manager) Cleanup() (RetentionResult, error) {
	result, err := ApplyRetention(m.dir, m.retention)
	if err != nil {
		return result, err
	}

	for _, warning := range result.Warnings {
		log.Printf("Backup: %s", warning)
	}

	if len(result.Deleted) == 0 {
		return result, nil
	}

	names := make([]string, 0, len(result.Deleted))
	for _, deletion := range result.Deleted {
		names = append(names, deletion.Name)
		m.emit(EventDeleted, map[string]interface{}{
			"filename": deletion.Name,
			"size":     deletion.Size,
			"reason":   deletion.Reason,
		})
	}

	m.emit(EventRetentionCleanup, map[string]interface{}{
		"deleted":        names,
		"reason":         result.Deleted[len(result.Deleted)-1].Reason,
		"remaining":      result.Remaining,
		"remaining_size": result.RemainingSize,
	})

	return result, nil
}

// Restore verifies an archive against every PART 21 restore check and then
// unpacks it. A version mismatch warns rather than blocking.
func (m *Manager) Restore(path, password string) error {
	manifest, err := Verify(path, password)
	if err != nil {
		details := map[string]interface{}{
			"filename": filepath.Base(path),
			"error":    err.Error(),
		}
		var failure *VerificationError
		if errors.As(err, &failure) {
			details["check"] = failure.Check
		}
		m.emit(EventVerificationFailed, details)
		return err
	}

	if m.appVersion != "" && manifest.AppVersion != "" && manifest.AppVersion != m.appVersion {
		log.Printf("Backup: version mismatch - archive was created by %s, this build is %s; restoring anyway",
			manifest.AppVersion, m.appVersion)
	}

	if err := Restore(path, password); err != nil {
		m.emit(EventFailed, map[string]interface{}{
			"filename": filepath.Base(path),
			"error":    err.Error(),
		})
		return err
	}

	m.emit(EventRestored, map[string]interface{}{
		"filename":    filepath.Base(path),
		"app_version": manifest.AppVersion,
	})
	return nil
}

// checkDiskSpace enforces PART 21 step 2: abort when free space is under
// twice the most recent backup, or when the volume is past the threshold.
func (m *Manager) checkDiskSpace() error {
	usage, err := disk.Usage(m.dir)
	if err != nil {
		// A volume that cannot be measured is not a reason to skip a backup;
		// the verification stage still catches a truncated archive.
		log.Printf("Backup: unable to read disk usage for %s: %v", m.dir, err)
		return nil
	}

	required := m.requiredFreeSpace()
	if usage.UsedPercent > m.diskThreshold || (required > 0 && usage.Free < uint64(required)) {
		details := map[string]interface{}{
			"free_bytes":    usage.Free,
			"disk_usage":    usage.UsedPercent,
			"threshold":     m.diskThreshold,
			"required_free": required,
		}
		m.emit(EventSkippedDiskFull, details)
		log.Printf("Backup: [WARN] disk_space_low disk_usage=%.1f%% threshold=%.1f%% free=%s required=%s",
			usage.UsedPercent, m.diskThreshold, formatSize(int64(usage.Free)), formatSize(required))

		// AI.md PART 17 routes "Disk space low" through the security_alert
		// template: WARN in the log, emailed to the operator.
		email.OperatorSecurityAlert("Disk space low", "",
			fmt.Sprintf("Backup skipped for %s: disk usage %.1f%% (threshold %.1f%%), free %s, required %s",
				m.dir, usage.UsedPercent, m.diskThreshold, formatSize(int64(usage.Free)), formatSize(required)))
		return fmt.Errorf("%w: disk usage %.1f%%, free %d bytes", ErrDiskFull, usage.UsedPercent, usage.Free)
	}

	return nil
}

// requiredFreeSpace returns twice the size of the most recent archive, which
// is the headroom PART 21 demands before a new backup starts.
func (m *Manager) requiredFreeSpace() int64 {
	files, err := ListBackups(m.dir)
	if err != nil || len(files) == 0 {
		return 0
	}
	newest := files[len(files)-1]
	return newest.Size * freeSpaceMultiplier
}

// latestFull returns the newest full backup's filename and creation time, the
// baseline an incremental archive is taken against.
func (m *Manager) latestFull() (string, time.Time) {
	files, err := ListBackups(m.dir)
	if err != nil {
		return "", time.Time{}
	}

	for i := len(files) - 1; i >= 0; i-- {
		file := files[i]
		if file.Kind == KindFull || file.Kind == KindManual {
			if info, err := os.Stat(file.Path); err == nil {
				return file.Name, info.ModTime()
			}
			return file.Name, file.Date
		}
	}
	return "", time.Time{}
}
