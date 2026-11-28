package backup

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/apimgr/api/src/config"
	"github.com/stretchr/testify/require"
)

func TestSourcesWithConfigRequiresServerYAML(t *testing.T) {
	_, _, err := SourcesWithConfig(&config.ServerConfig{}, t.TempDir(), t.TempDir(), false, false)
	require.ErrorContains(t, err, "required backup source server.yml")
}

func TestManifestUsesCanonicalSortedChecksumField(t *testing.T) {
	files := []ManifestFile{
		{Path: "z/file", SHA256: "bbbb"},
		{Path: "a/file", SHA256: "aaaa"},
	}
	first := ManifestSHA256(files)
	second := ManifestSHA256([]ManifestFile{files[1], files[0]})
	require.Equal(t, first, second)
	require.Len(t, first, 64)
	encoded, err := json.Marshal(Manifest{SHA256: first})
	require.NoError(t, err)
	require.Contains(t, string(encoded), `"sha256":"`+first+`"`)
	require.NotContains(t, string(encoded), `"checksum"`)
}

func TestManifestFileUsesUnixMTimeAndBareContentHash(t *testing.T) {
	encoded, err := json.Marshal(ManifestFile{Path: "server.yml", Size: 3, Modified: time.Unix(1736937000, 0).UTC(), SHA256: "abcdef"})
	require.NoError(t, err)
	require.JSONEq(t, `{"path":"server.yml","size":3,"mtime":1736937000,"sha256":"abcdef"}`, string(encoded))
}

func TestRedactBackupSecretsOnlyBlanksRequiredCredentials(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secrets.yml")
	content := []byte("server:\n  token: operator-secret\n  backup:\n    encryption_password: archive-secret\n    other: preserve-me\nother:\n  value: keep\n")
	require.NoError(t, os.WriteFile(path, content, 0600))
	require.NoError(t, redactBackupSecrets(path))
	redacted, err := os.ReadFile(path)
	require.NoError(t, err)
	require.NotContains(t, string(redacted), "operator-secret")
	require.NotContains(t, string(redacted), "archive-secret")
	require.Contains(t, string(redacted), "other: preserve-me")
	require.Contains(t, string(redacted), "value: keep")
	info, err := os.Stat(path)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0600), info.Mode().Perm())
}

func TestSourcesAndRestoreRootsIncludeSecurityKeyMaterial(t *testing.T) {
	configDir := t.TempDir()
	dataDir := filepath.Join(t.TempDir(), "data")
	securityDir := filepath.Join(configDir, "security")
	require.NoError(t, os.MkdirAll(securityDir, 0700))
	require.NoError(t, os.WriteFile(filepath.Join(securityDir, "pgp.pub.asc"), []byte("public"), 0600))
	require.NoError(t, os.WriteFile(filepath.Join(securityDir, "pgp.priv.asc.enc"), []byte("encrypted"), 0600))
	require.NoError(t, os.WriteFile(filepath.Join(securityDir, "keyservers.state"), []byte("{}"), 0600))

	require.Contains(t, Sources(configDir, dataDir, false, false), securityDir)
	require.Contains(t, RestoreRoots(configDir, dataDir, false, false), securityDir)
}

func TestSourcesWithConfigIncludesOnlyReferencedTLSFiles(t *testing.T) {
	configDir := t.TempDir()
	dataDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "server.yml"), []byte("server: {}\n"), 0600))
	sslDir := filepath.Join(configDir, "ssl")
	require.NoError(t, os.MkdirAll(sslDir, 0700))
	cacheCA := filepath.Join(sslDir, "cache-ca.pem")
	require.NoError(t, os.WriteFile(cacheCA, []byte("ca"), 0600))
	externalCert := filepath.Join(t.TempDir(), "manual.pem")
	require.NoError(t, os.WriteFile(externalCert, []byte("cert"), 0644))
	externalKey := filepath.Join(t.TempDir(), "manual.key")
	require.NoError(t, os.WriteFile(externalKey, []byte("key"), 0600))
	cfg := &config.ServerConfig{}
	cfg.Server.Cache.TLSCA = "cache-ca.pem"
	cfg.Server.SSL.Cert = externalCert
	cfg.Server.SSL.Key = externalKey

	sources, supplemental, err := SourcesWithConfig(cfg, configDir, dataDir, false, false)
	require.NoError(t, err)
	require.NotContains(t, sources, sslDir)
	require.Len(t, supplemental, 3)
	require.Contains(t, supplemental, SupplementalSource{Path: cacheCA, ArchivePath: "ssl/cache-ca.pem"})
	require.Contains(t, supplemental, SupplementalSource{Path: externalCert, ArchivePath: "ssl-external/cert.pem"})
	require.Contains(t, supplemental, SupplementalSource{Path: externalKey, ArchivePath: "ssl-external/key.pem"})
}

