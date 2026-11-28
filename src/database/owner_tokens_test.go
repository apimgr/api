package database

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func newOwnerTokenTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	require.NoError(t, err)
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, createSchema(db))
	return db
}

func TestOwnerTokenIssueValidateAndReveal(t *testing.T) {
	ctx := context.Background()
	db := newOwnerTokenTestDB(t)
	key := []byte(strings.Repeat("k", 32))
	raw, owner, err := IssueOwnerToken(ctx, db, key)
	require.NoError(t, err)
	require.Regexp(t, `^own_[A-Za-z0-9]{32}$`, raw)

	resolved, err := ValidateOwnerToken(ctx, db, raw, "")
	require.NoError(t, err)
	require.Equal(t, owner.OwnerID, resolved.OwnerID)
	require.Equal(t, "primary", resolved.Kind)
	require.Equal(t, "global", resolved.Scope)

	revealed, err := RevealOwnerToken(ctx, db, owner.OwnerID, owner.TokenID, key)
	require.NoError(t, err)
	require.Equal(t, raw, revealed)

	items, err := ListOwnerTokens(ctx, db, owner.OwnerID)
	require.NoError(t, err)
	require.Len(t, items, 1)
	require.Equal(t, raw[len(raw)-4:], items[0].Last4)
}

func TestLinkedOwnerTokenScopePersistsAndDefaultsSafely(t *testing.T) {
	ctx := context.Background()
	db := newOwnerTokenTestDB(t)
	key := []byte(strings.Repeat("k", 32))
	_, owner, err := IssueOwnerToken(ctx, db, key)
	require.NoError(t, err)

	readToken, read, err := CreateLinkedOwnerToken(ctx, db, key, owner.OwnerID, "read-only", nil, 10, "read")
	require.NoError(t, err)
	require.Equal(t, "read", read.Scope)
	resolved, err := ValidateOwnerToken(ctx, db, readToken, "")
	require.NoError(t, err)
	require.Equal(t, "read", resolved.Scope)

	_, defaultScope, err := CreateLinkedOwnerToken(ctx, db, key, owner.OwnerID, "default-scope", nil, 10)
	require.NoError(t, err)
	require.Equal(t, "read-write", defaultScope.Scope)

	items, err := ListOwnerTokens(ctx, db, owner.OwnerID)
	require.NoError(t, err)
	for _, item := range items {
		if item.ID == read.ID {
			require.Equal(t, "read", item.Scope)
		}
		if item.ID == defaultScope.ID {
			require.Equal(t, "read-write", item.Scope)
		}
	}
	_, _, err = CreateLinkedOwnerToken(ctx, db, key, owner.OwnerID, "invalid-scope", nil, 10, "global")
	require.ErrorIs(t, err, ErrLinkedTokenValidation)
}

func TestActiveOwnerTokenCounts(t *testing.T) {
	ctx := context.Background()
	db := newOwnerTokenTestDB(t)
	key := []byte(strings.Repeat("k", 32))
	_, primary, err := IssueOwnerToken(ctx, db, key)
	require.NoError(t, err)
	primaryID, err := uuid.Parse(primary.TokenID)
	require.NoError(t, err)
	require.Equal(t, uuid.Version(7), primaryID.Version())
	_, linked, err := CreateLinkedOwnerToken(ctx, db, key, primary.OwnerID, "worker", nil, 10)
	require.NoError(t, err)
	linkedID, err := uuid.Parse(linked.ID)
	require.NoError(t, err)
	require.Equal(t, uuid.Version(7), linkedID.Version())

	primaryCount, linkedCount, err := ActiveOwnerTokenCounts(ctx, db)
	require.NoError(t, err)
	require.Equal(t, float64(1), primaryCount)
	require.Equal(t, float64(1), linkedCount)

	require.NoError(t, RevokeLinkedOwnerToken(ctx, db, primary.OwnerID, linked.ID))
	primaryCount, linkedCount, err = ActiveOwnerTokenCounts(ctx, db)
	require.NoError(t, err)
	require.Equal(t, float64(1), primaryCount)
	require.Equal(t, float64(0), linkedCount)
}

