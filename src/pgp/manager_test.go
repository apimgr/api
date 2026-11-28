package pgp

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ProtonMail/go-crypto/openpgp"
	"github.com/apimgr/api/src/config"
	"github.com/apimgr/api/src/database"
	"github.com/apimgr/api/src/paths"
	servicecrypto "github.com/apimgr/api/src/service/crypto"
	"github.com/stretchr/testify/require"
)

func initPGPTestEnvironment(t *testing.T) context.Context {
	t.Helper()
	configDir, dataDir := t.TempDir(), t.TempDir()
	paths.SetApplicationPathOverrides(configDir, dataDir, t.TempDir())
	t.Setenv("DATABASE_DIR", filepath.Join(dataDir, "db"))
	require.NoError(t, database.InitDatabase(config.DatabaseConfig{Driver: "sqlite"}, dataDir))
	t.Cleanup(func() {
		require.NoError(t, database.Close())
	})
	ctx := context.Background()
	require.NoError(t, database.EnsureAppSecrets(ctx))
	return ctx
}

func TestRotateReencryptsStoredReportsAndPreservesActiveKeyOnFailure(t *testing.T) {
	t.Run("retained PGP reports decrypt with the replacement key", func(t *testing.T) {
		ctx := initPGPTestEnvironment(t)
		cfg := &config.ServerConfig{}
		cfg.Server.Branding.Title = "API"
		cfg.Server.Contact.Security.Email = "security@example.invalid"

		oldMetadata, err := Generate(ctx, cfg)
		require.NoError(t, err)
		oldPublic, err := ReadPublic()
		require.NoError(t, err)
		oldPrivate, err := ReadPrivate(ctx)
		require.NoError(t, err)

		const reportText = "stored vulnerability report"
		body, err := servicecrypto.PGPEncrypt(reportText, string(oldPublic))
		require.NoError(t, err)
		report := database.SecurityReportInput{
			TrackingID: "sec_0123456789abcdef",
			IDWindow:   time.Now().UTC().Unix() / (48 * 60 * 60),
			Severity:   "high",
			Component:  "api",
			Body:       []byte(body),
			BodyAlg:    "pgp",
			Contact:    []byte("encrypted-contact"),
			CreditPref: "anon",
			TokenHash:  strings.Repeat("a", 64),
		}
		require.NoError(t, database.StoreSecurityReport(ctx, report))

		newMetadata, err := Rotate(ctx, cfg)
		require.NoError(t, err)
		require.NotEqual(t, oldMetadata.Fingerprint, newMetadata.Fingerprint)
		require.NotNil(t, newMetadata.LastRotatedAt)
		activePublic, err := ReadPublic()
		require.NoError(t, err)
		activeFingerprint, err := servicecrypto.PGPKeyFingerprint(string(activePublic))
		require.NoError(t, err)
		require.Equal(t, newMetadata.Fingerprint, activeFingerprint)

		activePrivate, err := ReadPrivate(ctx)
		require.NoError(t, err)
		storedBody, algorithm, err := database.GetSecurityReportEncryptedBody(ctx, report.TrackingID)
		require.NoError(t, err)
		require.Equal(t, "pgp", algorithm)
		decrypted, err := servicecrypto.PGPDecrypt(string(storedBody), activePrivate)
		require.NoError(t, err)
		require.Equal(t, reportText, decrypted)
		_, err = servicecrypto.PGPDecrypt(string(storedBody), oldPrivate)
		require.Error(t, err)

		_, _, oldPrivatePath, statePath := KeyPaths()
		retiredCiphertext, err := os.ReadFile(oldPrivatePath)
		require.NoError(t, err)
		secret, err := database.LoadSecret(ctx, database.SecretInstallationSecret)
		require.NoError(t, err)
		retiredPrivate, err := openPrivateKey(retiredCiphertext, secret.Value)
		require.NoError(t, err)
		retiredFingerprint, err := servicecrypto.PGPKeyFingerprint(retiredPrivate)
		require.NoError(t, err)
		require.Equal(t, oldMetadata.Fingerprint, retiredFingerprint)
		state, err := loadState()
		require.NoError(t, err)
		require.False(t, state.OldUntil.IsZero())
		_, err = os.Stat(statePath)
		require.NoError(t, err)
	})

	t.Run("a report that cannot be decrypted leaves the active key untouched", func(t *testing.T) {
		ctx := initPGPTestEnvironment(t)
		cfg := &config.ServerConfig{}
		cfg.Server.Branding.Title = "API"
		cfg.Server.Contact.Security.Email = "security@example.invalid"
		metadata, err := Generate(ctx, cfg)
		require.NoError(t, err)
		beforePublic, err := ReadPublic()
		require.NoError(t, err)
		_, privatePath, oldPrivatePath, _ := KeyPaths()
		beforePrivate, err := os.ReadFile(privatePath)
		require.NoError(t, err)

		report := database.SecurityReportInput{
			TrackingID: "sec_fedcba9876543210",
			IDWindow:   time.Now().UTC().Unix() / (48 * 60 * 60),
			Severity:   "high",
			Component:  "api",
			Body:       []byte("not a valid PGP message"),
			BodyAlg:    "pgp",
			Contact:    []byte("encrypted-contact"),
			CreditPref: "anon",
			TokenHash:  strings.Repeat("b", 64),
		}
		require.NoError(t, database.StoreSecurityReport(ctx, report))
		_, err = Rotate(ctx, cfg)
		require.Error(t, err)

		afterMetadata, err := database.GetSecurityPGPMetadata(ctx)
		require.NoError(t, err)
		require.Equal(t, metadata.Fingerprint, afterMetadata.Fingerprint)
		afterPublic, err := ReadPublic()
		require.NoError(t, err)
		require.Equal(t, beforePublic, afterPublic)
		afterPrivate, err := os.ReadFile(privatePath)
		require.NoError(t, err)
		require.Equal(t, beforePrivate, afterPrivate)
		_, err = os.Stat(oldPrivatePath)
		require.ErrorIs(t, err, os.ErrNotExist)
		storedBody, algorithm, err := database.GetSecurityReportEncryptedBody(ctx, report.TrackingID)
		require.NoError(t, err)
		require.Equal(t, "pgp", algorithm)
		require.Equal(t, report.Body, storedBody)
	})
}

