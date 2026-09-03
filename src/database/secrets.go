package database

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"time"
)

// Secret names stored in the app_secrets table. These are the project-level
// cryptographic secrets described in AI.md PART 11 "Cryptographic Keys".
const (
	// SecretInstallationSecret is the root secret every other piece of
	// derived material hangs off. Rotated manually only.
	SecretInstallationSecret = "installation_secret"
	// SecretCookieSigningKey signs session cookies to detect tampering.
	SecretCookieSigningKey = "cookie_signing_key"
	// SecretCSRFTokenSecret is the HMAC base for CSRF double-submit tokens.
	SecretCSRFTokenSecret = "csrf_token_secret"
)

// secretByteLength is the generated length of every project secret.
const secretByteLength = 32

// cookieSigningKeyRotation is the automatic rotation interval for the cookie
// signing key.
const cookieSigningKeyRotation = 90 * 24 * time.Hour

// csrfTokenSecretRotation is the automatic rotation interval for the CSRF
// token secret.
const csrfTokenSecretRotation = 180 * 24 * time.Hour

// secretGraceWindow is how long a rotated-out secret stays valid so that
// in-flight material signed with it still verifies.
const secretGraceWindow = 7 * 24 * time.Hour

// secretQueryTimeout bounds every app_secrets query.
const secretQueryTimeout = 5 * time.Second

// ErrSecretNotFound is returned when a named secret has never been generated.
var ErrSecretNotFound = errors.New("secret not found")

// autoRotationIntervals maps the auto-rotated secrets to their intervals.
// Secrets absent from this map are manual-rotation only.
var autoRotationIntervals = map[string]time.Duration{
	SecretCookieSigningKey: cookieSigningKeyRotation,
	SecretCSRFTokenSecret:  csrfTokenSecretRotation,
}

// Secret is one stored project secret plus the previous value retained during
// its grace window.
type Secret struct {
	Name              string
	Value             []byte
	Previous          []byte
	PreviousExpiresAt time.Time
	CreatedAt         time.Time
	RotatedAt         time.Time
}

// PreviousValid reports whether the retained previous value is still inside
// its grace window and must therefore still be accepted for verification.
func (s Secret) PreviousValid(now time.Time) bool {
	return len(s.Previous) > 0 && !s.PreviousExpiresAt.IsZero() && now.Before(s.PreviousExpiresAt)
}

// generateSecretBytes returns cryptographically random secret material.
func generateSecretBytes() ([]byte, error) {
	buf := make([]byte, secretByteLength)
	if _, err := rand.Read(buf); err != nil {
		return nil, fmt.Errorf("generate secret: %w", err)
	}
	return buf, nil
}

// EnsureAppSecrets generates any missing project secret on first start and
// applies the automatic rotation schedule to the secrets that have one. It is
// safe to call on every startup.
func EnsureAppSecrets(ctx context.Context) error {
	names := []string{SecretInstallationSecret, SecretCookieSigningKey, SecretCSRFTokenSecret}
	for _, name := range names {
		secret, err := LoadSecret(ctx, name)
		if errors.Is(err, ErrSecretNotFound) {
			if _, err := generateSecret(ctx, name); err != nil {
				return err
			}
			continue
		}
		if err != nil {
			return err
		}
		interval, auto := autoRotationIntervals[name]
		if !auto {
			continue
		}
		age := secret.RotatedAt
		if age.IsZero() {
			age = secret.CreatedAt
		}
		if time.Since(age) < interval {
			continue
		}
		if err := RotateSecret(ctx, name); err != nil {
			return err
		}
	}
	return nil
}

// generateSecret creates and persists a brand new secret under name.
func generateSecret(ctx context.Context, name string) (Secret, error) {
	value, err := generateSecretBytes()
	if err != nil {
		return Secret{}, err
	}
	queryCtx, cancel := context.WithTimeout(ctx, secretQueryTimeout)
	defer cancel()
	now := time.Now().UTC()
	_, err = execContext(queryCtx, serverDB,
		`INSERT INTO app_secrets (name, value, created_at) VALUES (?, ?, ?)
		 ON CONFLICT(name) DO NOTHING`,
		name, base64.StdEncoding.EncodeToString(value), now.Unix())
	if err != nil {
		return Secret{}, fmt.Errorf("store secret %s: %w", name, err)
	}
	return LoadSecret(ctx, name)
}

// LoadSecret returns the named secret, or ErrSecretNotFound if it has never
// been generated. The raw value is never logged.
func LoadSecret(ctx context.Context, name string) (Secret, error) {
	queryCtx, cancel := context.WithTimeout(ctx, secretQueryTimeout)
	defer cancel()

	var (
		encoded         string
		previous        sql.NullString
		previousExpires sql.NullInt64
		createdAt       int64
		rotatedAt       sql.NullInt64
	)
	const loadSecretQuery = `SELECT value, previous_value, previous_expires_at, created_at, rotated_at
		 FROM app_secrets WHERE name = ?`

	row := queryRowContext(queryCtx, serverDB, loadSecretQuery, name)
	if err := row.Scan(&encoded, &previous, &previousExpires, &createdAt, &rotatedAt); err != nil {
		recordQueryError(loadSecretQuery, err)
		if errors.Is(err, sql.ErrNoRows) {
			return Secret{}, ErrSecretNotFound
		}
		return Secret{}, fmt.Errorf("load secret %s: %w", name, err)
	}

	value, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return Secret{}, fmt.Errorf("decode secret %s: %w", name, err)
	}

	secret := Secret{
		Name:      name,
		Value:     value,
		CreatedAt: time.Unix(createdAt, 0).UTC(),
	}
	if previous.Valid && previous.String != "" {
		prev, err := base64.StdEncoding.DecodeString(previous.String)
		if err != nil {
			return Secret{}, fmt.Errorf("decode previous secret %s: %w", name, err)
		}
		secret.Previous = prev
	}
	if previousExpires.Valid {
		secret.PreviousExpiresAt = time.Unix(previousExpires.Int64, 0).UTC()
	}
	if rotatedAt.Valid {
		secret.RotatedAt = time.Unix(rotatedAt.Int64, 0).UTC()
	}
	return secret, nil
}

// RotateSecret replaces the named secret with fresh material, retaining the
// outgoing value for the grace window so in-flight signatures still verify.
func RotateSecret(ctx context.Context, name string) error {
	current, err := LoadSecret(ctx, name)
	if errors.Is(err, ErrSecretNotFound) {
		_, err = generateSecret(ctx, name)
		return err
	}
	if err != nil {
		return err
	}

	next, err := generateSecretBytes()
	if err != nil {
		return err
	}

	queryCtx, cancel := context.WithTimeout(ctx, secretQueryTimeout)
	defer cancel()
	now := time.Now().UTC()
	_, err = execContext(queryCtx, serverDB,
		`UPDATE app_secrets
		 SET value = ?, previous_value = ?, previous_expires_at = ?, rotated_at = ?
		 WHERE name = ?`,
		base64.StdEncoding.EncodeToString(next),
		base64.StdEncoding.EncodeToString(current.Value),
		now.Add(secretGraceWindow).Unix(),
		now.Unix(),
		name)
	if err != nil {
		return fmt.Errorf("rotate secret %s: %w", name, err)
	}
	return nil
}
