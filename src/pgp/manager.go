package pgp

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/ProtonMail/go-crypto/openpgp"
	"github.com/ProtonMail/go-crypto/openpgp/armor"
	"github.com/ProtonMail/go-crypto/openpgp/packet"
	"github.com/apimgr/api/src/config"
	"github.com/apimgr/api/src/database"
	"github.com/apimgr/api/src/paths"
	servicecrypto "github.com/apimgr/api/src/service/crypto"
)

const (
	privateFileName = "pgp.priv.asc.enc"
	publicFileName  = "pgp.pub.asc"
	oldPrivateName  = "pgp.priv.old.asc.enc"
	stateFileName   = "keyservers.state"
	stagedSuffix    = ".new"
	keyLifetime     = 2 * 365 * 24 * time.Hour
	oldKeyLifetime  = 30 * 24 * time.Hour
	maxPGPKeyBytes  = 128 << 10
)

var keyLifecycleMu sync.Mutex

type keyserverEntry struct {
	Attempts    int       `json:"attempts"`
	NextAttempt time.Time `json:"next_attempt,omitempty"`
	PublishedAt time.Time `json:"published_at,omitempty"`
	LastError   string    `json:"last_error,omitempty"`
	Final       bool      `json:"final,omitempty"`
}

type persistedState struct {
	Version            int                                  `json:"version"`
	Keys               map[string]map[string]keyserverEntry `json:"keys"`
	RotationSignatures map[string]string                    `json:"rotation_signatures,omitempty"`
	OldUntil           time.Time                            `json:"old_until,omitempty"`
}

// KeyPaths reports the configured key files without exposing key data.
func KeyPaths() (publicPath, privatePath, oldPrivatePath, statePath string) {
	dir := filepath.Join(paths.ConfigDir(), "security")
	return filepath.Join(dir, publicFileName), filepath.Join(dir, privateFileName), filepath.Join(dir, oldPrivateName), filepath.Join(dir, stateFileName)
}

// Generate creates and persists the disclosure keypair and initial metadata.
func Generate(ctx context.Context, cfg *config.Config) (database.SecurityPGPMetadata, error) {
	keyLifecycleMu.Lock()
	defer keyLifecycleMu.Unlock()
	unlockReports := database.LockSecurityReportKey()
	defer unlockReports()
	publicPath, privatePath, _, statePath := KeyPaths()
	if _, err := os.Lstat(publicPath); err == nil {
		return database.SecurityPGPMetadata{}, fmt.Errorf("security PGP key already exists")
	} else if !os.IsNotExist(err) {
		return database.SecurityPGPMetadata{}, fmt.Errorf("inspect public key path: %w", err)
	}
	name, email := securityIdentity(cfg)
	created := time.Now().UTC().Truncate(time.Second)
	publicKey, privateKey, fingerprint, err := servicecrypto.GenerateSecurityPGPKeys(name, email, created)
	if err != nil {
		return database.SecurityPGPMetadata{}, err
	}
	secret, err := database.LoadSecret(ctx, database.SecretInstallationSecret)
	if err != nil {
		return database.SecurityPGPMetadata{}, fmt.Errorf("load installation secret: %w", err)
	}
	sealed, err := sealPrivateKey(privateKey, secret.Value)
	if err != nil {
		return database.SecurityPGPMetadata{}, err
	}
	if err := ensureSecurityDir(); err != nil {
		return database.SecurityPGPMetadata{}, err
	}
	metadata := database.SecurityPGPMetadata{Fingerprint: fingerprint, CreatedAt: created, ExpiresAt: created.Add(keyLifetime), KeyserversPublished: map[string]string{}}
	if err := installKeyMaterial(privatePath, publicPath, statePath, sealed, []byte(publicKey), true,
		persistedState{Version: 1, Keys: map[string]map[string]keyserverEntry{}}, metadata,
		atomicWriteFile, saveState, func(value database.SecurityPGPMetadata) error { return database.SaveSecurityPGPMetadata(ctx, value) }); err != nil {
		return database.SecurityPGPMetadata{}, err
	}
	return metadata, nil
}

// ReadPublic returns the armored public key.
func ReadPublic() ([]byte, error) {
	path, _, _, _ := KeyPaths()
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if _, err := servicecrypto.PGPKeyFingerprint(string(data)); err != nil {
		return nil, fmt.Errorf("invalid stored public key: %w", err)
	}
	return data, nil
}

// ReadPrivate decrypts the private key using the current or grace-period
// installation secret.
func ReadPrivate(ctx context.Context) (string, error) {
	_, path, _, _ := KeyPaths()
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	secret, err := database.LoadSecret(ctx, database.SecretInstallationSecret)
	if err != nil {
		return "", fmt.Errorf("load installation secret: %w", err)
	}
	previous := secret.Previous
	if !secret.PreviousValid(time.Now().UTC()) {
		previous = nil
	}
	for _, key := range [][]byte{secret.Value, previous} {
		if len(key) == 0 {
			continue
		}
		private, decryptErr := openPrivateKey(data, key)
		if decryptErr == nil {
			return private, nil
		}
	}
	return "", fmt.Errorf("decrypt stored PGP private key with current or retained installation secret")
}

// DecryptPrivateKey opens a backed-up encrypted PGP private key with the
// installation secret stored in that same backup.
func DecryptPrivateKey(sealed, installationSecret []byte) (string, error) {
	return openPrivateKey(sealed, installationSecret)
}

// ReencryptPrivateKeyForSecretRotation changes the wrapping key used for the
// active encrypted private key and returns a rollback operation for the caller's
// database transaction.
func ReencryptPrivateKeyForSecretRotation(oldSecret, nextSecret []byte) (func() error, error) {
	keyLifecycleMu.Lock()
	defer keyLifecycleMu.Unlock()
	return reencryptPrivateKeyForSecretRotation(oldSecret, nextSecret)
}