func TestPatchLinkedOwnerTokenPreservesOmittedFields(t *testing.T) {
	ctx := context.Background()
	db := newOwnerTokenTestDB(t)
	key := []byte(strings.Repeat("k", 32))
	_, owner, err := IssueOwnerToken(ctx, db, key)
	require.NoError(t, err)
	expires := time.Now().UTC().Add(24 * time.Hour).Truncate(time.Second)
	_, linked, err := CreateLinkedOwnerToken(ctx, db, key, owner.OwnerID, "worker", &expires, 10)
	require.NoError(t, err)

	name := "renamed"
	require.NoError(t, PatchLinkedOwnerToken(ctx, db, owner.OwnerID, linked.ID, &name, nil, false))
	tokens, err := ListOwnerTokens(ctx, db, owner.OwnerID)
	require.NoError(t, err)
	var updated OwnerToken
	for _, token := range tokens {
		if token.ID == linked.ID {
			updated = token
		}
	}
	require.Equal(t, "renamed", updated.Name)
	require.Equal(t, expires, *updated.ExpiresAt)

	newExpiry := expires.Add(24 * time.Hour)
	require.NoError(t, PatchLinkedOwnerToken(ctx, db, owner.OwnerID, linked.ID, nil, &newExpiry, true))
	tokens, err = ListOwnerTokens(ctx, db, owner.OwnerID)
	require.NoError(t, err)
	for _, token := range tokens {
		if token.ID == linked.ID {
			updated = token
		}
	}
	require.Equal(t, "renamed", updated.Name)
	require.Equal(t, newExpiry, *updated.ExpiresAt)
}

func TestOwnerTokenUsageFlushCoalescesOwnerActivity(t *testing.T) {
	ctx := context.Background()
	db := newOwnerTokenTestDB(t)
	key := []byte(strings.Repeat("k", 32))
	_, primary, err := IssueOwnerToken(ctx, db, key)
	require.NoError(t, err)
	_, linked, err := CreateLinkedOwnerToken(ctx, db, key, primary.OwnerID, "worker", nil, 10)
	require.NoError(t, err)

	first := time.Now().UTC().Truncate(time.Second)
	second := first.Add(5 * time.Second)
	usage := &ownerTokenUsageCache{
		db:   db,
		last: make(map[string]time.Time),
		pending: map[string]ownerTokenUsageItem{
			primary.TokenID: {ownerID: primary.OwnerID, usedAt: first},
			linked.ID:       {ownerID: primary.OwnerID, usedAt: second},
		},
	}
	require.NoError(t, usage.flush(ctx))
	require.Empty(t, usage.pending)

	var primaryUsed, linkedUsed, ownerSeen int64
	require.NoError(t, db.QueryRowContext(ctx, `SELECT last_used_at FROM api_tokens WHERE id = ?`, primary.TokenID).Scan(&primaryUsed))
	require.NoError(t, db.QueryRowContext(ctx, `SELECT last_used_at FROM api_tokens WHERE id = ?`, linked.ID).Scan(&linkedUsed))
	require.NoError(t, db.QueryRowContext(ctx, `SELECT last_seen_at FROM owners WHERE id = ?`, primary.OwnerID).Scan(&ownerSeen))
	require.Equal(t, first.Unix(), primaryUsed)
	require.Equal(t, second.Unix(), linkedUsed)
	require.Equal(t, second.Unix(), ownerSeen)
}

func TestOwnerTokenUsageCachePrunesToBound(t *testing.T) {
	now := time.Now()
	usage := ownerTokenUsageCache{last: make(map[string]time.Time)}
	for id := 0; id <= ownerTokenUsageMaxEntries; id++ {
		usage.last[fmt.Sprint(id)] = now
	}
	usage.pruneLocked(now)
	require.Len(t, usage.last, ownerTokenUsageMaxEntries)
}

func TestOwnerTokenOperatorAndLinkedLifecycle(t *testing.T) {
	ctx := context.Background()
	db := newOwnerTokenTestDB(t)
	key := []byte(strings.Repeat("e", 32))
	operator, err := ValidateOwnerToken(ctx, db, "operator-secret", "operator-secret")
	require.NoError(t, err)
	require.Equal(t, "operator", operator.Kind)

	primaryRaw, owner, err := IssueOwnerToken(ctx, db, key)
	require.NoError(t, err)
	rotated, err := RotateOwnerToken(ctx, db, key, owner.OwnerID, owner.TokenID)
	require.NoError(t, err)
	_, err = ValidateOwnerToken(ctx, db, primaryRaw, "")
	require.ErrorIs(t, err, ErrInvalidOwnerToken)
	resolvedPrimary, err := ValidateOwnerToken(ctx, db, rotated, "")
	require.NoError(t, err)
	require.Equal(t, "primary", resolvedPrimary.Kind)
	expires := time.Now().UTC().Add(time.Hour)
	linkedRaw, linked, err := CreateLinkedOwnerToken(ctx, db, key, owner.OwnerID, "automation", &expires, 1)
	require.NoError(t, err)
	resolved, err := ValidateOwnerToken(ctx, db, linkedRaw, "")
	require.NoError(t, err)
	require.Equal(t, "linked", resolved.Kind)

	_, _, err = CreateLinkedOwnerToken(ctx, db, key, owner.OwnerID, "second", nil, 1)
	require.Error(t, err)
	require.NoError(t, RevokeLinkedOwnerToken(ctx, db, owner.OwnerID, linked.ID))
	_, err = ValidateOwnerToken(ctx, db, linkedRaw, "")
	require.ErrorIs(t, err, ErrInvalidOwnerToken)
}