func TestRecoverRotationDiscardsUncommittedStage(t *testing.T) {
	ctx := initPGPTestEnvironment(t)
	cfg := &config.ServerConfig{}
	cfg.Server.Branding.Title = "API"
	cfg.Server.Contact.Security.Email = "security@example.invalid"
	oldMetadata, err := Generate(ctx, cfg)
	require.NoError(t, err)
	oldPublic, err := ReadPublic()
	require.NoError(t, err)
	oldFingerprint, err := servicecrypto.PGPKeyFingerprint(string(oldPublic))
	require.NoError(t, err)
	newMetadata, stagePrivate, stagePublic, stageState, err := prepareStagedRotation(ctx, cfg)
	require.NoError(t, err)
	require.NotEqual(t, oldMetadata.Fingerprint, newMetadata.Fingerprint)

	require.NoError(t, RecoverRotation(ctx, cfg))
	activePublic, err := ReadPublic()
	require.NoError(t, err)
	activeFingerprint, err := servicecrypto.PGPKeyFingerprint(string(activePublic))
	require.NoError(t, err)
	require.Equal(t, oldFingerprint, activeFingerprint)
	for _, path := range []string{stagePrivate, stagePublic, stageState} {
		_, err := os.Stat(path)
		require.ErrorIs(t, err, os.ErrNotExist)
	}
}

