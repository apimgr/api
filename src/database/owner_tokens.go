package database

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/google/uuid"
)

var (
	ErrInvalidOwnerToken       = errors.New("invalid owner token")
	ErrLinkedTokenValidation   = errors.New("invalid linked token name or expiry")
	ErrLinkedTokenNameConflict = errors.New("linked token name must be unique for this owner")
	ErrLinkedTokenLimit        = errors.New("owner has reached the maximum active linked tokens")
)

const ownerTokenAlphabet = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"

type OwnerToken struct {
	ID        string     `json:"id"`
	OwnerID   string     `json:"-"`
	Kind      string     `json:"kind"`
	Name      string     `json:"name,omitempty"`
	Prefix    string     `json:"prefix"`
	Last4     string     `json:"last4"`
	CreatedAt time.Time  `json:"created_at"`
	ExpiresAt *time.Time `json:"expires_at"`
	LastUsed  *time.Time `json:"last_used_at"`
	RevokedAt *time.Time `json:"revoked_at"`
	Status    string     `json:"status"`
}

type OwnerCredential struct {
	OwnerID string
	TokenID string
	Kind    string
	Prefix  string
}

// ActiveOwnerTokenCounts returns unexpired, non-revoked primary and linked
// token counts for the owner-token metrics collector.
func ActiveOwnerTokenCounts(ctx context.Context, db *sql.DB) (primary, linked float64, err error) {
	if db == nil {
		return 0, 0, errors.New("owner token database is unavailable")
	}
	query := fmt.Sprintf(`SELECT kind, COUNT(*) FROM %s
		WHERE revoked_at IS NULL AND (expires_at IS NULL OR expires_at > ?) GROUP BY kind`, TableName("api_tokens"))
	rows, err := queryContext(ctx, db, query, time.Now().Unix())
	if err != nil {
		return 0, 0, err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var kind string
		var count int64
		if err := rows.Scan(&kind, &count); err != nil {
			return 0, 0, err
		}
		switch kind {
		case "primary":
			primary = float64(count)
		case "linked":
			linked = float64(count)
		}
	}
	return primary, linked, rows.Err()
}

// IssueOwnerToken creates an anonymous owner and primary bearer credential.
func IssueOwnerToken(ctx context.Context, db *sql.DB, encryptionKey []byte) (string, OwnerCredential, error) {
	if db == nil {
		return "", OwnerCredential{}, errors.New("owner token database is unavailable")
	}
	token, err := newOwnerToken()
	if err != nil {
		return "", OwnerCredential{}, err
	}
	encoded, err := encryptOwnerToken(token, encryptionKey)
	if err != nil {
		return "", OwnerCredential{}, err
	}
	ownerID, err := randomOwnerID()
	if err != nil {
		return "", OwnerCredential{}, err
	}
	var id string
	err = withWriteTransaction(ctx, db, func(tx *sql.Tx) error {
		if _, err := execTxContext(ctx, tx, `INSERT INTO owners (id) VALUES (?)`, ownerID); err != nil {
			return err
		}
		var insertErr error
		id, insertErr = insertOwnerToken(ctx, tx, ownerID, "primary", "", token, encoded, nil)
		return insertErr
	})
	if err != nil {
		return "", OwnerCredential{}, err
	}
	return token, OwnerCredential{OwnerID: ownerID, TokenID: id, Kind: "primary", Prefix: tokenPrefix(token)}, nil
}

// ValidateOwnerToken resolves a token or the operator credential. Operator
// comparison uses constant-time SHA-256 hashes and does not require the DB.
func ValidateOwnerToken(ctx context.Context, db *sql.DB, raw, operatorToken string) (OwnerCredential, error) {
	if raw == "" {
		return OwnerCredential{}, ErrInvalidOwnerToken
	}
	providedHash := sha256.Sum256([]byte(raw))
	if operatorToken != "" {
		operatorHash := sha256.Sum256([]byte(operatorToken))
		if subtle.ConstantTimeCompare(providedHash[:], operatorHash[:]) == 1 {
			return OwnerCredential{Kind: "operator", Prefix: "adm_"}, nil
		}
	}
	if db == nil {
		return OwnerCredential{}, ErrInvalidOwnerToken
	}
	hash := hex.EncodeToString(providedHash[:])
	var credential OwnerCredential
	var expires, lastUsed sql.NullInt64
	err := queryRowContext(ctx, db, `SELECT t.id, t.owner_id, t.kind, t.token_prefix, t.expires_at, t.last_used_at
		FROM api_tokens t WHERE t.token_hash = ? AND t.revoked_at IS NULL`, hash).Scan(
		&credential.TokenID, &credential.OwnerID, &credential.Kind, &credential.Prefix, &expires, &lastUsed)
	if errors.Is(err, sql.ErrNoRows) {
		return OwnerCredential{}, ErrInvalidOwnerToken
	}
	if err != nil {
		return OwnerCredential{}, err
	}
	if expires.Valid && expires.Int64 <= time.Now().Unix() {
		return OwnerCredential{}, ErrInvalidOwnerToken
	}
	if err := recordOwnerTokenUsage(ctx, db, credential.TokenID, credential.OwnerID, time.Now(), lastUsed); err != nil {
		return OwnerCredential{}, err
	}
	return credential, nil
}