func reencryptPrivateKeyForSecretRotation(oldSecret, nextSecret []byte) (func() error, error) {
	_, path, _, _ := KeyPaths()
	snapshot, err := snapshotFile(path)
	if err != nil {
		return nil, fmt.Errorf("snapshot encrypted PGP private key: %w", err)
	}
	if !snapshot.exists {
		return func() error { return nil }, nil
	}
	if _, err := openPrivateKey(snapshot.data, nextSecret); err == nil {
		return func() error { return nil }, nil
	}
	private, err := openPrivateKey(snapshot.data, oldSecret)
	if err != nil {
		return nil, fmt.Errorf("decrypt PGP private key with current installation secret: %w", err)
	}
	sealed, err := sealPrivateKey(private, nextSecret)
	if err != nil {
		return nil, fmt.Errorf("encrypt PGP private key with replacement installation secret: %w", err)
	}
	if err := atomicWriteFile(path, sealed, snapshot.mode); err != nil {
		return nil, fmt.Errorf("store PGP private key under replacement installation secret: %w", err)
	}
	return func() error { return restoreKeyFile(path, snapshot.data, true, snapshot.mode) }, nil
}

// ResumeInstallationSecretRotation finishes wrapping the encrypted private
// key with the current secret after an interrupted installation-secret
// rotation.
func ResumeInstallationSecretRotation(ctx context.Context) error {
	secret, err := database.LoadSecret(ctx, database.SecretInstallationSecret)
	if err != nil {
		if errors.Is(err, database.ErrSecretNotFound) {
			return nil
		}
		return err
	}
	if !secret.PreviousValid(time.Now().UTC()) {
		return nil
	}
	keyLifecycleMu.Lock()
	defer keyLifecycleMu.Unlock()
	_, err = reencryptPrivateKeyForSecretRotation(secret.Previous, secret.Value)
	return err
}

// RotateInstallationSecret serializes installation-secret rotation against
// every PGP key lifecycle operation while the database and private-key file
// transition is in progress.
func RotateInstallationSecret(ctx context.Context) error {
	keyLifecycleMu.Lock()
	defer keyLifecycleMu.Unlock()
	return database.RotateSecretWithTransition(ctx, database.SecretInstallationSecret, reencryptPrivateKeyForSecretRotation)
}

// Fingerprint returns the OpenPGP v4 fingerprint of an armored key.
func Fingerprint(armored string) (string, error) {
	return servicecrypto.PGPKeyFingerprint(armored)
}

// Rotate generates a replacement key and re-encrypts retained PGP reports
// transactionally before switching the on-disk keypair.
func Rotate(ctx context.Context, cfg *config.Config) (database.SecurityPGPMetadata, error) {
	keyLifecycleMu.Lock()
	defer keyLifecycleMu.Unlock()
	if _, err := recoverStagedRotation(ctx, cfg); err != nil {
		return database.SecurityPGPMetadata{}, fmt.Errorf("recover earlier PGP rotation: %w", err)
	}
	oldMeta, err := database.GetSecurityPGPMetadata(ctx)
	if err != nil {
		return database.SecurityPGPMetadata{}, err
	}
	if state, stateErr := loadState(); stateErr == nil && time.Now().UTC().Before(state.OldUntil) {
		return database.SecurityPGPMetadata{}, fmt.Errorf("cannot rotate PGP key while the prior private key is retained")
	} else if stateErr != nil && !errors.Is(stateErr, os.ErrNotExist) {
		return database.SecurityPGPMetadata{}, fmt.Errorf("read PGP rotation state: %w", stateErr)
	}
	oldPublic, err := ReadPublic()
	if err != nil {
		return database.SecurityPGPMetadata{}, err
	}
	publicFingerprint, err := servicecrypto.PGPKeyFingerprint(string(oldPublic))
	if err != nil || publicFingerprint != oldMeta.Fingerprint {
		return database.SecurityPGPMetadata{}, fmt.Errorf("stored PGP metadata does not match the active public key")
	}
	oldPrivate, err := ReadPrivate(ctx)
	if err != nil {
		return database.SecurityPGPMetadata{}, err
	}
	privateFingerprint, err := servicecrypto.PGPKeyFingerprint(oldPrivate)
	if err != nil || privateFingerprint != publicFingerprint {
		return database.SecurityPGPMetadata{}, fmt.Errorf("stored PGP private key does not match the active public key")
	}
	name, email := securityIdentity(cfg)
	created := time.Now().UTC().Truncate(time.Second)
	newPublic, newPrivate, fingerprint, err := servicecrypto.GenerateSecurityPGPKeys(name, email, created)
	if err != nil {
		return database.SecurityPGPMetadata{}, err
	}
	rotationSignature, err := signRotation(oldPrivate, newPublic)
	if err != nil {
		return database.SecurityPGPMetadata{}, fmt.Errorf("sign rotated public key: %w", err)
	}
	newPublic, err = servicecrypto.CrossSignPGPPublicKey(newPublic, oldPrivate)
	if err != nil {
		return database.SecurityPGPMetadata{}, err
	}
	secret, err := database.LoadSecret(ctx, database.SecretInstallationSecret)
	if err != nil {
		return database.SecurityPGPMetadata{}, err
	}
	sealed, err := sealPrivateKey(newPrivate, secret.Value)
	if err != nil {
		return database.SecurityPGPMetadata{}, err
	}
	publicPath, privatePath, oldPrivatePath, statePath := KeyPaths()
	if err := ensureSecurityDir(); err != nil {
		return database.SecurityPGPMetadata{}, err
	}
	newMeta := database.SecurityPGPMetadata{Fingerprint: fingerprint, CreatedAt: created, ExpiresAt: created.Add(keyLifetime), LastRotatedAt: &created, KeyserversPublished: map[string]string{}}
	stagePublicPath := publicPath + stagedSuffix
	stagePrivatePath := privatePath + stagedSuffix
	stageStatePath := statePath + stagedSuffix
	if err := stageRotationFiles(stagePublicPath, stagePrivatePath, stageStatePath, []byte(newPublic), sealed, fingerprint, rotationSignature, created.Add(oldKeyLifetime)); err != nil {
		return database.SecurityPGPMetadata{}, fmt.Errorf("stage rotated PGP key: %w", err)
	}
	cleanupStages := func() error { return removeRotationStages(stagePublicPath, stagePrivatePath, stageStatePath) }
	activate := func() error {
		if err := activateStagedRotation(privatePath, publicPath, oldPrivatePath, statePath, stagePrivatePath, stagePublicPath, stageStatePath); err != nil {
			return err
		}
		return completeRotationMetadata(ctx, fingerprint)
	}
	transactionMetadata := newMeta
	transactionMetadata.LastRotatedAt = nil
	if err := database.RotatePGPSecurityReports(ctx, transactionMetadata, func(body []byte) ([]byte, error) {
		plaintext, err := servicecrypto.PGPDecrypt(string(body), oldPrivate)
		if err != nil {
			return nil, err
		}
		ciphertext, err := servicecrypto.PGPEncrypt(plaintext, newPublic)
		if err != nil {
			return nil, err
		}
		return []byte(ciphertext), nil
	}, activate, cleanupStages); err != nil {
		committed, recoveryErr := recoverStagedRotation(ctx, cfg)
		if recoveryErr != nil {
			return database.SecurityPGPMetadata{}, errors.Join(err, fmt.Errorf("recover staged PGP rotation: %w", recoveryErr))
		}
		if committed {
			return newMeta, nil
		}
		if metadata, metadataErr := database.GetSecurityPGPMetadata(ctx); metadataErr == nil && metadata.Fingerprint == fingerprint {
			return newMeta, nil
		} else if metadataErr != nil && !errors.Is(metadataErr, database.ErrSecurityPGPMetadataNotFound) {
			return database.SecurityPGPMetadata{}, errors.Join(err, fmt.Errorf("verify committed PGP rotation metadata: %w", metadataErr))
		}
		return database.SecurityPGPMetadata{}, err
	}
	return newMeta, nil
}

