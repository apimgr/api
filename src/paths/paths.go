package paths

import (
	"os"
	"os/user"
	"path/filepath"
	"runtime"
	"strconv"
	"time"
)

const (
	// OrgName is the organization name for directory structure
	OrgName = "apimgr"
	// ProjectName is the project name
	ProjectName = "api"
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

// isElevatedNow reports whether the current process is running with
// root/Administrator privileges at this instant
func isElevatedNow() bool {
	if runtime.GOOS == "windows" {
		return os.Getenv("USERDOMAIN") == os.Getenv("COMPUTERNAME")
	}
	return os.Geteuid() == 0
}

// IsElevated returns whether the process started with elevated privileges,
// captured once at startup before any privilege drop (see startedElevated).
func IsElevated() bool {
	return startedElevated
}

// Init initializes the paths with optional overrides
func Init(config, data, logs string) {
	if config != "" {
		configDir = config
	}
	if data != "" {
		dataDir = data
	}
	if logs != "" {
		logsDir = logs
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

	// Check if running as root/admin
	isRoot := false
	if runtime.GOOS == "windows" {
		isRoot = os.Getenv("USERDOMAIN") == os.Getenv("COMPUTERNAME")
	} else {
		isRoot = os.Geteuid() == 0
	}

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

	isRoot := false
	if runtime.GOOS == "windows" {
		isRoot = os.Getenv("USERDOMAIN") == os.Getenv("COMPUTERNAME")
	} else {
		isRoot = os.Geteuid() == 0
	}

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
	// Check for Docker
	if _, err := os.Stat("/.dockerenv"); err == nil {
		return true
	}
	// Check for common container init systems
	data, err := os.ReadFile("/proc/1/comm")
	if err != nil {
		return false
	}
	comm := string(data)
	return comm == "tini\n" || comm == "tini" || comm == "dumb-init\n"
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
	if IsRunningInContainer() {
		return filepath.Join("/data", ProjectName, ProjectName+".pid")
	}

	if startedElevated {
		switch runtime.GOOS {
		case "windows":
			programData := os.Getenv("ProgramData")
			if programData == "" {
				programData = "C:\\ProgramData"
			}
			return filepath.Join(programData, OrgName, ProjectName, ProjectName+".pid")
		default:
			return filepath.Join("/var/run", OrgName, ProjectName+".pid")
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
	f.Close()
	os.Remove(testFile)
	return true
}
