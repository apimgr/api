package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHasSourceFiles(t *testing.T) {
	configDir, _ := initTestPaths(t)
	assert.False(t, HasSourceFiles())

	require.NoError(t, os.MkdirAll(filepath.Join(configDir, "yaml.d"), 0700))
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "yaml.d", "branding.yml"), []byte("web: {}\n"), 0600))
	assert.True(t, HasSourceFiles())
}