// RecoverRotation completes or discards staged PGP key files left by an
// interrupted rotation. Call after database and app-secret initialization,
// before accepting security-report submissions.
func RecoverRotation(ctx context.Context, cfg *config.Config) error {
	keyLifecycleMu.Lock()
	defer keyLifecycleMu.Unlock()
	_, err := recoverStagedRotation(ctx, cfg)
	return err
}

func rotationStagePaths() (publicStage, privateStage, stateStage string) {
	publicPath, privatePath, _, statePath := KeyPaths()
	return publicPath + stagedSuffix, privatePath + stagedSuffix, statePath + stagedSuffix
}

func stageRotationFiles(publicStage, privateStage, stateStage string, publicKey, privateKey []byte, fingerprint, signature string, oldUntil time.Time) error {
	if err := atomicWriteFile(publicStage, publicKey, 0644); err != nil {
		return err
	}
	if err := atomicWriteFile(privateStage, privateKey, 0600); err != nil {
		return errors.Join(err, removeRotationStages(publicStage, privateStage, stateStage))
	}
	state, err := loadState()
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return errors.Join(err, removeRotationStages(publicStage, privateStage, stateStage))
	}
	if state.Keys == nil {
		state.Keys = map[string]map[string]keyserverEntry{}
	}
	if state.RotationSignatures == nil {
		state.RotationSignatures = map[string]string{}
	}
	state.RotationSignatures[fingerprint] = signature
	state.OldUntil = oldUntil.UTC()
	state.Version = 1
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return errors.Join(err, removeRotationStages(publicStage, privateStage, stateStage))
	}
	if err := atomicWriteFile(stateStage, append(data, '\n'), 0600); err != nil {
		return errors.Join(err, removeRotationStages(publicStage, privateStage, stateStage))
	}
	return nil
}

func removeRotationStages(paths ...string) error {
	var result error
	for _, path := range paths {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			result = errors.Join(result, fmt.Errorf("remove staged file %s: %w", filepath.Base(path), err))
		}
	}
	return result
}

func activateStagedRotation(privatePath, publicPath, oldPrivatePath, statePath, privateStage, publicStage, stateStage string) error {
	if _, err := os.Stat(privateStage); err == nil {
		if _, activeErr := os.Stat(privatePath); activeErr == nil {
			if err := os.Remove(oldPrivatePath); err != nil && !errors.Is(err, os.ErrNotExist) {
				return fmt.Errorf("remove expired retired PGP private key: %w", err)
			}
			if err := os.Rename(privatePath, oldPrivatePath); err != nil {
				return fmt.Errorf("move old PGP private key to retired path: %w", err)
			}
		} else if !errors.Is(activeErr, os.ErrNotExist) {
			return fmt.Errorf("inspect active PGP private key: %w", activeErr)
		} else if _, retiredErr := os.Stat(oldPrivatePath); retiredErr != nil {
			return fmt.Errorf("old PGP private key is missing during recovery: %w", retiredErr)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect staged PGP private key: %w", err)
	} else if _, err := os.Stat(oldPrivatePath); err != nil {
		return fmt.Errorf("retired PGP private key is missing during recovery: %w", err)
	}
	for _, file := range [][2]string{{privateStage, privatePath}, {publicStage, publicPath}, {stateStage, statePath}} {
		if err := renameStagedFile(file[0], file[1]); err != nil {
			return err
		}
	}
	return nil
}

func renameStagedFile(source, destination string) error {
	if err := os.Rename(source, destination); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			if _, destinationErr := os.Stat(destination); destinationErr == nil {
				return nil
			} else if !errors.Is(destinationErr, os.ErrNotExist) {
				return fmt.Errorf("inspect active PGP file %s: %w", filepath.Base(destination), destinationErr)
			}
		} else {
			if removeErr := os.Remove(destination); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
				return fmt.Errorf("replace active PGP file %s: %w", filepath.Base(destination), err)
			}
			if renameErr := os.Rename(source, destination); renameErr == nil {
				return nil
			} else {
				return fmt.Errorf("activate staged PGP file %s: %w", filepath.Base(destination), renameErr)
			}
		}
		return fmt.Errorf("activate staged PGP file %s: %w", filepath.Base(destination), err)
	}
	return nil
}

