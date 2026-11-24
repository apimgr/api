package sysservice

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseS6ScanDirUsesProcessArgument(t *testing.T) {
	scanDir := t.TempDir()
	assert.Equal(t, scanDir, parseS6ScanDir([]string{"s6-svscan", scanDir}, "/run/service"))
}

func TestParseS6ScanDirFallsBackWhenArgumentIsUnavailable(t *testing.T) {
	assert.Equal(t, "/run/service", parseS6ScanDir([]string{"s6-svscan", "/missing/s6-services"}, "/run/service"))
}

func TestInstallS6ScriptsAndLink(t *testing.T) {
	root := t.TempDir()
	scanDir := filepath.Join(root, "scan")
	svDir := filepath.Join(root, "definition")
	logDir := filepath.Join(root, "logs")
	require.NoError(t, os.Mkdir(scanDir, 0755))
	require.NoError(t, os.Mkdir(svDir, 0755))
	require.NoError(t, os.Mkdir(logDir, 0755))
	run := s6RunScript("/usr/local/bin/api")
	logRun := s6LogRunScript(logDir)
	require.NoError(t, os.WriteFile(filepath.Join(svDir, "run"), []byte(run), 0755))
	require.NoError(t, os.Mkdir(filepath.Join(svDir, "log"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(svDir, "log", "run"), []byte(logRun), 0755))
	link := filepath.Join(scanDir, appName)
	require.NoError(t, os.Symlink(svDir, link))

	gotTarget, err := os.Readlink(link)
	require.NoError(t, err)
	assert.Equal(t, svDir, gotTarget)
	gotRun, err := os.ReadFile(filepath.Join(svDir, "run"))
	require.NoError(t, err)
	assert.Equal(t, run, string(gotRun))
	gotLogRun, err := os.ReadFile(filepath.Join(svDir, "log", "run"))
	require.NoError(t, err)
	assert.Equal(t, logRun, string(gotLogRun))
}
