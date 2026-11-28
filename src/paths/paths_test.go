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

	SetApplicationPathOverrides("/custom/config", "/custom/data", "/custom/logs")
	assert.Equal(t, "/custom/config", ConfigDir())
	assert.Equal(t, "/custom/data", DataDir())
	assert.Equal(t, "/custom/logs", LogDir())
}

func TestInit_EmptyValuesDoNotOverride(t *testing.T) {
	resetOverrides(t)

	SetApplicationPathOverrides("/first/config", "/first/data", "/first/logs")
	// Calling Init again with empty strings must not clear the existing
	// overrides.
	SetApplicationPathOverrides("", "", "")
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
			t.Cleanup(func() { _ = os.RemoveAll(dir) })
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
	assert.IsType(t, false, IsRunningInContainer())
}

func TestDetectContainerSignals(t *testing.T) {
	tests := []struct {
		name      string
		env       map[string]string
		files     map[string]bool
		cgroup    string
		parent    string
		parentPID int
		want      bool
	}{
		{name: "docker marker", files: map[string]bool{"/.dockerenv": true}, want: true},
		{name: "podman marker", files: map[string]bool{"/run/.containerenv": true}, want: true},
		{name: "kubernetes environment", env: map[string]string{"KUBERNETES_SERVICE_HOST": "10.0.0.1"}, want: true},
		{name: "lxc environment", env: map[string]string{"container": "lxc"}, want: true},
		{name: "init wrapper", parent: "s6-svscan", want: true},
		{name: "self wrapper", parent: ProjectName, want: true},
		{name: "podman cgroup", cgroup: "0::/libpod-123.scope", want: true},
		{name: "systemd service wins", env: map[string]string{"container": "lxc", "INVOCATION_ID": "abc"}, want: false},
		{name: "openrc system container wins", env: map[string]string{"container": "lxc", "RC_SVCNAME": "api"}, want: false},
		{name: "systemd shell in system container wins", files: map[string]bool{"/dev/lxc": true, "/run/systemd/system": true}, want: false},
		{name: "openrc shell in system container wins", files: map[string]bool{"/dev/lxc": true, "/sbin/openrc-run": true, "/run/openrc": true}, want: false},
		{name: "openrc init process wins", files: map[string]bool{"/dev/lxc": true, "/run/openrc": true}, parentPID: 1, want: false},
		{name: "host", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			getenv := func(key string) string { return tt.env[key] }
			exists := func(path string) bool { return tt.files[path] }
			readFile := func(path string) string {
				if path == "/proc/1/cgroup" {
					return tt.cgroup
				}
				return ""
			}
			assert.Equal(t, tt.want, detectContainerSignals(getenv, exists, readFile, func() string { return tt.parent }, tt.parentPID))
		})
	}
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
	expectedRootPIDPath := filepath.Join("/var/run", OrgName, ProjectName, ProjectName+".pid")
	if runtime.GOOS == "windows" {
		programData := os.Getenv("ProgramData")
		if programData == "" {
			programData = `C:\ProgramData`
		}
		expectedRootPIDPath = filepath.Join(programData, OrgName, ProjectName, ProjectName+".pid")
	}
	assert.Equal(t, expectedRootPIDPath, defaultPIDPath(false, true))
	assert.Equal(t, filepath.Join("/data", ProjectName, ProjectName+".pid"), defaultPIDPath(true, true))

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

func TestGetDatabaseDir_EnvOverride(t *testing.T) {
	t.Setenv("DATABASE_DIR", "/custom/db")
	assert.Equal(t, "/custom/db", GetDatabaseDir("/some/data"))
}

func TestGetDatabaseDir_DefaultsToDataDbSubdir(t *testing.T) {
	t.Setenv("DATABASE_DIR", "")
	if IsRunningInContainer() {
		assert.Equal(t, filepath.Join("/data", "db", "sqlite"), GetDatabaseDir("/some/data"))
		return
	}
	assert.Equal(t, filepath.Join("/some/data", "db"), GetDatabaseDir("/some/data"))
}

func TestDatabaseDirForWindowsSystemMode(t *testing.T) {
	dataDir := filepath.Join("ProgramData", "apimgr", "api", "data")
	assert.Equal(t, filepath.Join("ProgramData", "apimgr", "api", "data", "db"), databaseDirFor(dataDir, "", "windows", false, true))
}

func TestDatabaseDirForContainer(t *testing.T) {
	assert.Equal(t, filepath.Join(string(filepath.Separator), "data", "db", "sqlite"), databaseDirFor("/data/api", "", "linux", true, false))
}

func TestDatabaseDirOverridePrecedesPlatformDefaults(t *testing.T) {
	assert.Equal(t, "/custom/db", databaseDirFor("/data/api", "/custom/db", "windows", true, true))
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
		if err := os.Chmod(roDir, 0o755); err != nil {
			t.Errorf("restore permissions: %v", err)
		}
	})

	target := filepath.Join(roDir, "child")
	assert.False(t, isWritable(target))
}