func recoverStagedRotation(ctx context.Context, cfg *config.Config) (bool, error) {
	publicStage, privateStage, stateStage := rotationStagePaths()
	publicPath, privatePath, oldPrivatePath, statePath := KeyPaths()
	stages := []string{publicStage, privateStage, stateStage}
	anyStage := false
	for _, path := range stages {
		if _, err := os.Stat(path); err == nil {
			anyStage = true
		} else if !errors.Is(err, os.ErrNotExist) {
			return false, fmt.Errorf("inspect staged PGP file: %w", err)
		}
	}
	if !anyStage {
		return false, completePendingRotationMetadata(ctx)
	}
	newPublicPath := publicStage
	if _, err := os.Stat(newPublicPath); errors.Is(err, os.ErrNotExist) {
		newPublicPath = publicPath
	} else if err != nil {
		return false, fmt.Errorf("inspect staged PGP public key: %w", err)
	}
	newPublic, err := os.ReadFile(newPublicPath)
	if err != nil {
		return false, fmt.Errorf("read replacement PGP public key: %w", err)
	}
	fingerprint, err := servicecrypto.PGPKeyFingerprint(string(newPublic))
	if err != nil {
		return false, fmt.Errorf("read replacement PGP fingerprint: %w", err)
	}
	newKeyIDs, err := publicKeyIDs(string(newPublic))
	if err != nil {
		return false, fmt.Errorf("read replacement PGP recipient key IDs: %w", err)
	}
	bodies, err := database.SecurityReportPGPBodies(ctx)
	if err != nil {
		return false, err
	}
	committed := false
	for _, body := range bodies {
		recipients, parseErr := encryptedRecipientKeyIDs(body)
		if parseErr != nil {
			return false, fmt.Errorf("inspect stored PGP report during rotation recovery: %w", parseErr)
		}
		if intersectsKeyIDs(recipients, newKeyIDs) {
			committed = true
			break
		}
	}
	metadata, metadataErr := database.GetSecurityPGPMetadata(ctx)
	if metadataErr != nil && !errors.Is(metadataErr, database.ErrSecurityPGPMetadataNotFound) {
		return false, metadataErr
	}
	if !committed && metadataErr == nil && metadata.Fingerprint == fingerprint {
		// With no PGP report body, the metadata row is the only durable commit
		// marker available; AI.md's packet-based rule cannot resolve that case.
		committed = true
	}
	if !committed {
		if err := removeRotationStages(stages...); err != nil {
			return false, err
		}
		return false, nil
	}
	privateCandidate := privateStage
	if _, err := os.Stat(privateCandidate); errors.Is(err, os.ErrNotExist) {
		privateCandidate = privatePath
	} else if err != nil {
		return false, fmt.Errorf("inspect staged PGP private key: %w", err)
	}
	sealedPrivate, err := os.ReadFile(privateCandidate)
	if err != nil {
		return false, fmt.Errorf("read replacement encrypted PGP private key: %w", err)
	}
	secret, err := database.LoadSecret(ctx, database.SecretInstallationSecret)
	if err != nil {
		return false, fmt.Errorf("load installation secret for PGP recovery: %w", err)
	}
	privateKey, err := openPrivateKey(sealedPrivate, secret.Value)
	if err != nil {
		return false, fmt.Errorf("decrypt replacement PGP private key: %w", err)
	}
	privateFingerprint, err := servicecrypto.PGPKeyFingerprint(privateKey)
	if err != nil || privateFingerprint != fingerprint {
		return false, errors.New("replacement PGP private and public keys do not match")
	}
	if err := activateStagedRotation(privatePath, publicPath, oldPrivatePath, statePath, privateStage, publicStage, stateStage); err != nil {
		return false, err
	}
	if err := completeRotationMetadata(ctx, fingerprint); err != nil {
		return false, err
	}
	if cfg != nil {
		if err := publishLocked(ctx, cfg); err != nil {
			log.Printf("PGP: recovered key publish will retry: %v", err)
		}
	}
	return true, nil
}

func completePendingRotationMetadata(ctx context.Context) error {
	metadata, err := database.GetSecurityPGPMetadata(ctx)
	if err != nil {
		if errors.Is(err, database.ErrSecurityPGPMetadataNotFound) {
			return nil
		}
		return err
	}
	if metadata.LastRotatedAt != nil {
		return nil
	}
	state, err := loadState()
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if state.OldUntil.IsZero() {
		return nil
	}
	publicKey, err := ReadPublic()
	if err != nil {
		return err
	}
	fingerprint, err := servicecrypto.PGPKeyFingerprint(string(publicKey))
	if err != nil {
		return err
	}
	if fingerprint != metadata.Fingerprint {
		return nil
	}
	return completeRotationMetadata(ctx, fingerprint)
}

