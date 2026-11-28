package paths

import (
	"os"
	"os/user"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const (
	// OrgName is the organization name for directory structure
	OrgName = "apimgr"
	// ProjectName is the project name
	ProjectName = "api"
	// OwnerTokenCookieName is the project-unique cookie and localStorage key
	// recorded in IDEA.md for the anonymous owner credential.
	OwnerTokenCookieName = "api_owner_token_suexgbvs"
)

var (
	configDir string
	dataDir   string
	logsDir   string
	cacheDir  string
	backupDir string
)

// startedElevated captures whether the process began with elevated
// privileges, evaluated once at package init before any privilege drop.
// GetBackupDir's fallback logic must not flip modes mid-run.
var startedElevated = isElevatedNow()

// IsElevated returns whether the process started with elevated privileges,
// captured once at startup before any privilege drop (see startedElevated).
func IsElevated() bool {
	return startedElevated
}

// SetApplicationPathOverrides sets non-empty configuration, data, and log directory overrides.
func SetApplicationPathOverrides(configurationDirectory, dataDirectory, logDirectory string) {
	if configurationDirectory != "" {
		configDir = configurationDirectory
	}
	if dataDirectory != "" {
		dataDir = dataDirectory
	}
	if logDirectory != "" {
		logsDir = logDirectory
	}
}

// ConfigDir returns the configuration directory
func ConfigDir() string {
	if configDir != "" {
		return configDir
	}
	cfg, _, _ := GetDefaultDirs()
	return cfg
}

// DataDir returns the data directory
func DataDir() string {
	if dataDir != "" {
		return dataDir
	}
	_, data, _ := GetDefaultDirs()
	return data
}

// LogDir returns the log directory
func LogDir() string {
	if logsDir != "" {
		return logsDir
	}
	_, _, logs := GetDefaultDirs()
	return logs
}

// GetDatabaseDir returns the configured SQLite directory or the platform
// default. Containers use their dedicated SQLite mount; native systems use
// dataDir/db.
func GetDatabaseDir(dataDir string) string {
	return databaseDirFor(dataDir, os.Getenv("DATABASE_DIR"), runtime.GOOS, IsRunningInContainer(), startedElevated)
}

func databaseDirFor(dataDir, override, operatingSystem string, inContainer, elevated bool) string {
	if override != "" {
		return override
	}
	if inContainer {
		return filepath.Join(string(filepath.Separator), "data", "db", "sqlite")
	}
	return filepath.Join(dataDir, "db")
}

// InitCache sets the cache directory override (from --cache flag)
func InitCache(cache string) {
	if cache != "" {
		cacheDir = cache
	}
}

// InitBackup sets the backup directory override (from --backup flag)
func InitBackup(backup string) {
	if backup != "" {
		backupDir = backup
	}
}

// BackupDir returns the backup directory, honoring the --backup override
func BackupDir() string {
	if backupDir != "" {
		return backupDir
	}
	return GetBackupDir()
}

// CacheDir returns the cache directory
func CacheDir() string {
	if cacheDir != "" {
		return cacheDir
	}
	return GetCacheDir()
}

// GetDefaultDirs returns OS-specific default directories based on privileges
func GetDefaultDirs() (configDir, dataDir, logsDir string) {
	// Check if running in container
	if IsRunningInContainer() {
		return filepath.Join("/config", ProjectName),
			filepath.Join("/data", ProjectName),
			filepath.Join("/data/log", ProjectName)
	}

	// Use the startup privilege state for path selection.
	isRoot := startedElevated

	if isRoot {
		switch runtime.GOOS {
		case "windows":
			programData := os.Getenv("ProgramData")
			if programData == "" {
				programData = "C:\\ProgramData"
			}
			configDir = filepath.Join(programData, OrgName, ProjectName)
			dataDir = filepath.Join(programData, OrgName, ProjectName, "data")
			logsDir = filepath.Join(programData, OrgName, ProjectName, "logs")
		case "darwin":
			// macOS privileged
			configDir = filepath.Join("/Library/Application Support", OrgName, ProjectName)
			dataDir = filepath.Join("/Library/Application Support", OrgName, ProjectName, "data")
			logsDir = filepath.Join("/Library/Logs", OrgName, ProjectName)
		case "freebsd", "openbsd", "netbsd":
			// BSD privileged
			configDir = filepath.Join("/usr/local/etc", OrgName, ProjectName)
			dataDir = filepath.Join("/var/db", OrgName, ProjectName)
			logsDir = filepath.Join("/var/log", OrgName, ProjectName)
		default:
			// Linux privileged
			configDir = filepath.Join("/etc", OrgName, ProjectName)
			dataDir = filepath.Join("/var/lib", OrgName, ProjectName)
			logsDir = filepath.Join("/var/log", OrgName, ProjectName)
		}
	} else {
		var homeDir string
		currentUser, err := user.Current()
		if err == nil {
			homeDir = currentUser.HomeDir
		} else {
			homeDir = os.Getenv("HOME")
			if homeDir == "" {
				homeDir = os.Getenv("USERPROFILE")
			}
		}

		switch runtime.GOOS {
		case "windows":
			appData := os.Getenv("APPDATA")
			if appData == "" {
				appData = filepath.Join(homeDir, "AppData", "Roaming")
			}
			localAppData := os.Getenv("LOCALAPPDATA")
			if localAppData == "" {
				localAppData = filepath.Join(homeDir, "AppData", "Local")
			}
			configDir = filepath.Join(appData, OrgName, ProjectName)
			dataDir = filepath.Join(localAppData, OrgName, ProjectName)
			logsDir = filepath.Join(localAppData, OrgName, ProjectName, "logs")
		case "darwin":
			// macOS user
			configDir = filepath.Join(homeDir, "Library", "Application Support", OrgName, ProjectName)
			dataDir = filepath.Join(homeDir, "Library", "Application Support", OrgName, ProjectName)
			logsDir = filepath.Join(homeDir, "Library", "Logs", OrgName, ProjectName)
		default:
			// Linux, BSD user
			xdgConfig := os.Getenv("XDG_CONFIG_HOME")
			if xdgConfig == "" {
				xdgConfig = filepath.Join(homeDir, ".config")
			}
			xdgData := os.Getenv("XDG_DATA_HOME")
			if xdgData == "" {
				xdgData = filepath.Join(homeDir, ".local", "share")
			}
			configDir = filepath.Join(xdgConfig, OrgName, ProjectName)
			dataDir = filepath.Join(xdgData, OrgName, ProjectName)
			logsDir = filepath.Join(homeDir, ".local", "log", OrgName, ProjectName)
		}
	}

	return configDir, dataDir, logsDir
}

// GetCacheDir returns the OS-specific default cache directory based on
// privileges, per AI.md PART 4 (Cache row of the path tables)
func GetCacheDir() string {
	if IsRunningInContainer() {
		return filepath.Join("/data", ProjectName, "cache")
	}

	isRoot := startedElevated

	if isRoot {
		switch runtime.GOOS {
		case "windows":
			programData := os.Getenv("ProgramData")
			if programData == "" {
				programData = "C:\\ProgramData"
			}
			return filepath.Join(programData, OrgName, ProjectName, "cache")
		case "darwin":
			return filepath.Join("/Library/Caches", OrgName, ProjectName)
		default:
			// Linux and BSD privileged
			return filepath.Join("/var/cache", OrgName, ProjectName)
		}
	}

	var homeDir string
	currentUser, err := user.Current()
	if err == nil {
		homeDir = currentUser.HomeDir
	} else {
		homeDir = os.Getenv("HOME")
		if homeDir == "" {
			homeDir = os.Getenv("USERPROFILE")
		}
	}

	switch runtime.GOOS {
	case "windows":
		localAppData := os.Getenv("LOCALAPPDATA")
		if localAppData == "" {
			localAppData = filepath.Join(homeDir, "AppData", "Local")
		}
		return filepath.Join(localAppData, OrgName, ProjectName, "cache")
	case "darwin":
		return filepath.Join(homeDir, "Library", "Caches", OrgName, ProjectName)
	default:
		// Linux and BSD user
		xdgCache := os.Getenv("XDG_CACHE_HOME")
		if xdgCache == "" {
			xdgCache = filepath.Join(homeDir, ".cache")
		}
		return filepath.Join(xdgCache, OrgName, ProjectName)
	}
}

// EnsureDir creates a directory if it doesn't exist
func EnsureDir(path string) error {
	return os.MkdirAll(path, 0755)
}

// EnsureDirectories creates all required directories
func EnsureDirectories() error {
	cfg, data, logs := GetDefaultDirs()
	for _, dir := range []string{cfg, data, logs} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return err
		}
	}
	return nil
}

