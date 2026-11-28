package config

import (
	"encoding/base64"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestBackupPasswordMigratesEncryptedToSecretsAndLoadsInMemory(t *testing.T) {
	configDir, _ := initTestPaths(t)
	require.NoError(t, os.WriteFile(configDir+"/server.yml", []byte("server:\n  backup:\n    encryption_password: legacy-backup-password\n"), 0644))

	loaded, err := Load(nil)
	require.NoError(t, err)
	assert.Equal(t, "legacy-backup-password", loaded.Server.Backup.EncryptionPassword)
	assert.False(t, loaded.Server.Backup.EncryptionPasswordUnreadable)

	secretBytes, err := os.ReadFile(configDir + "/secrets.yml")
	require.NoError(t, err)
	assert.NotContains(t, string(secretBytes), "legacy-backup-password")
	assert.Contains(t, string(secretBytes), "enc:v1:")
	for _, name := range []string{"server.yml", "yaml.d/maintenance.yml"} {
		content, readErr := os.ReadFile(configDir + "/" + name)
		require.NoError(t, readErr)
		assert.NotContains(t, string(content), "legacy-backup-password")
	}
	secretInfo, err := os.Stat(configDir + "/secrets.yml")
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0600), secretInfo.Mode().Perm())

	var secrets map[string]interface{}
	require.NoError(t, yaml.Unmarshal(secretBytes, &secrets))
	storedValue, ok := getConfigPath(secrets, "server.backup.encryption_password")
	require.True(t, ok)
	stored, ok := storedValue.(string)
	require.True(t, ok)
	assert.True(t, strings.HasPrefix(stored, "enc:v1:"))
	plain, err := DecryptAtRestSecret(stored, loaded.Server.Security.EncryptionKey, "")
	require.NoError(t, err)
	assert.Equal(t, "legacy-backup-password", plain)
}

func TestBackupPasswordUnreadableCipherIsPreservedAndMarked(t *testing.T) {
	configDir, _ := initTestPaths(t)
	key := base64.StdEncoding.EncodeToString([]byte("01234567890123456789012345678901"))
	wrongKey := base64.StdEncoding.EncodeToString([]byte("abcdefghijklmnopqrstuvwxyz123456"))
	sealed, err := EncryptAtRestSecret("backup-secret", wrongKey)
	require.NoError(t, err)
	secrets := map[string]interface{}{}
	setConfigPath(secrets, "server.security.encryption_key", key)
	setConfigPath(secrets, "server.security.encryption_key_version", 1)
	setConfigPath(secrets, "server.backup.encryption_password", sealed)
	secretBytes, err := yaml.Marshal(secrets)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(configDir+"/secrets.yml", secretBytes, 0600))

	loaded, err := Load(nil)
	require.NoError(t, err)
	assert.True(t, loaded.Server.Backup.EncryptionPasswordUnreadable)
	assert.Empty(t, loaded.Server.Backup.EncryptionPassword)
	stored, ok := readConfigFile(configDir+"/secrets.yml", false, false)
	require.True(t, ok)
	storedValue, ok := getConfigPath(stored, "server.backup.encryption_password")
	require.True(t, ok)
	assert.Equal(t, sealed, storedValue)
}

func TestPlaintextBackupPasswordInSecretsIsEncryptedAtLoad(t *testing.T) {
	configDir, _ := initTestPaths(t)
	key := base64.StdEncoding.EncodeToString([]byte("01234567890123456789012345678901"))
	secrets := map[string]interface{}{}
	setConfigPath(secrets, "server.security.encryption_key", key)
	setConfigPath(secrets, "server.security.encryption_key_version", 1)
	setConfigPath(secrets, "server.backup.encryption_password", "plaintext-secret-file")
	data, err := yaml.Marshal(secrets)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(configDir+"/secrets.yml", data, 0600))

	loaded, err := Load(nil)
	require.NoError(t, err)
	assert.Equal(t, "plaintext-secret-file", loaded.Server.Backup.EncryptionPassword)
	stored, ok := readConfigFile(configDir+"/secrets.yml", false, false)
	require.True(t, ok)
	storedValue, ok := getConfigPath(stored, "server.backup.encryption_password")
	require.True(t, ok)
	assert.True(t, strings.HasPrefix(storedValue.(string), "enc:v1:"))
}

func TestSaveEncryptsBackupPassword(t *testing.T) {
	configDir, _ := initTestPaths(t)
	cfg := defaultConfig()
	cfg.Server.Security.EncryptionKey = base64.StdEncoding.EncodeToString([]byte("01234567890123456789012345678901"))
	cfg.Server.Backup.EncryptionPassword = "operator-backup-password"
	require.NoError(t, Save(cfg))

	secrets, ok := readConfigFile(configDir+"/secrets.yml", false, false)
	require.True(t, ok)
	storedValue, ok := getConfigPath(secrets, "server.backup.encryption_password")
	require.True(t, ok)
	stored, ok := storedValue.(string)
	require.True(t, ok)
	assert.True(t, strings.HasPrefix(stored, "enc:v1:"))
	assert.NotContains(t, stored, "operator-backup-password")
	plain, err := DecryptAtRestSecret(stored, cfg.Server.Security.EncryptionKey, "")
	require.NoError(t, err)
	assert.Equal(t, "operator-backup-password", plain)

	loaded, err := Load(nil)
	require.NoError(t, err)
	assert.Equal(t, "operator-backup-password", loaded.Server.Backup.EncryptionPassword)
	assert.False(t, loaded.Server.Backup.EncryptionPasswordUnreadable)
}

func TestRotateEncryptionKeyRefusesUnreadableBackupPassword(t *testing.T) {
	initTestPaths(t)
	cfg := defaultConfig()
	cfg.Server.Backup.EncryptionPasswordUnreadable = true
	err := RotateEncryptionKey(cfg, nil)
	assert.ErrorIs(t, err, ErrBackupPasswordUnreadable)
}
