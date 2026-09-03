package database

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// UpsertSchedulerTask must insert a new row on first call and update the
// mutable fields (task_name, schedule, enabled) on conflict, without
// resetting run_count/fail_count.
func TestUpsertAndGetSchedulerTask(t *testing.T) {
	db := GetServerDB()
	_, err := db.Exec(`DELETE FROM scheduler_tasks WHERE task_id = ?`, "task-upsert")
	require.NoError(t, err)

	next := time.Now().Add(time.Hour).UTC().Truncate(time.Second)
	require.NoError(t, UpsertSchedulerTask("task-upsert", "First Name", "@daily", next, true))

	state, err := GetSchedulerTask("task-upsert")
	require.NoError(t, err)
	require.NotNil(t, state)
	assert.Equal(t, "task-upsert", state.TaskID)
	assert.Equal(t, "First Name", state.TaskName)
	assert.Equal(t, "@daily", state.Schedule)
	assert.True(t, state.Enabled)

	// Conflict path: same task_id, changed name/schedule/enabled.
	require.NoError(t, UpsertSchedulerTask("task-upsert", "Second Name", "@hourly", next, false))
	state2, err := GetSchedulerTask("task-upsert")
	require.NoError(t, err)
	require.NotNil(t, state2)
	assert.Equal(t, "Second Name", state2.TaskName)
	assert.Equal(t, "@hourly", state2.Schedule)
	assert.False(t, state2.Enabled)
}

// GetSchedulerTask for a task_id that was never upserted must return
// (nil, nil) rather than an error.
func TestGetSchedulerTaskNotFound(t *testing.T) {
	state, err := GetSchedulerTask("does-not-exist")
	require.NoError(t, err)
	assert.Nil(t, state)
}

// RecordSchedulerRun must update last_run/last_status/next_run, increment
// run_count on every call and fail_count only on non-"success" status, and
// append a matching scheduler_history row.
func TestRecordSchedulerRun(t *testing.T) {
	db := GetServerDB()
	taskID := "task-record"
	_, err := db.Exec(`DELETE FROM scheduler_tasks WHERE task_id = ?`, taskID)
	require.NoError(t, err)
	_, err = db.Exec(`DELETE FROM scheduler_history WHERE task_id = ?`, taskID)
	require.NoError(t, err)

	next := time.Now().Add(time.Hour).UTC().Truncate(time.Second)
	require.NoError(t, UpsertSchedulerTask(taskID, "Record Task", "@daily", next, true))

	started := time.Now().UTC().Truncate(time.Second)
	completed := started.Add(2 * time.Second)
	nextRun := started.Add(24 * time.Hour).UTC().Truncate(time.Second)

	RecordSchedulerRun(taskID, started, completed, "success", nil, nextRun)

	state, err := GetSchedulerTask(taskID)
	require.NoError(t, err)
	require.NotNil(t, state)
	assert.Equal(t, "success", state.LastStatus)
	assert.Empty(t, state.LastError)
	assert.EqualValues(t, 1, state.RunCount)
	assert.EqualValues(t, 0, state.FailCount)

	// A failing run must increment fail_count and persist the error text.
	RecordSchedulerRun(taskID, started, completed, "error", errors.New("boom"), nextRun)
	state2, err := GetSchedulerTask(taskID)
	require.NoError(t, err)
	require.NotNil(t, state2)
	assert.Equal(t, "error", state2.LastStatus)
	assert.Equal(t, "boom", state2.LastError)
	assert.EqualValues(t, 2, state2.RunCount)
	assert.EqualValues(t, 1, state2.FailCount)

	var historyCount int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM scheduler_history WHERE task_id = ?`, taskID).Scan(&historyCount))
	assert.Equal(t, 2, historyCount)
}

// RecordSchedulerRun against a task_id with no scheduler_tasks row must not
// panic; the UPDATE simply affects zero rows while the history insert
// still succeeds.
func TestRecordSchedulerRunUnknownTask(t *testing.T) {
	db := GetServerDB()
	taskID := "task-unknown-record"
	_, err := db.Exec(`DELETE FROM scheduler_history WHERE task_id = ?`, taskID)
	require.NoError(t, err)

	started := time.Now()
	RecordSchedulerRun(taskID, started, started.Add(time.Second), "success", nil, started.Add(time.Hour))

	var historyCount int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM scheduler_history WHERE task_id = ?`, taskID).Scan(&historyCount))
	assert.Equal(t, 1, historyCount)
}

// DueSchedulerTasks must return only enabled tasks whose next_run is at or
// before "now", excluding disabled tasks and tasks scheduled in the
// future.
func TestDueSchedulerTasks(t *testing.T) {
	db := GetServerDB()
	_, err := db.Exec(`DELETE FROM scheduler_tasks WHERE task_id LIKE 'due-%'`)
	require.NoError(t, err)

	now := time.Now().UTC().Truncate(time.Second)
	past := now.Add(-time.Hour)
	future := now.Add(time.Hour)

	require.NoError(t, UpsertSchedulerTask("due-past-enabled", "Past Enabled", "@daily", past, true))
	require.NoError(t, UpsertSchedulerTask("due-future-enabled", "Future Enabled", "@daily", future, true))
	require.NoError(t, UpsertSchedulerTask("due-past-disabled", "Past Disabled", "@daily", past, false))
	require.NoError(t, UpsertSchedulerTask("due-exact-now", "Exact Now", "@daily", now, true))

	due, err := DueSchedulerTasks(now)
	require.NoError(t, err)

	gotIDs := map[string]bool{}
	for _, d := range due {
		gotIDs[d.TaskID] = true
	}
	assert.True(t, gotIDs["due-past-enabled"])
	assert.True(t, gotIDs["due-exact-now"])
	assert.False(t, gotIDs["due-future-enabled"])
	assert.False(t, gotIDs["due-past-disabled"])
}