// ListOwnerTokens returns metadata only; it never decrypts stored credentials.
func ListOwnerTokens(ctx context.Context, db *sql.DB, ownerID string) ([]OwnerToken, error) {
	rows, err := queryContext(ctx, db, `SELECT id, owner_id, kind, COALESCE(name,''), token_prefix, token_last4,
		created_at, expires_at, last_used_at, revoked_at FROM api_tokens WHERE owner_id = ? ORDER BY kind, created_at`, ownerID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var tokens []OwnerToken
	for rows.Next() {
		var token OwnerToken
		var created int64
		var expires, lastUsed, revoked sql.NullInt64
		if err := rows.Scan(&token.ID, &token.OwnerID, &token.Kind, &token.Name, &token.Prefix, &token.Last4, &created, &expires, &lastUsed, &revoked); err != nil {
			return nil, err
		}
		token.CreatedAt = time.Unix(created, 0).UTC()
		token.ExpiresAt = nullableOwnerTime(expires)
		token.LastUsed = nullableOwnerTime(lastUsed)
		token.RevokedAt = nullableOwnerTime(revoked)
		switch {
		case token.RevokedAt != nil:
			token.Status = "revoked"
		case token.ExpiresAt != nil && !token.ExpiresAt.After(time.Now()):
			token.Status = "expired"
		default:
			token.Status = "active"
		}
		tokens = append(tokens, token)
	}
	return tokens, rows.Err()
}

// CreateLinkedOwnerToken adds a named, independently revocable credential.
func CreateLinkedOwnerToken(ctx context.Context, db *sql.DB, encryptionKey []byte, ownerID, name string, expiresAt *time.Time, maxActive int) (string, OwnerToken, error) {
	name = strings.TrimSpace(name)
	if n := len([]rune(name)); n < 1 || n > 64 {
		return "", OwnerToken{}, fmt.Errorf("%w: name must be 1 to 64 characters", ErrLinkedTokenValidation)
	}
	if maxActive <= 0 {
		maxActive = 25
	}
	if expiresAt != nil && !expiresAt.After(time.Now()) {
		return "", OwnerToken{}, fmt.Errorf("%w: expiry must be in the future", ErrLinkedTokenValidation)
	}
	token, err := newOwnerToken()
	if err != nil {
		return "", OwnerToken{}, err
	}
	encoded, err := encryptOwnerToken(token, encryptionKey)
	if err != nil {
		return "", OwnerToken{}, err
	}
	var id string
	err = withWriteTransaction(ctx, db, func(tx *sql.Tx) error {
		var active int
		if err := queryRowTxContext(ctx, tx, `SELECT COUNT(*) FROM api_tokens WHERE owner_id = ? AND kind='linked' AND revoked_at IS NULL AND (expires_at IS NULL OR expires_at > strftime('%s','now'))`, ownerID).Scan(&active); err != nil {
			return err
		}
		if active >= maxActive {
			return fmt.Errorf("%w: %d", ErrLinkedTokenLimit, maxActive)
		}
		var duplicate int
		if err := queryRowTxContext(ctx, tx, `SELECT COUNT(*) FROM api_tokens WHERE owner_id = ? AND kind = 'linked' AND name = ?`, ownerID, name).Scan(&duplicate); err != nil {
			return err
		}
		if duplicate > 0 {
			return ErrLinkedTokenNameConflict
		}
		var expires any
		if expiresAt != nil {
			expires = expiresAt.Unix()
		}
		var insertErr error
		id, insertErr = insertOwnerToken(ctx, tx, ownerID, "linked", name, token, encoded, expires)
		return insertErr
	})
	if err != nil {
		return "", OwnerToken{}, err
	}
	return token, OwnerToken{ID: id, OwnerID: ownerID, Kind: "linked", Name: name, Prefix: tokenPrefix(token), Last4: token[len(token)-4:], CreatedAt: time.Now().UTC(), ExpiresAt: expiresAt}, nil
}

// RevealOwnerToken decrypts one token for an explicit reveal operation.
func RevealOwnerToken(ctx context.Context, db *sql.DB, ownerID, tokenID string, encryptionKeys ...[]byte) (string, error) {
	var encoded string
	err := queryRowContext(ctx, db, `SELECT token_enc FROM api_tokens WHERE owner_id = ? AND id = ? AND kind = 'primary' AND revoked_at IS NULL`, ownerID, tokenID).Scan(&encoded)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrInvalidOwnerToken
	}
	if err != nil {
		return "", err
	}
	plaintext, keyIndex, err := decryptOwnerTokenWithIndex(encoded, encryptionKeys...)
	if err != nil {
		return "", err
	}
	if keyIndex > 0 && len(encryptionKeys) > 0 && len(encryptionKeys[0]) == 32 {
		reencrypted, err := encryptOwnerToken(plaintext, encryptionKeys[0])
		if err != nil {
			return "", err
		}
		if _, err := execContext(ctx, db, `UPDATE api_tokens SET token_enc = ? WHERE owner_id = ? AND id = ?`, reencrypted, ownerID, tokenID); err != nil {
			return "", err
		}
	}
	return plaintext, nil
}

