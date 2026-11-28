package database

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"time"
)

// SchedulerTaskState is the persisted state of one scheduled task, backed by
// the scheduler_tasks table (see database.go schema). Persistence lets the
// scheduler survive restarts and catch up on missed runs, per AI.md PART 18.
type SchedulerTaskState struct {
	TaskID     string
	TaskName   string
	Schedule   string
	LastRun    sql.NullTime
	LastStatus string
	LastError  string
	NextRun    time.Time
	RunCount   int64
	FailCount  int64
	Enabled    bool
}

// UpsertSchedulerTask inserts or updates a task's persisted schedule state.
// Called on registration so the schedule/enabled columns always reflect the
// current in-code task definitions.
func UpsertSchedulerTask(taskID, taskName, schedule string, nextRun time.Time, enabled bool) error {
	db := serverWriteDB()
	if db == nil {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeoutWrite)
	defer cancel()

	_, err := execContext(ctx, db, `
		INSERT INTO scheduler_tasks (id, name, schedule, next_run, enabled)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			name = excluded.name,
			schedule = excluded.schedule,
			enabled = excluded.enabled
	`, taskID, taskName, schedule, nextRun.Unix(), enabled)

	return err
}

// GetSchedulerTask loads the persisted state for one task, if any.
func GetSchedulerTask(taskID string) (*SchedulerTaskState, error) {
	db := GetServerDB()
	if db == nil {
		return nil, nil
	}

	const getTaskQuery = `
		SELECT id, name, schedule, last_run, last_status, last_error,
			next_run, run_count, fail_count, enabled
		FROM scheduler_tasks
		WHERE id = ?
	`

	ctx, cancel := context.WithTimeout(context.Background(), timeoutSimpleSelect)
	defer cancel()

	row := queryRowContext(ctx, db, getTaskQuery, taskID)

	var state SchedulerTaskState
	var lastRun, nextRun sql.NullInt64
	var lastStatus, lastError sql.NullString
	if err := row.Scan(&state.TaskID, &state.TaskName, &state.Schedule, &lastRun,
		&lastStatus, &lastError, &nextRun, &state.RunCount, &state.FailCount, &state.Enabled); err != nil {
		recordQueryError(getTaskQuery, err)
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}

	state.LastStatus = lastStatus.String
	state.LastError = lastError.String
	if lastRun.Valid {
		state.LastRun = sql.NullTime{Time: time.Unix(lastRun.Int64, 0).UTC(), Valid: true}
	}
	if nextRun.Valid {
		state.NextRun = time.Unix(nextRun.Int64, 0).UTC()
	}

	return &state, nil
}

// RecordSchedulerRun persists the outcome of a task run: it updates the
// task's last_run/next_run/status/counters and appends a scheduler_history
// row, matching AI.md PART 18's "Database-backed state" requirement.
func RecordSchedulerRun(taskID string, startedAt, finishedAt time.Time, status string, runErr error, nextRun time.Time) {
	db := serverWriteDB()
	if db == nil {
		return
	}

	errText := ""
	if runErr != nil {
		errText = runErr.Error()
	}

	failIncrement := 0
	if status != "success" {
		failIncrement = 1
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeoutWrite)
	defer cancel()

	if _, err := execContext(ctx, db, `
		UPDATE scheduler_tasks
		SET last_run = ?, last_status = ?, last_error = ?, next_run = ?,
			run_count = run_count + 1, fail_count = fail_count + ?
		WHERE id = ?
	`, startedAt.Unix(), status, errText, nextRun.Unix(), failIncrement, taskID); err != nil {
		log.Printf("Database: failed to update scheduler task '%s': %v", taskID, err)
	}

	durationMS := finishedAt.Sub(startedAt).Milliseconds()
	historyCtx, cancelHistory := context.WithTimeout(context.Background(), timeoutWrite)
	defer cancelHistory()

	if _, err := execContext(historyCtx, db, `
		INSERT INTO scheduler_history (task_id, started_at, finished_at, status, error, duration_ms)
		VALUES (?, ?, ?, ?, ?, ?)
	`, taskID, startedAt.Unix(), finishedAt.Unix(), status, errText, durationMS); err != nil {
		log.Printf("Database: failed to record scheduler history for '%s': %v", taskID, err)
	}
}

