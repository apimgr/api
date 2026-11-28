package database

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestTemporaryIPBlockPersistsAndExpires(t *testing.T) {
	now := time.Date(2026, 11, 27, 12, 0, 0, 0, time.UTC)
	block := IPBlock{IP: "2001:db8::1", Reason: "request_flood", BlockedAt: now, ExpiresAt: now.Add(time.Hour), OffenseCount: 1201, AutoBlocked: true}
	require.NoError(t, SaveTemporaryIPBlock(context.Background(), block))

	active, err := GetActiveIPBlock(context.Background(), block.IP, now.Add(time.Minute))
	require.NoError(t, err)
	require.Equal(t, &block, active)
	expired, err := GetActiveIPBlock(context.Background(), block.IP, now.Add(time.Hour))
	require.NoError(t, err)
	require.Nil(t, expired)
	missing, err := GetActiveIPBlock(context.Background(), block.IP, now.Add(time.Hour))
	require.NoError(t, err)
	require.Nil(t, missing)
}

func TestTemporaryIPBlockCleanupExpiresAndReleasesAllowlistedAddress(t *testing.T) {
	now := time.Date(2026, 11, 27, 12, 0, 0, 0, time.UTC)
	blocks := []IPBlock{
		{IP: "198.51.100.4", Reason: "request_flood", BlockedAt: now.Add(-time.Hour), ExpiresAt: now.Add(time.Hour), OffenseCount: 20, AutoBlocked: true},
		{IP: "198.51.100.5", Reason: "request_flood", BlockedAt: now.Add(-2 * time.Hour), ExpiresAt: now.Add(-time.Second), OffenseCount: 20, AutoBlocked: true},
		{IP: "198.51.100.6", Reason: "request_flood", BlockedAt: now.Add(-time.Hour), ExpiresAt: now.Add(time.Hour), OffenseCount: 20, AutoBlocked: true},
	}
	for _, block := range blocks {
		require.NoError(t, SaveTemporaryIPBlock(context.Background(), block))
	}
	removed, err := CleanupTemporaryIPBlocks(context.Background(), now, []string{"198.51.100.4/32"})
	require.NoError(t, err)
	require.Len(t, removed, 2)
	for _, block := range removed {
		if block.IP == "198.51.100.4" || block.IP == "198.51.100.5" {
			continue
		}
		t.Fatalf("unexpected released block: %s", block.IP)
	}
	allowed, err := GetActiveIPBlock(context.Background(), "198.51.100.4", now)
	require.NoError(t, err)
	require.Nil(t, allowed)
	expired, err := GetActiveIPBlock(context.Background(), "198.51.100.5", now)
	require.NoError(t, err)
	require.Nil(t, expired)
	active, err := GetActiveIPBlock(context.Background(), "198.51.100.6", now)
	require.NoError(t, err)
	require.NotNil(t, active)
}
