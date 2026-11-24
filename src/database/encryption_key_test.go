package database

import (
	"context"
	"encoding/base64"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestPreviousEncryptionKeyIsWrappedAndRetiredAfterGrace(t *testing.T) {
	ctx := context.Background()
	previous := []byte("0123456789abcdef0123456789abcdef")
	current := []byte("abcdef0123456789abcdef0123456789")
	now := time.Now().UTC()
	until := now.Add(time.Hour)
	require.NoError(t, StorePreviousEncryptionKey(ctx, previous, current, until))

	wrapped, err := GetAppSecretValue(ctx, SecretEncryptionKeyPrevious)
	require.NoError(t, err)
	require.NotContains(t, wrapped, base64.StdEncoding.EncodeToString(previous))
	loaded, err := LoadPreviousEncryptionKey(ctx, current, now)
	require.NoError(t, err)
	require.Equal(t, previous, loaded)
	require.NoError(t, CleanupPreviousEncryptionKey(ctx, now))

	require.NoError(t, CleanupPreviousEncryptionKey(ctx, until.Add(time.Second)))
	_, err = LoadPreviousEncryptionKey(ctx, current, until.Add(time.Second))
	require.ErrorIs(t, err, ErrSecretNotFound)
	require.NoError(t, DeleteAppSecretValue(ctx, SecretEncryptionKeyPrevious))
	require.NoError(t, DeleteAppSecretValue(ctx, SecretEncryptionKeyPreviousUntil))
}

func TestLoadPreviousEncryptionKeyRejectsWrongCurrentKey(t *testing.T) {
	ctx := context.Background()
	previous := []byte("0123456789abcdef0123456789abcdef")
	current := []byte("abcdef0123456789abcdef0123456789")
	wrong := []byte("fedcba9876543210fedcba9876543210")
	require.NoError(t, StorePreviousEncryptionKey(ctx, previous, current, time.Now().UTC().Add(time.Hour)))
	_, err := LoadPreviousEncryptionKey(ctx, wrong, time.Now().UTC())
	require.Error(t, err)
	require.False(t, errors.Is(err, ErrSecretNotFound))
	require.NoError(t, DeleteAppSecretValue(ctx, SecretEncryptionKeyPrevious))
	require.NoError(t, DeleteAppSecretValue(ctx, SecretEncryptionKeyPreviousUntil))
}

func TestPendingEncryptionKeyCanRecoverInterruptedConfigTransition(t *testing.T) {
	ctx := context.Background()
	current := []byte("0123456789abcdef0123456789abcdef")
	next := []byte("abcdef0123456789abcdef0123456789")
	until := time.Now().UTC().Add(time.Hour)
	require.NoError(t, StorePendingEncryptionKey(ctx, current, next, until))
	recovered, expiry, err := LoadPendingEncryptionKey(ctx, current, time.Now().UTC())
	require.NoError(t, err)
	require.Equal(t, next, recovered)
	require.WithinDuration(t, until, expiry, time.Second)
	require.NoError(t, DeletePendingEncryptionKey(ctx))
	_, _, err = LoadPendingEncryptionKey(ctx, current, time.Now().UTC())
	require.ErrorIs(t, err, ErrSecretNotFound)
}