func completeRotationMetadata(ctx context.Context, fingerprint string) error {
	metadata, err := database.GetSecurityPGPMetadata(ctx)
	if err != nil {
		return err
	}
	if metadata.Fingerprint != fingerprint {
		return errors.New("active PGP metadata does not match the completed rotation")
	}
	if metadata.LastRotatedAt != nil {
		return nil
	}
	state, err := loadState()
	if err != nil {
		return fmt.Errorf("load completed PGP rotation timestamp: %w", err)
	}
	if state.OldUntil.IsZero() {
		return errors.New("completed PGP rotation has no retired-key expiry")
	}
	rotatedAt := state.OldUntil.Add(-oldKeyLifetime).UTC()
	metadata.LastRotatedAt = &rotatedAt
	return database.SaveSecurityPGPMetadata(ctx, metadata)
}

func publicKeyIDs(armored string) (map[uint64]struct{}, error) {
	entities, err := openpgp.ReadArmoredKeyRing(strings.NewReader(armored))
	if err != nil {
		return nil, err
	}
	ids := make(map[uint64]struct{})
	for _, entity := range entities {
		ids[entity.PrimaryKey.KeyId] = struct{}{}
		for _, subkey := range entity.Subkeys {
			ids[subkey.PublicKey.KeyId] = struct{}{}
		}
	}
	return ids, nil
}

func encryptedRecipientKeyIDs(armored []byte) (map[uint64]struct{}, error) {
	block, err := armor.Decode(bytes.NewReader(armored))
	if err != nil {
		return nil, err
	}
	reader := packet.NewReader(block.Body)
	ids := make(map[uint64]struct{})
	for {
		value, err := reader.Next()
		if errors.Is(err, io.EOF) {
			return ids, nil
		}
		if err != nil {
			return nil, err
		}
		if encryptedKey, ok := value.(*packet.EncryptedKey); ok {
			ids[encryptedKey.KeyId] = struct{}{}
		}
		if _, ok := value.(packet.EncryptedDataPacket); ok {
			return ids, nil
		}
	}
}

func intersectsKeyIDs(left, right map[uint64]struct{}) bool {
	for keyID := range left {
		if keyID != 0 {
			if _, ok := right[keyID]; ok {
				return true
			}
		}
	}
	return false
}

// Publish submits the stored public key to configured HTTPS keyservers.
func Publish(ctx context.Context, cfg *config.Config) error {
	keyLifecycleMu.Lock()
	defer keyLifecycleMu.Unlock()
	return publishLocked(ctx, cfg)
}

func publishLocked(ctx context.Context, cfg *config.Config) error {
	publicKey, err := ReadPublic()
	if err != nil {
		return err
	}
	fingerprint, err := servicecrypto.PGPKeyFingerprint(string(publicKey))
	if err != nil {
		return err
	}
	metadata, err := database.GetSecurityPGPMetadata(ctx)
	if err != nil {
		return err
	}
	state, err := loadState()
	if errors.Is(err, os.ErrNotExist) {
		state = persistedState{Version: 1, Keys: map[string]map[string]keyserverEntry{}}
	} else if err != nil {
		return err
	}
	if len(state.Keys) == 0 {
		state.Keys = map[string]map[string]keyserverEntry{}
	}
	if state.Keys[fingerprint] == nil {
		state.Keys[fingerprint] = map[string]keyserverEntry{}
	}
	var publishErrors []string
	for _, rawHost := range cfg.Web.Security.Keyservers {
		host := strings.TrimSpace(strings.ToLower(rawHost))
		if host == "" || validateHost(host) != nil {
			publishErrors = append(publishErrors, fmt.Sprintf("invalid keyserver host %q", rawHost))
			continue
		}
		entry := state.Keys[fingerprint][host]
		if !entry.PublishedAt.IsZero() || entry.Final || (!entry.NextAttempt.IsZero() && time.Now().Before(entry.NextAttempt)) {
			continue
		}
		err := submitKey(ctx, host, string(publicKey), fingerprint)
		if err == nil {
			entry.PublishedAt = time.Now().UTC()
			entry.NextAttempt = time.Time{}
			entry.LastError = ""
			metadata.KeyserversPublished[host] = entry.PublishedAt.Format(time.RFC3339)
		} else {
			entry.LastError = err.Error()
			if !isRetryable(err) {
				entry.Final = true
			} else if entry.Attempts < 8 {
				entry.Attempts++
				delay := time.Minute << (entry.Attempts - 1)
				if delay > 6*time.Hour {
					delay = 6 * time.Hour
				}
				entry.NextAttempt = time.Now().UTC().Add(delay)
			} else {
				entry.Final = true
			}
			publishErrors = append(publishErrors, host+": "+err.Error())
		}
		state.Keys[fingerprint][host] = entry
		if err := saveState(state); err != nil {
			return err
		}
	}
	if err := database.SaveSecurityPGPMetadata(ctx, metadata); err != nil {
		return err
	}
	if len(publishErrors) > 0 {
		return fmt.Errorf("PGP key publish: %s", strings.Join(publishErrors, "; "))
	}
	return nil
}

