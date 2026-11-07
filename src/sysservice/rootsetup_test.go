package sysservice

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/apimgr/api/src/paths"
)

// RuntimeDirs must cover every directory the running service writes to,
// including the PID file's parent, which is written after the drop.
func TestRuntimeDirsCoversEveryWritableLocation(t *testing.T) {
	dirs := RuntimeDirs()

	for _, want := range []string{
		paths.ConfigDir(),
		paths.DataDir(),
		paths.CacheDir(),
		paths.LogDir(),
		paths.BackupDir(),
		filepath.Dir(paths.DefaultPIDPath()),
	} {
		assert.Contains(t, dirs, want)
	}
}

// ClaimRuntimeDirs must be a no-op when no privilege drop is going to
// happen, so an unprivileged start never tries to chown anything.
func TestClaimRuntimeDirsNoOpWithoutTarget(t *testing.T) {
	assert.NoError(t, ClaimRuntimeDirs(0, 0))
	assert.NoError(t, ClaimRuntimeDirs(-1, -1))
}

// chownTree must walk the whole tree and must not fail when re-applying
// the ownership the caller already holds.
func TestChownTreeIsRecursive(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX ownership is Unix-only")
	}

	root := t.TempDir()
	nested := filepath.Join(root, "a", "b")
	require.NoError(t, os.MkdirAll(nested, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(nested, "file.txt"), []byte("x\n"), 0o644))

	assert.NoError(t, chownTree(root, os.Getuid(), os.Getgid()))
}

// chownTree must ignore a tree that disappeared rather than aborting
// startup over a directory that is no longer there.
func TestChownTreeIgnoresMissingTree(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX ownership is Unix-only")
	}

	missing := filepath.Join(t.TempDir(), "gone")
	assert.NoError(t, chownTree(missing, os.Getuid(), os.Getgid()))
}

// RootSetup must not create a system account or ask for a privilege drop
// when the process is already unprivileged (AI.md PART 24 Run Mode table:
// user mode never drops).
func TestRootSetupUnprivilegedRequiresNoDrop(t *testing.T) {
	if paths.IsElevated() {
		t.Skip("this test covers the unprivileged path")
	}

	uid, gid, err := RootSetup()
	require.NoError(t, err)
	assert.Equal(t, 0, uid)
	assert.Equal(t, 0, gid)
}
