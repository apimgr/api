//go:build !windows

package database

import (
	"os"
	"os/user"
	"strconv"
	"strings"
)

func resolveAuditGroup(name string, systemMode bool) (int, bool, string) {
	if !systemMode || strings.TrimSpace(name) == "" {
		return 0, false, ""
	}
	group, err := user.LookupGroup(strings.TrimSpace(name))
	if err != nil {
		return 0, false, "configured audit group is unavailable; using owner-only permissions"
	}
	groupID, err := strconv.Atoi(group.Gid)
	if err != nil || groupID < 0 {
		return 0, false, "configured audit group is unusable; using owner-only permissions"
	}
	return groupID, true, ""
}

func setAuditPathPermissions(path string, directory, groupEnabled bool, groupID int) error {
	mode := os.FileMode(0600)
	if directory {
		mode = 0700
	}
	if groupEnabled {
		mode = 0640
		if directory {
			mode = 0750
		}
	}
	if err := os.Chmod(path, mode); err != nil {
		return err
	}
	if groupEnabled {
		return os.Chown(path, -1, groupID)
	}
	return nil
}
