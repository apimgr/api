package database

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"fmt"
	"time"
)

const captchaTimestampLayout = time.RFC3339Nano

// EnsureCaptchaSchema creates the CAPTCHA challenge table and expiry index.
// It is safe to call at every startup and supports both configured SQL drivers.
func EnsureCaptchaSchema(ctx context.Context) error {
	if _, err := execContext(ctx, serverDB, `CREATE TABLE IF NOT EXISTS captcha_challenges (
		id VARCHAR(32) PRIMARY KEY,
		answer_hash VARCHAR(64) NOT NULL,
		expires_at TIMESTAMP NOT NULL
	)`); err != nil {
		return fmt.Errorf("create captcha challenge table: %w", err)
	}
	if _, err := execContext(ctx, serverDB, `CREATE INDEX IF NOT EXISTS idx_captcha_challenges_expires ON captcha_challenges(expires_at)`); err != nil {
		return fmt.Errorf("create captcha expiry index: %w", err)
	}
	return nil
}

// StoreCaptchaChallenge persists only an HMAC of the answer. The random
// challenge ID is included in the MAC, so database disclosure does not make
// the small arithmetic answer space recoverable without the app secret.
func StoreCaptchaChallenge(ctx context.Context, id, answer string, expiresAt time.Time) error {
	secret, err := LoadSecret(ctx, SecretCookieSigningKey)
	if err != nil {
		return fmt.Errorf("load captcha signing key: %w", err)
	}
	hash := captchaAnswerHash(secret.Value, id, answer)
	_, err = execContext(ctx, serverDB,
		`INSERT INTO captcha_challenges (id, answer_hash, expires_at) VALUES (?, ?, ?)`,
		id, hash, expiresAt.UTC().Format(captchaTimestampLayout))
	if err != nil {
		return fmt.Errorf("store captcha challenge: %w", err)
	}
	return nil
}

// VerifyAndConsumeCaptcha atomically accepts a valid, unexpired challenge at
// most once. A wrong answer does not consume the challenge; successful use
// deletes it with a conditional DELETE to resolve concurrent submissions.
func VerifyAndConsumeCaptcha(ctx context.Context, id, answer string, now time.Time) (bool, error) {
	var stored string
	var expires string
	err := queryRowContext(ctx, serverDB,
		`SELECT answer_hash, expires_at FROM captcha_challenges WHERE id = ?`, id).Scan(&stored, &expires)
	if err != nil {
		if err == sql.ErrNoRows {
			return false, nil
		}
		return false, fmt.Errorf("load captcha challenge: %w", err)
	}
	expiresAt, err := time.Parse(captchaTimestampLayout, expires)
	if err != nil {
		return false, fmt.Errorf("parse captcha expiry: %w", err)
	}
	if !now.Before(expiresAt) {
		return false, nil
	}
	secret, err := LoadSecret(ctx, SecretCookieSigningKey)
	if err != nil {
		return false, fmt.Errorf("load captcha signing key: %w", err)
	}
	want := captchaAnswerHash(secret.Value, id, answer)
	valid := subtle.ConstantTimeCompare([]byte(stored), []byte(want))
	if valid != 1 && secret.PreviousValid(now) {
		previous := captchaAnswerHash(secret.Previous, id, answer)
		valid = subtle.ConstantTimeCompare([]byte(stored), []byte(previous))
	}
	if valid != 1 {
		return false, nil
	}
	result, err := execContext(ctx, serverDB,
		`DELETE FROM captcha_challenges WHERE id = ? AND answer_hash = ? AND expires_at > ?`,
		id, stored, now.UTC().Format(captchaTimestampLayout))
	if err != nil {
		return false, fmt.Errorf("consume captcha challenge: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("confirm captcha consumption: %w", err)
	}
	return count == 1, nil
}

// DeleteExpiredCaptchaChallenges removes expired or already-used challenges.
// It is called by the required token_cleanup scheduler task.
func DeleteExpiredCaptchaChallenges(ctx context.Context, now time.Time) (int64, error) {
	result, err := execContext(ctx, serverDB,
		`DELETE FROM captcha_challenges WHERE expires_at <= ?`, now.UTC().Format(captchaTimestampLayout))
	if err != nil {
		return 0, fmt.Errorf("delete expired captcha challenges: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("count expired captcha challenges: %w", err)
	}
	return count, nil
}

func captchaAnswerHash(key []byte, id, answer string) string {
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte(id + ":" + answer))
	return hex.EncodeToString(mac.Sum(nil))
}