// SchedulerHistoryEntry is one persisted execution of a scheduled task,
// backing the `scheduler history <id>` CLI command in AI.md PART 18.
type SchedulerHistoryEntry struct {
	ID         any
	TaskID     string
	StartedAt  time.Time
	FinishedAt time.Time
	Status     string
	Error      string
	DurationMS int64
}

// SchedulerHistory returns the most recent executions of one task, newest
// first, capped at limit rows. A non-positive limit defaults to 50.
func SchedulerHistory(taskID string, limit int) ([]SchedulerHistoryEntry, error) {
	if limit <= 0 {
		limit = 50
	}

	db := GetServerDB()
	if db == nil {
		return nil, fmt.Errorf("scheduler history: database not initialized")
	}

	const historyQuery = `
		SELECT id, task_id, started_at, finished_at, status, error, duration_ms
		FROM scheduler_history
		WHERE task_id = ?
		ORDER BY started_at DESC
		LIMIT ?
	`

	// The deadline has to outlive the rows iteration below, so cancel is
	// deferred to function scope rather than released after the call.
	ctx, cancel := context.WithTimeout(context.Background(), timeoutComplexSelect)
	defer cancel()

	rows, err := queryContext(ctx, db, historyQuery, taskID, limit)
	if err != nil {
		return nil, fmt.Errorf("query scheduler history: %w", err)
	}
	defer func() { _ = rows.Close() }()

	entries := make([]SchedulerHistoryEntry, 0, limit)
	for rows.Next() {
		var (
			entry      SchedulerHistoryEntry
			startedAt  int64
			finishedAt sql.NullInt64
			runError   sql.NullString
			durationMS sql.NullInt64
		)
		if err := rows.Scan(&entry.ID, &entry.TaskID, &startedAt, &finishedAt,
			&entry.Status, &runError, &durationMS); err != nil {
			recordQueryError(historyQuery, err)
			return nil, fmt.Errorf("scan scheduler history: %w", err)
		}
		if finishedAt.Valid {
			entry.FinishedAt = time.Unix(finishedAt.Int64, 0).UTC()
		}
		entry.StartedAt = time.Unix(startedAt, 0).UTC()
		if runError.Valid {
			entry.Error = runError.String
		}
		if durationMS.Valid {
			entry.DurationMS = durationMS.Int64
		}
		entries = append(entries, entry)
	}

	if err := rows.Err(); err != nil {
		recordQueryError(historyQuery, err)
		return nil, fmt.Errorf("iterate scheduler history: %w", err)
	}

	return entries, nil
}

// DueSchedulerTasks returns persisted tasks whose next_run is on or before
// "now" - used both for normal due-task polling and, when "now" is the
// startup time, for catch-up detection (see AI.md PART 18 startup flow).
func DueSchedulerTasks(now time.Time) ([]SchedulerTaskState, error) {
	db := GetServerDB()
	if db == nil {
		return nil, nil
	}

	const dueTasksQuery = `
		SELECT id, name, schedule, last_run, last_status, last_error,
			next_run, run_count, fail_count, enabled
		FROM scheduler_tasks
		WHERE enabled = 1 AND next_run <= ?
	`

	// The deadline has to outlive the rows iteration below, so cancel is
	// deferred to function scope rather than released after the call. This
	// is a scanning multi-row read, so it uses the complex-select bound
	// rather than the single-row simple-select bound.
	ctx, cancel := context.WithTimeout(context.Background(), timeoutComplexSelect)
	defer cancel()

	rows, err := queryContext(ctx, db, dueTasksQuery, now.Unix())
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var states []SchedulerTaskState
	for rows.Next() {
		var state SchedulerTaskState
		var lastRun, nextRun sql.NullInt64
		var lastStatus, lastError sql.NullString
		if err := rows.Scan(&state.TaskID, &state.TaskName, &state.Schedule, &lastRun,
			&lastStatus, &lastError, &nextRun, &state.RunCount, &state.FailCount, &state.Enabled); err != nil {
			recordQueryError(dueTasksQuery, err)
			return nil, err
		}
		state.LastStatus = lastStatus.String
		state.LastError = lastError.String
		if lastRun.Valid {
			state.LastRun = sql.NullTime{Time: time.Unix(lastRun.Int64, 0).UTC(), Valid: true}
		}
		if nextRun.Valid {
			state.NextRun = time.Unix(nextRun.Int64, 0).UTC()
		}
		states = append(states, state)
	}

	if err := rows.Err(); err != nil {
		recordQueryError(dueTasksQuery, err)
		return nil, err
	}

	return states, nil
}
