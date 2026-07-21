// Package paths resolves the user-scope directories used by the api-cli
// client (config, data, cache, logs). Unlike the server, the client always
// operates in the invoking user's home/profile directories, even when run
// as root/Administrator, per AI.md PART 32.
package paths

import (
	"os"
	"path/filepath"
	"runtime"
)

const (
	// OrgName is the organization name for directory structure.
	OrgName = "apimgr"
	// ProjectName is the project name.
	ProjectName = "api"
)

// ConfigDir returns the CLI config directory.
func ConfigDir() string {
	if runtime.GOOS == "windows" {
		return filepath.Join(os.Getenv("APPDATA"), OrgName, ProjectName)
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", OrgName, ProjectName)
}

// DataDir returns the CLI data directory.
func DataDir() string {
	if runtime.GOOS == "windows" {
		return filepath.Join(os.Getenv("LOCALAPPDATA"), OrgName, ProjectName, "data")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "share", OrgName, ProjectName)
}

// CacheDir returns the CLI cache directory.
func CacheDir() string {
	if runtime.GOOS == "windows" {
		return filepath.Join(os.Getenv("LOCALAPPDATA"), OrgName, ProjectName, "cache")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".cache", OrgName, ProjectName)
}

// LogDir returns the CLI log directory.
func LogDir() string {
	if runtime.GOOS == "windows" {
		return filepath.Join(os.Getenv("LOCALAPPDATA"), OrgName, ProjectName, "log")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "log", OrgName, ProjectName)
}

// ConfigFile returns the default CLI config file path.
func ConfigFile() string {
	return filepath.Join(ConfigDir(), "cli.yml")
}

// NamedConfigFile returns the config file path for a named profile, for
// example "dev" resolves to dev.yml in the same config directory.
func NamedConfigFile(name string) string {
	if name == "" || name == "cli" {
		return ConfigFile()
	}
	return filepath.Join(ConfigDir(), name+".yml")
}

// LogFile returns the CLI log file path.
func LogFile() string {
	return filepath.Join(LogDir(), "cli.log")
}

// EnsureDirs creates the config/data/cache/log directories with user-only
// permissions. Called on every startup before any file operations.
func EnsureDirs() error {
	dirs := []string{ConfigDir(), DataDir(), CacheDir(), LogDir()}
	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return err
		}
		if runtime.GOOS != "windows" {
			if err := os.Chmod(dir, 0o700); err != nil {
				return err
			}
		}
	}
	return nil
}
