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
	SecretCSRFTokenSecret            = "csrf_token_secret"
	SecretDNSValidatedAt             = "dns_validated_at"
	SecretDNSConfigFingerprint       = "dns_config_fingerprint"
	SecretEncryptionKeyPrevious      = "encryption_key_previous"
	SecretEncryptionKeyPreviousUntil = "encryption_key_previous_until"
	SecretEncryptionKeyPending       = "encryption_key_pending"
	SecretEncryptionKeyPendingUntil  = "encryption_key_pending_until"
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
	offlineReadOnly := CacheReadOnlyOffline()
	names := []string{SecretInstallationSecret, SecretCookieSigningKey, SecretCSRFTokenSecret}
	missing := make(map[string][]byte)
	for _, name := range names {
		_, err := LoadSecret(ctx, name)
		if errors.Is(err, ErrSecretNotFound) {
			if offlineReadOnly {
				return fmt.Errorf("required secret %s is missing from the fresh read-only libsql cache", name)
			}
			value, err := generateSecretBytes()
			if err != nil {
				return err
			}
			missing[name] = value
			continue
		}
		if err != nil {
			return err
		}
	}
	if len(missing) > 0 {
		queryCtx, cancel := context.WithTimeout(ctx, secretQueryTimeout)
		now := time.Now().UTC().Format(time.RFC3339Nano)
		err := withWriteTransaction(queryCtx, serverDB, func(tx *sql.Tx) error {
			for _, name := range names {
				value, needed := missing[name]
				if !needed {
					continue
				}
				if _, err := execTxContext(queryCtx, tx, `INSERT INTO app_secrets (name, value, updated_at) VALUES (?, ?, ?) ON CONFLICT(name) DO NOTHING`, name, base64.StdEncoding.EncodeToString(value), now); err != nil {
					return err
				}
				if _, err := execTxContext(queryCtx, tx, `INSERT INTO app_secrets (name, value, updated_at) VALUES (?, ?, ?) ON CONFLICT(name) DO NOTHING`, name+"_at", now, now); err != nil {
					return err
				}
			}
			return nil
		})
		cancel()
		if err != nil {
			return fmt.Errorf("initialize app secrets transaction: %w", err)
		}
	}
	for _, name := range names {
		secret, err := LoadSecret(ctx, name)
		if err != nil {
			return err
		}
		if len(secret.Value) != 32 {
			return fmt.Errorf("required secret %s in database has an invalid length", name)
		}
		if secret.PreviousValid(time.Now().UTC()) && len(secret.Previous) != 32 {
			return fmt.Errorf("previous secret %s in database has an invalid length", name)
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
		if offlineReadOnly {
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
	now := time.Now().UTC().Format(time.RFC3339Nano)
	err = withWriteTransaction(queryCtx, serverDB, func(tx *sql.Tx) error {
		if _, err := execTxContext(queryCtx, tx, `INSERT INTO app_secrets (name, value, updated_at) VALUES (?, ?, ?) ON CONFLICT(name) DO NOTHING`, name, base64.StdEncoding.EncodeToString(value), now); err != nil {
			return err
		}
		_, err := execTxContext(queryCtx, tx, `INSERT INTO app_secrets (name, value, updated_at) VALUES (?, ?, ?) ON CONFLICT(name) DO NOTHING`, name+"_at", now, now)
		return err
	})
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

	var encoded, updatedAt string
	const loadSecretQuery = `SELECT value, updated_at FROM app_secrets WHERE name = ?`

	row := queryRowContext(queryCtx, serverDB, loadSecretQuery, name)
	if err := row.Scan(&encoded, &updatedAt); err != nil {
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

	secret := Secret{Name: name, Value: value}
	if parsed, err := time.Parse(time.RFC3339Nano, updatedAt); err == nil {
		secret.CreatedAt = parsed.UTC()
	}
	if previousValue, err := GetAppSecretValue(ctx, name+"_previous"); err == nil && previousValue != "" {
		prev, err := base64.StdEncoding.DecodeString(previousValue)
		if err != nil {
			return Secret{}, fmt.Errorf("decode previous secret %s: %w", name, err)
		}
		secret.Previous = prev
	} else if err != nil && !errors.Is(err, ErrSecretNotFound) {
		return Secret{}, err
	}
	if expiry, err := GetAppSecretValue(ctx, name+"_previous_until"); err == nil {
		secret.PreviousExpiresAt, err = time.Parse(time.RFC3339Nano, expiry)
		if err != nil {
			return Secret{}, fmt.Errorf("decode previous secret expiry %s: %w", name, err)
		}
	} else if !errors.Is(err, ErrSecretNotFound) {
		return Secret{}, err
	}
	if rotatedAt, err := GetAppSecretValue(ctx, name+"_at"); err == nil {
		secret.RotatedAt, err = time.Parse(time.RFC3339Nano, rotatedAt)
		if err != nil {
			return Secret{}, fmt.Errorf("decode secret rotation time %s: %w", name, err)
		}
	} else if !errors.Is(err, ErrSecretNotFound) {
		return Secret{}, err
	}
	return secret, nil
}

// SetAppSecretValue stores a server-written app_secrets value that is not
// cryptographic key material and therefore is not base64 encoded.
func SetAppSecretValue(ctx context.Context, name, value string) error {
	queryCtx, cancel := context.WithTimeout(ctx, secretQueryTimeout)
	defer cancel()
	_, err := execContext(queryCtx, serverDB,
		`INSERT INTO app_secrets (name, value, updated_at) VALUES (?, ?, ?)
		 ON CONFLICT(name) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`,
		name, value, time.Now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		return fmt.Errorf("store app secret value %s: %w", name, err)
	}
	return nil
}

// GetAppSecretValue returns a server-written app_secrets value without
// interpreting it as encoded key material.
func GetAppSecretValue(ctx context.Context, name string) (string, error) {
	queryCtx, cancel := context.WithTimeout(ctx, secretQueryTimeout)
	defer cancel()
	const query = `SELECT value FROM app_secrets WHERE name = ?`
	var value string
	if err := queryRowContext(queryCtx, serverDB, query, name).Scan(&value); err != nil {
		recordQueryError(query, err)
		if errors.Is(err, sql.ErrNoRows) {
			return "", ErrSecretNotFound
		}
		return "", fmt.Errorf("load app secret value %s: %w", name, err)
	}
	return value, nil
}

// DeleteAppSecretValue removes a server-written app_secrets value.
func DeleteAppSecretValue(ctx context.Context, name string) error {
	queryCtx, cancel := context.WithTimeout(ctx, secretQueryTimeout)
	defer cancel()
	if _, err := execContext(queryCtx, serverDB, `DELETE FROM app_secrets WHERE name = ?`, name); err != nil {
		return fmt.Errorf("delete app secret value %s: %w", name, err)
	}
	return nil
}

// RotateSecret replaces the named secret with fresh material, retaining the
// outgoing value for the grace window so in-flight signatures still verify.
func RotateSecret(ctx context.Context, name string) error {
	return RotateSecretWithTransition(ctx, name, nil)
}

// RotateSecretWithTransition updates a secret and a dependent encrypted
// artifact as one coordinated operation. The transition runs before commit;
// rollback restores any filesystem artifact if the database commit fails.
func RotateSecretWithTransition(ctx context.Context, name string, transition func(oldValue, newValue []byte) (rollback func() error, err error)) error {
	current, err := LoadSecret(ctx, name)
	if errors.Is(err, ErrSecretNotFound) {
		if transition != nil {
			return fmt.Errorf("rotate secret %s: dependent artifact cannot be transitioned without the current secret", name)
		}
		_, err = generateSecret(ctx, name)
		return err
	}
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	if current.PreviousValid(now) {
		return fmt.Errorf("rotate secret %s: a previous value is still inside its grace window", name)
	}

	next, err := generateSecretBytes()
	if err != nil {
		return err
	}

	queryCtx, cancel := context.WithTimeout(ctx, secretQueryTimeout)
	defer cancel()
	if err := withWriteTransaction(queryCtx, serverDB, func(tx *sql.Tx) error {
		for _, row := range []struct{ name, value string }{
			{name + "_previous", base64.StdEncoding.EncodeToString(current.Value)},
			{name + "_previous_until", now.Add(secretGraceWindow).UTC().Format(time.RFC3339Nano)},
		} {
			if _, err := execTxContext(queryCtx, tx, `INSERT INTO app_secrets (name, value, updated_at) VALUES (?, ?, ?) ON CONFLICT(name) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`, row.name, row.value, now.Format(time.RFC3339Nano)); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		return fmt.Errorf("store previous secret %s: %w", name, err)
	}
	if err := withWriteTransaction(queryCtx, serverDB, func(tx *sql.Tx) error {
		result, err := execTxContext(queryCtx, tx,
			`UPDATE app_secrets SET value = ?, updated_at = ? WHERE name = ?`,
			base64.StdEncoding.EncodeToString(next), now.Format(time.RFC3339Nano), name)
		if err != nil {
			return err
		}
		updated, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if updated != 1 {
			return fmt.Errorf("secret row changed during rotation")
		}
		_, err = execTxContext(queryCtx, tx, `INSERT INTO app_secrets (name, value, updated_at) VALUES (?, ?, ?) ON CONFLICT(name) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`, name+"_at", now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano))
		return err
	}); err != nil {
		clearErr := withWriteTransaction(queryCtx, serverDB, func(tx *sql.Tx) error {
			_, clearErr := execTxContext(queryCtx, tx, `DELETE FROM app_secrets WHERE name IN (?, ?)`, name+"_previous", name+"_previous_until")
			return clearErr
		})
		return fmt.Errorf("persist rotated secret %s: %w", name, errors.Join(err, clearErr))
	}
	var rollback func() error
	if transition != nil {
		rollback, err = transition(current.Value, next)
		if err != nil {
			if rollback != nil {
				if rollbackErr := rollback(); rollbackErr != nil {
					return fmt.Errorf("rotate secret %s: transition failed: %v (rollback failed: %w)", name, err, rollbackErr)
				}
			}
			return fmt.Errorf("rotate secret %s: transition dependent artifact; rotation will resume at startup: %w", name, err)
		}
	}
	return nil
}
