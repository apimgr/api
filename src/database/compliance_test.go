package database

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestExportOwnerDataContainsOnlySelectedMetadata(t *testing.T) {
	ctx := context.Background()
	db := newOwnerTokenTestDB(t)
	raw, owner, err := IssueOwnerToken(ctx, db, []byte(strings.Repeat("k", 32)))
	require.NoError(t, err)

	export, err := ExportOwnerData(ctx, db, owner.Prefix)
	require.NoError(t, err)
	require.Equal(t, 1, export.Format)
	require.Equal(t, owner.OwnerID, export.Owner.ID)
	require.Len(t, export.Tokens, 1)
	require.Equal(t, owner.Prefix, export.Tokens[0].Prefix)
	require.Empty(t, export.Resources)

	encoded, err := json.Marshal(export)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), raw)
	require.NotContains(t, string(encoded), hashOwnerToken(raw))
	require.NotContains(t, string(encoded), raw[len(raw)-4:])
}

func TestDeleteOwnerByTokenPrefixRemovesOwnerAndAllTokens(t *testing.T) {
	ctx := context.Background()
	db := newOwnerTokenTestDB(t)
	key := []byte(strings.Repeat("k", 32))
	_, owner, err := IssueOwnerToken(ctx, db, key)
	require.NoError(t, err)
	_, _, err = CreateLinkedOwnerToken(ctx, db, key, owner.OwnerID, "linked", nil, 25)
	require.NoError(t, err)

	deletedOwner, tokenCount, resourceCount, err := DeleteOwnerByTokenPrefix(ctx, db, owner.Prefix)
	require.NoError(t, err)
	require.Equal(t, owner.OwnerID, deletedOwner)
	require.EqualValues(t, 2, tokenCount)
	require.Zero(t, resourceCount)

	var owners, tokens int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM owners`).Scan(&owners))
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM api_tokens`).Scan(&tokens))
	require.Zero(t, owners)
	require.Zero(t, tokens)
}

func TestGetComplianceSummaryCountsOnlyRecentComplianceEvents(t *testing.T) {
	ctx := context.Background()
	db := newOwnerTokenTestDB(t)
	now := time.Date(2026, 11, 27, 12, 0, 0, 0, time.UTC)
	_, _, err := IssueOwnerToken(ctx, db, []byte(strings.Repeat("k", 32)))
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO audit_log (timestamp, event) VALUES (?, ?), (?, ?), (?, ?)`,
		now.Add(-29*24*time.Hour).Format("2006-01-02T15:04:05.000Z"), "compliance.consent_updated",
		now.Add(-31*24*time.Hour).Format("2006-01-02T15:04:05.000Z"), "compliance.consent_updated",
		now.Format("2006-01-02T15:04:05.000Z"), "security.ip_blocked")
	require.NoError(t, err)

	summary, err := GetComplianceSummary(ctx, db, now)
	require.NoError(t, err)
	require.EqualValues(t, 1, summary.Owners)
	require.EqualValues(t, 1, summary.ActiveTokens)
	require.EqualValues(t, 1, summary.EventsLast30Days)
	require.Zero(t, summary.Resources)
	require.Zero(t, summary.OpenReports)
}

func TestChangedComplianceStandardsReturnsOnlyTransitions(t *testing.T) {
	changes := ChangedComplianceStandards(
		map[string]bool{"gdpr": false, "hipaa": true},
		map[string]bool{"gdpr": true, "hipaa": true, "ccpa": false},
	)
	require.Equal(t, []ComplianceStandardChange{{Name: "gdpr", Enabled: true}}, changes)
}