func TestRecoverRotationCompletesCommittedStageFromRecipientKeyID(t *testing.T) {
	ctx := initPGPTestEnvironment(t)
	cfg := &config.ServerConfig{}
	cfg.Server.Branding.Title = "API"
	cfg.Server.Contact.Security.Email = "security@example.invalid"
	oldMetadata, err := Generate(ctx, cfg)
	require.NoError(t, err)
	oldPublic, err := ReadPublic()
	require.NoError(t, err)
	oldPrivate, err := ReadPrivate(ctx)
	require.NoError(t, err)
	newMetadata, stagePrivate, stagePublic, stageState, err := prepareStagedRotation(ctx, cfg)
	require.NoError(t, err)
	newPublicBytes, err := os.ReadFile(stagePublic)
	require.NoError(t, err)

	body, err := servicecrypto.PGPEncrypt("rotation recovery report", string(oldPublic))
	require.NoError(t, err)
	require.NoError(t, database.StoreSecurityReport(ctx, database.SecurityReportInput{
		TrackingID: "sec_0123456789abcdef",
		IDWindow:   time.Now().UTC().Unix() / (48 * 60 * 60),
		Severity:   "high",
		Component:  "api",
		Body:       []byte(body),
		BodyAlg:    "pgp",
		Contact:    []byte("contact"),
		CreditPref: "anon",
		TokenHash:  strings.Repeat("b", 64),
	}))
	newPublic := string(newPublicBytes)
	require.NoError(t, database.RotatePGPSecurityReports(ctx, newMetadata, func(encrypted []byte) ([]byte, error) {
		plaintext, decryptErr := servicecrypto.PGPDecrypt(string(encrypted), oldPrivate)
		if decryptErr != nil {
			return nil, decryptErr
		}
		rotated, encryptErr := servicecrypto.PGPEncrypt(plaintext, newPublic)
		return []byte(rotated), encryptErr
	}, func() error { return nil }, func() error { return nil }))
	_, _, oldPrivatePath, _ := KeyPaths()
	require.NoError(t, RecoverRotation(ctx, cfg))

	activePublic, err := ReadPublic()
	require.NoError(t, err)
	activeFingerprint, err := servicecrypto.PGPKeyFingerprint(string(activePublic))
	require.NoError(t, err)
	require.Equal(t, newMetadata.Fingerprint, activeFingerprint)
	activePrivate, err := ReadPrivate(ctx)
	require.NoError(t, err)
	storedBody, algorithm, err := database.GetSecurityReportEncryptedBody(ctx, "sec_0123456789abcdef")
	require.NoError(t, err)
	require.Equal(t, "pgp", algorithm)
	decrypted, err := servicecrypto.PGPDecrypt(string(storedBody), activePrivate)
	require.NoError(t, err)
	require.Equal(t, "rotation recovery report", decrypted)
	retiredCiphertext, err := os.ReadFile(oldPrivatePath)
	require.NoError(t, err)
	secret, err := database.LoadSecret(ctx, database.SecretInstallationSecret)
	require.NoError(t, err)
	retiredPrivate, err := openPrivateKey(retiredCiphertext, secret.Value)
	require.NoError(t, err)
	retiredFingerprint, err := servicecrypto.PGPKeyFingerprint(retiredPrivate)
	require.NoError(t, err)
	require.Equal(t, oldMetadata.Fingerprint, retiredFingerprint)
	for _, path := range []string{stagePrivate, stagePublic, stageState} {
		_, err := os.Stat(path)
		require.ErrorIs(t, err, os.ErrNotExist)
	}
}

func TestRecoverRotationUsesMetadataWhenNoPGPReportsExist(t *testing.T) {
	ctx := initPGPTestEnvironment(t)
	cfg := &config.ServerConfig{}
	cfg.Server.Branding.Title = "API"
	cfg.Server.Contact.Security.Email = "security@example.invalid"
	_, err := Generate(ctx, cfg)
	require.NoError(t, err)
	newMetadata, stagePrivate, stagePublic, stageState, err := prepareStagedRotation(ctx, cfg)
	require.NoError(t, err)
	require.NoError(t, database.RotatePGPSecurityReports(ctx, newMetadata, func(body []byte) ([]byte, error) {
		return body, nil
	}, func() error { return nil }, func() error { return nil }))

	require.NoError(t, RecoverRotation(ctx, cfg))
	activePublic, err := ReadPublic()
	require.NoError(t, err)
	fingerprint, err := servicecrypto.PGPKeyFingerprint(string(activePublic))
	require.NoError(t, err)
	require.Equal(t, newMetadata.Fingerprint, fingerprint)
	for _, path := range []string{stagePrivate, stagePublic, stageState} {
		_, err := os.Stat(path)
		require.ErrorIs(t, err, os.ErrNotExist)
	}
}

