package database

import (
	"context"
	"database/sql"
	"testing"

	"github.com/stretchr/testify/require"
)

func requestStatsTestDB(t *testing.T) {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	require.NoError(t, err)
	db.SetMaxOpenConns(1)

	mu.Lock()
	previousDB, previousStore, previousDriver := serverDB, serverStore, serverDriver
	serverDriver = "sqlite"
	mu.Unlock()
	require.NoError(t, createSchema(db))
	mu.Lock()
	serverDB, serverStore = db, nil
	mu.Unlock()
	t.Cleanup(func() {
		mu.Lock()
		serverDB, serverStore, serverDriver = previousDB, previousStore, previousDriver
		mu.Unlock()
		require.NoError(t, db.Close())
	})
}

func TestRequestStatsPersistLifetimeAndHourlyBuckets(t *testing.T) {
	requestStatsTestDB(t)
	ctx := context.Background()
	oldestHour := int64(100)

	require.NoError(t, RecordRequestStats(ctx, 3, []RequestStatsBucket{{Hour: 123, Count: 2}, {Hour: 124, Count: 1}}, oldestHour))
	require.NoError(t, RecordRequestStats(ctx, 2, []RequestStatsBucket{{Hour: 124, Count: 2}, {Hour: 99, Count: 1}}, oldestHour))

	snapshot, err := LoadRequestStats(ctx, oldestHour)
	require.NoError(t, err)
	require.EqualValues(t, 5, snapshot.Total)
	require.Equal(t, []RequestStatsBucket{{Hour: 123, Count: 2}, {Hour: 124, Count: 3}}, snapshot.Buckets)
}

func TestRequestStatsRejectInvalidBatchWithoutPartialWrite(t *testing.T) {
	requestStatsTestDB(t)
	ctx := context.Background()
	err := RecordRequestStats(ctx, 4, []RequestStatsBucket{{Hour: 123, Count: 1}, {Hour: 124, Count: 0}}, 100)
	require.Error(t, err)
	snapshot, loadErr := LoadRequestStats(ctx, 100)
	require.NoError(t, loadErr)
	require.Zero(t, snapshot.Total)
	require.Empty(t, snapshot.Buckets)
}
