package sysservice

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"

	"github.com/apimgr/api/src/paths"
)

// runtimeDirMode is the permission applied to every runtime directory that
// the service owns while running elevated.
const runtimeDirMode = 0o755

// RuntimeDirs returns every directory the running service needs to own,
// in creation order. The PID file's parent directory is included because
// the PID file is written after the privilege drop.
func RuntimeDirs() []string {
	dirs := []string{
		paths.ConfigDir(),
		paths.DataDir(),
		paths.CacheDir(),
		paths.LogDir(),
		paths.BackupDir(),
	}

	if pidDir := filepath.Dir(paths.DefaultPIDPath()); pidDir != "" && pidDir != "." {
		dirs = append(dirs, pidDir)
	}

	return dirs
}

// PrepareRuntimeDirs creates every runtime directory the service needs.
// It is safe to call repeatedly and never lowers the permissions of a
// directory an operator has deliberately tightened.
func PrepareRuntimeDirs() error {
	for _, dir := range RuntimeDirs() {
		if dir == "" {
			continue
		}
		if err := os.MkdirAll(dir, runtimeDirMode); err != nil {
			return fmt.Errorf("failed to create %s: %w", dir, err)
		}
	}

	return nil
}

// RootSetup performs the privileged part of startup, per AI.md PART 8's
// startup sequence and PART 23's system user requirements: it creates the
// dedicated service account (home directory first, then the user) and
// every runtime directory the service needs.
//
// It returns the uid/gid the caller must later drop to, after it has
// finished every step that genuinely requires elevation (writing the
// first-run config, binding privileged ports). Ownership is transferred
// separately by ClaimRuntimeDirs so files written while still elevated
// are covered too. A returned uid of 0 means no drop is required - either
// the process is already unprivileged, or the platform (Windows) manages
// the service identity itself.
func RootSetup() (uid, gid int, err error) {
	if !paths.IsElevated() || runtime.GOOS == "windows" {
		if dirErr := PrepareRuntimeDirs(); dirErr != nil {
			return 0, 0, dirErr
		}
		return 0, 0, nil
	}

	// The home directory has to exist before the account is created, so
	// the data directory is created ahead of the rest of the tree.
	if mkErr := os.MkdirAll(paths.DataDir(), runtimeDirMode); mkErr != nil {
		return 0, 0, fmt.Errorf("failed to create %s: %w", paths.DataDir(), mkErr)
	}
	if createErr := CreateServiceUser(paths.DataDir()); createErr != nil {
		return 0, 0, fmt.Errorf("failed to create the %s service account: %w", ServiceUserName, createErr)
	}

	if dirErr := PrepareRuntimeDirs(); dirErr != nil {
		return 0, 0, dirErr
	}

	uid, gid, err = ServiceAccountIDs()
	if err != nil {
		return 0, 0, err
	}

	return uid, gid, nil
}

// ClaimRuntimeDirs hands every runtime directory - and everything already
// inside it, such as a freshly generated server.yml - to the service
// account, so the process can still read and write them once it has
// dropped privileges. It is a no-op when no drop is going to happen.
func ClaimRuntimeDirs(uid, gid int) error {
	if uid <= 0 || gid <= 0 {
		return nil
	}

	for _, dir := range RuntimeDirs() {
		if dir == "" {
			continue
		}
		if err := chownTree(dir, uid, gid); err != nil {
			return err
		}
	}

	return nil
}

// chownTree recursively transfers ownership of a directory to the service
// account. Entries that vanish mid-walk are ignored; anything else is a
// real failure because the process would be unable to write there after
// the privilege drop.
func chownTree(root string, uid, gid int) error {
	return filepath.WalkDir(root, func(path string, _ fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			if os.IsNotExist(walkErr) {
				return nil
			}
			return walkErr
		}
		if err := os.Lchown(path, uid, gid); err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return fmt.Errorf("failed to set ownership of %s to %d:%d: %w", path, uid, gid, err)
		}
		return nil
	})
}
