package backup

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeTarEntry(t *testing.T, name string, mode int64, data []byte) string {
	t.Helper()
	archivePath := filepath.Join(t.TempDir(), "fixture.tar.gz")
	file, err := os.Create(archivePath)
	require.NoError(t, err)
	gz := gzip.NewWriter(file)
	tarWriter := tar.NewWriter(gz)
	require.NoError(t, tarWriter.WriteHeader(&tar.Header{Name: name, Mode: mode, Size: int64(len(data)), Typeflag: tar.TypeReg}))
	_, err = tarWriter.Write(data)
	require.NoError(t, err)
	require.NoError(t, tarWriter.Close())
	require.NoError(t, gz.Close())
	require.NoError(t, file.Close())
	return archivePath
}

func TestCreatedArchivesUseRelativeSourcePaths(t *testing.T) {
	root := filepath.Join(t.TempDir(), "source-root")
	require.NoError(t, os.MkdirAll(filepath.Join(root, "nested"), 0700))
	require.NoError(t, os.WriteFile(filepath.Join(root, "nested", "hello.txt"), []byte("hello"), 0600))
	archivePath := filepath.Join(t.TempDir(), "backup.tar.gz")
	require.NoError(t, Create(archivePath, []string{root}, ""))
	file, err := os.Open(archivePath)
	require.NoError(t, err)
	gz, err := gzip.NewReader(file)
	require.NoError(t, err)
	tarReader := tar.NewReader(gz)
	for {
		header, err := tarReader.Next()
		require.NoError(t, err)
		if header.Name == legacyMetadataName {
			continue
		}
		assert.Equal(t, "source-root/nested/hello.txt", header.Name)
		assert.False(t, filepath.IsAbs(header.Name))
		break
	}
	require.NoError(t, gz.Close())
	require.NoError(t, file.Close())
}

func TestCreateReplacesLiveSQLiteSourceWithSnapshot(t *testing.T) {
	root := t.TempDir()
	liveDB := filepath.Join(root, "server.db")
	snapshotDB := filepath.Join(root, "snapshot.db")
	require.NoError(t, os.WriteFile(liveDB, []byte("live"), 0600))
	require.NoError(t, os.WriteFile(snapshotDB, []byte("snapshot"), 0600))
	archivePath := filepath.Join(t.TempDir(), "backup.tar.gz")
	_, err := CreateWithOptions(archivePath, CreateOptions{
		Sources:            []string{liveDB},
		SQLiteSourcePath:   liveDB,
		SQLiteSnapshotPath: snapshotDB,
	})
	require.NoError(t, err)

	file, err := os.Open(archivePath)
	require.NoError(t, err)
	gz, err := gzip.NewReader(file)
	require.NoError(t, err)
	tarReader := tar.NewReader(gz)
	found := false
	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		require.NoError(t, err)
		if header.Name != "server.db" {
			continue
		}
		contents, err := io.ReadAll(tarReader)
		require.NoError(t, err)
		assert.Equal(t, []byte("snapshot"), contents)
		found = true
	}
	require.True(t, found)
	require.NoError(t, gz.Close())
	require.NoError(t, file.Close())
}

func TestCreateRejectsDistinctFilesWithTheSameArchiveName(t *testing.T) {
	root := t.TempDir()
	first := filepath.Join(root, "first", "same.txt")
	second := filepath.Join(root, "second", "same.txt")
	require.NoError(t, os.MkdirAll(filepath.Dir(first), 0700))
	require.NoError(t, os.MkdirAll(filepath.Dir(second), 0700))
	require.NoError(t, os.WriteFile(first, []byte("first"), 0600))
	require.NoError(t, os.WriteFile(second, []byte("second"), 0600))
	_, err := CreateWithOptions(filepath.Join(t.TempDir(), "backup.tar.gz"), CreateOptions{Sources: []string{first, second}})
	require.ErrorContains(t, err, "duplicate backup archive path: same.txt")
}