// RetryDue attempts configured keyservers whose persisted backoff time passed.
func RetryDue(ctx context.Context, cfg *config.Config) error {
	keyLifecycleMu.Lock()
	defer keyLifecycleMu.Unlock()
	state, err := loadState()
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if len(state.Keys) == 0 {
		return nil
	}
	metadata, err := database.GetSecurityPGPMetadata(ctx)
	if errors.Is(err, database.ErrSecurityPGPMetadataNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if metadata.Fingerprint == "" {
		return nil
	}
	return publishLocked(ctx, cfg)
}

// ExportPrivate decrypts and returns the private key for an audited CLI flow.
func ExportPrivate(ctx context.Context) (string, error) {
	keyLifecycleMu.Lock()
	defer keyLifecycleMu.Unlock()
	return ReadPrivate(ctx)
}

// VerifyEncryptedKeyPair checks a backed-up encrypted private key against its
// public key without changing the live key files or database state.
func VerifyEncryptedKeyPair(sealedPrivate, installationSecret, publicKey []byte) error {
	private, err := openPrivateKey(sealedPrivate, installationSecret)
	if err != nil {
		return fmt.Errorf("decrypt backed-up PGP private key: %w", err)
	}
	privateFingerprint, err := servicecrypto.PGPKeyFingerprint(private)
	if err != nil {
		return fmt.Errorf("read backed-up PGP private key: %w", err)
	}
	publicFingerprint, err := servicecrypto.PGPKeyFingerprint(string(publicKey))
	if err != nil {
		return fmt.Errorf("read backed-up PGP public key: %w", err)
	}
	if privateFingerprint != publicFingerprint {
		return fmt.Errorf("backed-up PGP public and private fingerprints do not match")
	}
	return nil
}

// ImportPrivate validates, encrypts, and installs an existing private key.
func ImportPrivate(ctx context.Context, cfg *config.Config, armored string, allowIdentityMismatch bool) (database.SecurityPGPMetadata, error) {
	keyLifecycleMu.Lock()
	defer keyLifecycleMu.Unlock()
	unlockReports := database.LockSecurityReportKey()
	defer unlockReports()
	fingerprint, err := servicecrypto.PGPKeyFingerprint(armored)
	if err != nil {
		return database.SecurityPGPMetadata{}, err
	}
	if current, currentErr := database.GetSecurityPGPMetadata(ctx); currentErr == nil && current.Fingerprint != fingerprint {
		hasReports, reportsErr := database.HasPGPSecurityReports(ctx)
		if reportsErr != nil {
			return database.SecurityPGPMetadata{}, reportsErr
		}
		if hasReports {
			return database.SecurityPGPMetadata{}, fmt.Errorf("imported key does not match retained PGP-encrypted reports; use key rotation to preserve them")
		}
	} else if currentErr != nil && !errors.Is(currentErr, database.ErrSecurityPGPMetadataNotFound) {
		return database.SecurityPGPMetadata{}, currentErr
	}
	name, email := securityIdentity(cfg)
	matches, err := servicecrypto.PGPIdentityMatches(armored, name, email)
	if err != nil {
		return database.SecurityPGPMetadata{}, err
	}
	if !matches && !allowIdentityMismatch {
		return database.SecurityPGPMetadata{}, fmt.Errorf("key identity does not match %q <%s>; explicit override required", name, email)
	}
	existingMetadata, metadataErr := database.GetSecurityPGPMetadata(ctx)
	if metadataErr != nil && !errors.Is(metadataErr, database.ErrSecurityPGPMetadataNotFound) {
		return database.SecurityPGPMetadata{}, metadataErr
	}
	hasReports, err := database.HasPGPSecurityReports(ctx)
	if err != nil {
		return database.SecurityPGPMetadata{}, err
	}
	if hasReports && (metadataErr != nil || !strings.EqualFold(existingMetadata.Fingerprint, fingerprint)) {
		return database.SecurityPGPMetadata{}, fmt.Errorf("cannot replace a different or unrecorded PGP key while PGP-encrypted reports exist; use `--maintenance pgp rotate` with the active key installed")
	}
	entities, err := openpgp.ReadArmoredKeyRing(strings.NewReader(armored))
	if err != nil || len(entities) == 0 || entities[0].PrivateKey == nil {
		return database.SecurityPGPMetadata{}, fmt.Errorf("input does not contain a private key")
	}
	publicKey, err := serializePublicKey(entities[0])
	if err != nil {
		return database.SecurityPGPMetadata{}, err
	}
	created := entities[0].PrimaryKey.CreationTime
	if created.IsZero() {
		created = time.Now().UTC()
	}
	secret, err := database.LoadSecret(ctx, database.SecretInstallationSecret)
	if err != nil {
		return database.SecurityPGPMetadata{}, err
	}
	sealed, err := sealPrivateKey(armored, secret.Value)
	if err != nil {
		return database.SecurityPGPMetadata{}, err
	}
	publicPath, privatePath, _, statePath := KeyPaths()
	if err := ensureSecurityDir(); err != nil {
		return database.SecurityPGPMetadata{}, err
	}
	metadata := database.SecurityPGPMetadata{Fingerprint: fingerprint, CreatedAt: created.UTC(), ExpiresAt: created.Add(keyLifetime).UTC(), KeyserversPublished: map[string]string{}}
	stateSnapshot, err := snapshotFile(statePath)
	if err != nil {
		return database.SecurityPGPMetadata{}, fmt.Errorf("inspect keyserver state: %w", err)
	}
	writeState := !stateSnapshot.exists
	if writeState {
		if err := installKeyMaterial(privatePath, publicPath, statePath, sealed, []byte(publicKey), true,
			persistedState{Version: 1, Keys: map[string]map[string]keyserverEntry{}}, metadata,
			atomicWriteFile, saveState, func(value database.SecurityPGPMetadata) error { return database.SaveSecurityPGPMetadata(ctx, value) }); err != nil {
			return database.SecurityPGPMetadata{}, err
		}
	} else {
		if err := installKeyMaterial(privatePath, publicPath, statePath, sealed, []byte(publicKey), false,
			persistedState{}, metadata, atomicWriteFile, saveState,
			func(value database.SecurityPGPMetadata) error { return database.SaveSecurityPGPMetadata(ctx, value) }); err != nil {
			return database.SecurityPGPMetadata{}, err
		}
	}
	return metadata, nil
}

// Delete removes installed key files but retains a revoked fingerprint record.
func Delete(ctx context.Context, cfg *config.Config) error {
	keyLifecycleMu.Lock()
	defer keyLifecycleMu.Unlock()
	unlockReports := database.LockSecurityReportKey()
	defer unlockReports()
	metadata, err := database.GetSecurityPGPMetadata(ctx)
	if err != nil && !errors.Is(err, database.ErrSecurityPGPMetadataNotFound) {
		return err
	}
	publicPath, privatePath, oldPrivatePath, statePath := KeyPaths()
	return deleteKeyMaterial(cfg, metadata, []string{publicPath, privatePath, oldPrivatePath, statePath}, config.Save,
		func(value database.SecurityPGPMetadata) error { return database.SaveSecurityPGPMetadata(ctx, value) }, os.Remove)
}

// DeleteRetiredPrivateKey removes the retired key after its 30-day grace.
func DeleteRetiredPrivateKey(now time.Time) error {
	keyLifecycleMu.Lock()
	defer keyLifecycleMu.Unlock()
	_, _, oldPrivatePath, _ := KeyPaths()
	state, err := loadState()
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if state.OldUntil.IsZero() || now.Before(state.OldUntil) {
		return nil
	}
	if err := os.Remove(oldPrivatePath); err != nil && !os.IsNotExist(err) {
		return err
	}
	state.OldUntil = time.Time{}
	return saveState(state)
}

// signRotation produces a durable detached signature over the replacement
// public key using the retired signing key.
func signRotation(oldPrivate, newPublic string) (string, error) {
	entities, err := openpgp.ReadArmoredKeyRing(strings.NewReader(oldPrivate))
	if err != nil || len(entities) == 0 || entities[0].PrivateKey == nil {
		return "", fmt.Errorf("retired signing key is invalid")
	}
	var signature bytes.Buffer
	if err := openpgp.ArmoredDetachSign(&signature, entities[0], strings.NewReader(newPublic), nil); err != nil {
		return "", err
	}
	return signature.String(), nil
}

func securityIdentity(cfg *config.Config) (string, string) {
	name := strings.TrimSpace(cfg.Server.Branding.Title)
	if name == "" {
		name = "API"
	}
	return name + " Security", strings.TrimSpace(cfg.Server.Contact.ResolveSecurity().Email)
}

func ensureSecurityDir() error {
	_, privatePath, _, _ := KeyPaths()
	dir := filepath.Dir(privatePath)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("create security key directory: %w", err)
	}
	return os.Chmod(dir, 0700)
}

