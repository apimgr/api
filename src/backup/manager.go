package backup

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
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

// BackupManagerOptions configures a Manager.
type BackupManagerOptions struct {
	// Dir is the backup directory resolved at startup. PART 21 forbids
	// re-resolving it later, so the Manager caches exactly this value.
	Dir string
	// Sources are the files and directories every archive contains.
	Sources []string
	// SupplementalSources are explicitly referenced files with stable archive
	// paths outside the ordinary source roots.
	SupplementalSources []SupplementalSource
	// SQLiteSourcePath identifies the live SQLite database included in Sources.
	SQLiteSourcePath string
	// SnapshotSQLite writes a consistent database snapshot to the given path.
	SnapshotSQLite func(context.Context, string) error
	// ExportAppSecrets captures generated secrets, including rotation rows, for
	// archives whose database is remote or lacks a local server.db.
	ExportAppSecrets func(context.Context) ([]byte, error)
	// ExportAppSecretsFromSQLiteSnapshot reads generated-secret rows from the
	// same SQLite snapshot included in the archive.
	ExportAppSecretsFromSQLiteSnapshot func(context.Context, string) ([]byte, error)
	// RestoreAppSecrets applies the app_secrets.json payload after the file
	// restore has installed files but before its rollback backups are removed.
	RestoreAppSecrets func(context.Context, []byte) error
	// RollbackAppSecrets restores external database availability after a failed
	// coordinated filesystem/app_secrets restore.
	RollbackAppSecrets func(context.Context) error
	// RestoreRoots are the exact config/data destinations this manager may
	// overwrite during restore.
	RestoreRoots []string
	// Password enables encryption when non-empty.
	Password string
	// Retention is the configured policy; it is normalized once here.
	Retention config.BackupRetentionConfig
	// RestoreLimits bounds archive reads and extraction for this manager.
	RestoreLimits RestoreLimits
	// ComplianceEnabled mirrors whether any server.compliance standard is on and forces
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
	dir                   string
	sources               []string
	supplementalSources   []SupplementalSource
	sqliteSourcePath      string
	snapshotSQLite        func(context.Context, string) error
	exportAppSecrets      func(context.Context) ([]byte, error)
	exportSnapshotSecrets func(context.Context, string) ([]byte, error)
	restoreAppSecrets     func(context.Context, []byte) error
	rollbackAppSecrets    func(context.Context) error
	restoreRoots          []string
	password              string
	retention             config.BackupRetentionConfig
	restoreLimits         RestoreLimits
	complianceEnabled     bool
	diskThreshold         float64
	appVersion            string
	audit                 AuditFunc
	suppressFailure       bool
}

