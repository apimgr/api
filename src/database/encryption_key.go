package database

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"sort"
	"time"
)

// StorePreviousEncryptionKey retains an outgoing AES key wrapped under the
// new key until the specified UTC instant.
func StorePreviousEncryptionKey(ctx context.Context, previousKey, currentKey []byte, until time.Time) error {
	if len(previousKey) != 32 || len(currentKey) != 32 || time.Until(until) <= 0 {
		return errors.New("previous encryption key, current key, or expiry is invalid")
	}
	block, err := aes.NewCipher(currentKey)
	if err != nil {
		return errors.New("current encryption key is invalid")
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return errors.New("cannot initialize current encryption key")
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return fmt.Errorf("generate previous-key nonce: %w", err)
	}
	wrapped := aead.Seal(nonce, nonce, previousKey, nil)
	queryCtx, cancel := context.WithTimeout(ctx, secretQueryTimeout)
	defer cancel()
	return withWriteTransaction(queryCtx, serverDB, func(tx *sql.Tx) error {
		entries := []struct{ name, value string }{
			{SecretEncryptionKeyPrevious, base64.StdEncoding.EncodeToString(wrapped)},
			{SecretEncryptionKeyPreviousUntil, until.UTC().Format(time.RFC3339Nano)},
		}
		for _, entry := range entries {
			if _, err := execTxContext(queryCtx, tx, `INSERT INTO app_secrets (name, value, created_at) VALUES (?, ?, ?)
			ON CONFLICT(name) DO UPDATE SET value = excluded.value`, entry.name, entry.value, time.Now().UTC().Unix()); err != nil {
				return fmt.Errorf("store previous encryption-key metadata: %w", err)
			}
		}
		return nil
	})
}

// StorePendingEncryptionKey keeps the not-yet-active key recoverable if the
// process stops between database preparation and the config-file rename.
func StorePendingEncryptionKey(ctx context.Context, currentKey, nextKey []byte, until time.Time) error {
	if len(currentKey) != 32 || len(nextKey) != 32 || !until.After(time.Now()) {
		return errors.New("current encryption key, next key, or expiry is invalid")
	}
	wrapped, err := wrapEncryptionKey(currentKey, nextKey)
	if err != nil {
		return err
	}
	return storeAppSecretValues(ctx, map[string]string{
		SecretEncryptionKeyPending:      base64.StdEncoding.EncodeToString(wrapped),
		SecretEncryptionKeyPendingUntil: until.UTC().Format(time.RFC3339Nano),
	})
}

// LoadPendingEncryptionKey recovers a key prepared while currentKey was
// active but not yet saved as the config's current key.
func LoadPendingEncryptionKey(ctx context.Context, currentKey []byte, now time.Time) ([]byte, time.Time, error) {
	wrappedText, err := GetAppSecretValue(ctx, SecretEncryptionKeyPending)
	if err != nil {
		return nil, time.Time{}, err
	}
	untilText, err := GetAppSecretValue(ctx, SecretEncryptionKeyPendingUntil)
	if err != nil {
		return nil, time.Time{}, err
	}
	until, err := time.Parse(time.RFC3339Nano, untilText)
	if err != nil || !now.Before(until) {
		return nil, time.Time{}, ErrSecretNotFound
	}
	wrapped, err := base64.StdEncoding.DecodeString(wrappedText)
	if err != nil {
		return nil, time.Time{}, errors.New("pending encryption-key wrapper is invalid")
	}
	next, err := unwrapEncryptionKey(currentKey, wrapped)
	if err != nil {
		return nil, time.Time{}, err
	}
	return next, until, nil
}

// DeletePendingEncryptionKey removes the config-transition recovery record.
func DeletePendingEncryptionKey(ctx context.Context) error {
	queryCtx, cancel := context.WithTimeout(ctx, secretQueryTimeout)
	defer cancel()
	if _, err := execContext(queryCtx, serverDB, `DELETE FROM app_secrets WHERE name IN (?, ?)`, SecretEncryptionKeyPending, SecretEncryptionKeyPendingUntil); err != nil {
		return fmt.Errorf("delete pending encryption-key record: %w", err)
	}
	return nil
}

func storeAppSecretValues(ctx context.Context, values map[string]string) error {
	queryCtx, cancel := context.WithTimeout(ctx, secretQueryTimeout)
	defer cancel()
	return withWriteTransaction(queryCtx, serverDB, func(tx *sql.Tx) error {
		names := make([]string, 0, len(values))
		for name := range values {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			if _, err := execTxContext(queryCtx, tx, `INSERT INTO app_secrets (name, value, created_at) VALUES (?, ?, ?)
				ON CONFLICT(name) DO UPDATE SET value = excluded.value`, name, values[name], time.Now().UTC().Unix()); err != nil {
				return fmt.Errorf("store app secret metadata %s: %w", name, err)
			}
		}
		return nil
	})
}

