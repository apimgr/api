package paths

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// resetOverrides clears the package-level override variables so tests don't
// leak state into one another, and restores them afterward.
func resetOverrides(t *testing.T) {
	t.Helper()
	origConfig, origData, origLogs, origCache, origBackup := configDir, dataDir, logsDir, cacheDir, backupDir
	configDir, dataDir, logsDir, cacheDir, backupDir = "", "", "", "", ""
	t.Cleanup(func() {
		configDir, dataDir, logsDir, cacheDir, backupDir = origConfig, origData, origLogs, origCache, origBackup
	})
}

func TestIsElevatedNow(t *testing.T) {
	// Just verify it runs without panic and returns a bool consistent with
	// the actual euid on non-Windows.
	got := isElevatedNow()
	if runtime.GOOS != "windows" {
		assert.Equal(t, os.Geteuid() == 0, got)
	}
}

func TestIsElevated(t *testing.T) {
	// IsElevated should return the same value captured at package init.
	assert.Equal(t, startedElevated, IsElevated())
}

func TestInit(t *testing.T) {
	resetOverrides(t)

	Init("/custom/config", "/custom/data", "/custom/logs")
	assert.Equal(t, "/custom/config", ConfigDir())
	assert.Equal(t, "/custom/data", DataDir())
	assert.Equal(t, "/custom/logs", LogDir())
}

func TestInit_EmptyValuesDoNotOverride(t *testing.T) {
	resetOverrides(t)

	Init("/first/config", "/first/data", "/first/logs")
	// Calling Init again with empty strings must not clear the existing
	// overrides.
	Init("", "", "")
	assert.Equal(t, "/first/config", ConfigDir())
	assert.Equal(t, "/first/data", DataDir())
	assert.Equal(t, "/first/logs", LogDir())
}

func TestConfigDir_FallsBackToDefault(t *testing.T) {
	resetOverrides(t)

	cfg, _, _ := GetDefaultDirs()
	assert.Equal(t, cfg, ConfigDir())
}

func TestDataDir_FallsBackToDefault(t *testing.T) {
	resetOverrides(t)

	_, data, _ := GetDefaultDirs()
	assert.Equal(t, data, DataDir())
}

func TestLogDir_FallsBackToDefault(t *testing.T) {
	resetOverrides(t)

	_, _, logs := GetDefaultDirs()
	assert.Equal(t, logs, LogDir())
}

func TestInitCache(t *testing.T) {
	resetOverrides(t)

	InitCache("/custom/cache")
	assert.Equal(t, "/custom/cache", CacheDir())

	// Empty value must not clear an existing override.
	InitCache("")
	assert.Equal(t, "/custom/cache", CacheDir())
}

func TestCacheDir_FallsBackToDefault(t *testing.T) {
	resetOverrides(t)

	assert.Equal(t, GetCacheDir(), CacheDir())
}

func TestInitBackup(t *testing.T) {
	resetOverrides(t)

	InitBackup("/custom/backup")
	assert.Equal(t, "/custom/backup", BackupDir())

	// Empty value must not clear an existing override.
	InitBackup("")
	assert.Equal(t, "/custom/backup", BackupDir())
}

func TestGetDefaultDirs_ContainsOrgAndProject(t *testing.T) {
	resetOverrides(t)

	cfg, data, logs := GetDefaultDirs()

	require.NotEmpty(t, cfg)
	require.NotEmpty(t, data)
	require.NotEmpty(t, logs)

	if IsRunningInContainer() {
		assert.Equal(t, filepath.Join("/config", ProjectName), cfg)
		assert.Equal(t, filepath.Join("/data", ProjectName), data)
		assert.Equal(t, filepath.Join("/data/log", ProjectName), logs)
		return
	}

	assert.Contains(t, cfg, OrgName)
	assert.Contains(t, cfg, ProjectName)
	assert.Contains(t, data, OrgName)
	assert.Contains(t, data, ProjectName)
	assert.Contains(t, logs, OrgName)
	assert.Contains(t, logs, ProjectName)
}

func TestGetCacheDir_ContainsOrgAndProject(t *testing.T) {
	resetOverrides(t)

	cache := GetCacheDir()
	require.NotEmpty(t, cache)

	if IsRunningInContainer() {
		assert.Equal(t, filepath.Join("/data", ProjectName, "cache"), cache)
		return
	}

	assert.Contains(t, cache, OrgName)
	assert.Contains(t, cache, ProjectName)
}

