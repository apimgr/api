package scheduler

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/apimgr/api/src/backup"
	"github.com/apimgr/api/src/config"
	"github.com/apimgr/api/src/database"
	"github.com/apimgr/api/src/paths"
)

// initTestPaths points the paths package at fresh temp directories for
// config/data/logs/backups so the backup tasks can read/write server.yml and
// backup archives in isolation, and creates the "db" source dir they expect
// to exist under DataDir().
func initTestPaths(t *testing.T) {
	t.Helper()
	configDir := t.TempDir()
	dataDir := t.TempDir()
	logDir := t.TempDir()
	paths.Init(configDir, dataDir, logDir)
	paths.InitBackup(t.TempDir())
	require.NoError(t, os.MkdirAll(filepath.Join(dataDir, "db"), 0755))
	require.NoError(t, database.Init(config.DatabaseConfig{Driver: "sqlite"}, dataDir))
	t.Cleanup(func() { _ = database.Close() })
}

func TestFormatDiskBytes(t *testing.T) {
	tests := []struct {
		bytes uint64
		want  string
	}{
		{bytes: 0, want: "0 B"},
		{bytes: 1023, want: "1023 B"},
		{bytes: 1024, want: "1.0 KiB"},
		{bytes: 1024 * 1024, want: "1.0 MiB"},
	}
	for _, test := range tests {
		assert.Equal(t, test.want, formatDiskBytes(test.bytes))
	}
}

func TestSanitizeDatabaseErrorRemovesCredentials(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Server.Database.URL = "https://db.example.test/?authToken=url-secret"
	cfg.Server.Database.Token = "separate-secret"
	message := sanitizeDatabaseError("connect https://db.example.test/?authToken=url-secret failed: token=driver-secret password=other-secret", cfg)
	assert.NotContains(t, message, "url-secret")
	assert.NotContains(t, message, "separate-secret")
	assert.NotContains(t, message, "driver-secret")
	assert.NotContains(t, message, "other-secret")
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

	require.NoError(t, os.Unsetenv("API_BACKUP_PASSWORD"))

	err = New().dailyBackupTask()
	require.Error(t, err)
	assert.ErrorIs(t, err, backup.ErrEncryptionRequired)

	entries, readErr := os.ReadDir(paths.BackupDir())
	if readErr == nil {
		assert.Empty(t, entries, "no backup archive should be written when compliance blocks the task")
	}
}

// TestBackupTask_InvalidConfigUsesDefaults verifies the scheduler uses the
// built-in defaults when an unparsable config file is ignored by the loader.
func TestBackupTask_InvalidConfigUsesDefaults(t *testing.T) {
	initTestPaths(t)
	configFile := filepath.Join(paths.ConfigDir(), "server.yml")
	require.NoError(t, os.WriteFile(configFile, []byte("not: valid: yaml: content: [["), 0644))

	require.NoError(t, New().dailyBackupTask())
}

// TestBackupTask_IgnoresUndocumentedEnvPassword ensures only the configured
// secrets.yml value controls unattended backup encryption.
func TestBackupTask_IgnoresUndocumentedEnvPassword(t *testing.T) {
	initTestPaths(t)
	cfg, err := config.Load()
	require.NoError(t, err)
	cfg.Server.Compliance.Enabled = false
	cfg.Server.Backup.EncryptionPassword = "config-password"
	require.NoError(t, config.Save(cfg))

	t.Setenv("API_BACKUP_PASSWORD", "env-password")

	require.NoError(t, New().dailyBackupTask())

	entries, err := os.ReadDir(paths.BackupDir())
	require.NoError(t, err)
	require.NotEmpty(t, entries, "backup archive should be created")
	for _, entry := range entries {
		if filepath.Ext(entry.Name()) == ".enc" {
			return
		}
	}
	assert.Fail(t, "configured backup password must encrypt the scheduled archive")
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

	require.NoError(t, os.Unsetenv("API_BACKUP_PASSWORD"))

	require.NoError(t, New().dailyBackupTask())

	entries, err := os.ReadDir(paths.BackupDir())
	require.NoError(t, err)
	assert.NotEmpty(t, entries, "backup archive should be created")
}

// TestHourlyBackupTask_UsesRollingName covers PART 21's rolling hourly
// incremental: the archive always has the same fixed name so a second run
// replaces the first rather than accumulating files.
func TestHourlyBackupTask_UsesRollingName(t *testing.T) {
	initTestPaths(t)
	cfg, err := config.Load()
	require.NoError(t, err)
	cfg.Server.Compliance.Enabled = false
	cfg.Server.Backup.EncryptionPassword = ""
	require.NoError(t, config.Save(cfg))

	require.NoError(t, os.Unsetenv("API_BACKUP_PASSWORD"))

	s := New()
	require.NoError(t, s.dailyBackupTask())
	require.NoError(t, s.hourlyBackupTask())
	require.NoError(t, s.hourlyBackupTask())

	entries, err := os.ReadDir(paths.BackupDir())
	require.NoError(t, err)
	require.Len(t, entries, 2, "hourly incremental should add one rolling file alongside its required full base")
	assert.Equal(t, "api-hourly.tar.gz", entries[0].Name())
	assert.True(t, strings.HasPrefix(entries[1].Name(), "api_backup_"))
}