type fileSnapshot struct {
	data   []byte
	exists bool
	mode   os.FileMode
}

func installKeyMaterial(privatePath, publicPath, statePath string, sealedPrivate, publicKey []byte, writeState bool, state persistedState, metadata database.SecurityPGPMetadata,
	writeFile func(string, []byte, os.FileMode) error, writeStateFile func(persistedState) error, saveMetadata func(database.SecurityPGPMetadata) error) error {
	paths := []string{privatePath, publicPath, statePath}
	snapshots := make([]fileSnapshot, len(paths))
	for i, path := range paths {
		snapshot, err := snapshotFile(path)
		if err != nil {
			return fmt.Errorf("snapshot key file %s: %w", filepath.Base(path), err)
		}
		snapshots[i] = snapshot
	}
	rollback := func() error { return restoreFileSnapshots(paths, snapshots) }
	if err := writeFile(privatePath, sealedPrivate, 0600); err != nil {
		return errors.Join(fmt.Errorf("store private key: %w", err), rollback())
	}
	if err := writeFile(publicPath, publicKey, 0644); err != nil {
		return errors.Join(fmt.Errorf("store public key: %w", err), rollback())
	}
	if writeState {
		if err := writeStateFile(state); err != nil {
			return errors.Join(fmt.Errorf("store keyserver state: %w", err), rollback())
		}
	}
	if err := saveMetadata(metadata); err != nil {
		return errors.Join(fmt.Errorf("store PGP metadata: %w", err), rollback())
	}
	return nil
}

func deleteKeyMaterial(cfg *config.Config, metadata database.SecurityPGPMetadata, paths []string,
	saveConfig func(*config.Config) error, saveMetadata func(database.SecurityPGPMetadata) error, removeFile func(string) error) error {
	snapshots := make([]fileSnapshot, len(paths))
	for i, path := range paths {
		snapshot, err := snapshotFile(path)
		if err != nil {
			return fmt.Errorf("snapshot key file %s: %w", filepath.Base(path), err)
		}
		snapshots[i] = snapshot
	}
	oldConfig := *cfg
	nextConfig := *cfg
	nextConfig.Web.Security.PublishPGPKey = false
	updatedMetadata := metadata
	metadataChanged := metadata.Fingerprint != ""
	updatedMetadata.Revoked = true
	rollback := func() error {
		var rollbackErr error
		rollbackErr = errors.Join(rollbackErr, restoreFileSnapshots(paths, snapshots))
		if metadataChanged {
			rollbackErr = errors.Join(rollbackErr, saveMetadata(metadata))
		}
		rollbackErr = errors.Join(rollbackErr, saveConfig(&oldConfig))
		return rollbackErr
	}
	if err := saveConfig(&nextConfig); err != nil {
		return errors.Join(fmt.Errorf("disable public key publishing: %w", err), rollback())
	}
	if metadataChanged {
		if err := saveMetadata(updatedMetadata); err != nil {
			return errors.Join(fmt.Errorf("revoke PGP metadata: %w", err), rollback())
		}
	}
	for _, path := range paths {
		if err := removeFile(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return errors.Join(fmt.Errorf("remove security key file %s: %w", filepath.Base(path), err), rollback())
		}
	}
	cfg.Web.Security.PublishPGPKey = false
	return nil
}

func restoreFileSnapshots(paths []string, snapshots []fileSnapshot) error {
	var result error
	for i := len(paths) - 1; i >= 0; i-- {
		if err := restoreKeyFile(paths[i], snapshots[i].data, snapshots[i].exists, snapshots[i].mode); err != nil {
			result = errors.Join(result, fmt.Errorf("restore %s: %w", filepath.Base(paths[i]), err))
		}
	}
	return result
}

