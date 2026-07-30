package tor

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// TestEnsureTorDirs verifies all three Tor directories are created with
// 0700 permissions, per PART 31 "Runtime Directory Handling".
func TestEnsureTorDirs(t *testing.T) {
	configDir := t.TempDir()
	dataDir := t.TempDir()

	if err := ensureTorDirs(configDir, dataDir); err != nil {
		t.Fatalf("ensureTorDirs failed: %v", err)
	}

	dirs := []string{
		filepath.Join(configDir, "tor"),
		filepath.Join(dataDir, "tor"),
		filepath.Join(dataDir, "tor", "site"),
	}

	for _, dir := range dirs {
		info, err := os.Stat(dir)
		if err != nil {
			t.Fatalf("expected dir %s to exist: %v", dir, err)
		}
		if !info.IsDir() {
			t.Fatalf("expected %s to be a directory", dir)
		}
		if runtime.GOOS != "windows" {
			if perm := info.Mode().Perm(); perm != 0700 {
				t.Errorf("expected %s to have 0700 permissions, got %o", dir, perm)
			}
		}
	}
}

// TestEnsureTorDirsIdempotent verifies calling ensureTorDirs twice does not
// error and re-enforces permissions on already-existing directories.
func TestEnsureTorDirsIdempotent(t *testing.T) {
	configDir := t.TempDir()
	dataDir := t.TempDir()

	if err := ensureTorDirs(configDir, dataDir); err != nil {
		t.Fatalf("first ensureTorDirs failed: %v", err)
	}
	if err := ensureTorDirs(configDir, dataDir); err != nil {
		t.Fatalf("second ensureTorDirs failed: %v", err)
	}
}

// TestEnsureTorrcCreatesFile verifies a new torrc is created with 0600
// permissions and the given content when none exists yet.
func TestEnsureTorrcCreatesFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tor", "torrc")
	content := []byte("# test torrc\n")

	created, err := ensureTorrc(path, content)
	if err != nil {
		t.Fatalf("ensureTorrc failed: %v", err)
	}
	if !created {
		t.Error("expected created to be true for a new file")
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read written torrc: %v", err)
	}
	if string(got) != string(content) {
		t.Errorf("torrc content = %q, want %q", got, content)
	}

	if runtime.GOOS != "windows" {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("failed to stat torrc: %v", err)
		}
		if perm := info.Mode().Perm(); perm != 0600 {
			t.Errorf("expected torrc to have 0600 permissions, got %o", perm)
		}
	}
}

// TestEnsureTorrcPreservesExisting verifies an already-existing torrc is
// never silently overwritten by a normal startup call.
func TestEnsureTorrcPreservesExisting(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tor", "torrc")

	original := []byte("# original operator content\n")
	if _, err := ensureTorrc(path, original); err != nil {
		t.Fatalf("initial ensureTorrc failed: %v", err)
	}

	created, err := ensureTorrc(path, []byte("# different content\n"))
	if err != nil {
		t.Fatalf("second ensureTorrc failed: %v", err)
	}
	if created {
		t.Error("expected created to be false when torrc already exists")
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read torrc: %v", err)
	}
	if string(got) != string(original) {
		t.Errorf("expected original content to be preserved, got %q", got)
	}
}

// TestUpdateTorrcOverwrites verifies updateTorrc (used only for explicit
// operator config changes) does overwrite existing content.
func TestUpdateTorrcOverwrites(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tor", "torrc")

	if _, err := ensureTorrc(path, []byte("# original\n")); err != nil {
		t.Fatalf("initial ensureTorrc failed: %v", err)
	}

	updated := []byte("# updated content\n")
	if err := updateTorrc(path, updated); err != nil {
		t.Fatalf("updateTorrc failed: %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read torrc: %v", err)
	}
	if string(got) != string(updated) {
		t.Errorf("expected updated content, got %q", got)
	}
}

// TestEnsureTorFile verifies ensureTorFile creates parent dirs and writes
// content with 0600 permissions.
func TestEnsureTorFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tor", "site", "hs_ed25519_secret_key")
	content := []byte("fake-key-blob")

	if err := ensureTorFile(path, content); err != nil {
		t.Fatalf("ensureTorFile failed: %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read file: %v", err)
	}
	if string(got) != string(content) {
		t.Errorf("file content = %q, want %q", got, content)
	}

	if runtime.GOOS != "windows" {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("failed to stat file: %v", err)
		}
		if perm := info.Mode().Perm(); perm != 0600 {
			t.Errorf("expected 0600 permissions, got %o", perm)
		}
	}
}