// RevealLinkedOwnerToken decrypts only an active linked token.
func RevealLinkedOwnerToken(ctx context.Context, db *sql.DB, ownerID, tokenID string, encryptionKeys ...[]byte) (string, error) {
	var encoded string
	err := queryRowContext(ctx, db, `SELECT token_enc FROM api_tokens WHERE owner_id = ? AND id = ? AND kind = 'linked' AND revoked_at IS NULL`, ownerID, tokenID).Scan(&encoded)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrInvalidOwnerToken
	}
	if err != nil {
		return "", err
	}
	plaintext, keyIndex, err := decryptOwnerTokenWithIndex(encoded, encryptionKeys...)
	if err != nil {
		return "", err
	}
	if keyIndex > 0 && len(encryptionKeys) > 0 && len(encryptionKeys[0]) == 32 {
		reencrypted, err := encryptOwnerToken(plaintext, encryptionKeys[0])
		if err != nil {
			return "", err
		}
		if _, err := execContext(ctx, db, `UPDATE api_tokens SET token_enc = ? WHERE owner_id = ? AND id = ? AND kind = 'linked'`, reencrypted, ownerID, tokenID); err != nil {
			return "", err
		}
	}
	return plaintext, nil
}

// RotateOwnerToken changes one token value in place while preserving owner
// identity and every other linked credential.
func RotateOwnerToken(ctx context.Context, db *sql.DB, encryptionKey []byte, ownerID, tokenID string) (string, error) {
	return rotateOwnerToken(ctx, db, encryptionKey, ownerID, tokenID, "primary")
}

// RotateLinkedOwnerToken rotates only a linked credential.
func RotateLinkedOwnerToken(ctx context.Context, db *sql.DB, encryptionKey []byte, ownerID, tokenID string) (string, error) {
	return rotateOwnerToken(ctx, db, encryptionKey, ownerID, tokenID, "linked")
}

func rotateOwnerToken(ctx context.Context, db *sql.DB, encryptionKey []byte, ownerID, tokenID, kind string) (string, error) {
	token, err := newOwnerToken()
	if err != nil {
		return "", err
	}
	encoded, err := encryptOwnerToken(token, encryptionKey)
	if err != nil {
		return "", err
	}
	result, err := execContext(ctx, db, `UPDATE api_tokens SET token_hash = ?, token_prefix = ?, token_last4 = ?, token_enc = ?, rotated_at = strftime('%s','now') WHERE owner_id = ? AND id = ? AND kind = ? AND revoked_at IS NULL`, hashOwnerToken(token), tokenPrefix(token), token[len(token)-4:], encoded, ownerID, tokenID, kind)
	if err != nil {
		return "", err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return "", err
	}
	if count != 1 {
		return "", ErrInvalidOwnerToken
	}
	return token, nil
}

// RevokeLinkedOwnerToken permanently revokes a linked credential.
func RevokeLinkedOwnerToken(ctx context.Context, db *sql.DB, ownerID, tokenID string) error {
	result, err := execContext(ctx, db, `UPDATE api_tokens SET revoked_at = strftime('%s','now'), revoked_reason = 'owner' WHERE owner_id = ? AND id = ? AND kind = 'linked' AND revoked_at IS NULL`, ownerID, tokenID)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count != 1 {
		return ErrInvalidOwnerToken
	}
	return nil
}

