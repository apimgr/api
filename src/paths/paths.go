package paths

import (
	"os"
	"path/filepath"
)

var (
	configDir string
	dataDir   string
)

// Init initializes the paths with optional overrides
func Init(config, data string) {
	if config != "" {
		configDir = config
	}
	if data != "" {
		dataDir = data
	}
}

// ConfigDir returns the configuration directory
func ConfigDir() string {
	if configDir != "" {
		return configDir
	}

	// Check if running as root
	if os.Geteuid() == 0 {
		return "/etc/apimgr/api"
	}

	// User directory
	home, err := os.UserHomeDir()
	if err != nil {
		return "./config"
	}

	return filepath.Join(home, ".config", "apimgr", "api")
}

// DataDir returns the data directory
func DataDir() string {
	if dataDir != "" {
		return dataDir
	}

	// Check if running as root
	if os.Geteuid() == 0 {
		return "/var/lib/apimgr/api"
	}

	// User directory
	home, err := os.UserHomeDir()
	if err != nil {
		return "./data"
	}

	return filepath.Join(home, ".local", "share", "apimgr", "api")
}

// LogDir returns the log directory
func LogDir() string {
	// Check if running as root
	if os.Geteuid() == 0 {
		return "/var/log/apimgr/api"
	}

	// User directory
	return filepath.Join(DataDir(), "logs")
}