func prepareStagedRotation(ctx context.Context, cfg *config.ServerConfig) (database.SecurityPGPMetadata, string, string, string, error) {
	oldPrivate, err := ReadPrivate(ctx)
	if err != nil {
		return database.SecurityPGPMetadata{}, "", "", "", err
	}
	secret, err := database.LoadSecret(ctx, database.SecretInstallationSecret)
	if err != nil {
		return database.SecurityPGPMetadata{}, "", "", "", err
	}
	name, email := securityIdentity(cfg)
	created := time.Now().UTC().Truncate(time.Second)
	publicKey, privateKey, fingerprint, err := servicecrypto.GenerateSecurityPGPKeys(name, email, created)
	if err != nil {
		return database.SecurityPGPMetadata{}, "", "", "", err
	}
	signature, err := signRotation(oldPrivate, publicKey)
	if err != nil {
		return database.SecurityPGPMetadata{}, "", "", "", err
	}
	publicKey, err = servicecrypto.CrossSignPGPPublicKey(publicKey, oldPrivate)
	if err != nil {
		return database.SecurityPGPMetadata{}, "", "", "", err
	}
	sealed, err := sealPrivateKey(privateKey, secret.Value)
	if err != nil {
		return database.SecurityPGPMetadata{}, "", "", "", err
	}
	metadata := database.SecurityPGPMetadata{Fingerprint: fingerprint, CreatedAt: created, ExpiresAt: created.Add(keyLifetime), LastRotatedAt: &created, KeyserversPublished: map[string]string{}}
	publicPath, privatePath, _, statePath := KeyPaths()
	publicStage, privateStage, stateStage := publicPath+stagedSuffix, privatePath+stagedSuffix, statePath+stagedSuffix
	err = stageRotationFiles(publicStage, privateStage, stateStage, []byte(publicKey), sealed, fingerprint, signature, created.Add(oldKeyLifetime))
	return metadata, privateStage, publicStage, stateStage, err
}

func TestSignRotationUsesRetiredKeyToSignReplacementPublicKey(t *testing.T) {
	created := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	oldPublic, oldPrivate, _, err := servicecrypto.GenerateSecurityPGPKeys("API Security", "security@example.invalid", created)
	require.NoError(t, err)
	newPublic, _, _, err := servicecrypto.GenerateSecurityPGPKeys("API Security", "security@example.invalid", created.Add(time.Minute))
	require.NoError(t, err)
	signature, err := signRotation(oldPrivate, newPublic)
	require.NoError(t, err)
	keyring, err := openpgp.ReadArmoredKeyRing(strings.NewReader(oldPublic))
	require.NoError(t, err)
	_, err = openpgp.CheckArmoredDetachedSignature(keyring, strings.NewReader(newPublic), strings.NewReader(signature), nil)
	require.NoError(t, err)
}

func TestInstallKeyMaterialRestoresFilesWhenMetadataSaveFails(t *testing.T) {
	dir := t.TempDir()
	privatePath := filepath.Join(dir, "private")
	publicPath := filepath.Join(dir, "public")
	statePath := filepath.Join(dir, "state")
	require.NoError(t, os.WriteFile(privatePath, []byte("old-private"), 0600))
	require.NoError(t, os.WriteFile(publicPath, []byte("old-public"), 0644))
	require.NoError(t, os.WriteFile(statePath, []byte("old-state"), 0600))
	wantErr := errors.New("injected metadata failure")
	err := installKeyMaterial(privatePath, publicPath, statePath, []byte("new-private"), []byte("new-public"), true,
		persistedState{Version: 1, Keys: map[string]map[string]keyserverEntry{}}, database.SecurityPGPMetadata{},
		atomicWriteFile, func(persistedState) error { return nil }, func(database.SecurityPGPMetadata) error { return wantErr })
	require.ErrorIs(t, err, wantErr)
	private, readErr := os.ReadFile(privatePath)
	require.NoError(t, readErr)
	require.Equal(t, "old-private", string(private))
	public, readErr := os.ReadFile(publicPath)
	require.NoError(t, readErr)
	require.Equal(t, "old-public", string(public))
	state, readErr := os.ReadFile(statePath)
	require.NoError(t, readErr)
	require.Equal(t, "old-state", string(state))
}