// IsRunningInContainer checks if running inside a container
func IsRunningInContainer() bool {
	return detectContainerSignals(os.Getenv, func(path string) bool {
		_, err := os.Stat(path)
		return err == nil
	}, func(path string) string {
		data, err := os.ReadFile(path)
		if err != nil {
			return ""
		}
		return string(data)
	}, func() string {
		return processName(os.Getppid())
	}, os.Getppid())
}

func detectContainerSignals(getenv func(string) string, exists func(string) bool, readFile func(string) string, parentName func() string, parentPID int) bool {
	// A full system container with systemd or OpenRC keeps normal service
	// manager behavior; those signals take priority over its container marker.
	if getenv("INVOCATION_ID") != "" ||
		getenv("RC_SVCNAME") != "" ||
		exists("/run/systemd/system") ||
		(exists("/sbin/openrc-run") && exists("/run/openrc")) ||
		(parentPID == 1 && exists("/run/openrc")) {
		return false
	}
	for _, path := range []string{"/.dockerenv", "/run/.containerenv", "/dev/lxc"} {
		if exists(path) {
			return true
		}
	}
	if getenv("container") != "" || getenv("KUBERNETES_SERVICE_HOST") != "" {
		return true
	}
	parent := strings.TrimSpace(parentName())
	switch parent {
	case "tini", "dumb-init", "s6-svscan", "runsv", "runsvdir", "catatonit":
		return true
	}
	if strings.EqualFold(parent, ProjectName) {
		return true
	}
	for _, path := range []string{"/proc/1/cgroup", "/proc/self/cgroup"} {
		content := strings.ToLower(readFile(path))
		for _, marker := range []string{"docker", "kubepods", "lxc", "libpod", "podman", "containerd", "cri-o", "incus"} {
			if strings.Contains(content, marker) {
				return true
			}
		}
	}
	return false
}

