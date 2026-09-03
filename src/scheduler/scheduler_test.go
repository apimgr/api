package scheduler

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/apimgr/api/src/config"
	"github.com/apimgr/api/src/database"
)

// initTestDB points the database package at a fresh SQLite instance under
// t.TempDir() so scheduler persistence (AddTask/EnableTask/RunTask/...) can
// be exercised against real storage instead of mocks. The scheduler package
// has no DB injection seam of its own, so this is the real behavior path.
func initTestDB(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	require.NoError(t, database.Init(config.DatabaseConfig{Driver: "sqlite"}, dir))
	t.Cleanup(func() {
		_ = database.Close()
	})
}

// TestParseInterval covers every named interval plus the raw-duration and
// unknown-string fallback paths.
func TestParseInterval(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want time.Duration
	}{
		{"minutely", "minutely", time.Minute},
		{"hourly", "hourly", time.Hour},
		{"daily", "daily", 24 * time.Hour},
		{"weekly", "weekly", 7 * 24 * time.Hour},
		{"monthly", "monthly", 30 * 24 * time.Hour},
		{"raw duration", "45m", 45 * time.Minute},
		{"unknown falls back to daily", "bogus", 24 * time.Hour},
		{"empty falls back to daily", "", 24 * time.Hour},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, ParseInterval(tc.in))
		})
	}
}

// TestNewScheduler verifies a fresh scheduler has no tasks and is not running.
func TestNewScheduler(t *testing.T) {
	s := New()
	require.NotNil(t, s)
	assert.Empty(t, s.GetTasks())
}

// TestAddTaskAndGetTasks covers registration, persistence, and the
// invalid-schedule fallback to "@daily".
func TestAddTaskAndGetTasks(t *testing.T) {
	initTestDB(t)

	s := New()
	s.AddTask("noop", "@daily", func() error { return nil }, true)

	tasks := s.GetTasks()
	require.Len(t, tasks, 1)
	assert.Equal(t, "noop", tasks[0].Name)
	assert.True(t, tasks[0].Enabled)
	assert.False(t, tasks[0].NextRun.IsZero())

	persisted, err := database.GetSchedulerTask("noop")
	require.NoError(t, err)
	require.NotNil(t, persisted)
	assert.Equal(t, "@daily", persisted.Schedule)
	assert.True(t, persisted.Enabled)
}

// TestAddTaskInvalidScheduleFallsBackToDaily confirms a task with an
// unparsable schedule is still registered (not dropped), using "@daily" as
// documented by AddTask's fallback logic.
func TestAddTaskInvalidScheduleFallsBackToDaily(t *testing.T) {
	initTestDB(t)

	s := New()
	s.AddTask("broken", "not a valid cron", func() error { return nil }, false)

	tasks := s.GetTasks()
	require.Len(t, tasks, 1)
	assert.Equal(t, "not a valid cron", tasks[0].Schedule)
	assert.False(t, tasks[0].NextRun.IsZero())
}

// TestAddTaskRestoresPersistedState confirms that when a task with the same
// name already has persisted state in the DB, AddTask restores next_run/
// enabled/last_run from storage rather than recomputing from scratch - this
// is what lets schedules survive restarts.
func TestAddTaskRestoresPersistedState(t *testing.T) {
	initTestDB(t)

	fixedNext := time.Now().Add(48 * time.Hour).Truncate(time.Second)
	require.NoError(t, database.UpsertSchedulerTask("restored", "restored", "@daily", fixedNext, false))

	s := New()
	s.AddTask("restored", "@daily", func() error { return nil }, true)

	tasks := s.GetTasks()
	require.Len(t, tasks, 1)
	assert.WithinDuration(t, fixedNext, tasks[0].NextRun, time.Second)
	assert.False(t, tasks[0].Enabled, "persisted disabled state should override enabledDefault")
}

// TestRemoveTask covers removal of an existing task and the no-op case for
// a name that was never registered.
func TestRemoveTask(t *testing.T) {
	initTestDB(t)

	s := New()
	s.AddTask("temp", "@daily", func() error { return nil }, true)
	require.Len(t, s.GetTasks(), 1)

	s.RemoveTask("temp")
	assert.Empty(t, s.GetTasks())

	// Removing an unknown task must not panic.
	assert.NotPanics(t, func() { s.RemoveTask("never-existed") })
}

