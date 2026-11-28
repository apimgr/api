package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLoadPerformsOnlyEarlyPeekUntilLoadFull(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "server.yml"), []byte("server:\n  port: \"8123\"\n  address: 127.0.0.1\n  pidfile: false\n  daemonize: true\n  mode: development\n"), 0600))

	early, err := Load([]string{"--config", dir})
	require.NoError(t, err)
	require.Equal(t, "8123", early.Server.Port)
	require.Equal(t, "127.0.0.1", early.Server.Address)
	require.False(t, early.Server.PIDFile)
	require.True(t, early.Server.Daemonize)
	require.Equal(t, "production", early.Server.Mode)
	require.Empty(t, early.Build.Version)

	early.Build = BuildInfo{Version: "1.2.3", CommitID: "abc123", BuildEpoch: "42", OfficialSite: "https://example.test", BuildDate: "date"}
	full, err := LoadFull(early)
	require.NoError(t, err)
	require.Equal(t, "development", full.Server.Mode)
	require.Equal(t, early.Build, full.Build)
	_, err = os.Stat(filepath.Join(dir, "server.yml"))
	require.NoError(t, err)
}

func TestEarlyLoadDoesNotCreateFirstRunConfiguration(t *testing.T) {
	dir := t.TempDir()
	early, err := Load([]string{"--config", dir})
	require.NoError(t, err)
	_, err = os.Stat(filepath.Join(dir, "server.yml"))
	require.ErrorIs(t, err, os.ErrNotExist)

	_, err = LoadFull(early)
	require.NoError(t, err)
	_, err = os.Stat(filepath.Join(dir, "server.yml"))
	require.NoError(t, err)
}
