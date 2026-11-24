package backup

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/apimgr/api/src/config"
)

func TestRestoreIncrementalReconstructsBaseOverlayAndDeletions(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "config")
	require.NoError(t, os.MkdirAll(source, 0700))
	require.NoError(t, os.WriteFile(filepath.Join(source, "unchanged.yml"), []byte("same"), 0600))
	require.NoError(t, os.WriteFile(filepath.Join(source, "changed.yml"), []byte("before"), 0600))
	require.NoError(t, os.WriteFile(filepath.Join(source, "removed.yml"), []byte("remove me"), 0600))

	backupDir := filepath.Join(root, "backups")
	require.NoError(t, os.MkdirAll(backupDir, 0700))
	baseName := "base.tar.gz"
	basePath := filepath.Join(backupDir, baseName)
	_, err := CreateWithOptions(basePath, CreateOptions{
		SnapshotAt: time.Date(2026, 11, 1, 2, 0, 0, 0, time.UTC),
		Sources:    []string{source},
		Kind:       KindFull,
	})
	require.NoError(t, err)
	baseSHA, err := fileSHA256(basePath)
	require.NoError(t, err)

	require.NoError(t, os.WriteFile(filepath.Join(source, "changed.yml"), []byte("after"), 0600))
	require.NoError(t, os.WriteFile(filepath.Join(source, "added.yml"), []byte("new"), 0600))
	require.NoError(t, os.Remove(filepath.Join(source, "removed.yml")))
	incrementalPath := filepath.Join(backupDir, "incremental.tar.gz")
	_, err = CreateWithOptions(incrementalPath, CreateOptions{
		SnapshotAt: time.Date(2026, 11, 2, 2, 0, 0, 0, time.UTC),
		Kind:       KindDailyIncremental,
		BaseBackup: baseName,
		BaseSHA256: baseSHA,
		Deleted:    []string{"config/removed.yml"},
		SnapshotFiles: []SnapshotFile{
			{ArchivePath: "config/changed.yml", Path: filepath.Join(source, "changed.yml")},
			{ArchivePath: "config/added.yml", Path: filepath.Join(source, "added.yml")},
		},
	})
	require.NoError(t, err)

	output := filepath.Join(root, "restored")
	require.NoError(t, RestoreWithLimits(incrementalPath, "", DefaultRestoreLimits(), output))
	restoredConfig := filepath.Join(output, "config")
	unchanged, err := os.ReadFile(filepath.Join(restoredConfig, "unchanged.yml"))
	require.NoError(t, err)
	require.Equal(t, "same", string(unchanged))
	changed, err := os.ReadFile(filepath.Join(restoredConfig, "changed.yml"))
	require.NoError(t, err)
	require.Equal(t, "after", string(changed))
	added, err := os.ReadFile(filepath.Join(restoredConfig, "added.yml"))
	require.NoError(t, err)
	require.Equal(t, "new", string(added))
	require.NoFileExists(t, filepath.Join(restoredConfig, "removed.yml"))
}

func TestManagerDailyIncrementalUsesPreviousFullAndRetainsItsBase(t *testing.T) {
	manager, _, source := newTestManager(t, "", config.BackupRetentionConfig{MaxBackups: 1, MaxTotalSize: "0"})
	firstRun := time.Date(2026, 11, 10, 2, 0, 0, 0, time.UTC)
	secondRun := firstRun.Add(24 * time.Hour)
	require.NoError(t, os.WriteFile(filepath.Join(source, "removed.yml"), []byte("old"), 0600))
	require.NoError(t, manager.RunDaily(firstRun))
	require.NoError(t, os.WriteFile(filepath.Join(source, "server.yml"), []byte("server:\n  port: 9090\n"), 0600))
	require.NoError(t, os.WriteFile(filepath.Join(source, "added.yml"), []byte("new"), 0600))
	require.NoError(t, os.Remove(filepath.Join(source, "removed.yml")))
	require.NoError(t, manager.RunDaily(secondRun))

	dailyPath := filepath.Join(manager.Dir(), DailyIncrementalName(false))
	manifest, err := Verify(dailyPath, "")
	require.NoError(t, err)
	require.Equal(t, FullBackupName(firstRun, false), manifest.BaseBackup)
	require.FileExists(t, filepath.Join(manager.Dir(), manifest.BaseBackup))
	require.Equal(t, []string{filepath.ToSlash(filepath.Join(filepath.Base(source), "removed.yml"))}, manifest.Deleted)
	require.Len(t, manifest.Files, 2)
}

func TestVerifyIncrementalRejectsMissingBase(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "config")
	require.NoError(t, os.MkdirAll(source, 0700))
	require.NoError(t, os.WriteFile(filepath.Join(source, "server.yml"), []byte("server: {}\n"), 0600))
	basePath := filepath.Join(root, "base.tar.gz")
	_, err := CreateWithOptions(basePath, CreateOptions{Sources: []string{source}, Kind: KindFull})
	require.NoError(t, err)
	baseSHA, err := fileSHA256(basePath)
	require.NoError(t, err)
	incrementalPath := filepath.Join(root, "incremental.tar.gz")
	_, err = CreateWithOptions(incrementalPath, CreateOptions{
		Kind:       KindDailyIncremental,
		BaseBackup: filepath.Base(basePath),
		BaseSHA256: baseSHA,
		SnapshotFiles: []SnapshotFile{{
			ArchivePath: "config/server.yml",
			Path:        filepath.Join(source, "server.yml"),
		}},
	})
	require.NoError(t, err)
	require.NoError(t, os.Remove(basePath))
	output := filepath.Join(root, "restored")
	serverConfig := filepath.Join(output, "config", "server.yml")
	require.NoError(t, os.MkdirAll(filepath.Dir(serverConfig), 0700))
	require.NoError(t, os.WriteFile(serverConfig, []byte("keep"), 0600))

	err = RestoreWithLimits(incrementalPath, "", DefaultRestoreLimits(), output)
	require.ErrorIs(t, err, ErrBaseMissing)
	contents, err := os.ReadFile(serverConfig)
	require.NoError(t, err)
	require.Equal(t, "keep", string(contents))
}

func TestVerifyIncrementalRejectsDifferentEncryptionState(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "config")
	require.NoError(t, os.MkdirAll(source, 0700))
	require.NoError(t, os.WriteFile(filepath.Join(source, "server.yml"), []byte("server: {}\n"), 0600))
	basePath := filepath.Join(root, "base.tar.gz")
	_, err := CreateWithOptions(basePath, CreateOptions{Sources: []string{source}, Kind: KindFull})
	require.NoError(t, err)
	baseSHA, err := fileSHA256(basePath)
	require.NoError(t, err)
	incrementalPath := filepath.Join(root, "incremental.tar.gz.enc")
	_, err = CreateWithOptions(incrementalPath, CreateOptions{
		Sources:    []string{source},
		Password:   "test-password",
		Kind:       KindDailyIncremental,
		BaseBackup: filepath.Base(basePath),
		BaseSHA256: baseSHA,
	})
	require.NoError(t, err)

	_, err = Verify(incrementalPath, "test-password")
	require.True(t, errors.Is(err, ErrBaseCorrupt), "expected base-corrupt error, got %v", err)
}