func wrapEncryptionKey(wrappingKey, key []byte) ([]byte, error) {
	block, err := aes.NewCipher(wrappingKey)
	if err != nil {
		return nil, errors.New("encryption-key wrapper key is invalid")
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, errors.New("cannot initialize encryption-key wrapper")
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("generate encryption-key wrapper nonce: %w", err)
	}
	return aead.Seal(nonce, nonce, key, nil), nil
}

func unwrapEncryptionKey(wrappingKey, wrapped []byte) ([]byte, error) {
	if len(wrappingKey) != 32 {
		return nil, errors.New("encryption-key wrapper key is invalid")
	}
	block, err := aes.NewCipher(wrappingKey)
	if err != nil {
		return nil, errors.New("encryption-key wrapper key is invalid")
	}
	aead, err := cipher.NewGCM(block)
	if err != nil || len(wrapped) < aead.NonceSize()+aead.Overhead() {
		return nil, errors.New("encryption-key wrapper is invalid")
	}
	nonce, ciphertext := wrapped[:aead.NonceSize()], wrapped[aead.NonceSize():]
	key, err := aead.Open(nil, nonce, ciphertext, nil)
	if err != nil || len(key) != 32 {
		return nil, errors.New("encryption-key wrapper cannot be opened")
	}
	return key, nil
}

// LoadPreviousEncryptionKey opens the retained key only while its grace
// period remains active.
func LoadPreviousEncryptionKey(ctx context.Context, currentKey []byte, now time.Time) ([]byte, error) {
	if len(currentKey) != 32 {
		return nil, errors.New("current encryption key must contain exactly 32 bytes")
	}
	wrappedText, err := GetAppSecretValue(ctx, SecretEncryptionKeyPrevious)
	if err != nil {
		return nil, err
	}
	untilText, err := GetAppSecretValue(ctx, SecretEncryptionKeyPreviousUntil)
	if err != nil {
		return nil, err
	}
	until, err := time.Parse(time.RFC3339Nano, untilText)
	if err != nil || !now.Before(until) {
		return nil, ErrSecretNotFound
	}
	wrapped, err := base64.StdEncoding.DecodeString(wrappedText)
	if err != nil {
		return nil, errors.New("previous encryption-key wrapper is invalid")
	}
	block, err := aes.NewCipher(currentKey)
	if err != nil {
		return nil, errors.New("current encryption key is invalid")
	}
	aead, err := cipher.NewGCM(block)
	if err != nil || len(wrapped) < aead.NonceSize()+aead.Overhead() {
		return nil, errors.New("previous encryption-key wrapper is invalid")
	}
	nonce, ciphertext := wrapped[:aead.NonceSize()], wrapped[aead.NonceSize():]
	previous, err := aead.Open(nil, nonce, ciphertext, nil)
	if err != nil || len(previous) != 32 {
		return nil, errors.New("previous encryption key cannot be opened")
	}
	return previous, nil
}

// CleanupPreviousEncryptionKey removes the wrapped key after its grace
// period has elapsed.
func CleanupPreviousEncryptionKey(ctx context.Context, now time.Time) error {
	queryCtx, cancel := context.WithTimeout(ctx, secretQueryTimeout)
	defer cancel()
	for _, record := range []struct{ expiry, value string }{
		{expiry: SecretEncryptionKeyPreviousUntil, value: SecretEncryptionKeyPrevious},
		{expiry: SecretEncryptionKeyPendingUntil, value: SecretEncryptionKeyPending},
	} {
		var untilText string
		err := queryRowContext(queryCtx, serverDB, `SELECT value FROM app_secrets WHERE name = ?`, record.expiry).Scan(&untilText)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return fmt.Errorf("load encryption-key expiry: %w", err)
		}
		until, err := time.Parse(time.RFC3339Nano, untilText)
		if err != nil {
			return errors.New("encryption-key expiry is invalid")
		}
		if !now.Before(until) {
			if _, err := execContext(queryCtx, serverDB, `DELETE FROM app_secrets WHERE name IN (?, ?)`, record.value, record.expiry); err != nil {
				return fmt.Errorf("delete expired encryption-key state: %w", err)
			}
		}
	}
	return nil
}