func TestSourcesWithConfigExcludesRemoteDatabaseAndCache(t *testing.T) {
	configDir := t.TempDir()
	dataDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "server.yml"), []byte("server: {}\n"), 0600))
	databaseDir := filepath.Join(dataDir, "db")
	require.NoError(t, os.MkdirAll(databaseDir, 0700))
	serverDB := filepath.Join(databaseDir, "server.db")
	cacheDB := filepath.Join(databaseDir, "cache.db")
	require.NoError(t, os.WriteFile(serverDB, []byte("restored seed"), 0600))
	require.NoError(t, os.WriteFile(cacheDB, []byte("local cache"), 0600))
	cfg := &config.ServerConfig{}
	cfg.Server.Database.Driver = "turso"
	cfg.Server.Database.Cache = config.DatabaseCacheConfig{Enabled: true, Path: cacheDB}

	sources, supplemental, err := SourcesWithConfig(cfg, configDir, dataDir, false, false)
	require.NoError(t, err)
	require.NotContains(t, sources, serverDB)
	require.NotContains(t, supplemental, SupplementalSource{Path: cacheDB, ArchivePath: "server.db"})
	require.Empty(t, supplemental)
}

func TestRestoreRewritesExternalTLSPathsAndProtectsKeyMode(t *testing.T) {
	root := t.TempDir()
	configDir := filepath.Join(root, "restored-config")
	snapshot := filepath.Join(root, "snapshot")
	require.NoError(t, os.MkdirAll(snapshot, 0700))
	serverConfig := filepath.Join(snapshot, "server.yml")
	require.NoError(t, os.WriteFile(serverConfig, []byte("server:\n  ssl:\n    cert: /old/cert.pem\n    key: /old/key.pem\n"), 0600))
	cert := filepath.Join(snapshot, "cert.pem")
	require.NoError(t, os.WriteFile(cert, []byte("certificate"), 0644))
	key := filepath.Join(snapshot, "key.pem")
	require.NoError(t, os.WriteFile(key, []byte("private key"), 0644))
	archive := filepath.Join(root, "external-tls.tar.gz")
	_, err := CreateWithOptions(archive, CreateOptions{
		Kind: KindFull,
		SnapshotFiles: []SnapshotFile{
			{ArchivePath: "server.yml", Path: serverConfig},
			{ArchivePath: "ssl-external/cert.pem", Path: cert},
			{ArchivePath: "ssl-external/key.pem", Path: key},
		},
	})
	require.NoError(t, err)

	sslDir := filepath.Join(configDir, "ssl")
	require.NoError(t, RestoreWithLimits(archive, "", DefaultRestoreLimits(), filepath.Join(configDir, "server.yml"), sslDir))
	certPath := filepath.Join(sslDir, "cert.pem")
	keyPath := filepath.Join(sslDir, "key.pem")
	certContents, err := os.ReadFile(certPath)
	require.NoError(t, err)
	require.Equal(t, "certificate", string(certContents))
	keyInfo, err := os.Stat(keyPath)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0600), keyInfo.Mode().Perm())
	configContents, err := os.ReadFile(filepath.Join(configDir, "server.yml"))
	require.NoError(t, err)
	require.Contains(t, string(configContents), "cert: "+certPath)
	require.Contains(t, string(configContents), "key: "+keyPath)
}