func TestReencryptOwnerTokensMovesStoredCopiesToNewKey(t *testing.T) {
	ctx := context.Background()
	db := newOwnerTokenTestDB(t)
	oldKey := []byte(strings.Repeat("o", 32))
	newKey := []byte(strings.Repeat("n", 32))
	raw, owner, err := IssueOwnerToken(ctx, db, oldKey)
	require.NoError(t, err)
	require.NoError(t, ReencryptOwnerTokens(ctx, db, oldKey, newKey))
	revealed, err := RevealOwnerToken(ctx, db, owner.OwnerID, owner.TokenID, newKey)
	require.NoError(t, err)
	require.Equal(t, raw, revealed)
	_, err = RevealOwnerToken(ctx, db, owner.OwnerID, owner.TokenID, oldKey)
	require.Error(t, err)
}

func TestOwnerTokenEncryptionRejectsWrongKey(t *testing.T) {
	key := []byte(strings.Repeat("a", 32))
	encoded, err := encryptOwnerToken("own_test", key)
	require.NoError(t, err)
	_, err = decryptOwnerToken(encoded, []byte(strings.Repeat("b", 32)))
	require.Error(t, err)
	_, err = encryptOwnerToken("own_test", []byte("short"))
	require.Error(t, err)
}

func TestLegacyTokenTableIsExtendedWhenEmpty(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	require.NoError(t, err)
	db.SetMaxOpenConns(1)
	defer func() { _ = db.Close() }()
	_, err = db.Exec(`CREATE TABLE api_tokens (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		token_hash TEXT NOT NULL UNIQUE,
		token_prefix TEXT NOT NULL,
		resource_type TEXT NOT NULL,
		resource_id TEXT NOT NULL,
		created_at INTEGER NOT NULL DEFAULT (strftime('%s','now')),
		expires_at INTEGER,
		last_used_at INTEGER,
		revoked_at INTEGER,
		revoked_reason TEXT
	)`)
	require.NoError(t, err)
	require.NoError(t, createSchema(db))
	var ownerIDNotNull int
	require.NoError(t, db.QueryRow(`SELECT "notnull" FROM pragma_table_info('api_tokens') WHERE name = 'owner_id'`).Scan(&ownerIDNotNull))
	require.Zero(t, ownerIDNotNull)
	var idType string
	require.NoError(t, db.QueryRow(`SELECT type FROM pragma_table_info('api_tokens') WHERE name = 'id'`).Scan(&idType))
	require.Equal(t, "INTEGER", idType)
	var publicIDColumn int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('api_tokens') WHERE name = 'public_id'`).Scan(&publicIDColumn))
	require.Equal(t, 1, publicIDColumn)
}

func TestLegacyTokenRowsFailMigrationWithoutBeingDeleted(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	require.NoError(t, err)
	db.SetMaxOpenConns(1)
	defer func() { _ = db.Close() }()
	_, err = db.Exec(`CREATE TABLE api_tokens (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		token_hash TEXT NOT NULL UNIQUE,
		token_prefix TEXT NOT NULL,
		resource_type TEXT NOT NULL,
		resource_id TEXT NOT NULL,
		created_at INTEGER NOT NULL DEFAULT (strftime('%s','now')),
		expires_at INTEGER,
		last_used_at INTEGER,
		revoked_at INTEGER,
		revoked_reason TEXT
	)`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO api_tokens (token_hash, token_prefix, resource_type, resource_id) VALUES ('hash', 'own_12345678', 'item', 'one')`)
	require.NoError(t, err)
	err = createSchema(db)
	require.Error(t, err)
	var count int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM api_tokens`).Scan(&count))
	require.Equal(t, 1, count)
}

func TestLegacyOwnerTokenPublicIDPreservesIntegerPrimaryKey(t *testing.T) {
	ctx := context.Background()
	db, err := sql.Open("sqlite", ":memory:")
	require.NoError(t, err)
	db.SetMaxOpenConns(1)
	defer func() { _ = db.Close() }()
	_, err = db.Exec(`CREATE TABLE owners (id TEXT PRIMARY KEY, created_at INTEGER NOT NULL, last_seen_at INTEGER NOT NULL);
		INSERT INTO owners (id, created_at, last_seen_at) VALUES ('owner', 1, 1);
		CREATE TABLE api_tokens (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			owner_id TEXT NOT NULL REFERENCES owners(id) ON DELETE CASCADE,
			kind TEXT NOT NULL,
			name TEXT,
			token_hash TEXT NOT NULL UNIQUE,
			token_prefix TEXT NOT NULL,
			token_last4 TEXT NOT NULL DEFAULT '',
			token_enc TEXT NOT NULL DEFAULT '',
			created_at INTEGER NOT NULL,
			rotated_at INTEGER,
			expires_at INTEGER,
			last_used_at INTEGER,
			revoked_at INTEGER,
			revoked_reason TEXT
		);
		INSERT INTO api_tokens (owner_id, kind, token_hash, token_prefix, token_last4, token_enc, created_at)
		VALUES ('owner', 'primary', 'hash', 'own_prefix', 'last', 'encrypted', 1);`)
	require.NoError(t, err)
	require.NoError(t, migrateOwnerTokenSchema(ctx, db, "sqlite"))
	var internalID int64
	var publicID, ownerID, kind, tokenHash, scope string
	require.NoError(t, db.QueryRow(`SELECT id, public_id, owner_id, kind, token_hash, scope FROM api_tokens`).Scan(&internalID, &publicID, &ownerID, &kind, &tokenHash, &scope))
	parsed, err := uuid.Parse(publicID)
	require.NoError(t, err)
	require.Equal(t, uuid.Version(7), parsed.Version())
	require.EqualValues(t, 1, internalID)
	require.Equal(t, "owner", ownerID)
	require.Equal(t, "primary", kind)
	require.Equal(t, "global", scope)
	require.Equal(t, "hash", tokenHash)
}

func TestLegacyOwnerTokenPublicIDSupportsExternalOperations(t *testing.T) {
	ctx := context.Background()
	db, err := sql.Open("sqlite", ":memory:")
	require.NoError(t, err)
	db.SetMaxOpenConns(1)
	defer func() { _ = db.Close() }()
	_, err = db.Exec(`CREATE TABLE owners (id TEXT PRIMARY KEY, created_at INTEGER NOT NULL, last_seen_at INTEGER NOT NULL);
		INSERT INTO owners (id, created_at, last_seen_at) VALUES ('owner', 1, 1);
		CREATE TABLE api_tokens (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			owner_id TEXT NOT NULL REFERENCES owners(id) ON DELETE CASCADE,
			kind TEXT NOT NULL,
			name TEXT,
			token_hash TEXT NOT NULL UNIQUE,
			token_prefix TEXT NOT NULL,
			token_last4 TEXT NOT NULL DEFAULT '',
			token_enc TEXT NOT NULL DEFAULT '',
			created_at INTEGER NOT NULL,
			rotated_at INTEGER,
			expires_at INTEGER,
			last_used_at INTEGER,
			revoked_at INTEGER,
			revoked_reason TEXT
		);`)
	require.NoError(t, err)
	require.NoError(t, migrateOwnerTokenSchema(ctx, db, "sqlite"))
	key := []byte(strings.Repeat("k", 32))
	raw, credential, err := IssueOwnerToken(ctx, db, key)
	require.NoError(t, err)
	var internalID int64
	require.NoError(t, db.QueryRow(`SELECT id FROM api_tokens WHERE public_id = ?`, credential.TokenID).Scan(&internalID))
	require.NotEqual(t, credential.TokenID, fmt.Sprint(internalID))
	validated, err := ValidateOwnerToken(ctx, db, raw, "")
	require.NoError(t, err)
	require.Equal(t, credential.TokenID, validated.TokenID)
	rotated, err := RotateOwnerToken(ctx, db, key, credential.OwnerID, credential.TokenID)
	require.NoError(t, err)
	validated, err = ValidateOwnerToken(ctx, db, rotated, "")
	require.NoError(t, err)
	require.Equal(t, credential.TokenID, validated.TokenID)
	linkedRaw, linked, err := CreateLinkedOwnerToken(ctx, db, key, credential.OwnerID, "legacy linked", nil, 5)
	require.NoError(t, err)
	linkedCredential, err := ValidateOwnerToken(ctx, db, linkedRaw, "")
	require.NoError(t, err)
	require.Equal(t, linked.ID, linkedCredential.TokenID)
	require.Equal(t, "read-write", linkedCredential.Scope)
	require.NoError(t, RevokeLinkedOwnerToken(ctx, db, credential.OwnerID, linked.ID))
	_, err = ValidateOwnerToken(ctx, db, linkedRaw, "")
	require.ErrorIs(t, err, ErrInvalidOwnerToken)
	require.NoError(t, RevokeOwner(ctx, db, credential.OwnerID))
}
