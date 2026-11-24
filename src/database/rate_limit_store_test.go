package database

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestRateLimitStoreUsesWeightedTwoWindowCounter(t *testing.T) {
	ctx := context.Background()
	require.NoError(t, EnsureRateLimitSchema(ctx))
	now := time.Now().UTC().Truncate(time.Second)
	key := "weighted:" + t.Name()
	store := RateLimitStore{}
	count, _, err := store.SlidingWindow(ctx, key, now, time.Minute)
	require.NoError(t, err)
	require.Equal(t, 1, count)
	count, _, err = store.SlidingWindow(ctx, key, now.Add(time.Second), time.Minute)
	require.NoError(t, err)
	require.Equal(t, 2, count)
	counter, found, err := store.LoadCounter(ctx, key)
	require.NoError(t, err)
	require.True(t, found)
	require.EqualValues(t, 2, counter.Count)
	require.Zero(t, counter.PrevCount)
	count, _, err = store.CountWindow(ctx, key, now.Add(time.Minute), time.Minute)
	require.NoError(t, err)
	require.Equal(t, 2, count)
	count, _, err = store.CountWindow(ctx, key, now.Add(90*time.Second), time.Minute)
	require.NoError(t, err)
	require.Equal(t, 1, count)
	count, _, err = store.CountWindow(ctx, key, now.Add(2*time.Minute), time.Minute)
	require.NoError(t, err)
	require.Zero(t, count)
}

func TestRateLimitStoreAdvancesAfterSkippedWindows(t *testing.T) {
	ctx := context.Background()
	require.NoError(t, EnsureRateLimitSchema(ctx))
	now := time.Now().UTC().Truncate(time.Second)
	key := "advance:" + t.Name()
	store := RateLimitStore{}
	_, _, err := store.SlidingWindow(ctx, key, now, time.Minute)
	require.NoError(t, err)
	count, _, err := store.SlidingWindow(ctx, key, now.Add(3*time.Minute), time.Minute)
	require.NoError(t, err)
	require.Equal(t, 1, count)
	counter, found, err := store.LoadCounter(ctx, key)
	require.NoError(t, err)
	require.True(t, found)
	require.EqualValues(t, 1, counter.Count)
	require.Zero(t, counter.PrevCount)
	require.Equal(t, now.Add(3*time.Minute), counter.WindowStart)
}

func TestRateLimitStoreRecordBatchPersistsSnapshotsAtomically(t *testing.T) {
	ctx := context.Background()
	require.NoError(t, EnsureRateLimitSchema(ctx))
	now := time.Now().UTC().Truncate(time.Second)
	keyA := "batch-a:" + t.Name()
	keyB := "batch-b:" + t.Name()
	counters := []RateLimitCounter{
		{Key: keyA, Count: 3, PrevCount: 2, WindowStart: now, UpdatedAt: now},
		{Key: keyB, Count: 7, PrevCount: 0, WindowStart: now, UpdatedAt: now},
	}
	store := RateLimitStore{}
	require.NoError(t, store.RecordBatch(ctx, counters))
	for _, expected := range counters {
		actual, found, err := store.LoadCounter(ctx, expected.Key)
		require.NoError(t, err)
		require.True(t, found)
		require.Equal(t, expected, actual)
	}
	err := store.RecordBatch(ctx, []RateLimitCounter{
		{Key: "partial:" + t.Name(), Count: 1, WindowStart: now, UpdatedAt: now},
		{Key: "", Count: 1, WindowStart: now, UpdatedAt: now},
	})
	require.Error(t, err)
	_, found, err := store.LoadCounter(ctx, "partial:"+t.Name())
	require.NoError(t, err)
	require.False(t, found)
}
