package config

import (
	"encoding/base64"
	"os"
	"strings"
	"testing"

	"github.com/apimgr/api/src/paths"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestDNSCredentialCipherRoundTripAndRotationKey(t *testing.T) {
	key := base64.StdEncoding.EncodeToString([]byte("01234567890123456789012345678901"))
	previous := base64.StdEncoding.EncodeToString([]byte("abcdefghijklmnopqrstuvwxyz123456"))
	sealed, err := EncryptDNSCredential("dns-secret", key)
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(sealed, "enc:v1:"))
	assert.NotContains(t, sealed, "dns-secret")
	plain, err := DecryptDNSCredential(sealed, key, "")
	require.NoError(t, err)
	assert.Equal(t, "dns-secret", plain)
	oldCiphertext, err := EncryptDNSCredential("dns-secret", previous)
	require.NoError(t, err)
	plain, err = DecryptDNSCredential(oldCiphertext, key, previous)
	require.NoError(t, err)
	assert.Equal(t, "dns-secret", plain)
	_, err = DecryptDNSCredential(sealed, previous, "")
	assert.Error(t, err)
}

func TestProtectDNSCredentialsEncryptsSecretsAndReturnsPlaintext(t *testing.T) {
	configDir := t.TempDir()
	paths.SetApplicationPathOverrides(configDir, t.TempDir(), t.TempDir())
	key := base64.StdEncoding.EncodeToString([]byte("01234567890123456789012345678901"))
	merged := map[string]interface{}{
		"server": map[string]interface{}{
			"security": map[string]interface{}{"encryption_key": key, "encryption_key_version": 1},
			"ssl": map[string]interface{}{
				"letsencrypt": map[string]interface{}{
					"dns_credentials": map[string]interface{}{"CF_DNS_API_TOKEN": "dns-secret"},
				},
			},
		},
	}
	require.NoError(t, protectDNSCredentials(merged))
	secretBytes, err := os.ReadFile(configDir + "/secrets.yml")
	require.NoError(t, err)
	assert.NotContains(t, string(secretBytes), "dns-secret")
	assert.Contains(t, string(secretBytes), "enc:v1:")
	var secrets map[string]interface{}
	require.NoError(t, yaml.Unmarshal(secretBytes, &secrets))
	stored, ok := getConfigPath(secrets, "server.ssl.letsencrypt.dns_credentials.CF_DNS_API_TOKEN")
	require.True(t, ok)
	assert.True(t, strings.HasPrefix(stored.(string), "enc:v1:"))
	plain, ok := getConfigPath(merged, "server.ssl.letsencrypt.dns_credentials.CF_DNS_API_TOKEN")
	require.True(t, ok)
	assert.Equal(t, "dns-secret", plain)

	require.NoError(t, protectDNSCredentials(merged))
	plain, _ = getConfigPath(merged, "server.ssl.letsencrypt.dns_credentials.CF_DNS_API_TOKEN")
	assert.Equal(t, "dns-secret", plain)
}

func TestRotateEncryptionKeyReencryptsDNSCredentials(t *testing.T) {
	configDir := t.TempDir()
	paths.SetApplicationPathOverrides(configDir, t.TempDir(), t.TempDir())
	oldKey := base64.StdEncoding.EncodeToString([]byte("01234567890123456789012345678901"))
	cfg := defaultConfig()
	cfg.Server.Security.EncryptionKey = oldKey
	cfg.Server.Security.EncryptionKeyVersion = 1
	cfg.Server.SSL.LetsEncrypt.DNSCredentials = map[string]string{"CF_DNS_API_TOKEN": "dns-secret"}
	cfg.Server.Backup.EncryptionPassword = "backup-secret"
	require.NoError(t, Save(cfg))

	loaded, err := Load(nil)
	require.NoError(t, err)
	require.Equal(t, "dns-secret", loaded.Server.SSL.LetsEncrypt.DNSCredentials["CF_DNS_API_TOKEN"])
	oldKey = loaded.Server.Security.EncryptionKey
	require.NoError(t, RotateEncryptionKey(loaded, nil))
	newKey := loaded.Server.Security.EncryptionKey
	require.NotEqual(t, oldKey, newKey)

	secrets, ok := readConfigFile(configDir+"/secrets.yml", false, false)
	require.True(t, ok)
	storedValue, ok := getConfigPath(secrets, "server.ssl.letsencrypt.dns_credentials.CF_DNS_API_TOKEN")
	require.True(t, ok)
	stored, ok := storedValue.(string)
	require.True(t, ok)
	require.True(t, strings.HasPrefix(stored, dnsCredentialCipherPrefix))
	plain, err := DecryptDNSCredential(stored, newKey, "")
	require.NoError(t, err)
	require.Equal(t, "dns-secret", plain)
	_, err = DecryptDNSCredential(stored, oldKey, "")
	require.Error(t, err)
	backupStoredValue, ok := getConfigPath(secrets, "server.backup.encryption_password")
	require.True(t, ok)
	backupStored, ok := backupStoredValue.(string)
	require.True(t, ok)
	backupPlain, err := DecryptAtRestSecret(backupStored, newKey, "")
	require.NoError(t, err)
	require.Equal(t, "backup-secret", backupPlain)
	_, err = DecryptAtRestSecret(backupStored, oldKey, "")
	require.Error(t, err)

	reloaded, err := Load(nil)
	require.NoError(t, err)
	require.Equal(t, "dns-secret", reloaded.Server.SSL.LetsEncrypt.DNSCredentials["CF_DNS_API_TOKEN"])
}
