package config

import (
	"bytes"
	"log"
	"os"
	"path/filepath"
	"testing"

	"github.com/apimgr/api/src/paths"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFindConfigValueLocationTracksYamlLineAndOverrideSource(t *testing.T) {
	configDir := t.TempDir()
	paths.SetApplicationPathOverrides(configDir, t.TempDir(), t.TempDir())
	require.NoError(t, os.MkdirAll(filepath.Join(configDir, "yaml.d"), 0700))
	server := "server:\n  security:\n    allowlist:\n      - cidr: 192.0.2.1/32\n"
	dropin := "server:\n  security:\n    allowlist:\n      - cidr: invalid-range\n"
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "server.yml"), []byte(server), 0600))
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "yaml.d", "security.yml"), []byte(dropin), 0600))

	location := findConfigValueLocation("server.security.allowlist[0].cidr")
	assert.Equal(t, "security.yml", location.file)
	assert.Equal(t, 4, location.line)

	var output bytes.Buffer
	previousOutput := log.Writer()
	log.SetOutput(&output)
	defer log.SetOutput(previousOutput)
	logConfigWarning("server.security.allowlist[0].cidr is invalid; skipping entry")
	assert.Contains(t, output.String(), "security.yml:4: server.security.allowlist[0].cidr")
}