func TestRestoreRejectsAbsolutePathsAndUnsafeModes(t *testing.T) {
	for _, fixture := range []struct {
		name string
		mode int64
	}{
		{name: "absolute-entry", mode: 0600},
		{name: "setuid-entry", mode: 04755},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			archiveName := "payload/secret.txt"
			if fixture.name == "absolute-entry" {
				archiveName = "/outside/secret.txt"
			}
			archivePath := writeTarEntry(t, archiveName, fixture.mode, []byte("secret"))
			destination := filepath.Join(t.TempDir(), "payload")
			err := Restore(archivePath, "", destination)
			assert.Error(t, err)
			assert.NoDirExists(t, destination)
		})
	}
}

func TestPrepareRestoreDestinationResolvesTrustedRootSymlink(t *testing.T) {
	tmp := t.TempDir()
	realRoot := filepath.Join(tmp, "real-data")
	linkRoot := filepath.Join(tmp, "data")
	require.NoError(t, os.Mkdir(realRoot, 0700))
	if err := os.Symlink(realRoot, linkRoot); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	destination, err := prepareRestoreDestination("data/nested/file.txt", []string{linkRoot})
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(realRoot, "nested", "file.txt"), destination)
}

func TestRestoreLimitsRejectRatioAndLeaveNoPartialOutput(t *testing.T) {
	archivePath := writeTarEntry(t, "payload/repeated.txt", 0600, bytes.Repeat([]byte("z"), 64*1024))
	destination := filepath.Join(t.TempDir(), "payload")
	err := Restore(archivePath, "", destination)
	assert.Error(t, err)
	assert.NoDirExists(t, destination)
}

func TestRestoreFailurePreservesExistingTargets(t *testing.T) {
	var buffer bytes.Buffer
	gz := gzip.NewWriter(&buffer)
	tarWriter := tar.NewWriter(gz)
	for _, body := range []string{"replacement", "duplicate"} {
		require.NoError(t, tarWriter.WriteHeader(&tar.Header{Name: "payload/keep.txt", Mode: 0600, Size: int64(len(body)), Typeflag: tar.TypeReg}))
		_, err := tarWriter.Write([]byte(body))
		require.NoError(t, err)
	}
	require.NoError(t, tarWriter.Close())
	require.NoError(t, gz.Close())
	archivePath := filepath.Join(t.TempDir(), "duplicate.tar.gz")
	require.NoError(t, os.WriteFile(archivePath, buffer.Bytes(), 0600))
	destination := filepath.Join(t.TempDir(), "payload")
	require.NoError(t, os.MkdirAll(destination, 0700))
	keepPath := filepath.Join(destination, "keep.txt")
	require.NoError(t, os.WriteFile(keepPath, []byte("original"), 0600))

	err := Restore(archivePath, "", destination)
	require.Error(t, err)
	contents, readErr := os.ReadFile(keepPath)
	require.NoError(t, readErr)
	assert.Equal(t, "original", string(contents))
}

func TestRestoreLimitsNormalizeEachField(t *testing.T) {
	limits := NormalizeRestoreLimits(RestoreLimits{MaxFiles: 3, MaxRatio: 20})
	assert.Equal(t, int64(1<<30), limits.MaxArchiveSize)
	assert.Equal(t, int64(4<<30), limits.MaxExpandedSize)
	assert.Equal(t, 3, limits.MaxFiles)
	assert.Equal(t, int64(512<<20), limits.MaxFileSize)
	assert.Equal(t, int64(20), limits.MaxRatio)
}

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

	require.NoError(t, Restore(backupPath, "", srcDir))

	restored, err := os.ReadFile(filePath)
	require.NoError(t, err)
	assert.Equal(t, "hello backup", string(restored))
	info, err = os.Stat(filePath)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0600), info.Mode().Perm())
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
	err := Restore(backupPath, "not-the-password", srcDir)
	assert.Error(t, err)

	// Correct password restores the file.
	require.NoError(t, Restore(backupPath, "s3cr3t", srcDir))
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
	err := Restore(filepath.Join(tmp, "nope.tar.gz"), "", tmp)
	assert.Error(t, err)
}

// Restore of a file that isn't gzip-compressed must fail at the
// decompression stage.
func TestRestoreInvalidGzip(t *testing.T) {
	tmp := t.TempDir()
	badPath := filepath.Join(tmp, "bad.tar.gz")
	require.NoError(t, os.WriteFile(badPath, []byte("not a gzip stream"), 0644))

	err := Restore(badPath, "", tmp)
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
