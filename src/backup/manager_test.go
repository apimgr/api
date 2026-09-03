package backup

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/apimgr/api/src/config"
)

// auditRecorder captures the audit events a Manager emits.
type auditRecorder struct {
	events  []string
	details []map[string]interface{}
}

// record is the AuditFunc handed to the Manager under test.
func (a *auditRecorder) record(event string, details map[string]interface{}) {
	a.events = append(a.events, event)
	a.details = append(a.details, details)
}

// has reports whether an event was emitted.
func (a *auditRecorder) has(event string) bool {
	for _, candidate := range a.events {
		if candidate == event {
			return true
		}
	}
	return false
}

// newTestManager builds a Manager over a temp backup dir with a small source
// tree to archive.
func newTestManager(t *testing.T, password string, retention config.BackupRetentionConfig) (*Manager, *auditRecorder, string) {
	t.Helper()

	source := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(source, "server.yml"), []byte("server:\n  port: 8080\n"), 0600))

	recorder := &auditRecorder{}
	manager := NewManager(Options{
		Dir:        t.TempDir(),
		Sources:    []string{source},
		Password:   password,
		Retention:  retention,
		AppVersion: "1.2.3",
		Audit:      recorder.record,
	})

	return manager, recorder, source
}

// A daily run creates a verified full backup plus a verified incremental and
// emits the PART 21 audit events for both.
func TestManagerRunDailyCreatesAndVerifies(t *testing.T) {
	manager, recorder, _ := newTestManager(t, "", config.BackupRetentionConfig{MaxBackups: 1, MaxTotalSize: "0"})
	now := time.Date(2026, 5, 20, 2, 0, 0, 0, time.UTC)

	require.NoError(t, manager.RunDaily(now))

	assert.FileExists(t, filepath.Join(manager.Dir(), FullBackupName(now, false)))
	assert.FileExists(t, filepath.Join(manager.Dir(), DailyIncrementalName(false)))
	assert.True(t, recorder.has(EventCreated))
	assert.True(t, recorder.has(EventDailyUpdated))
	assert.False(t, recorder.has(EventVerificationFailed))
}

// Encrypted runs must produce .enc archives that verify with the password.
func TestManagerRunDailyEncrypted(t *testing.T) {
	manager, _, _ := newTestManager(t, "correct-horse", config.BackupRetentionConfig{MaxBackups: 1, MaxTotalSize: "0"})
	now := time.Date(2026, 5, 21, 2, 0, 0, 0, time.UTC)

	require.NoError(t, manager.RunDaily(now))

	path := filepath.Join(manager.Dir(), FullBackupName(now, true))
	require.FileExists(t, path)

	manifest, err := Verify(path, "correct-horse")
	require.NoError(t, err)
	assert.True(t, manifest.Encrypted)
	assert.Equal(t, EncryptionMethod, manifest.EncryptionMethod)
}

// A successful daily run prunes older fulls only after both verifications
// pass.
func TestManagerRunDailyAppliesRetention(t *testing.T) {
	manager, recorder, _ := newTestManager(t, "", config.BackupRetentionConfig{MaxBackups: 1, MaxTotalSize: "0"})
	writeBackupFile(t, manager.Dir(), "api_backup_2026-05-01.tar.gz", 10)
	writeBackupFile(t, manager.Dir(), "api_backup_2026-05-02.tar.gz", 10)

	now := time.Date(2026, 5, 22, 2, 0, 0, 0, time.UTC)
	require.NoError(t, manager.RunDaily(now))

	assert.NoFileExists(t, filepath.Join(manager.Dir(), "api_backup_2026-05-01.tar.gz"))
	assert.NoFileExists(t, filepath.Join(manager.Dir(), "api_backup_2026-05-02.tar.gz"))
	assert.FileExists(t, filepath.Join(manager.Dir(), FullBackupName(now, false)))
	assert.True(t, recorder.has(EventRetentionCleanup))
	assert.True(t, recorder.has(EventDeleted))
}