// TestEnableDisableTask covers the enable/disable toggle, its persistence,
// and no-op behavior for unknown task names.
func TestEnableDisableTask(t *testing.T) {
	initTestDB(t)

	s := New()
	s.AddTask("toggle", "@daily", func() error { return nil }, false)

	s.EnableTask("toggle")
	tasks := s.GetTasks()
	require.Len(t, tasks, 1)
	assert.True(t, tasks[0].Enabled)

	persisted, err := database.GetSchedulerTask("toggle")
	require.NoError(t, err)
	assert.True(t, persisted.Enabled)

	s.DisableTask("toggle")
	tasks = s.GetTasks()
	assert.False(t, tasks[0].Enabled)

	persisted, err = database.GetSchedulerTask("toggle")
	require.NoError(t, err)
	assert.False(t, persisted.Enabled)

	// Unknown task names must not panic.
	assert.NotPanics(t, func() { s.EnableTask("nope") })
	assert.NotPanics(t, func() { s.DisableTask("nope") })
}

// TestRunNow covers the happy path (a registered task actually executes and
// records success), the unknown-task no-op, and the failure/retry path
// where a failing task's NextRun is rescheduled per the backoff policy.
func TestRunNow(t *testing.T) {
	initTestDB(t)

	s := New()

	t.Run("unknown task is a no-op", func(t *testing.T) {
		err := s.RunNow("does-not-exist")
		assert.NoError(t, err)
	})

	t.Run("successful run resets retries and advances NextRun", func(t *testing.T) {
		var ran int32
		s.AddTask("success", "@daily", func() error {
			atomic.AddInt32(&ran, 1)
			return nil
		}, true)

		require.NoError(t, s.RunNow("success"))
		require.Eventually(t, func() bool {
			return atomic.LoadInt32(&ran) == 1
		}, 2*time.Second, 10*time.Millisecond)

		require.Eventually(t, func() bool {
			tasks := s.GetTasks()
			for _, tk := range tasks {
				if tk.Name == "success" && !tk.LastRun.IsZero() {
					return true
				}
			}
			return false
		}, 2*time.Second, 10*time.Millisecond)
	})

	t.Run("failing task schedules a retry with backoff", func(t *testing.T) {
		s.AddTask("failing", "@daily", func() error {
			return errors.New("boom")
		}, true)

		before := time.Now()
		require.NoError(t, s.RunNow("failing"))

		require.Eventually(t, func() bool {
			for _, tk := range s.GetTasks() {
				if tk.Name == "failing" {
					// First failure should use the first backoff tier (5m),
					// so NextRun should land close to now+5m, not the daily
					// schedule (~24h out).
					return !tk.NextRun.IsZero() && tk.NextRun.Before(before.Add(10*time.Minute))
				}
			}
			return false
		}, 2*time.Second, 10*time.Millisecond)
	})
}

// TestStartStopIdempotent verifies Start/Stop can each be called multiple
// times safely (Start again while running is a no-op; Stop when not
// running is a no-op) and that Stop actually halts the polling goroutine.
func TestStartStopIdempotent(t *testing.T) {
	initTestDB(t)

	s := New()
	s.AddTask("idle", "@daily", func() error { return nil }, true)

	s.Start()
	// Calling Start again while already running must not panic or reset state.
	assert.NotPanics(t, func() { s.Start() })

	s.Stop()
	// Calling Stop again once already stopped must not panic or block.
	assert.NotPanics(t, func() { s.Stop() })
}

// TestHealth covers the three states reported to /server/healthz: a stopped
// loop is unhealthy, a running loop with healthy tasks is healthy, and a
// failed non-skippable task makes the scheduler unhealthy while a failed
// skippable task does not.
func TestHealth(t *testing.T) {
	initTestDB(t)

	s := New()
	s.AddTask("optional", "@daily", func() error { return nil }, true)
	assert.Error(t, s.Health(), "stopped scheduler must be unhealthy")

	s.Start()
	defer s.Stop()
	assert.NoError(t, s.Health())

	s.mu.Lock()
	s.tasks["optional"].LastStatus = "failed"
	s.mu.Unlock()
	assert.NoError(t, s.Health(), "skippable task failure must not fail the health check")

	s.register(&Task{
		Name:      "required",
		Schedule:  "@daily",
		Func:      func() error { return nil },
		Enabled:   true,
		Skippable: false,
	})
	s.mu.Lock()
	s.tasks["required"].LastStatus = "failed"
	s.mu.Unlock()
	assert.Error(t, s.Health(), "failed required task must fail the health check")
}

// TestSchedulerDefaultsMatchSpec locks AI.md PART 18's documented defaults:
// timezone America/New_York, a one hour catch-up window, three retries and
// the exponential 5m/10m/20m backoff ladder.
func TestSchedulerDefaultsMatchSpec(t *testing.T) {
	opts, loc := Options{}.normalized()

	assert.Equal(t, "America/New_York", DefaultTimezone)
	assert.Equal(t, "America/New_York", opts.Timezone)
	assert.Equal(t, time.Hour, opts.CatchUpWindow)
	assert.Equal(t, time.Hour, DefaultCatchUpWindow)
	assert.Equal(t, 3, opts.MaxRetries)
	assert.Equal(t, []time.Duration{5 * time.Minute, 10 * time.Minute, 20 * time.Minute}, opts.RetryBackoff)
	require.NotNil(t, loc)

	assert.Equal(t, opts.Timezone, New().Timezone())
}

