//go:build windows

package database

import (
	"os"
	"strings"
)

func resolveAuditGroup(name string, systemMode bool) (int, bool, string) {
	if systemMode && strings.TrimSpace(name) != "" {
		return 0, false, "audit group permissions are unavailable on Windows; using owner-only permissions"
	}
	return 0, false, ""
}

func setAuditPathPermissions(path string, directory, groupEnabled bool, groupID int) error {
	mode := os.FileMode(0600)
	if directory {
		mode = 0700
	}
	return os.Chmod(path, mode)
}
