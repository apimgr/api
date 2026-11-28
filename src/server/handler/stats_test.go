package handler

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestRequestStatsCountsOnlySelectedRequestsAndKeepsActiveGauge(t *testing.T) {
	ResetRequestStats()
	t.Cleanup(ResetRequestStats)
	now := time.Now().UTC()

	done := RequestStartedCounted(false)
	if total, _, active := RequestStatsSnapshot(); total != 0 || active != 1 {
		t.Fatalf("excluded request snapshot = total %d, active %d; want 0, 1", total, active)
	}
	done()

	stats.begin(now, true)
	total, recent, active := stats.snapshot(now)
	require.EqualValues(t, 1, total)
	require.EqualValues(t, 1, recent)
	require.Equal(t, 1, active)
	stats.finish()
}

func TestRequestStatsSnapshotUsesRollingHours(t *testing.T) {
	var local requestStats
	now := time.Date(2026, time.January, 2, 12, 0, 0, 0, time.UTC)
	local.begin(now.Add(-25*time.Hour), true)
	local.finish()
	local.begin(now.Add(-23*time.Hour), true)
	local.finish()
	total, recent, active := local.snapshot(now)
	require.EqualValues(t, 2, total)
	require.EqualValues(t, 1, recent)
	require.Zero(t, active)
}
