package backup

import (
	"context"
	"database/sql"
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/apimgr/api/src/config"
	"github.com/apimgr/api/src/database"
	"github.com/apimgr/api/src/paths"
	"github.com/apimgr/api/src/pgp"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

func TestVerifyRejectsBackupWithIncompletePGPKeyPair(t *testing.T) {
	root := t.TempDir()
	securityDir := filepath.Join(root, "config", "security")
	require.NoError(t, os.MkdirAll(securityDir, 0700))
	require.NoError(t, os.WriteFile(filepath.Join(securityDir, "pgp.pub.asc"), []byte("public key"), 0600))
	archive := filepath.Join(root, FullBackupName(time.Now().UTC(), false))
	_, err := CreateWithOptions(archive, CreateOptions{Sources: []string{filepath.Join(root, "config")}, Kind: KindFull})
	require.NoError(t, err)
	_, err = Verify(archive, "")
	var verifyErr *VerificationError
	require.ErrorAs(t, err, &verifyErr)
	require.Equal(t, CheckPGPKeyPair, verifyErr.Check)
}

func TestBackupRestorePreservesPGPKeyPairAndInstallationSecret(t *testing.T) {
	root := t.TempDir()
	configDir := filepath.Join(root, "config")
	dataDir := filepath.Join(root, "data")
	require.NoError(t, os.MkdirAll(configDir, 0700))
	paths.SetApplicationPathOverrides(configDir, dataDir, filepath.Join(root, "cache"))
	require.NoError(t, database.InitDatabase(config.DatabaseConfig{Driver: "sqlite"}, dataDir))
	t.Cleanup(func() { require.NoError(t, database.Close()) })
	ctx := context.Background()
	require.NoError(t, database.EnsureAppSecrets(ctx))
	secret, err := database.LoadSecret(ctx, database.SecretInstallationSecret)
	require.NoError(t, err)
	cfg := &config.ServerConfig{}
	cfg.Server.Branding.Title = "API"
	cfg.Server.Contact.Security.Email = "security@example.invalid"
	_, err = pgp.Generate(ctx, cfg)
	require.NoError(t, err)

	databasePath := filepath.Join(paths.GetDatabaseDir(dataDir), "server.db")
	require.NoError(t, database.Close())
	archive := filepath.Join(root, FullBackupName(time.Now().UTC(), false))
	_, err = CreateWithOptions(archive, CreateOptions{
		Sources: []string{databasePath, configDir},
		Kind:    KindFull,
	})
	require.NoError(t, err)
	_, err = Verify(archive, "")
	require.NoError(t, err)

	restoredConfig := filepath.Join(root, "restored", "config")
	restoredDatabase := filepath.Join(root, "restored", "server.db")
	require.NoError(t, RestoreWithLimits(archive, "", DefaultRestoreLimits(), restoredConfig, restoredDatabase))
	publicKey, err := os.ReadFile(filepath.Join(restoredConfig, "security", "pgp.pub.asc"))
	require.NoError(t, err)
	privateKey, err := os.ReadFile(filepath.Join(restoredConfig, "security", "pgp.priv.asc.enc"))
	require.NoError(t, err)
	state, err := os.ReadFile(filepath.Join(restoredConfig, "security", "keyservers.state"))
	require.NoError(t, err)
	require.NotEmpty(t, state)
	require.NoError(t, pgp.VerifyEncryptedKeyPair(privateKey, secret.Value, publicKey))
	restoredDB, err := sql.Open("sqlite", "file:"+filepath.ToSlash(restoredDatabase)+"?mode=ro")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, restoredDB.Close()) })
	var encodedSecret string
	require.NoError(t, restoredDB.QueryRow(`SELECT value FROM app_secrets WHERE name = ?`, database.SecretInstallationSecret).Scan(&encodedSecret))
	require.Equal(t, secret.Value, mustDecodeBackupSecret(t, encodedSecret))
}

func mustDecodeBackupSecret(t *testing.T, encoded string) []byte {
	t.Helper()
	decoded, err := base64.StdEncoding.DecodeString(encoded)
	require.NoError(t, err)
	return decoded
}