func TestDeleteKeyMaterialRollsBackPartialRemoval(t *testing.T) {
	dir := t.TempDir()
	filePaths := []string{filepath.Join(dir, "public"), filepath.Join(dir, "private"), filepath.Join(dir, "retired"), filepath.Join(dir, "state")}
	for i, path := range filePaths {
		require.NoError(t, os.WriteFile(path, []byte{byte('a' + i)}, 0600))
	}
	cfg := &config.ServerConfig{}
	cfg.Web.Security.PublishPGPKey = true
	originalMetadata := database.SecurityPGPMetadata{Fingerprint: strings.Repeat("a", 40), Revoked: false}
	metadataWrites := []bool{}
	configWrites := []bool{}
	removeCalls := 0
	wantErr := errors.New("injected remove failure")
	err := deleteKeyMaterial(cfg, originalMetadata, filePaths,
		func(value *config.ServerConfig) error {
			configWrites = append(configWrites, value.Web.Security.PublishPGPKey)
			return nil
		},
		func(value database.SecurityPGPMetadata) error {
			metadataWrites = append(metadataWrites, value.Revoked)
			return nil
		},
		func(path string) error {
			removeCalls++
			if removeCalls == 2 {
				return wantErr
			}
			return os.Remove(path)
		})
	require.ErrorIs(t, err, wantErr)
	require.Equal(t, []bool{false, true}, configWrites)
	require.Equal(t, []bool{true, false}, metadataWrites)
	require.True(t, cfg.Web.Security.PublishPGPKey)
	for i, path := range filePaths {
		contents, readErr := os.ReadFile(path)
		require.NoError(t, readErr)
		require.Equal(t, []byte{byte('a' + i)}, contents)
	}
}

func TestDeleteKeyMaterialRestoresAfterConfigOrMetadataFailure(t *testing.T) {
	for _, failAt := range []string{"config", "metadata"} {
		t.Run(failAt, func(t *testing.T) {
			dir := t.TempDir()
			filePaths := []string{filepath.Join(dir, "public"), filepath.Join(dir, "private"), filepath.Join(dir, "retired"), filepath.Join(dir, "state")}
			for i, path := range filePaths {
				require.NoError(t, os.WriteFile(path, []byte{byte('a' + i)}, 0600))
			}
			cfg := &config.ServerConfig{}
			cfg.Web.Security.PublishPGPKey = true
			metadata := database.SecurityPGPMetadata{Fingerprint: strings.Repeat("b", 40)}
			configCalls, metadataCalls := 0, 0
			wantErr := errors.New("injected " + failAt + " failure")
			err := deleteKeyMaterial(cfg, metadata, filePaths,
				func(value *config.ServerConfig) error {
					configCalls++
					if failAt == "config" && configCalls == 1 {
						return wantErr
					}
					return nil
				},
				func(value database.SecurityPGPMetadata) error {
					metadataCalls++
					if failAt == "metadata" && metadataCalls == 1 {
						return wantErr
					}
					return nil
				}, os.Remove)
			require.ErrorIs(t, err, wantErr)
			require.True(t, cfg.Web.Security.PublishPGPKey)
			for i, path := range filePaths {
				contents, readErr := os.ReadFile(path)
				require.NoError(t, readErr)
				require.Equal(t, []byte{byte('a' + i)}, contents)
			}
		})
	}
}