func snapshotFile(path string) (fileSnapshot, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return fileSnapshot{}, nil
	}
	if err != nil {
		return fileSnapshot{}, err
	}
	if !info.Mode().IsRegular() || info.Size() > maxPGPKeyBytes*2 {
		return fileSnapshot{}, fmt.Errorf("not a regular file or exceeds size limit")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return fileSnapshot{}, err
	}
	return fileSnapshot{data: data, exists: true, mode: info.Mode().Perm()}, nil
}

func restoreKeyFile(path string, contents []byte, existed bool, mode os.FileMode) error {
	if !existed {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return nil
	}
	return atomicWriteFile(path, contents, mode)
}

func derivePrivateKey(key []byte) []byte {
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte("apimgr/security/pgp-private-key/v1"))
	return mac.Sum(nil)
}

func sealPrivateKey(plaintext string, secret []byte) ([]byte, error) {
	if len(secret) < 32 {
		return nil, fmt.Errorf("installation secret is too short")
	}
	block, err := aes.NewCipher(derivePrivateKey(secret))
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	sealed := gcm.Seal(nonce, nonce, []byte(plaintext), []byte("apimgr-pgp-private-v1"))
	return []byte("v1:" + base64.StdEncoding.EncodeToString(sealed)), nil
}

func openPrivateKey(sealed, secret []byte) (string, error) {
	if len(secret) < 32 || !bytes.HasPrefix(sealed, []byte("v1:")) {
		return "", fmt.Errorf("invalid encrypted private key format")
	}
	data, err := base64.StdEncoding.DecodeString(string(sealed[3:]))
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(derivePrivateKey(secret))
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	if len(data) < gcm.NonceSize()+gcm.Overhead() {
		return "", fmt.Errorf("encrypted private key is truncated")
	}
	plain, err := gcm.Open(nil, data[:gcm.NonceSize()], data[gcm.NonceSize():], []byte("apimgr-pgp-private-v1"))
	if err != nil {
		return "", err
	}
	return string(plain), nil
}

func atomicWriteFile(path string, data []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".pgp-*.tmp")
	if err != nil {
		return err
	}
	tmp := file.Name()
	defer func() { _ = os.Remove(tmp) }()
	if err := file.Chmod(mode); err != nil {
		_ = file.Close()
		return err
	}
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return replaceFileAtomically(tmp, path)
}

func loadState() (persistedState, error) {
	_, _, _, path := KeyPaths()
	data, err := os.ReadFile(path)
	if err != nil {
		return persistedState{}, err
	}
	var state persistedState
	if err := json.Unmarshal(data, &state); err != nil {
		return persistedState{}, fmt.Errorf("decode keyserver state: %w", err)
	}
	if state.Version != 1 {
		return persistedState{}, fmt.Errorf("unsupported keyserver state version %d", state.Version)
	}
	return state, nil
}

func saveState(state persistedState) error {
	_, _, _, path := KeyPaths()
	state.Version = 1
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	return atomicWriteFile(path, append(data, '\n'), 0600)
}

func validateHost(host string) error {
	if strings.ContainsAny(host, "/?#@\\") || strings.Contains(host, "://") {
		return fmt.Errorf("expected a hostname without scheme, path, or credentials")
	}
	if net.ParseIP(host) != nil || strings.Contains(host, ":") {
		return fmt.Errorf("expected a DNS hostname")
	}
	if _, _, err := net.SplitHostPort(host); err == nil {
		return fmt.Errorf("ports are not allowed")
	}
	return nil
}

type publishError struct {
	status    int
	retryable bool
	message   string
}

func (e publishError) Error() string { return e.message }
func isRetryable(err error) bool {
	var target publishError
	return errors.As(err, &target) && target.retryable
}

func submitKey(ctx context.Context, host, publicKey, fingerprint string) error {
	var request *http.Request
	var err error
	if host == "keys.openpgp.org" {
		body, _ := json.Marshal(map[string]string{"keytext": publicKey})
		request, err = http.NewRequestWithContext(ctx, http.MethodPost, "https://keys.openpgp.org/vks/v1/upload", bytes.NewReader(body))
		if err == nil {
			request.Header.Set("Content-Type", "application/json")
		}
	} else {
		form := url.Values{"keytext": []string{publicKey}}
		request, err = http.NewRequestWithContext(ctx, http.MethodPost, "https://"+host+"/pks/add", strings.NewReader(form.Encode()))
		if err == nil {
			request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		}
	}
	if err != nil {
		return publishError{retryable: false, message: err.Error()}
	}
	client := &http.Client{Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Do(request)
	if err != nil {
		return publishError{retryable: true, message: err.Error()}
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		message := fmt.Sprintf("keyserver returned HTTP %d", response.StatusCode)
		return publishError{status: response.StatusCode, retryable: response.StatusCode == 429 || response.StatusCode >= 500, message: message}
	}
	if host == "keys.openpgp.org" {
		var result struct {
			Fingerprint string `json:"key_fpr"`
			Status      string `json:"status"`
		}
		if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&result); err != nil {
			return publishError{retryable: false, message: "invalid keys.openpgp.org response"}
		}
		if !strings.EqualFold(result.Fingerprint, fingerprint) || result.Status == "" {
			return publishError{retryable: false, message: "keyserver response does not confirm the submitted fingerprint"}
		}
	}
	return nil
}

func serializePublicKey(entity *openpgp.Entity) (string, error) {
	var buffer bytes.Buffer
	writer, err := armor.Encode(&buffer, openpgp.PublicKeyType, nil)
	if err != nil {
		return "", err
	}
	if err := entity.Serialize(writer); err != nil {
		_ = writer.Close()
		return "", err
	}
	if err := writer.Close(); err != nil {
		return "", err
	}
	return buffer.String(), nil
}