func TestEnsureDir(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "a", "b", "c")

	require.NoError(t, EnsureDir(target))

	info, err := os.Stat(target)
	require.NoError(t, err)
	assert.True(t, info.IsDir())
}

func TestEnsureDirectories(t *testing.T) {
	if !IsRunningInContainer() {
		t.Skip("EnsureDirectories always targets GetDefaultDirs() (no override); only" +
			" safe to exercise inside an ephemeral container, where it resolves to" +
			" /config and /data rather than real host user/system directories")
	}
	resetOverrides(t)

	cfg, data, logs := GetDefaultDirs()

	// Only clean up directories this test actually creates, not ones that
	// already existed.
	for _, dir := range []string{cfg, data, logs} {
		if _, err := os.Stat(dir); os.IsNotExist(err) {
			dir := dir
			t.Cleanup(func() { os.RemoveAll(dir) })
		}
	}

	require.NoError(t, EnsureDirectories())

	for _, dir := range []string{cfg, data, logs} {
		info, err := os.Stat(dir)
		require.NoError(t, err)
		assert.True(t, info.IsDir())
	}
}

func TestIsRunningInContainer(t *testing.T) {
	// Just verify it doesn't panic and returns a stable bool consistent
	// with the actual environment markers.
	got := IsRunningInContainer()
	_, dockerErr := os.Stat("/.dockerenv")
	expected := dockerErr == nil
	if !expected {
		data, err := os.ReadFile("/proc/1/comm")
		if err == nil {
			comm := string(data)
			expected = comm == "tini\n" || comm == "tini" || comm == "dumb-init\n"
		}
	}
	assert.Equal(t, expected, got)
}

func TestGetBackupDir(t *testing.T) {
	resetOverrides(t)

	dir := GetBackupDir()
	require.NotEmpty(t, dir)

	if IsRunningInContainer() {
		assert.Equal(t, filepath.Join("/data/backups", ProjectName), dir)
	}
}

func TestBackupDir_FallsBackToDefault(t *testing.T) {
	resetOverrides(t)

	assert.Equal(t, GetBackupDir(), BackupDir())
}

func TestSystemBackupDir(t *testing.T) {
	dir := systemBackupDir()
	require.NotEmpty(t, dir)
	assert.Contains(t, dir, OrgName)
	assert.Contains(t, dir, ProjectName)

	switch runtime.GOOS {
	case "windows":
		assert.Contains(t, dir, "Backups")
	case "darwin":
		assert.True(t, strings.HasPrefix(dir, "/Library/Backups"))
	case "freebsd", "openbsd", "netbsd":
		assert.True(t, strings.HasPrefix(dir, "/var/backups"))
	default:
		assert.True(t, strings.HasPrefix(dir, "/mnt/Backups"))
	}
}

func TestUserBackupDir(t *testing.T) {
	dir := userBackupDir()
	require.NotEmpty(t, dir)
	assert.Contains(t, dir, OrgName)
	assert.Contains(t, dir, ProjectName)
	assert.Contains(t, dir, "Backups")
}

func TestDefaultPIDPath(t *testing.T) {
	path := DefaultPIDPath()
	require.NotEmpty(t, path)
	assert.True(t, strings.HasSuffix(path, ProjectName+".pid"))

	if IsRunningInContainer() {
		assert.Equal(t, filepath.Join("/data", ProjectName, ProjectName+".pid"), path)
	}
}

func TestIsWritable_ExistingWritableDir(t *testing.T) {
	dir := t.TempDir()
	assert.True(t, isWritable(dir))
}

func TestIsWritable_CreatesMissingDir(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "nested", "path")

	assert.True(t, isWritable(target))

	info, err := os.Stat(target)
	require.NoError(t, err)
	assert.True(t, info.IsDir())
}

func TestIsWritable_ReadOnlyDirFails(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("permission bits behave differently on Windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("root bypasses permission checks")
	}

	dir := t.TempDir()
	roDir := filepath.Join(dir, "readonly")
	require.NoError(t, os.Mkdir(roDir, 0o555))
	t.Cleanup(func() {
		os.Chmod(roDir, 0o755)
	})

	target := filepath.Join(roDir, "child")
	assert.False(t, isWritable(target))
}