func TestGeneratedKeyFingerprintExpiryAndEncryptedBackupPair(t *testing.T) {
	created := time.Date(2026, 5, 6, 7, 8, 9, 0, time.UTC)
	publicKey, privateKey, fingerprint, err := servicecrypto.GenerateSecurityPGPKeys("API Security", "security@example.invalid", created)
	require.NoError(t, err)
	publicFingerprint, err := servicecrypto.PGPKeyFingerprint(publicKey)
	require.NoError(t, err)
	privateFingerprint, err := servicecrypto.PGPKeyFingerprint(privateKey)
	require.NoError(t, err)
	require.Equal(t, fingerprint, publicFingerprint)
	require.Equal(t, fingerprint, privateFingerprint)
	keyring, err := openpgp.ReadArmoredKeyRing(strings.NewReader(privateKey))
	require.NoError(t, err)
	require.Equal(t, created, keyring[0].PrimaryKey.CreationTime.UTC())
	metadata := struct{ CreatedAt, ExpiresAt time.Time }{CreatedAt: created, ExpiresAt: created.Add(keyLifetime)}
	require.Equal(t, keyLifetime, metadata.ExpiresAt.Sub(metadata.CreatedAt))

	secret := []byte("0123456789abcdef0123456789abcdef")
	sealed, err := sealPrivateKey(privateKey, secret)
	require.NoError(t, err)
	require.NoError(t, VerifyEncryptedKeyPair(sealed, secret, []byte(publicKey)))
	opened, err := openPrivateKey(sealed, secret)
	require.NoError(t, err)
	require.Equal(t, privateKey, opened)
	_, err = openPrivateKey(sealed, []byte("abcdef0123456789abcdef0123456789"))
	require.Error(t, err)
}

func TestReencryptPrivateKeyForSecretRotationUsesNewSecretAndIsIdempotent(t *testing.T) {
	configDir := t.TempDir()
	paths.SetApplicationPathOverrides(configDir, t.TempDir(), t.TempDir())
	_, privateKey, _, err := servicecrypto.GenerateSecurityPGPKeys("API Security", "security@example.invalid", time.Now().UTC())
	require.NoError(t, err)
	oldSecret := []byte("0123456789abcdef0123456789abcdef")
	newSecret := []byte("abcdef0123456789abcdef0123456789")
	sealed, err := sealPrivateKey(privateKey, oldSecret)
	require.NoError(t, err)
	_, privatePath, _, _ := KeyPaths()
	require.NoError(t, os.MkdirAll(filepath.Dir(privatePath), 0700))
	require.NoError(t, os.WriteFile(privatePath, sealed, 0600))

	_, err = reencryptPrivateKeyForSecretRotation(oldSecret, newSecret)
	require.NoError(t, err)
	_, err = reencryptPrivateKeyForSecretRotation(oldSecret, newSecret)
	require.NoError(t, err)
	updated, err := os.ReadFile(privatePath)
	require.NoError(t, err)
	opened, err := openPrivateKey(updated, newSecret)
	require.NoError(t, err)
	require.Equal(t, privateKey, opened)
	_, err = openPrivateKey(updated, oldSecret)
	require.Error(t, err)
}

func TestKeyserverRetryAndRotationStatePersistence(t *testing.T) {
	configDir := t.TempDir()
	paths.SetApplicationPathOverrides(configDir, "", "")
	created := time.Date(2026, 7, 8, 9, 10, 11, 0, time.UTC)
	state := persistedState{Version: 1, Keys: map[string]map[string]keyserverEntry{
		"FINGERPRINT": {"keys.example.invalid": {Attempts: 3, NextAttempt: created.Add(time.Hour), LastError: "temporary failure"}},
	}, RotationSignatures: map[string]string{"FINGERPRINT": "detached-signature"}, OldUntil: created.Add(oldKeyLifetime)}
	require.NoError(t, saveState(state))
	loaded, err := loadState()
	require.NoError(t, err)
	require.Equal(t, 3, loaded.Keys["FINGERPRINT"]["keys.example.invalid"].Attempts)
	require.Equal(t, created.Add(time.Hour), loaded.Keys["FINGERPRINT"]["keys.example.invalid"].NextAttempt)
	require.Equal(t, "detached-signature", loaded.RotationSignatures["FINGERPRINT"])
	require.True(t, loaded.OldUntil.Equal(created.Add(oldKeyLifetime)))
	info, err := os.Stat(filepath.Join(configDir, "security", stateFileName))
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0600), info.Mode().Perm())
}
