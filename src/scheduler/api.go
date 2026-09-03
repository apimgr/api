package scheduler

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"time"

	"github.com/apimgr/api/src/database"
)

// TaskInfo is the read-only view of a task returned to the CLI commands
// described in AI.md PART 18 (`scheduler list` / `scheduler show <id>`).
type TaskInfo struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	Schedule   string    `json:"schedule"`
	Enabled    bool      `json:"enabled"`
	Skippable  bool      `json:"skippable"`
	LastRun    time.Time `json:"last_run"`
	LastStatus string    `json:"last_status"`
	LastError  string    `json:"last_error"`
	NextRun    time.Time `json:"next_run"`
	RunCount   int64     `json:"run_count"`
	FailCount  int64     `json:"fail_count"`
	Running    bool      `json:"running"`
}

// HistoryEntry is one row of a task's execution history, backing
// `scheduler history <id>`.
type HistoryEntry struct {
	ID          int64     `json:"id"`
	TaskID      string    `json:"task_id"`
	StartedAt   time.Time `json:"started_at"`
	CompletedAt time.Time `json:"completed_at"`
	Status      string    `json:"status"`
	Error       string    `json:"error"`
	DurationMS  int64     `json:"duration_ms"`
}

// ErrUnknownTask is returned when a CLI command names a task that is not
// registered.
var ErrUnknownTask = fmt.Errorf("unknown scheduler task")

// ErrTaskRequired is returned when an operator tries to disable a task that
// AI.md PART 18 marks as not skippable.
var ErrTaskRequired = fmt.Errorf("task is required and cannot be disabled")

// info converts a task to its read-only view. The caller must hold the lock.
func (t *Task) info() TaskInfo {
	return TaskInfo{
		ID:         t.Name,
		Name:       t.Title,
		Schedule:   t.Schedule,
		Enabled:    t.Enabled,
		Skippable:  t.Skippable,
		LastRun:    t.LastRun,
		LastStatus: t.LastStatus,
		LastError:  t.LastError,
		NextRun:    t.NextRun,
		RunCount:   t.RunCount,
		FailCount:  t.FailCount,
		Running:    t.running,
	}
}

// ListTasks returns every registered task sorted by id, backing
// `scheduler list`.
func (s *Scheduler) ListTasks() []TaskInfo {
	s.mu.RLock()
	defer s.mu.RUnlock()

	list := make([]TaskInfo, 0, len(s.tasks))
	for _, task := range s.tasks {
		list = append(list, task.info())
	}
	sort.Slice(list, func(i, j int) bool { return list[i].ID < list[j].ID })
	return list
}

// ShowTask returns the detail view of one task, backing
// `scheduler show <id>`.
func (s *Scheduler) ShowTask(id string) (TaskInfo, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	task, ok := s.tasks[id]
	if !ok {
		return TaskInfo{}, fmt.Errorf("%w: %s", ErrUnknownTask, id)
	}
	return task.info(), nil
}

// RunTaskByID runs one task immediately and waits for it, backing
// `scheduler run <id>`. Unlike RunNow it reports an unknown task, and the
// task's own failure, as an error so the CLI can exit non-zero.
func (s *Scheduler) RunTaskByID(id string) error {
	s.mu.RLock()
	task, ok := s.tasks[id]
	s.mu.RUnlock()

	if !ok {
		return fmt.Errorf("%w: %s", ErrUnknownTask, id)
	}

	return s.execute(task)
}

// SetTaskEnabled toggles a task, backing `scheduler enable|disable <id>`.
// Disabling a non-skippable task is refused rather than silently ignored so
// the CLI can report the reason.
func (s *Scheduler) SetTaskEnabled(id string, enabled bool) error {
	s.mu.RLock()
	task, ok := s.tasks[id]
	skippable := ok && task.Skippable
	s.mu.RUnlock()

	if !ok {
		return fmt.Errorf("%w: %s", ErrUnknownTask, id)
	}
	if !enabled && !skippable {
		return fmt.Errorf("%w: %s", ErrTaskRequired, id)
	}

	if enabled {
		s.EnableTask(id)
	} else {
		s.DisableTask(id)
	}
	return nil
}

// TaskHistory returns the most recent executions of a task, newest first,
// backing `scheduler history <id>`. limit caps the number of rows; a
// non-positive limit defaults to 50.
func (s *Scheduler) TaskHistory(id string, limit int) ([]HistoryEntry, error) {
	if limit <= 0 {
		limit = 50
	}

	db := database.GetServerDB()
	if db == nil {
		return nil, fmt.Errorf("database not initialized")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	rows, err := db.QueryContext(ctx, `
		SELECT id, task_id, started_at, completed_at, status, error, duration_ms
		FROM scheduler_history
		WHERE task_id = ?
		ORDER BY started_at DESC
		LIMIT ?`, id, limit)
	if err != nil {
		return nil, fmt.Errorf("query scheduler history: %w", err)
	}
	defer rows.Close()

	entries := make([]HistoryEntry, 0, limit)
	for rows.Next() {
		var (
			entry       HistoryEntry
			completedAt sql.NullTime
			runError    sql.NullString
			durationMS  sql.NullInt64
		)
		if err := rows.Scan(&entry.ID, &entry.TaskID, &entry.StartedAt, &completedAt, &entry.Status, &runError, &durationMS); err != nil {
			return nil, fmt.Errorf("scan scheduler history: %w", err)
		}
		if completedAt.Valid {
			entry.CompletedAt = completedAt.Time
		}
		if runError.Valid {
			entry.Error = runError.String
		}
		if durationMS.Valid {
			entry.DurationMS = durationMS.Int64
		}
		entries = append(entries, entry)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate scheduler history: %w", err)
	}

	return entries, nil
}
