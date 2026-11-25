package main

import (
	"context"
	"encoding/base64"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/apimgr/api/src/config"
	"github.com/apimgr/api/src/database"
	"github.com/apimgr/api/src/paths"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestListenTCPAcceptsBracketedIPv6Wildcard(t *testing.T) {
	listener, address, err := listenTCP("[::]", "0")
	require.NoError(t, err)
	require.NotNil(t, listener)
	t.Cleanup(func() { require.NoError(t, listener.Close()) })
	require.Contains(t, []string{"::", "0.0.0.0"}, address)
	_, ok := listener.Addr().(*net.TCPAddr)
	require.True(t, ok)
}

func TestResumeEncryptionKeyRotationRecoversBeforeConfigSwitch(t *testing.T) {
	configDir := t.TempDir()
	dataDir := t.TempDir()
	paths.Init(configDir, dataDir, t.TempDir())
	t.Setenv("DATABASE_DIR", filepath.Join(dataDir, "db"))
	require.NoError(t, database.Init(config.DatabaseConfig{Driver: "sqlite"}, dataDir))
	t.Cleanup(func() {
		require.NoError(t, database.Close())
	})

	ctx := context.Background()
	oldKey := []byte("0123456789abcdef0123456789abcdef")
	newKey := []byte("abcdef0123456789abcdef0123456789")
	oldKeyEncoded := base64.StdEncoding.EncodeToString(oldKey)
	newKeyEncoded := base64.StdEncoding.EncodeToString(newKey)
	credential, err := config.EncryptDNSCredential("dns-api-secret", oldKeyEncoded)
	require.NoError(t, err)

	storedConfig := &config.Config{}
	storedConfig.Server.Security.EncryptionKey = oldKeyEncoded
	storedConfig.Server.Security.EncryptionKeyVersion = 1
	storedConfig.Server.SSL.LetsEncrypt.DNSCredentials = map[string]string{
		"CF_DNS_API_TOKEN": credential,
	}
	require.NoError(t, config.Save(storedConfig))

	until := time.Now().UTC().Add(time.Hour)
	require.NoError(t, database.StorePendingEncryptionKey(ctx, oldKey, newKey, until))
	require.NoError(t, database.StorePreviousEncryptionKey(ctx, oldKey, newKey, until))

	loadedConfig, err := config.Load()
	require.NoError(t, err)
	require.Equal(t, oldKeyEncoded, loadedConfig.Server.Security.EncryptionKey)
	require.Equal(t, "dns-api-secret", loadedConfig.Server.SSL.LetsEncrypt.DNSCredentials["CF_DNS_API_TOKEN"])

	require.NoError(t, resumeEncryptionKeyRotation(ctx, loadedConfig))
	require.Equal(t, newKeyEncoded, loadedConfig.Server.Security.EncryptionKey)
	require.Equal(t, "dns-api-secret", loadedConfig.Server.SSL.LetsEncrypt.DNSCredentials["CF_DNS_API_TOKEN"])

	reloadedConfig, err := config.Load()
	require.NoError(t, err)
	require.Equal(t, newKeyEncoded, reloadedConfig.Server.Security.EncryptionKey)
	require.Equal(t, "dns-api-secret", reloadedConfig.Server.SSL.LetsEncrypt.DNSCredentials["CF_DNS_API_TOKEN"])

	secretsFile, err := os.ReadFile(filepath.Join(configDir, "secrets.yml"))
	require.NoError(t, err)
	var secrets struct {
		Server struct {
			SSL struct {
				LetsEncrypt struct {
					DNSCredentials map[string]string `yaml:"dns_credentials"`
				} `yaml:"letsencrypt"`
			} `yaml:"ssl"`
		} `yaml:"server"`
	}
	require.NoError(t, yaml.Unmarshal(secretsFile, &secrets))
	sealedCredential := secrets.Server.SSL.LetsEncrypt.DNSCredentials["CF_DNS_API_TOKEN"]
	require.Contains(t, sealedCredential, "enc:v1:")
	plain, err := config.DecryptDNSCredential(sealedCredential, newKeyEncoded, "")
	require.NoError(t, err)
	require.Equal(t, "dns-api-secret", plain)
	_, err = config.DecryptDNSCredential(sealedCredential, oldKeyEncoded, "")
	require.Error(t, err)

	retainedPrevious, err := database.LoadPreviousEncryptionKey(ctx, newKey, time.Now().UTC())
	require.NoError(t, err)
	require.Equal(t, oldKey, retainedPrevious)
	_, _, err = database.LoadPendingEncryptionKey(ctx, oldKey, time.Now().UTC())
	require.ErrorIs(t, err, database.ErrSecretNotFound)
}
