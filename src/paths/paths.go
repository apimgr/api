package paths

import (
	"os"
	"os/user"
	"path/filepath"
	"runtime"
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
)

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

// GetDefaultDirs returns OS-specific default directories based on privileges
func GetDefaultDirs() (configDir, dataDir, logsDir string) {
	// Check if running in container
	if IsRunningInContainer() {
		return "/config", "/data", "/data/logs"
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
			logsDir = filepath.Join(xdgData, OrgName, ProjectName, "logs")
		}
	}

	return configDir, dataDir, logsDir
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

// GetBackupDir returns the default backup directory
func GetBackupDir() string {
	if IsRunningInContainer() {
		return "/data/backups"
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
			return filepath.Join(programData, "Backups", OrgName, ProjectName)
		case "darwin":
			return filepath.Join("/Library/Backups", OrgName, ProjectName)
		case "freebsd", "openbsd", "netbsd":
			return filepath.Join("/var/backups", OrgName, ProjectName)
		default:
			return filepath.Join("/mnt/Backups", OrgName, ProjectName)
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
		return filepath.Join(localAppData, "Backups", OrgName, ProjectName)
	case "darwin":
		return filepath.Join(homeDir, "Library", "Backups", OrgName, ProjectName)
	default:
		return filepath.Join(homeDir, ".local", "backups", OrgName, ProjectName)
	}
}