// UpdateLinkedOwnerToken changes a linked token's label and expiry only.
func UpdateLinkedOwnerToken(ctx context.Context, db *sql.DB, ownerID, tokenID, name string, expiresAt *time.Time) error {
	return PatchLinkedOwnerToken(ctx, db, ownerID, tokenID, &name, expiresAt, true)
}

// PatchLinkedOwnerToken updates only fields supplied by the API caller.
func PatchLinkedOwnerToken(ctx context.Context, db *sql.DB, ownerID, tokenID string, name *string, expiresAt *time.Time, updateExpiry bool) error {
	if name == nil && !updateExpiry {
		return fmt.Errorf("%w: at least one field is required", ErrLinkedTokenValidation)
	}
	var args []any
	var updates []string
	if name != nil {
		normalizedName := strings.TrimSpace(*name)
		if n := len([]rune(normalizedName)); n < 1 || n > 64 {
			return fmt.Errorf("%w: name must be 1 to 64 characters", ErrLinkedTokenValidation)
		}
		var duplicate int
		if err := queryRowContext(ctx, db, `SELECT COUNT(*) FROM api_tokens WHERE owner_id = ? AND kind = 'linked' AND name = ? AND id != ?`, ownerID, normalizedName, tokenID).Scan(&duplicate); err != nil {
			return err
		}
		if duplicate > 0 {
			return ErrLinkedTokenNameConflict
		}
		updates = append(updates, "name = ?")
		args = append(args, normalizedName)
	}
	if updateExpiry {
		var expires any
		if expiresAt != nil {
			if !expiresAt.After(time.Now()) {
				return fmt.Errorf("%w: expiry must be in the future", ErrLinkedTokenValidation)
			}
			expires = expiresAt.Unix()
		}
		updates = append(updates, "expires_at = ?")
		args = append(args, expires)
	}
	args = append(args, ownerID, tokenID)
	result, err := execContext(ctx, db, `UPDATE api_tokens SET `+strings.Join(updates, ", ")+` WHERE owner_id = ? AND id = ? AND kind = 'linked' AND revoked_at IS NULL`, args...)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count != 1 {
		return ErrInvalidOwnerToken
	}
	return nil
}

// RevokeOwner deletes an owner's identity and its linked token rows.
func RevokeOwner(ctx context.Context, db *sql.DB, ownerID string) error {
	return withWriteTransaction(ctx, db, func(tx *sql.Tx) error {
		if _, err := execTxContext(ctx, tx, `DELETE FROM api_tokens WHERE owner_id = ?`, ownerID); err != nil {
			return err
		}
		result, err := execTxContext(ctx, tx, `DELETE FROM owners WHERE id = ?`, ownerID)
		if err != nil {
			return err
		}
		count, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if count != 1 {
			return ErrInvalidOwnerToken
		}
		return nil
	})
}

// ReencryptOwnerTokens eagerly moves every recoverable token copy from the
// outgoing encryption key to the new key in one transaction.
func ReencryptOwnerTokens(ctx context.Context, db *sql.DB, previousKey, currentKey []byte) error {
	if len(previousKey) != 32 || len(currentKey) != 32 {
		return errors.New("owner token encryption keys must contain exactly 32 bytes")
	}
	return withWriteTransaction(ctx, db, func(tx *sql.Tx) error {
		rows, err := queryTxContext(ctx, tx, `SELECT id, token_enc FROM api_tokens WHERE token_enc != ''`)
		if err != nil {
			return err
		}
		type encryptedToken struct {
			id    string
			value string
		}
		var tokens []encryptedToken
		for rows.Next() {
			var token encryptedToken
			if err := rows.Scan(&token.id, &token.value); err != nil {
				_ = rows.Close()
				return err
			}
			tokens = append(tokens, token)
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return err
		}
		if err := rows.Close(); err != nil {
			return err
		}
		for index := range tokens {
			plain, err := decryptOwnerToken(tokens[index].value, previousKey, currentKey)
			if err != nil {
				return fmt.Errorf("decrypt owner token row %s: %w", tokens[index].id, err)
			}
			tokens[index].value, err = encryptOwnerToken(plain, currentKey)
			if err != nil {
				return err
			}
		}
		for _, token := range tokens {
			if _, err := execTxContext(ctx, tx, `UPDATE api_tokens SET token_enc = ? WHERE id = ?`, token.value, token.id); err != nil {
				return err
			}
		}
		return nil
	})
}

