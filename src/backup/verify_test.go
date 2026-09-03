package backup

import (
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newTestArchive writes one archive from a small source tree and returns its
// path.
func newTestArchive(t *testing.T, password string) string {
	t.Helper()

	source := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(source, "server.yml"), []byte("server:\n  port: 8080\n"), 0600))

	path := filepath.Join(t.TempDir(), FullBackupName(time.Now(), password != ""))
	_, err := CreateWithOptions(path, CreateOptions{
		Sources:    []string{source},
		Password:   password,
		Kind:       KindFull,
		AppVersion: "1.2.3",
	})
	require.NoError(t, err)

	return path
}

// writeTestDatabase creates a small, valid SQLite database on disk.
func writeTestDatabase(t *testing.T, path string) {
	t.Helper()

	db, err := sql.Open("sqlite", path)
	require.NoError(t, err)
	defer db.Close()

	_, err = db.Exec("CREATE TABLE config (key TEXT PRIMARY KEY, value TEXT)")
	require.NoError(t, err)
	_, err = db.Exec("INSERT INTO config (key, value) VALUES (?, ?)", "server.port", "8080")
	require.NoError(t, err)
}

// verificationCheck extracts the failed check name from an error.
func verificationCheck(t *testing.T, err error) string {
	t.Helper()
	var failure *VerificationError
	require.True(t, errors.As(err, &failure), "expected a VerificationError, got %v", err)
	return failure.Check
}

// A freshly created archive passes every check and yields its manifest.
func TestVerifyAcceptsFreshArchive(t *testing.T) {
	path := newTestArchive(t, "")

	manifest, err := Verify(path, "")
	require.NoError(t, err)

	assert.Equal(t, manifestVersion, manifest.Version)
	assert.Equal(t, "1.2.3", manifest.AppVersion)
	assert.Equal(t, KindFull, manifest.Kind)
	assert.False(t, manifest.Encrypted)
	assert.NotEmpty(t, manifest.Checksum)
}

// A missing file fails the file-exists check.
func TestVerifyMissingFile(t *testing.T) {
	_, err := Verify(filepath.Join(t.TempDir(), "api_backup_2026-01-01.tar.gz"), "")
	require.Error(t, err)
	assert.Equal(t, CheckFileExists, verificationCheck(t, err))
}

// An empty file fails the size check before anything else is attempted.
func TestVerifyEmptyFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "api_backup_2026-01-02.tar.gz")
	require.NoError(t, os.WriteFile(path, nil, 0600))

	_, err := Verify(path, "")
	require.Error(t, err)
	assert.Equal(t, CheckSize, verificationCheck(t, err))
}

// Corrupting the compressed payload must not pass verification.
func TestVerifyCorruptedArchive(t *testing.T) {
	path := newTestArchive(t, "")

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	data[len(data)/2] ^= 0xFF
	require.NoError(t, os.WriteFile(path, data, 0600))

	_, err = Verify(path, "")
	require.Error(t, err)
	assert.Contains(t,
		[]string{CheckFormat, CheckExtraction, CheckChecksum, CheckManifest},
		verificationCheck(t, err))
}

// A garbage file that is not a gzip stream fails the format check.
func TestVerifyInvalidFormat(t *testing.T) {
	path := filepath.Join(t.TempDir(), "api_backup_2026-01-03.tar.gz")
	require.NoError(t, os.WriteFile(path, []byte("this is not an archive"), 0600))

	_, err := Verify(path, "")
	require.Error(t, err)
	assert.Equal(t, CheckFormat, verificationCheck(t, err))
}

// An encrypted archive verifies with its password and fails the decrypt check
// without it or with the wrong one.
func TestVerifyEncryptedArchive(t *testing.T) {
	path := newTestArchive(t, "correct-horse")

	manifest, err := Verify(path, "correct-horse")
	require.NoError(t, err)
	assert.True(t, manifest.Encrypted)

	_, err = Verify(path, "")
	require.Error(t, err)
	assert.Equal(t, CheckDecrypt, verificationCheck(t, err))

	_, err = Verify(path, "wrong-password")
	require.Error(t, err)
	assert.Equal(t, CheckDecrypt, verificationCheck(t, err))
}

// A valid SQLite database inside the archive passes the integrity check.
func TestVerifyDatabaseIntegrity(t *testing.T) {
	source := t.TempDir()
	dbPath := filepath.Join(source, "server.db")
	writeTestDatabase(t, dbPath)

	path := filepath.Join(t.TempDir(), FullBackupName(time.Now(), false))
	_, err := CreateWithOptions(path, CreateOptions{
		Sources: []string{source},
		Kind:    KindFull,
	})
	require.NoError(t, err)

	_, err = Verify(path, "")
	require.NoError(t, err)
}

// A file that only pretends to be a SQLite database fails the integrity
// check rather than slipping through.
func TestVerifyRejectsCorruptedDatabase(t *testing.T) {
	source := t.TempDir()
	corrupted := append([]byte("SQLite format 3\x00"), make([]byte, 512)...)
	require.NoError(t, os.WriteFile(filepath.Join(source, "server.db"), corrupted, 0600))

	path := filepath.Join(t.TempDir(), FullBackupName(time.Now(), false))
	_, err := CreateWithOptions(path, CreateOptions{
		Sources: []string{source},
		Kind:    KindFull,
	})
	require.NoError(t, err)

	_, err = Verify(path, "")
	require.Error(t, err)
	assert.Equal(t, CheckDatabaseIntegrity, verificationCheck(t, err))
}
