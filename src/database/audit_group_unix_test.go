//go:build !windows

package database

import (
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAuditGroupResolutionAndPermissions(t *testing.T) {
	if _, enabled, warning := resolveAuditGroup("root", false); enabled || warning != "" {
		t.Fatalf("user mode group resolution = (%v, %q), want disabled without warning", enabled, warning)
	}
	if _, enabled, warning := resolveAuditGroup("__apimgr_missing_audit_group__", true); enabled || warning == "" {
		t.Fatalf("unknown group resolution = (%v, %q), want disabled with warning", enabled, warning)
	}

	current, err := user.Current()
	require.NoError(t, err)
	groupID, err := strconv.Atoi(current.Gid)
	require.NoError(t, err)
	group, err := user.LookupGroupId(current.Gid)
	require.NoError(t, err)
	resolvedID, enabled, warning := resolveAuditGroup(group.Name, true)
	require.True(t, enabled)
	require.Empty(t, warning)
	require.Equal(t, groupID, resolvedID)

	dir := t.TempDir()
	file := filepath.Join(dir, "audit.log")
	require.NoError(t, os.WriteFile(file, nil, 0600))
	require.NoError(t, setAuditPathPermissions(dir, true, true, resolvedID))
	require.NoError(t, setAuditPathPermissions(file, false, true, resolvedID))
	dirInfo, err := os.Stat(dir)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0750), dirInfo.Mode().Perm())
	fileInfo, err := os.Stat(file)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0640), fileInfo.Mode().Perm())
}
