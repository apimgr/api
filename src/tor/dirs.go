package tor

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

// ensureTorDirs creates the three Tor directories (config, data, hidden
// service keys) with 0700 permissions, per AI.md PART 31 "Runtime
// Directory Handling". It is idempotent - safe to call on every start,
// enforcing permissions/ownership even if the directories already exist.
func ensureTorDirs(configDir, dataDir string) error {
	dirs := []string{
		filepath.Join(configDir, "tor"),
		filepath.Join(dataDir, "tor"),
		filepath.Join(dataDir, "tor", "site"),
	}

	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0700); err != nil {
			return fmt.Errorf("create tor dir %s: %w", dir, err)
		}
		if err := os.Chmod(dir, 0700); err != nil {
			return fmt.Errorf("chmod tor dir %s: %w", dir, err)
		}
		if err := chownCurrentUser(dir); err != nil {
			return fmt.Errorf("chown tor dir %s: %w", dir, err)
		}
	}

	return nil
}

// ensureTorrc creates torrc only if it doesn't already exist - the file is
// persistent across restarts and is never silently overwritten by a normal
// startup, only by an explicit operator-driven config change (see
// updateTorrc). Returns true if a new file was created.
func ensureTorrc(path string, content []byte) (bool, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return false, fmt.Errorf("create parent dir: %w", err)
	}

	if _, err := os.Stat(path); err == nil {
		// Already exists - preserve content, just re-enforce permissions.
		if err := os.Chmod(path, 0600); err != nil {
			return false, fmt.Errorf("chmod file: %w", err)
		}
		return false, nil
	}

	if err := os.WriteFile(path, content, 0600); err != nil {
		return false, fmt.Errorf("write file: %w", err)
	}

	if err := chownCurrentUser(path); err != nil {
		return false, fmt.Errorf("chown file: %w", err)
	}

	return true, nil
}

// updateTorrc overwrites torrc with new content. Only called when the
// operator explicitly changes Tor settings in server.yml.
func updateTorrc(path string, content []byte) error {
	return ensureTorFile(path, content)
}

// ensureTorFile writes (or overwrites) a Tor-owned file with 0600
// permissions and current-user ownership - used for torrc updates and any
// non-torrc Tor file the server itself manages.
func ensureTorFile(path string, content []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return fmt.Errorf("create parent dir: %w", err)
	}

	if err := os.WriteFile(path, content, 0600); err != nil {
		return fmt.Errorf("write file: %w", err)
	}

	if err := os.Chmod(path, 0600); err != nil {
		return fmt.Errorf("chmod file: %w", err)
	}

	if err := chownCurrentUser(path); err != nil {
		return fmt.Errorf("chown file: %w", err)
	}

	return nil
}

// chownCurrentUser enforces ownership by the current uid/gid. It is a
// no-op on Windows, which has no POSIX chown and instead relies on ACLs
// inherited from the user profile.
func chownCurrentUser(path string) error {
	if runtime.GOOS == "windows" {
		return nil
	}
	return os.Chown(path, os.Getuid(), os.Getgid())
}