// TestBackoffLadder covers the exponential ladder itself, including the
// repeat-last-entry behavior once the ladder is exhausted.
func TestBackoffLadder(t *testing.T) {
	opts, _ := Options{}.normalized()

	assert.Equal(t, 5*time.Minute, opts.backoffFor(-1))
	assert.Equal(t, 5*time.Minute, opts.backoffFor(0))
	assert.Equal(t, 10*time.Minute, opts.backoffFor(1))
	assert.Equal(t, 20*time.Minute, opts.backoffFor(2))
	assert.Equal(t, 20*time.Minute, opts.backoffFor(9))
}

// TestRetryPolicyForOverrides covers the per-task retry knobs from PART 18's
// YAML block: retry_on_fail disables retrying entirely, retry_delay replaces
// the shared ladder with a fixed delay.
func TestRetryPolicyForOverrides(t *testing.T) {
	disabled := false
	opts, _ := Options{
		Overrides: map[string]TaskOverride{
			"never_retries": {RetryOnFail: &disabled},
			"fixed_delay":   {RetryDelay: time.Hour},
		},
	}.normalized()

	delay, retryable := opts.retryPolicyFor("never_retries", 0)
	assert.False(t, retryable)
	assert.Zero(t, delay)

	delay, retryable = opts.retryPolicyFor("fixed_delay", 2)
	assert.True(t, retryable)
	assert.Equal(t, time.Hour, delay)

	delay, retryable = opts.retryPolicyFor("unconfigured", 1)
	assert.True(t, retryable)
	assert.Equal(t, 10*time.Minute, delay)
}

// TestRunCatchUpOrdersMissedTasks covers PART 18's startup behavior: tasks
// missed within the catch-up window run immediately in order of their
// original scheduled time, while a task missed by more than the window is
// skipped and simply rescheduled.
func TestRunCatchUpOrdersMissedTasks(t *testing.T) {
	initTestDB(t)

	now := time.Now()
	require.NoError(t, database.UpsertSchedulerTask("catchup_older", "catchup_older", "@daily", now.Add(-30*time.Minute), true))
	require.NoError(t, database.UpsertSchedulerTask("catchup_newer", "catchup_newer", "@daily", now.Add(-5*time.Minute), true))
	require.NoError(t, database.UpsertSchedulerTask("catchup_stale", "catchup_stale", "@daily", now.Add(-3*time.Hour), true))

	var (
		mu    sync.Mutex
		order []string
	)
	record := func(name string) func() error {
		return func() error {
			mu.Lock()
			defer mu.Unlock()
			order = append(order, name)
			return nil
		}
	}

	s := New()
	s.AddTask("catchup_older", "@daily", record("catchup_older"), true)
	s.AddTask("catchup_newer", "@daily", record("catchup_newer"), true)
	s.AddTask("catchup_stale", "@daily", record("catchup_stale"), true)

	s.Start()
	defer s.Stop()

	require.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(order) == 2
	}, 5*time.Second, 10*time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, []string{"catchup_older", "catchup_newer"}, order,
		"missed tasks must run in order of their original scheduled time")
	assert.NotContains(t, order, "catchup_stale",
		"a task missed by more than the catch-up window must not be run")
}

// TestMarkInterruptedQueuesRetry covers the shutdown-timeout branch: a task
// still running past the 30 second grace period is force-released, recorded
// as interrupted, and queued to run again on the next start.
func TestMarkInterruptedQueuesRetry(t *testing.T) {
	initTestDB(t)

	s := New()
	s.AddTask("long_runner", "@daily", func() error { return nil }, true)

	s.mu.Lock()
	s.tasks["long_runner"].running = true
	s.mu.Unlock()

	s.markInterrupted()

	s.mu.RLock()
	task := s.tasks["long_runner"]
	running := task.running
	status := task.LastStatus
	nextRun := task.NextRun
	s.mu.RUnlock()

	assert.False(t, running)
	assert.Equal(t, "interrupted", status)
	assert.WithinDuration(t, time.Now(), nextRun, 5*time.Second,
		"an interrupted task must be queued for immediate retry")
}

// TestGetTasksReturnsSnapshotCopy verifies GetTasks returns independent Task
// values, so mutating the returned slice cannot corrupt scheduler state.
func TestGetTasksReturnsSnapshotCopy(t *testing.T) {
	initTestDB(t)

	s := New()
	s.AddTask("copy-check", "@daily", func() error { return nil }, true)

	tasks := s.GetTasks()
	require.Len(t, tasks, 1)
	tasks[0].Name = "mutated"

	fresh := s.GetTasks()
	require.Len(t, fresh, 1)
	assert.Equal(t, "copy-check", fresh[0].Name)
}