// Compliance mode blocks backups entirely until a password is configured.
func TestManagerComplianceRequiresPassword(t *testing.T) {
	recorder := &auditRecorder{}
	manager := NewManager(Options{
		Dir:               t.TempDir(),
		Sources:           []string{t.TempDir()},
		ComplianceEnabled: true,
		Retention:         config.BackupRetentionConfig{MaxBackups: 1, MaxTotalSize: "0"},
		Audit:             recorder.record,
	})

	err := manager.RunDaily(time.Now())
	require.ErrorIs(t, err, ErrEncryptionRequired)
	assert.True(t, recorder.has(EventFailed))
	assert.Empty(t, backupNames(t, manager.Dir()))
}

// A disk already past the threshold aborts the run with the skipped event and
// leaves existing backups alone.
func TestManagerRunDailySkipsWhenDiskFull(t *testing.T) {
	manager, recorder, _ := newTestManager(t, "", config.BackupRetentionConfig{MaxBackups: 5, MaxTotalSize: "0"})
	existing := writeBackupFile(t, manager.Dir(), "api_backup_2026-06-01.tar.gz", 10)

	// Any real volume is below 100% used, so a negative threshold is the way
	// to force the guard without touching the filesystem.
	manager.diskThreshold = -1

	err := manager.RunDaily(time.Date(2026, 6, 2, 2, 0, 0, 0, time.UTC))
	require.ErrorIs(t, err, ErrDiskFull)
	assert.True(t, recorder.has(EventSkippedDiskFull))
	assert.FileExists(t, existing)
}

// A manual backup uses the timestamped naming pattern and verifies clean.
func TestManagerCreateManual(t *testing.T) {
	manager, recorder, _ := newTestManager(t, "", config.BackupRetentionConfig{MaxBackups: 1, MaxTotalSize: "0"})
	now := time.Date(2026, 7, 4, 12, 30, 15, 0, time.UTC)

	path, err := manager.CreateManual("", now)
	require.NoError(t, err)

	assert.Equal(t, ManualBackupName(now, false), filepath.Base(path))
	assert.True(t, recorder.has(EventCreated))

	manifest, err := Verify(path, "")
	require.NoError(t, err)
	assert.Equal(t, KindManual, manifest.Kind)
}

// Restore verifies before unpacking and records backup.restored.
func TestManagerRestoreVerifiesFirst(t *testing.T) {
	manager, recorder, source := newTestManager(t, "", config.BackupRetentionConfig{MaxBackups: 1, MaxTotalSize: "0"})

	path, err := manager.CreateManual("", time.Date(2026, 8, 1, 9, 0, 0, 0, time.UTC))
	require.NoError(t, err)

	require.NoError(t, os.Remove(filepath.Join(source, "server.yml")))
	require.NoError(t, manager.Restore(path, ""))

	assert.FileExists(t, filepath.Join(source, "server.yml"))
	assert.True(t, recorder.has(EventRestored))
}

// A corrupted archive fails verification and is never restored.
func TestManagerRestoreRejectsCorruptedArchive(t *testing.T) {
	manager, recorder, _ := newTestManager(t, "", config.BackupRetentionConfig{MaxBackups: 1, MaxTotalSize: "0"})

	path, err := manager.CreateManual("", time.Date(2026, 8, 2, 9, 0, 0, 0, time.UTC))
	require.NoError(t, err)

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	data[len(data)/2] ^= 0xFF
	require.NoError(t, os.WriteFile(path, data, 0600))

	err = manager.Restore(path, "")
	require.Error(t, err)
	assert.True(t, recorder.has(EventVerificationFailed))
}

// TestFormatSize covers the operator-facing size rendering used by the
// backup_complete and backup_failed notifications (AI.md PART 17).
func TestFormatSize(t *testing.T) {
	assert.Equal(t, "0 B", formatSize(0))
	assert.Equal(t, "512 B", formatSize(512))
	assert.Equal(t, "1.0 KB", formatSize(1024))
	assert.Equal(t, "1.5 MB", formatSize(1024*1024*3/2))
	assert.Equal(t, "2.0 GB", formatSize(2*1024*1024*1024))
}

// TestArchiveSizeMissingFile proves a size lookup on a deleted archive
// degrades to zero rather than failing the notification path.
func TestArchiveSizeMissingFile(t *testing.T) {
	assert.Equal(t, int64(0), archiveSize(filepath.Join(t.TempDir(), "missing.tar.gz")))
}