// NewManager caches the backup directory and normalizes the retention policy,
// logging any validation warnings rather than failing startup.
func NewManager(opts BackupManagerOptions) *Manager {
	retention, warnings := opts.Retention.Normalized()
	for _, warning := range warnings {
		log.Printf("Backup: %s", warning)
	}

	threshold := opts.DiskThreshold
	if threshold <= 0 {
		threshold = DefaultDiskThreshold
	}
	restoreLimits := NormalizeRestoreLimits(opts.RestoreLimits)

	return &Manager{
		dir:                   opts.Dir,
		sources:               opts.Sources,
		supplementalSources:   append([]SupplementalSource(nil), opts.SupplementalSources...),
		sqliteSourcePath:      opts.SQLiteSourcePath,
		snapshotSQLite:        opts.SnapshotSQLite,
		exportAppSecrets:      opts.ExportAppSecrets,
		exportSnapshotSecrets: opts.ExportAppSecretsFromSQLiteSnapshot,
		restoreAppSecrets:     opts.RestoreAppSecrets,
		rollbackAppSecrets:    opts.RollbackAppSecrets,
		restoreRoots:          opts.RestoreRoots,
		password:              opts.Password,
		retention:             retention,
		restoreLimits:         restoreLimits,
		complianceEnabled:     opts.ComplianceEnabled,
		diskThreshold:         threshold,
		appVersion:            opts.AppVersion,
		audit:                 opts.Audit,
		suppressFailure:       opts.SuppressFailureNotify,
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
	snapshotAt := now.UTC()
	if now.IsZero() {
		snapshotAt = time.Now().UTC()
	}
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
	snapshotOptions, snapshotDir, err := m.snapshotSourcesAtRunStart()
	if err != nil {
		return err
	}
	if snapshotDir != "" {
		defer func() {
			if err := os.RemoveAll(snapshotDir); err != nil {
				log.Printf("Backup: failed to remove run snapshot workspace")
			}
		}()
	}

	// One source snapshot feeds both archives so their manifests describe the
	// same filesystem and SQLite state at the run's T0.
	// Steps 3 and 4 create the cumulative incremental against the prior full
	// first; staging protects the previous daily incremental if creation fails.
	baseName, baseAt := m.latestFull()
	var stagedIncremental, finalIncremental string
	var incrementalManifest *Manifest
	if baseName != "" {
		basePath := filepath.Join(m.dir, baseName)
		passwordMatches, headerErr := encryptionPasswordMatches(basePath, m.password)
		if headerErr != nil {
			log.Printf("Backup: [WARN] incremental base header is corrupt; skipping incremental and creating a new full: %v", headerErr)
		}
		if headerErr == nil && !passwordMatches {
			log.Printf("Backup: [INFO] backup password changed; skipping incremental and creating a new full")
		}
		if headerErr == nil && passwordMatches {
			baseManifest, err := VerifyWithLimits(basePath, m.password, m.restoreLimits)
			if err != nil {
				log.Printf("Backup: [WARN] incremental base failed verification; skipping incremental and creating a new full: %v", err)
			} else {
				baseSHA256 := baseManifest.SHA256
				changedFiles, deletedFiles, err := diffSnapshotFiles(snapshotOptions.SnapshotFiles, baseManifest.Files)
				if err != nil {
					return fmt.Errorf("compare incremental source snapshot: %w", err)
				}
				stagedIncremental, err = newIncrementalStagePath(m.dir, m.Encrypted())
				if err != nil {
					return err
				}
				defer func() {
					if err := os.Remove(stagedIncremental); err != nil && !errors.Is(err, os.ErrNotExist) {
						log.Printf("Backup: failed to remove staged incremental")
					}
				}()
				finalIncremental = filepath.Join(m.dir, DailyIncrementalName(m.Encrypted()))
				incrementalManifest, err = m.createVerified(stagedIncremental, CreateOptions{
					SnapshotAt:         snapshotAt,
					Sources:            m.sources,
					Password:           m.password,
					Kind:               KindDailyIncremental,
					BaseBackup:         baseName,
					BaseSHA256:         baseSHA256,
					Deleted:            deletedFiles,
					ModifiedSince:      baseAt,
					AppVersion:         m.appVersion,
					SQLiteSourcePath:   snapshotOptions.SQLiteSourcePath,
					SQLiteSnapshotPath: snapshotOptions.SQLiteSnapshotPath,
					SnapshotFiles:      changedFiles,
				})
				if err != nil {
					return err
				}
			}
		}
	}

	// Steps 5 and 6 write the new full from the same T0 snapshot and verify it
	// before retention may remove any existing backup.
	fullPath := filepath.Join(m.dir, FullBackupName(snapshotAt, m.Encrypted()))
	fullManifest, err := m.createVerified(fullPath, CreateOptions{
		SnapshotAt:         snapshotAt,
		Sources:            m.sources,
		Password:           m.password,
		Kind:               KindFull,
		AppVersion:         m.appVersion,
		SQLiteSourcePath:   snapshotOptions.SQLiteSourcePath,
		SQLiteSnapshotPath: snapshotOptions.SQLiteSnapshotPath,
		SnapshotFiles:      snapshotOptions.SnapshotFiles,
	})
	if err != nil {
		return err
	}
	m.emit(EventCreated, m.createdDetails(fullPath, fullManifest))

	if stagedIncremental != "" {
		if err := replaceBackupFile(stagedIncremental, finalIncremental); err != nil {
			return fmt.Errorf("finalize daily incremental: %w", err)
		}
		details := m.createdDetails(finalIncremental, incrementalManifest)
		details["base_backup"] = baseName
		details["changes_since"] = baseAt.UTC().Format(time.RFC3339)
		m.emit(EventDailyUpdated, details)
	}

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
	snapshotAt := now.UTC()
	if now.IsZero() {
		snapshotAt = time.Now().UTC()
	}
	if err := m.checkEncryptionPolicy(); err != nil {
		m.emit(EventFailed, map[string]interface{}{"error": err.Error()})
		return err
	}

	base, cutoff := m.latestFull()
	if base == "" {
		log.Printf("Backup: hourly incremental skipped because no verified full backup exists")
		return nil
	}
	basePath := filepath.Join(m.dir, base)
	passwordMatches, headerErr := encryptionPasswordMatches(basePath, m.password)
	if headerErr != nil {
		log.Printf("Backup: [WARN] hourly incremental base header is corrupt; skipping: %v", headerErr)
		return nil
	}
	if !passwordMatches {
		log.Printf("Backup: [INFO] backup password changed; skipping hourly incremental")
		return nil
	}
	baseManifest, err := VerifyWithLimits(basePath, m.password, m.restoreLimits)
	if err != nil {
		log.Printf("Backup: [WARN] hourly incremental base failed verification; skipping: %v", err)
		return nil
	}
	baseSHA256 := baseManifest.SHA256
	snapshotOptions, snapshotDir, err := m.snapshotSourcesAtRunStart()
	if err != nil {
		return err
	}
	defer func() {
		if err := os.RemoveAll(snapshotDir); err != nil {
			log.Printf("Backup: failed to remove hourly snapshot workspace")
		}
	}()
	changedFiles, deletedFiles, err := diffSnapshotFiles(snapshotOptions.SnapshotFiles, baseManifest.Files)
	if err != nil {
		return fmt.Errorf("compare hourly incremental source snapshot: %w", err)
	}
	path := filepath.Join(m.dir, HourlyIncrementalName(m.Encrypted()))
	manifest, err := m.createVerified(path, CreateOptions{
		SnapshotAt:         snapshotAt,
		Sources:            m.sources,
		Password:           m.password,
		Kind:               KindHourlyIncremental,
		BaseBackup:         base,
		BaseSHA256:         baseSHA256,
		Deleted:            deletedFiles,
		ModifiedSince:      cutoff,
		AppVersion:         m.appVersion,
		SQLiteSourcePath:   snapshotOptions.SQLiteSourcePath,
		SQLiteSnapshotPath: snapshotOptions.SQLiteSnapshotPath,
		SnapshotFiles:      changedFiles,
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

func newIncrementalStagePath(dir string, encrypted bool) (string, error) {
	suffix := ".tar.gz"
	if encrypted {
		suffix += ".enc"
	}
	file, err := os.CreateTemp(dir, ".backup-incremental-*"+suffix)
	if err != nil {
		reportCriticalWriteError(err)
		return "", fmt.Errorf("create incremental staging archive: %w", err)
	}
	path := file.Name()
	closeErr := file.Close()
	reportCriticalWriteError(closeErr)
	if err := errors.Join(closeErr, os.Remove(path)); err != nil {
		return "", fmt.Errorf("prepare incremental staging archive: %w", err)
	}
	return path, nil
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
	if name == "." || name == ".." || filepath.Base(name) != name || strings.ContainsAny(name, `/\\`) {
		return "", fmt.Errorf("backup filename must be a single filename")
	}
	path := filepath.Join(m.dir, name)

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
	if opts.SnapshotFiles == nil {
		snapshotOptions, snapshotDir, err := m.snapshotSourcesAtRunStart()
		if err != nil {
			return nil, err
		}
		if snapshotDir != "" {
			defer func() {
				if err := os.RemoveAll(snapshotDir); err != nil {
					log.Printf("Backup: failed to remove source snapshot workspace")
				}
			}()
		}
		opts.SnapshotFiles = snapshotOptions.SnapshotFiles
		opts.SQLiteSourcePath = snapshotOptions.SQLiteSourcePath
		opts.SQLiteSnapshotPath = snapshotOptions.SQLiteSnapshotPath
	}
	if _, err := CreateWithOptions(path, opts); err != nil {
		m.emit(EventFailed, map[string]interface{}{
			"filename": filepath.Base(path),
			"error":    err.Error(),
		})
		m.notifyBackupFailed(path, "create", err)
		return nil, err
	}

	manifest, err := VerifyWithLimits(path, opts.Password, m.restoreLimits)
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

func (m *Manager) snapshotSourcesAtRunStart() (CreateOptions, string, error) {
	dir, err := os.MkdirTemp(m.dir, ".backup-snapshot-")
	if err != nil {
		reportCriticalWriteError(err)
		return CreateOptions{}, "", fmt.Errorf("create backup snapshot workspace: %w", err)
	}
	options := CreateOptions{Sources: m.sources}
	var appSecrets []byte
	var appSecretsErr error
	if m.sqliteSourcePath != "" && m.snapshotSQLite != nil {
		path := filepath.Join(dir, "server.db")
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		err = m.snapshotSQLite(ctx, path)
		cancel()
		if err != nil {
			reportCriticalWriteError(err)
			removeErr := os.RemoveAll(dir)
			return CreateOptions{}, "", errors.Join(fmt.Errorf("snapshot SQLite database: %w", err), removeErr)
		}
		options.SQLiteSourcePath = m.sqliteSourcePath
		options.SQLiteSnapshotPath = path
	} else if m.exportAppSecrets != nil {
		// Remote database state is captured at T0 before filesystem sources are
		// copied, so app_secrets.json belongs to the same backup snapshot.
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		appSecrets, appSecretsErr = m.exportAppSecrets(ctx)
		cancel()
		if appSecretsErr != nil {
			return CreateOptions{}, "", errors.Join(fmt.Errorf("export app_secrets backup source: %w", appSecretsErr), os.RemoveAll(dir))
		}
	}
	files, err := captureSources(m.sources, dir, options.SQLiteSourcePath, options.SQLiteSnapshotPath)
	if err != nil {
		return CreateOptions{}, "", errors.Join(fmt.Errorf("capture backup source snapshot: %w", err), os.RemoveAll(dir))
	}
	for _, file := range files {
		if strings.HasSuffix(file.ArchivePath, "secrets.yml") {
			if err := redactBackupSecrets(file.Path); err != nil {
				return CreateOptions{}, "", errors.Join(fmt.Errorf("redact backup secrets: %w", err), os.RemoveAll(dir))
			}
		}
	}
	if options.SQLiteSnapshotPath != "" && m.exportSnapshotSecrets != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		appSecrets, appSecretsErr = m.exportSnapshotSecrets(ctx, options.SQLiteSnapshotPath)
		cancel()
		if appSecretsErr != nil {
			return CreateOptions{}, "", errors.Join(fmt.Errorf("export app_secrets backup source: %w", appSecretsErr), os.RemoveAll(dir))
		}
	}
	if appSecrets != nil {
		secretPath := filepath.Join(dir, "files", "app_secrets.json")
		if err := os.MkdirAll(filepath.Dir(secretPath), 0700); err != nil {
			reportCriticalWriteError(err)
			return CreateOptions{}, "", errors.Join(err, os.RemoveAll(dir))
		}
		if err := os.WriteFile(secretPath, appSecrets, 0600); err != nil {
			reportCriticalWriteError(err)
			return CreateOptions{}, "", errors.Join(err, os.RemoveAll(dir))
		}
		files = append(files, SnapshotFile{ArchivePath: "app_secrets.json", Path: secretPath})
	}
	files, err = captureSupplementalSources(files, m.supplementalSources, dir)
	if err != nil {
		return CreateOptions{}, "", errors.Join(fmt.Errorf("capture supplemental backup sources: %w", err), os.RemoveAll(dir))
	}
	options.SnapshotFiles = files
	return options, dir, nil
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
		details["checksum"] = manifest.SHA256
		details["kind"] = string(manifest.Kind)
	}
	return details
}

// Cleanup applies the retention policy to the cached backup directory and
// emits the retention audit events. It runs at startup and after every
// successful backup.
func (m *Manager) Cleanup() (RetentionResult, error) {
	dependencies := m.retainedIncrementalBases()
	result, err := ApplyRetentionWithDependencies(m.dir, m.retention, dependencies)
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

func (m *Manager) retainedIncrementalBases() map[string][]string {
	files, err := ListBackups(m.dir)
	if err != nil {
		return nil
	}
	protected := make(map[string][]string)
	for _, file := range files {
		if !file.Incremental() {
			continue
		}
		manifest, err := VerifyWithLimits(file.Path, m.password, m.restoreLimits)
		if err == nil && manifest.BaseBackup != "" {
			protected[manifest.BaseBackup] = append(protected[manifest.BaseBackup], file.Name)
		}
	}
	for name := range protected {
		sort.Strings(protected[name])
	}
	return protected
}

// Restore authorizes the caller against PART 21's restore authorization
// matrix, verifies the archive against every PART 21 restore check, and then
// unpacks it. A version mismatch warns rather than blocking.
func (m *Manager) Restore(caller Caller, path, password string) error {
	actor, err := AuthorizeRestore(caller)
	if err != nil {
		m.emit(EventFailed, map[string]interface{}{
			"filename":  filepath.Base(path),
			"operation": "restore",
			"error":     err.Error(),
		})
		return err
	}

	if RequiresPassword(path) && password == "" {
		return ErrRestorePasswordRequired
	}

	manifest, err := VerifyWithLimits(path, password, m.restoreLimits)
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

	roots := m.restoreRoots
	if len(roots) == 0 {
		roots = m.sources
	}
	var appSecrets []byte
	if m.restoreAppSecrets != nil {
		appSecrets, err = readAppSecretsPayload(path, password, m.restoreLimits, manifest)
		if err != nil {
			m.emit(EventFailed, map[string]interface{}{"filename": filepath.Base(path), "error": err.Error()})
			return err
		}
	}
	var commitAppSecrets func() error
	if appSecrets != nil {
		commitAppSecrets = func() error {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			return m.restoreAppSecrets(ctx, appSecrets)
		}
	}
	var rollbackAppSecrets func() error
	if commitAppSecrets != nil && m.rollbackAppSecrets != nil {
		rollbackAppSecrets = func() error {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			return m.rollbackAppSecrets(ctx)
		}
	}
	if err := restoreWithLimitsTransaction(path, password, m.restoreLimits, commitAppSecrets, rollbackAppSecrets, roots...); err != nil {
		m.emit(EventFailed, map[string]interface{}{
			"filename": filepath.Base(path),
			"error":    err.Error(),
		})
		return err
	}

	m.emit(EventRestored, map[string]interface{}{
		"filename":    filepath.Base(path),
		"app_version": manifest.AppVersion,
		"actor":       actor,
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
		log.Printf("Backup: [ERROR] backup.skipped_disk_full disk_usage=%.1f%% threshold=%.1f%% free=%s required=%s",
			usage.UsedPercent, m.diskThreshold, formatSize(int64(usage.Free)), formatSize(required))
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
			manifest, err := VerifyWithLimits(file.Path, m.password, m.restoreLimits)
			if err == nil {
				return file.Name, manifest.CreatedAt
			}
			log.Printf("Backup: ignoring invalid full backup %s as an incremental base: %v", file.Name, err)
		}
	}
	return "", time.Time{}
}