// PurgeIdleOwners deletes identities past the configured idle limit. This
// project defines no retained owner resources, so every idle owner is eligible.
func PurgeIdleOwners(ctx context.Context, db *sql.DB, idleDays int) (int64, error) {
	if db == nil || idleDays <= 0 {
		return 0, nil
	}
	cutoff := time.Now().UTC().AddDate(0, 0, -idleDays).Unix()
	var deleted int64
	err := withWriteTransaction(ctx, db, func(tx *sql.Tx) error {
		if _, err := execTxContext(ctx, tx, `DELETE FROM api_tokens WHERE owner_id IN (SELECT id FROM owners WHERE last_seen_at < ?)`, cutoff); err != nil {
			return err
		}
		result, err := execTxContext(ctx, tx, `DELETE FROM owners WHERE last_seen_at < ?`, cutoff)
		if err != nil {
			return err
		}
		count, err := result.RowsAffected()
		if err != nil {
			return err
		}
		deleted = count
		return nil
	})
	if err != nil {
		return 0, err
	}
	return deleted, nil
}

func insertOwnerToken(ctx context.Context, tx *sql.Tx, ownerID, kind, name, raw, encrypted string, expires any) (string, error) {
	var nullableName any
	if name != "" {
		nullableName = name
	}
	id, err := uuid.NewV7()
	if err != nil {
		return "", err
	}
	_, err = execTxContext(ctx, tx, `INSERT INTO api_tokens
		(id, owner_id, kind, name, token_hash, token_prefix, token_last4, token_enc, expires_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, id.String(), ownerID, kind, nullableName, hashOwnerToken(raw), tokenPrefix(raw), raw[len(raw)-4:], encrypted, expires)
	if err != nil {
		return "", err
	}
	return id.String(), nil
}

func newOwnerToken() (string, error) {
	const length = 32
	const cutoff = byte(256 - (256 % len(ownerTokenAlphabet)))
	buffer := make([]byte, length)
	result := make([]byte, 0, length)
	for len(result) < length {
		if _, err := io.ReadFull(rand.Reader, buffer); err != nil {
			return "", err
		}
		for _, value := range buffer {
			if value < cutoff {
				result = append(result, ownerTokenAlphabet[int(value)%len(ownerTokenAlphabet)])
				if len(result) == length {
					break
				}
			}
		}
	}
	return "own_" + string(result), nil
}

func randomOwnerID() (string, error) {
	var id [16]byte
	if _, err := io.ReadFull(rand.Reader, id[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(id[:]), nil
}

func tokenPrefix(raw string) string {
	if len(raw) < 12 {
		return raw
	}
	return raw[:12]
}

func hashOwnerToken(raw string) string {
	hash := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(hash[:])
}

func encryptOwnerToken(raw string, key []byte) (string, error) {
	if len(key) != 32 {
		return "", errors.New("owner token encryption key must contain exactly 32 bytes")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	ciphertext := gcm.Seal(nil, nonce, []byte(raw), nil)
	return base64.RawStdEncoding.EncodeToString(append(nonce, ciphertext...)), nil
}

func decryptOwnerToken(encoded string, keys ...[]byte) (string, error) {
	plaintext, _, err := decryptOwnerTokenWithIndex(encoded, keys...)
	return plaintext, err
}

func decryptOwnerTokenWithIndex(encoded string, keys ...[]byte) (string, int, error) {
	data, err := base64.RawStdEncoding.DecodeString(encoded)
	if err != nil {
		return "", -1, err
	}
	if len(keys) == 0 {
		return "", -1, errors.New("owner token encryption key is unavailable")
	}
	for index, key := range keys {
		if len(key) != 32 {
			continue
		}
		block, err := aes.NewCipher(key)
		if err != nil {
			continue
		}
		gcm, err := cipher.NewGCM(block)
		if err != nil || len(data) < gcm.NonceSize() {
			continue
		}
		nonce, ciphertext := data[:gcm.NonceSize()], data[gcm.NonceSize():]
		plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
		if err == nil {
			return string(plaintext), index, nil
		}
	}
	return "", -1, errors.New("unable to decrypt owner token with current or previous encryption key")
}

func nullableOwnerTime(value sql.NullInt64) *time.Time {
	if !value.Valid {
		return nil
	}
	result := time.Unix(value.Int64, 0).UTC()
	return &result
}
