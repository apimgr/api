package config

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/apimgr/api/src/paths"
)

// HasSourceFiles reports whether an operator configuration source already exists.
func HasSourceFiles() bool {
	configDir := paths.ConfigDir()
	for _, name := range []string{"server.yml", "secrets.yml"} {
		if _, err := os.Stat(filepath.Join(configDir, name)); err == nil {
			return true
		} else if !os.IsNotExist(err) {
			return true
		}
	}
	dropDir := filepath.Join(configDir, "yaml.d")
	entries, err := os.ReadDir(dropDir)
	if err != nil {
		return !os.IsNotExist(err)
	}
	for _, entry := range entries {
		name := entry.Name()
		ext := strings.ToLower(filepath.Ext(name))
		if entry.IsDir() || strings.HasPrefix(name, ".") || strings.HasSuffix(name, ".bak") || strings.HasSuffix(name, "~") || strings.HasSuffix(name, ".orig") || (ext != ".yml" && ext != ".yaml") {
			continue
		}
		return true
	}
	return false
}
