package backup

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// encrypt/decrypt must round-trip data with the correct password and must
// fail with the wrong one.
func TestEncryptDecryptRoundTrip(t *testing.T) {
	var buf bytes.Buffer
	ew, err := encrypt(&buf, "correct-horse")
	require.NoError(t, err)

	plaintext := []byte("the quick brown fox jumps over the lazy dog")
	n, err := ew.Write(plaintext)
	require.NoError(t, err)
	assert.Equal(t, len(plaintext), n)
	require.NoError(t, ew.Close())

	dr, err := decrypt(bytes.NewReader(buf.Bytes()), "correct-horse")
	require.NoError(t, err)
	got, err := io.ReadAll(dr)
	require.NoError(t, err)
	assert.Equal(t, plaintext, got)

	// Wrong password must fail to decrypt (GCM auth failure).
	dr2, err := decrypt(bytes.NewReader(buf.Bytes()), "wrong-password")
	require.NoError(t, err)
	_, err = io.ReadAll(dr2)
	assert.Error(t, err)
}

// encrypt/decrypt with an empty payload must still round-trip cleanly.
func TestEncryptDecryptEmptyPayload(t *testing.T) {
	var buf bytes.Buffer
	ew, err := encrypt(&buf, "pw")
	require.NoError(t, err)
	require.NoError(t, ew.Close())

	dr, err := decrypt(bytes.NewReader(buf.Bytes()), "pw")
	require.NoError(t, err)
	got, err := io.ReadAll(dr)
	require.NoError(t, err)
	assert.Empty(t, got)
}

// Create followed by Restore (unencrypted) must reproduce the original
// file contents at the same (absolute) path.
func TestCreateRestoreRoundTripUnencrypted(t *testing.T) {
	tmp := t.TempDir()
	srcDir := filepath.Join(tmp, "src")
	require.NoError(t, os.MkdirAll(srcDir, 0755))
	filePath := filepath.Join(srcDir, "hello.txt")
	require.NoError(t, os.WriteFile(filePath, []byte("hello backup"), 0644))

	backupPath := filepath.Join(tmp, "out", "backup-1.tar.gz")
	require.NoError(t, Create(backupPath, []string{srcDir}, ""))

	info, err := os.Stat(backupPath)
	require.NoError(t, err)
	assert.Greater(t, info.Size(), int64(0))

	// Destroy the original so Restore has to recreate it.
	require.NoError(t, os.Remove(filePath))

	require.NoError(t, Restore(backupPath, ""))

	restored, err := os.ReadFile(filePath)
	require.NoError(t, err)
	assert.Equal(t, "hello backup", string(restored))
}

// Create followed by Restore with a password must also round-trip, and
// restoring with the wrong password must fail.
func TestCreateRestoreRoundTripEncrypted(t *testing.T) {
	tmp := t.TempDir()
	srcDir := filepath.Join(tmp, "src")
	require.NoError(t, os.MkdirAll(srcDir, 0755))
	filePath := filepath.Join(srcDir, "secret.txt")
	require.NoError(t, os.WriteFile(filePath, []byte("top secret contents"), 0644))

	backupPath := filepath.Join(tmp, "backup-2.tar.gz")
	require.NoError(t, Create(backupPath, []string{srcDir}, "s3cr3t"))

	require.NoError(t, os.Remove(filePath))

	// Wrong password must not succeed.
	err := Restore(backupPath, "not-the-password")
	assert.Error(t, err)

	// Correct password restores the file.
	require.NoError(t, Restore(backupPath, "s3cr3t"))
	restored, err := os.ReadFile(filePath)
	require.NoError(t, err)
	assert.Equal(t, "top secret contents", string(restored))
}

// Create with a source that does not exist must return an error rather
// than silently producing an empty backup.
func TestCreateMissingSource(t *testing.T) {
	tmp := t.TempDir()
	backupPath := filepath.Join(tmp, "backup-missing.tar.gz")
	err := Create(backupPath, []string{filepath.Join(tmp, "does-not-exist")}, "")
	assert.Error(t, err)

	// The temp file must have been cleaned up, and no final backup left
	// behind on failure.
	_, statErr := os.Stat(backupPath)
	assert.True(t, os.IsNotExist(statErr))
}

// Restore of a nonexistent backup file must return an error.
func TestRestoreMissingFile(t *testing.T) {
	tmp := t.TempDir()
	err := Restore(filepath.Join(tmp, "nope.tar.gz"), "")
	assert.Error(t, err)
}

// Restore of a file that isn't gzip-compressed must fail at the
// decompression stage.
func TestRestoreInvalidGzip(t *testing.T) {
	tmp := t.TempDir()
	badPath := filepath.Join(tmp, "bad.tar.gz")
	require.NoError(t, os.WriteFile(badPath, []byte("not a gzip stream"), 0644))

	err := Restore(badPath, "")
	assert.Error(t, err)
}

// getHostname must return whatever os.Hostname() reports (falling back to
// "unknown" only if that call errors, which is not exercised here).
func TestGetHostname(t *testing.T) {
	want, err := os.Hostname()
	if err != nil {
		want = "unknown"
	}
	assert.Equal(t, want, getHostname())
}

// CleanupOldBackups must keep only the newest keepCount backups and remove
// the rest.
func TestCleanupOldBackups(t *testing.T) {
	tmp := t.TempDir()

	names := []string{"backup-1.tar.gz", "backup-2.tar.gz", "backup-3.tar.gz", "backup-4.tar.gz"}
	base := time.Now().Add(-1 * time.Hour)
	for i, name := range names {
		p := filepath.Join(tmp, name)
		require.NoError(t, os.WriteFile(p, []byte("x"), 0644))
		// Space out modification times so ordering is deterministic:
		// backup-1 is oldest, backup-4 is newest.
		modTime := base.Add(time.Duration(i) * time.Minute)
		require.NoError(t, os.Chtimes(p, modTime, modTime))
	}

	require.NoError(t, CleanupOldBackups(tmp, 2))

	remaining, err := filepath.Glob(filepath.Join(tmp, "backup-*.tar.gz"))
	require.NoError(t, err)
	assert.Len(t, remaining, 2)
	assert.ElementsMatch(t, []string{
		filepath.Join(tmp, "backup-3.tar.gz"),
		filepath.Join(tmp, "backup-4.tar.gz"),
	}, remaining)
}

// CleanupOldBackups must be a no-op when there are fewer backups than
// keepCount.
func TestCleanupOldBackupsNoopWhenUnderLimit(t *testing.T) {
	tmp := t.TempDir()
	p := filepath.Join(tmp, "backup-only.tar.gz")
	require.NoError(t, os.WriteFile(p, []byte("x"), 0644))

	require.NoError(t, CleanupOldBackups(tmp, 5))

	remaining, err := filepath.Glob(filepath.Join(tmp, "backup-*.tar.gz"))
	require.NoError(t, err)
	assert.Len(t, remaining, 1)
}

// CleanupOldBackups on a directory with no matching files must not error.
func TestCleanupOldBackupsEmptyDir(t *testing.T) {
	tmp := t.TempDir()
	assert.NoError(t, CleanupOldBackups(tmp, 3))
}
