package pidfile

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/apimgr/api/src/paths"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCheckPIDFile_NotExist(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nonexistent.pid")

	running, pid, err := CheckPIDFile(path)
	require.NoError(t, err)
	assert.False(t, running)
	assert.Equal(t, 0, pid)
}

func TestCheckPIDFile_CorruptContent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "corrupt.pid")
	require.NoError(t, os.WriteFile(path, []byte("not-a-pid"), 0644))

	running, pid, err := CheckPIDFile(path)
	require.NoError(t, err)
	assert.False(t, running)
	assert.Equal(t, 0, pid)

	// A corrupt PID file must be removed as stale.
	_, statErr := os.Stat(path)
	assert.True(t, os.IsNotExist(statErr))
}

func TestCheckPIDFile_DeadProcess(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "dead.pid")

	// PID 999999 is extremely unlikely to be a running process on any
	// test host.
	require.NoError(t, os.WriteFile(path, []byte("999999"), 0644))

	running, pid, err := CheckPIDFile(path)
	require.NoError(t, err)
	assert.False(t, running)
	assert.Equal(t, 0, pid)

	_, statErr := os.Stat(path)
	assert.True(t, os.IsNotExist(statErr))
}

func TestCheckPIDFile_RunningButNotOurBinary(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "other.pid")

	// The test binary's own PID is running, but its exe name is the Go
	// test binary, not "api" - isOurProcess must reject it as stale.
	require.NoError(t, os.WriteFile(path, []byte(strconv.Itoa(os.Getpid())), 0644))

	running, pid, err := CheckPIDFile(path)
	require.NoError(t, err)
	assert.False(t, running)
	assert.Equal(t, 0, pid)

	_, statErr := os.Stat(path)
	assert.True(t, os.IsNotExist(statErr))
}

func TestCheckPIDFile_WhitespaceTrimmed(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "whitespace.pid")
	require.NoError(t, os.WriteFile(path, []byte("  999999  \n"), 0644))

	running, pid, err := CheckPIDFile(path)
	require.NoError(t, err)
	assert.False(t, running)
	assert.Equal(t, 0, pid)
}

func TestWritePIDFile_SkippedInContainer(t *testing.T) {
	if !paths.IsRunningInContainer() {
		t.Skip("only meaningful inside a container, where WritePIDFile is a no-op")
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "container.pid")

	require.NoError(t, WritePIDFile(path))

	_, err := os.Stat(path)
	assert.True(t, os.IsNotExist(err), "WritePIDFile must not create a file inside a container")
}

func TestWritePIDFile_WritesAndDetectsExisting(t *testing.T) {
	if paths.IsRunningInContainer() {
		t.Skip("WritePIDFile is a no-op inside containers; covered by TestWritePIDFile_SkippedInContainer")
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "app.pid")

	require.NoError(t, WritePIDFile(path))

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, strconv.Itoa(os.Getpid()), string(data))

	// A second WritePIDFile call against a live PID naming our own process
	// exits early because isOurProcess only matches the exact binary name
	// "api", and the test binary is not named "api" - so the stale entry
	// is cleaned up and a fresh write succeeds rather than erroring.
	require.NoError(t, WritePIDFile(path))

	require.NoError(t, RemovePIDFile(path))
	_, statErr := os.Stat(path)
	assert.True(t, os.IsNotExist(statErr))
}

func TestRemovePIDFile_SkippedInContainer(t *testing.T) {
	if !paths.IsRunningInContainer() {
		t.Skip("only meaningful inside a container, where RemovePIDFile is a no-op")
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "container.pid")
	require.NoError(t, os.WriteFile(path, []byte("123"), 0644))

	// RemovePIDFile must be a no-op in a container and must not error even
	// though the file exists.
	assert.NoError(t, RemovePIDFile(path))

	_, err := os.Stat(path)
	assert.NoError(t, err, "file must remain untouched")
}

func TestRemovePIDFile_NonexistentReturnsError(t *testing.T) {
	if paths.IsRunningInContainer() {
		t.Skip("RemovePIDFile is a no-op inside containers")
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "missing.pid")

	err := RemovePIDFile(path)
	assert.Error(t, err)
}

func TestIsProcessRunning_CurrentProcess(t *testing.T) {
	assert.True(t, isProcessRunning(os.Getpid()))
}

func TestIsProcessRunning_DeadPID(t *testing.T) {
	assert.False(t, isProcessRunning(999999))
}

func TestIsOurProcess_TestBinaryDoesNotMatch(t *testing.T) {
	// The compiled test binary is never named exactly "api".
	assert.False(t, isOurProcess(os.Getpid()))
}
