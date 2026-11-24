package database

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type recordingRequestTiming struct {
	durations []time.Duration
}

func (recorder *recordingRequestTiming) RecordDatabaseTiming(duration time.Duration) {
	recorder.durations = append(recorder.durations, duration)
}

func TestRequestTimingRecorderCapturesDatabaseOperations(t *testing.T) {
	store, err := OpenSQLite(filepath.Join(t.TempDir(), "timing.db"), 2)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })
	recorder := &recordingRequestTiming{}
	ctx := WithRequestTimingRecorder(context.Background(), recorder)

	_, err = execContext(ctx, store.writeDB, `CREATE TABLE timing_probe (value INTEGER NOT NULL)`)
	require.NoError(t, err)
	rows, err := queryContext(ctx, store.readDB, `SELECT 1`)
	require.NoError(t, err)
	require.True(t, rows.Next())
	var value int
	require.NoError(t, rows.Scan(&value))
	require.Equal(t, 1, value)
	require.False(t, rows.Next())
	require.NoError(t, rows.Close())
	row := queryRowContext(ctx, store.readDB, `SELECT 2`)
	require.NoError(t, row.Scan(&value))
	require.Equal(t, 2, value)

	require.Len(t, recorder.durations, 3)
}