func processName(pid int) string {
	if pid <= 0 {
		return ""
	}
	data, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "comm"))
	if err != nil {
		return ""
	}
	return string(data)
}

// GetBackupDir returns the default backup directory. It prefers the
// system-level backup path if writable; otherwise it falls back per
// AI.md PART 8: system mode (started elevated) falls back inside the
// data dir (never a $HOME-derived path, since service accounts may have
// HOME pointed at the data dir); user mode falls back to the user
// backup dir.
func GetBackupDir() string {
	if IsRunningInContainer() {
		return filepath.Join("/data/backups", ProjectName)
	}

	sysBackup := systemBackupDir()
	if isWritable(sysBackup) {
		return sysBackup
	}

	if startedElevated {
		return filepath.Join(DataDir(), "backup")
	}

	return userBackupDir()
}

// systemBackupDir returns the system-level backup directory
func systemBackupDir() string {
	switch runtime.GOOS {
	case "windows":
		programData := os.Getenv("ProgramData")
		if programData == "" {
			programData = "C:\\ProgramData"
		}
		return filepath.Join(programData, "Backups", OrgName, ProjectName)
	case "darwin":
		return filepath.Join("/Library/Backups", OrgName, ProjectName)
	case "freebsd", "openbsd", "netbsd":
		return filepath.Join("/var/backups", OrgName, ProjectName)
	default:
		return filepath.Join("/mnt/Backups", OrgName, ProjectName)
	}
}

// userBackupDir returns the user-level backup directory
func userBackupDir() string {
	var homeDir string
	currentUser, err := user.Current()
	if err == nil {
		homeDir = currentUser.HomeDir
	} else {
		homeDir = os.Getenv("HOME")
		if homeDir == "" {
			homeDir = os.Getenv("USERPROFILE")
		}
	}

	switch runtime.GOOS {
	case "windows":
		localAppData := os.Getenv("LOCALAPPDATA")
		if localAppData == "" {
			localAppData = filepath.Join(homeDir, "AppData", "Local")
		}
		return filepath.Join(localAppData, "Backups", OrgName, ProjectName)
	case "darwin":
		return filepath.Join(homeDir, "Library", "Backups", OrgName, ProjectName)
	default:
		return filepath.Join(homeDir, ".local", "share", "Backups", OrgName, ProjectName)
	}
}

// DefaultPIDPath returns the OS-specific default PID file path, per AI.md
// PART 8's Directory Flags table (root vs. user rows). Containers use the
// data directory since the PID file is skipped entirely at write time
// anyway (see IsRunningInContainer callers in pidfile.WritePIDFile).
func DefaultPIDPath() string {
	return defaultPIDPath(IsRunningInContainer(), startedElevated)
}

func defaultPIDPath(inContainer, isElevated bool) string {
	if inContainer {
		return filepath.Join("/data", ProjectName, ProjectName+".pid")
	}

	if isElevated {
		switch runtime.GOOS {
		case "windows":
			programData := os.Getenv("ProgramData")
			if programData == "" {
				programData = "C:\\ProgramData"
			}
			return filepath.Join(programData, OrgName, ProjectName, ProjectName+".pid")
		default:
			return filepath.Join("/var/run", OrgName, ProjectName, ProjectName+".pid")
		}
	}

	var homeDir string
	currentUser, err := user.Current()
	if err == nil {
		homeDir = currentUser.HomeDir
	} else {
		homeDir = os.Getenv("HOME")
		if homeDir == "" {
			homeDir = os.Getenv("USERPROFILE")
		}
	}

	switch runtime.GOOS {
	case "windows":
		localAppData := os.Getenv("LOCALAPPDATA")
		if localAppData == "" {
			localAppData = filepath.Join(homeDir, "AppData", "Local")
		}
		return filepath.Join(localAppData, OrgName, ProjectName, ProjectName+".pid")
	default:
		return filepath.Join(homeDir, ".local", "share", OrgName, ProjectName, ProjectName+".pid")
	}
}

// isWritable checks if a directory is writable, creating its parent
// chain if needed to test
func isWritable(path string) bool {
	if err := os.MkdirAll(path, 0755); err != nil {
		return false
	}
	testFile := filepath.Join(path, ".write_test_"+strconv.FormatInt(time.Now().UnixNano(), 36))
	f, err := os.Create(testFile)
	if err != nil {
		return false
	}
	_ = f.Close()
	_ = os.Remove(testFile)
	return true
}