// TestRegisterDefaultTasks_MatchesSpec locks the AI.md PART 18 built-in task
// table: every task must be registered with the spec's schedule, default
// enabled state, and skippability.
func TestRegisterDefaultTasks_MatchesSpec(t *testing.T) {
	initTestDB(t)
	originalConfig := config.Get()
	configForTest := *originalConfig
	configForTest.Server.I2P.Enabled = false
	config.Set(&configForTest)
	t.Cleanup(func() { config.Set(originalConfig) })

	s := New()
	s.RegisterDefaultTasks()

	expected := map[string]struct {
		schedule  string
		enabled   bool
		skippable bool
	}{
		"ssl_renewal":      {"0 3 * * *", true, false},
		"geoip_update":     {"0 3 * * 0", true, true},
		"blocklist_update": {"0 4 * * *", true, true},
		"cve_update":       {"0 5 * * *", true, true},
		"update_check":     {"0 6 * * *", true, true},
		"token_cleanup":    {"@every 15m", true, false},
		"security_cleanup": {"30 3 * * *", true, false},
		"keyserver_retry":  {"@every 15m", true, false},
		"disk_check":       {"@every 5m", true, true},
		"database_check":   {"@every 1m", true, true},
		"log_rotation":     {"0 0 * * *", true, false},
		"backup_daily":     {"0 2 * * *", true, true},
		"backup_hourly":    {"@hourly", false, true},
		"healthcheck_self": {"@every 5m", true, false},
	}
	if torInstalled() {
		expected["tor_health"] = struct {
			schedule  string
			enabled   bool
			skippable bool
		}{"@every 10m", true, false}
	}

	tasks := s.ListTasks()
	byID := make(map[string]TaskInfo, len(tasks))
	for _, task := range tasks {
		byID[task.ID] = task
	}

	require.Len(t, tasks, len(expected))
	require.Len(t, expected, len(expected))

	for id, want := range expected {
		got, ok := byID[id]
		require.True(t, ok, "task %s must be registered", id)
		assert.Equal(t, want.schedule, got.Schedule, "task %s schedule", id)
		assert.Equal(t, want.enabled, got.Enabled, "task %s enabled default", id)
		assert.Equal(t, want.skippable, got.Skippable, "task %s skippable", id)
		assert.NotEmpty(t, got.Name, "task %s must have a title", id)
	}

	// I2P is disabled for this baseline case; its task is mode-bound.
	_, hasI2P := byID["i2p_health"]
	assert.False(t, hasI2P, "i2p_health must not be registered while I2P is disabled")
}

func TestRegisterDefaultTasks_RegistersI2PHealthOnlyWhenEnabled(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		t.Run(map[bool]string{false: "disabled", true: "enabled"}[enabled], func(t *testing.T) {
			initTestDB(t)
			originalConfig := config.Get()
			configForTest := *originalConfig
			configForTest.Server.I2P.Enabled = enabled
			config.Set(&configForTest)
			t.Cleanup(func() { config.Set(originalConfig) })

			s := New()
			s.RegisterDefaultTasks()
			found := false
			for _, task := range s.ListTasks() {
				if task.ID == "i2p_health" {
					found = true
					break
				}
			}
			assert.Equal(t, enabled, found, "i2p_health registration must follow server.i2p.enabled")
		})
	}
}

// TestRegisterDefaultTasks_EveryTaskHasAnImplementation guards against a task
// being registered with a nil body, which would panic on execution.
func TestRegisterDefaultTasks_EveryTaskHasAnImplementation(t *testing.T) {
	initTestDB(t)

	s := New()
	s.RegisterDefaultTasks()

	for _, task := range s.GetTasks() {
		assert.NotNil(t, task.Func, "task %s must have an implementation", task.Name)
	}
}

// TestSetTaskEnabled_RefusesRequiredTask covers PART 18's "Skippable: No"
// column: a required task cannot be disabled, while a skippable one can.
func TestSetTaskEnabled_RefusesRequiredTask(t *testing.T) {
	initTestDB(t)

	s := New()
	s.RegisterDefaultTasks()

	err := s.SetTaskEnabled("ssl_renewal", false)
	require.ErrorIs(t, err, ErrTaskRequired)

	info, err := s.ShowTask("ssl_renewal")
	require.NoError(t, err)
	assert.True(t, info.Enabled, "a required task stays enabled")

	require.NoError(t, s.SetTaskEnabled("geoip_update", false))
	info, err = s.ShowTask("geoip_update")
	require.NoError(t, err)
	assert.False(t, info.Enabled)

	require.NoError(t, s.SetTaskEnabled("geoip_update", true))
	info, err = s.ShowTask("geoip_update")
	require.NoError(t, err)
	assert.True(t, info.Enabled)
}

// TestTaskAPI_UnknownTask covers the CLI-facing error path shared by
// `scheduler show|run|enable|disable` for a task that does not exist.
func TestTaskAPI_UnknownTask(t *testing.T) {
	initTestDB(t)

	s := New()
	s.RegisterDefaultTasks()

	_, err := s.ShowTask("nope")
	require.ErrorIs(t, err, ErrUnknownTask)

	require.ErrorIs(t, s.RunTaskByID("nope"), ErrUnknownTask)
	require.ErrorIs(t, s.SetTaskEnabled("nope", true), ErrUnknownTask)
}

// TestTaskHistory_RecordsExecutions verifies `scheduler history <id>` reads
// back the rows written by a completed run.
func TestTaskHistory_RecordsExecutions(t *testing.T) {
	initTestDB(t)

	s := New()
	s.AddTask("history_probe", "@daily", func() error { return nil }, true)

	require.NoError(t, s.RunTaskByID("history_probe"))

	entries, err := s.TaskHistory("history_probe", 10)
	require.NoError(t, err)
	require.NotEmpty(t, entries)
	assert.Equal(t, "history_probe", entries[0].TaskID)
	assert.Equal(t, "success", entries[0].Status)
}
