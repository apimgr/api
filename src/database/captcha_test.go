package database

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func captchaTestID(t *testing.T) string {
	t.Helper()
	value := make([]byte, 16)
	_, err := rand.Read(value)
	require.NoError(t, err)
	return hex.EncodeToString(value)
}

func TestCaptchaChallengeSingleUseAndHashOnly(t *testing.T) {
	ctx := context.Background()
	require.NoError(t, EnsureAppSecrets(ctx))
	require.NoError(t, EnsureCaptchaSchema(ctx))
	id := captchaTestID(t)
	answer := "17"
	require.NoError(t, StoreCaptchaChallenge(ctx, id, answer, time.Now().Add(10*time.Minute)))

	var stored string
	require.NoError(t, queryRowContext(ctx, serverDB, `SELECT answer_hash FROM captcha_challenges WHERE id = ?`, id).Scan(&stored))
	require.NotEqual(t, answer, stored)
	valid, err := VerifyAndConsumeCaptcha(ctx, id, "16", time.Now())
	require.NoError(t, err)
	require.False(t, valid)
	valid, err = VerifyAndConsumeCaptcha(ctx, id, answer, time.Now())
	require.NoError(t, err)
	require.True(t, valid)
	valid, err = VerifyAndConsumeCaptcha(ctx, id, answer, time.Now())
	require.NoError(t, err)
	require.False(t, valid)
}

func TestCaptchaChallengeExpiryCleanup(t *testing.T) {
	ctx := context.Background()
	require.NoError(t, EnsureAppSecrets(ctx))
	require.NoError(t, EnsureCaptchaSchema(ctx))
	id := captchaTestID(t)
	require.NoError(t, StoreCaptchaChallenge(ctx, id, "4", time.Now().Add(-time.Minute)))
	valid, err := VerifyAndConsumeCaptcha(ctx, id, "4", time.Now())
	require.NoError(t, err)
	require.False(t, valid)
	deleted, err := DeleteExpiredCaptchaChallenges(ctx, time.Now())
	require.NoError(t, err)
	require.GreaterOrEqual(t, deleted, int64(1))
}
