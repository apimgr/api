package scheduler

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/apimgr/api/src/config"
	"github.com/apimgr/api/src/paths"
)

// initTestPaths points the paths package at fresh temp directories for
// config/data/logs so backupTask can read/write server.yml and backup
// archives in isolation, and creates the "db" source dir backupTask expects
// to exist under DataDir().
func initTestPaths(t *testing.T) {
	t.Helper()
	configDir := t.TempDir()
	dataDir := t.TempDir()
	logDir := t.TempDir()
	paths.Init(configDir, dataDir, logDir)
	require.NoError(t, os.MkdirAll(filepath.Join(dataDir, "db"), 0755))
}

// TestBackupTask_CompliancePasswordRequired covers AI.md PART 21's
// compliance-mode enforcement: when server.compliance.enabled is true and no
// backup.encryption_password is configured, the scheduled backup must be
// blocked rather than writing an unencrypted archive.
func TestBackupTask_CompliancePasswordRequired(t *testing.T) {
	initTestPaths(t)
	cfg, err := config.Load()
	require.NoError(t, err)
	cfg.Server.Compliance.Enabled = true
	cfg.Server.Backup.EncryptionPassword = ""
	require.NoError(t, config.Save(cfg))

	os.Unsetenv("API_BACKUP_PASSWORD")

	err = backupTask()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "compliance")

	entries, readErr := os.ReadDir(filepath.Join(paths.DataDir(), "backup"))
	if readErr == nil {
		assert.Empty(t, entries, "no backup archive should be written when compliance blocks the task")
	}
}

// TestBackupTask_ConfigLoadError covers the config-load-failure branch: an
// unparsable server.yml must surface the error from backupTask rather than
// proceeding with defaults.
func TestBackupTask_ConfigLoadError(t *testing.T) {
	initTestPaths(t)
	configFile := filepath.Join(paths.ConfigDir(), "server.yml")
	require.NoError(t, os.WriteFile(configFile, []byte("not: valid: yaml: content: [["), 0644))

	err := backupTask()
	require.Error(t, err)
}

// TestBackupTask_EnvPasswordOverridesConfig covers PART 21's unattended-run
// override: API_BACKUP_PASSWORD takes precedence over
// server.backup.encryption_password for this scheduled run only.
func TestBackupTask_EnvPasswordOverridesConfig(t *testing.T) {
	initTestPaths(t)
	cfg, err := config.Load()
	require.NoError(t, err)
	cfg.Server.Compliance.Enabled = false
	cfg.Server.Backup.EncryptionPassword = "config-password"
	require.NoError(t, config.Save(cfg))

	t.Setenv("API_BACKUP_PASSWORD", "env-password")

	require.NoError(t, backupTask())

	entries, err := os.ReadDir(filepath.Join(paths.DataDir(), "backup"))
	require.NoError(t, err)
	assert.NotEmpty(t, entries, "backup archive should be created")
}

// TestBackupTask_UsesConfigPassword covers the non-compliance path where no
// env override is set: the configured backup.encryption_password is used to
// encrypt the scheduled backup.
func TestBackupTask_UsesConfigPassword(t *testing.T) {
	initTestPaths(t)
	cfg, err := config.Load()
	require.NoError(t, err)
	cfg.Server.Compliance.Enabled = false
	cfg.Server.Backup.EncryptionPassword = "config-only-password"
	require.NoError(t, config.Save(cfg))

	os.Unsetenv("API_BACKUP_PASSWORD")

	require.NoError(t, backupTask())

	entries, err := os.ReadDir(filepath.Join(paths.DataDir(), "backup"))
	require.NoError(t, err)
	assert.NotEmpty(t, entries, "backup archive should be created")
}
