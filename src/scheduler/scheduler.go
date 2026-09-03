package scheduler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"sort"
	"sync"
	"time"

	"github.com/apimgr/api/src/database"
	"github.com/apimgr/api/src/metrics"
)

// failureKind selects which operator notification a failed task fires, per
// the suppression rules in AI.md PART 18: backup and SSL failures send their
// own dedicated event and suppress scheduler_error for the same execution;
// every other task falls through to scheduler_error.
type failureKind int

const (
	failureGeneric failureKind = iota
	failureBackup
	failureSSLRenewal
)

// Task represents a scheduled task.
type Task struct {
	// Name is the task identifier used everywhere (config key, DB task_id,
	// CLI argument).
	Name string
	// Title is the human-readable description shown by `scheduler list`.
	Title string
	// Schedule is the raw schedule expression as configured.
	Schedule string
	// Func is the task body.
	Func func() error
	// LastRun is the completion time of the most recent execution.
	LastRun time.Time
	// NextRun is the next scheduled execution time.
	NextRun time.Time
	// Enabled reports whether the task will run on schedule.
	Enabled bool
	// Skippable reports whether an operator is allowed to disable the task.
	// AI.md PART 18 marks ssl_renewal, token_cleanup, log_rotation and
	// healthcheck_self as not skippable.
	Skippable bool
	// LastStatus is "success", "failed", "interrupted" or "" if never run.
	LastStatus string
	// LastError is the error text from the most recent failed run.
	LastError string
	// RunCount is the total number of completed executions.
	RunCount int64
	// FailCount is the total number of failed executions.
	FailCount int64

	sched   schedule
	retries int
	running bool
	failure failureKind
}

// Scheduler manages periodic tasks. Per AI.md PART 18 the scheduler itself
// is always running - only individual tasks can be enabled or disabled.
type Scheduler struct {
	tasks   map[string]*Task
	stop    chan struct{}
	wg      sync.WaitGroup
	running bool
	mu      sync.RWMutex

	opts Options
	loc  *time.Location
}

// New creates a new scheduler with AI.md PART 18 defaults.
func New() *Scheduler {
	return NewWithOptions(Options{})
}

// NewWithOptions creates a scheduler configured from server.scheduler.
func NewWithOptions(opts Options) *Scheduler {
	normalized, loc := opts.normalized()
	return &Scheduler{
		tasks: make(map[string]*Task),
		stop:  make(chan struct{}),
		opts:  normalized,
		loc:   loc,
	}
}

// Timezone returns the IANA timezone cron schedules are evaluated in.
func (s *Scheduler) Timezone() string {
	return s.opts.Timezone
}

// now returns the current time in the scheduler's configured timezone, so
// cron fields (hour, day-of-month, weekday) are evaluated against the
// operator's zone rather than the host's.
func (s *Scheduler) now() time.Time {
	return time.Now().In(s.loc)
}

// AddTask adds a new task to the scheduler.
// schedule: 5-field cron expression, @hourly, @daily, @weekly, @monthly, or
// @every X. Persisted state (next_run, enabled, last_run) is restored from
// the database if a row already exists for this task, so schedules survive
// restarts per AI.md PART 18.
func (s *Scheduler) AddTask(name string, sched string, fn func() error, enabledDefault bool) {
	s.register(&Task{
		Name:      name,
		Title:     name,
		Schedule:  sched,
		Func:      fn,
		Enabled:   enabledDefault,
		Skippable: true,
	})
}

// register installs a fully described task, applying operator overrides from
// server.scheduler.tasks.<id> and restoring persisted state.
func (s *Scheduler) register(task *Task) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if task.Title == "" {
		task.Title = task.Name
	}

	if override, ok := s.opts.overrideFor(task.Name); ok {
		if override.Schedule != "" {
			task.Schedule = override.Schedule
		}
		if override.Enabled != nil {
			// A non-skippable task can never be turned off by config; the
			// operator's intent is logged and ignored.
			if !*override.Enabled && !task.Skippable {
				log.Printf("Scheduler: Task '%s' cannot be disabled, ignoring config override", task.Name)
			} else {
				task.Enabled = *override.Enabled
			}
		}
	}

	parsed, err := parseSchedule(task.Schedule)
	if err != nil {
		log.Printf("Scheduler: Failed to parse schedule '%s' for task '%s': %v", task.Schedule, task.Name, err)
		parsed, _ = parseSchedule("@daily")
	}
	task.sched = parsed
	task.NextRun = parsed.next(s.now())

	if persisted, perr := database.GetSchedulerTask(task.Name); perr == nil && persisted != nil {
		task.NextRun = persisted.NextRun
		task.Enabled = persisted.Enabled
		if !task.Skippable {
			task.Enabled = true
		}
		if persisted.LastRun.Valid {
			task.LastRun = persisted.LastRun.Time
		}
		task.LastStatus = persisted.LastStatus
		task.LastError = persisted.LastError
		task.RunCount = int64(persisted.RunCount)
		task.FailCount = int64(persisted.FailCount)
	}

	s.tasks[task.Name] = task

	if err := database.UpsertSchedulerTask(task.Name, task.Title, task.Schedule, task.NextRun, task.Enabled); err != nil {
		log.Printf("Scheduler: Failed to persist task '%s': %v", task.Name, err)
	}

	log.Printf("Scheduler: Added task '%s' (schedule: %s, next run: %s, enabled: %v)",
		task.Name, task.Schedule, task.NextRun.Format(time.RFC3339), task.Enabled)
}

// RemoveTask removes a task from the scheduler
func (s *Scheduler) RemoveTask(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.tasks, name)
	log.Printf("Scheduler: Removed task '%s'", name)
}

// EnableTask enables a task
func (s *Scheduler) EnableTask(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	task, ok := s.tasks[name]
	if !ok {
		return
	}
	task.Enabled = true
	task.NextRun = task.sched.next(s.now())
	if err := database.UpsertSchedulerTask(name, task.Title, task.Schedule, task.NextRun, true); err != nil {
		log.Printf("Scheduler: Failed to persist task '%s': %v", name, err)
	}
}

// DisableTask disables a task. Tasks marked not-skippable in AI.md PART 18
// (ssl_renewal, token_cleanup, log_rotation, healthcheck_self) refuse to be
// disabled.
func (s *Scheduler) DisableTask(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	task, ok := s.tasks[name]
	if !ok {
		return
	}
	if !task.Skippable {
		log.Printf("Scheduler: Task '%s' is required and cannot be disabled", name)
		return
	}
	task.Enabled = false
	if err := database.UpsertSchedulerTask(name, task.Title, task.Schedule, task.NextRun, false); err != nil {
		log.Printf("Scheduler: Failed to persist task '%s': %v", name, err)
	}
}

// Start begins the scheduler loop. Before entering the normal polling loop
// it runs catch-up: any enabled task whose persisted next_run already
// elapsed, but by no more than the catch-up window, is run immediately (in
// order of original scheduled time); tasks missed by more than the window
// have their missed run skipped and next_run recomputed, per AI.md PART 18.
func (s *Scheduler) Start() {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return
	}
	s.running = true
	s.stop = make(chan struct{})
	s.mu.Unlock()

	s.runCatchUp()

	s.mu.RLock()
	taskCount := len(s.tasks)
	s.mu.RUnlock()
	log.Printf("Scheduler: Started with %d tasks (timezone: %s)", taskCount, s.opts.Timezone)

	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-s.stop:
				log.Println("Scheduler: Stopped")
				return
			case <-ticker.C:
				s.runDueTasks()
			}
		}
	}()
}

// runCatchUp runs or reschedules tasks whose next_run has already elapsed.
func (s *Scheduler) runCatchUp() {
	now := s.now()
	window := s.opts.CatchUpWindow

	s.mu.Lock()
	var overdue []*Task
	for _, task := range s.tasks {
		if task.Enabled && now.After(task.NextRun) {
			overdue = append(overdue, task)
		}
	}
	sort.Slice(overdue, func(i, j int) bool { return overdue[i].NextRun.Before(overdue[j].NextRun) })

	var (
		toRun []*Task
		dueAt []time.Time
	)
	for _, task := range overdue {
		if now.Sub(task.NextRun) <= window {
			toRun = append(toRun, task)
			dueAt = append(dueAt, task.NextRun)
		} else {
			log.Printf("Scheduler: Task '%s' missed run at %s (outside %s catch-up window), skipping to next occurrence",
				task.Name, task.NextRun.Format(time.RFC3339), window)
			task.NextRun = task.sched.next(now)
			if err := database.UpsertSchedulerTask(task.Name, task.Title, task.Schedule, task.NextRun, task.Enabled); err != nil {
				log.Printf("Scheduler: Failed to persist task '%s': %v", task.Name, err)
			}
		}
	}
	s.mu.Unlock()

	if len(toRun) == 0 {
		return
	}

	// PART 18 requires missed tasks to be queued "in order of original
	// scheduled time", so the catch-up queue is drained sequentially on one
	// goroutine rather than dispatched concurrently. It is tracked by the
	// wait group so a shutdown during catch-up still waits for it.
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		for i, task := range toRun {
			log.Printf("Scheduler: Catching up missed run of task '%s' (was due %s)", task.Name, dueAt[i].Format(time.RFC3339))
			s.execute(task)
		}
	}()
}

// Stop stops the scheduler and waits for in-flight tasks to finish, up to
// 30 seconds, per AI.md PART 18 graceful shutdown. On timeout any task still
// running is marked interrupted so it is retried on the next start; task
// state is persisted before returning either way.
func (s *Scheduler) Stop() {
	s.mu.Lock()
	if !s.running {
		s.mu.Unlock()
		return
	}
	close(s.stop)
	s.running = false
	s.mu.Unlock()

	done := make(chan struct{})
	go func() {
		s.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(30 * time.Second):
		log.Println("Scheduler: Shutdown wait timed out after 30s, marking interrupted tasks for retry")
		s.markInterrupted()
	}

	s.persistAll()
}

// markInterrupted force-releases the locks of tasks still running past the
// shutdown grace period and queues them for retry on the next startup.
func (s *Scheduler) markInterrupted() {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, task := range s.tasks {
		if !task.running {
			continue
		}
		task.running = false
		task.LastStatus = "interrupted"
		task.LastError = "interrupted by shutdown"
		task.NextRun = time.Now().In(s.loc)
		log.Printf("Scheduler: Task '%s' interrupted by shutdown, queued for retry on next start", task.Name)
	}
}

// persistAll writes every task's current state to the database so a restart
// resumes exactly where shutdown left off.
func (s *Scheduler) persistAll() {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, task := range s.tasks {
		if err := database.UpsertSchedulerTask(task.Name, task.Title, task.Schedule, task.NextRun, task.Enabled); err != nil {
			log.Printf("Scheduler: Failed to persist task '%s' during shutdown: %v", task.Name, err)
		}
	}
}

// runDueTasks executes tasks that are due
func (s *Scheduler) runDueTasks() {
	s.mu.Lock()
	now := s.now()
	dueTasks := make([]*Task, 0)

	for _, task := range s.tasks {
		if task.Enabled && !task.running && now.After(task.NextRun) {
			dueTasks = append(dueTasks, task)
		}
	}
	s.mu.Unlock()

	for _, task := range dueTasks {
		s.runTask(task)
	}
}

// runTask runs one task asynchronously, recording its result to the
// database and applying the retry-with-backoff policy on failure.
func (s *Scheduler) runTask(t *Task) {
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		s.execute(t)
	}()
}

// execute runs one task synchronously, recording its result to the database,
// auditing it, applying the retry-with-backoff policy and dispatching the
// failure notification. It returns the task's own error so the CLI's
// `scheduler run <id>` can exit non-zero.
func (s *Scheduler) execute(t *Task) error {
	s.mu.Lock()
	t.running = true
	s.mu.Unlock()

	log.Printf("Scheduler: Running task '%s'", t.Name)
	started := time.Now().In(s.loc)
	executionID := fmt.Sprintf("%s-%d", t.Name, started.UnixNano())

	// PART 20 scheduler metrics. The task id is the only label value, so
	// cardinality stays bounded by the registered task set. Every exit path of
	// this function passes through the matching end call below, so a failed or
	// retried run still records its duration and an "error" outcome.
	taskMetrics := metrics.Get()
	taskMetrics.RecordSchedulerTaskStart(t.Name)

	runErr := t.Func()
	completed := time.Now().In(s.loc)
	taskMetrics.RecordSchedulerTaskEnd(t.Name, completed.Sub(started), runErr)

	s.mu.Lock()
	t.running = false
	t.LastRun = completed
	t.RunCount++

	status := "success"
	if runErr != nil {
		status = "failed"
		t.FailCount++
		t.LastError = runErr.Error()
		log.Printf("Scheduler: Task '%s' failed: %v", t.Name, runErr)

		delay, retryable := s.opts.retryPolicyFor(t.Name, t.retries)
		if retryable && t.retries < s.opts.MaxRetries {
			t.retries++
			t.NextRun = completed.Add(delay)
			log.Printf("Scheduler: Task '%s' will retry in %s (attempt %d/%d)", t.Name, delay, t.retries, s.opts.MaxRetries)
		} else if !retryable {
			log.Printf("Scheduler: Task '%s' has retry_on_fail disabled, resuming normal schedule", t.Name)
			t.retries = 0
			t.NextRun = t.sched.next(completed)
		} else {
			log.Printf("Scheduler: Task '%s' exhausted %d retries, resuming normal schedule", t.Name, s.opts.MaxRetries)
			t.retries = 0
			t.NextRun = t.sched.next(completed)
		}
	} else {
		log.Printf("Scheduler: Task '%s' completed", t.Name)
		t.retries = 0
		t.LastError = ""
		t.NextRun = t.sched.next(completed)
	}
	t.LastStatus = status

	nextRun := t.NextRun
	failure := t.failure
	taskTitle := t.Title
	s.mu.Unlock()

	database.RecordSchedulerRun(t.Name, started, completed, status, runErr, nextRun)
	s.audit(t.Name, taskTitle, status, started, completed, nextRun, runErr)

	if runErr != nil {
		s.notifyFailure(executionID, t.Name, failure, runErr, nextRun)
	}

	return runErr
}

// audit records one entry per execution in the database audit log and, when
// an audit hook is configured, in the file-based audit.log as well. AI.md
// PART 18 requires every execution to be audit logged.
func (s *Scheduler) audit(taskID, taskTitle, status string, started, completed, nextRun time.Time, runErr error) {
	details := map[string]interface{}{
		"task_id":     taskID,
		"task_name":   taskTitle,
		"status":      status,
		"started_at":  started.Format(time.RFC3339),
		"duration_ms": completed.Sub(started).Milliseconds(),
		"next_run":    nextRun.Format(time.RFC3339),
	}
	if runErr != nil {
		details["error"] = runErr.Error()
	}

	event := "scheduler.task_completed"
	if runErr != nil {
		event = "scheduler.task_failed"
	}

	if s.opts.Audit != nil {
		s.opts.Audit(event, details)
	}

	encoded, err := json.Marshal(details)
	if err != nil {
		log.Printf("Scheduler: Failed to encode audit details for task '%s': %v", taskID, err)
		return
	}

	db := database.GetServerDB()
	if db == nil {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if _, err := db.ExecContext(ctx,
		`INSERT INTO audit_log (timestamp, event, actor, details) VALUES (?, ?, ?, ?)`,
		completed.UTC(), event, "scheduler", string(encoded)); err != nil {
		log.Printf("Scheduler: Failed to write audit entry for task '%s': %v", taskID, err)
	}
}

// notifyFailure dispatches the operator notification for a failed run.
// Backup and SSL-renewal failures send their dedicated event, which
// suppresses scheduler_error for the same execution ID; every other task
// (including token_cleanup, log_rotation and update_check) fires
// scheduler_error normally, per AI.md PART 18.
func (s *Scheduler) notifyFailure(executionID, taskID string, kind failureKind, runErr error, nextRun time.Time) {
	notifier := s.opts.Notifier
	if notifier == nil || len(s.opts.NotifyTo) == 0 {
		return
	}

	to := s.opts.NotifyTo
	nextRunText := nextRun.Format(time.RFC3339)

	switch kind {
	case failureBackup:
		if err := notifier.NotifyBackupFailed(executionID, to, taskID, "0", runErr.Error()); err != nil {
			log.Printf("Scheduler: Failed to send backup_failed notification: %v", err)
		}
	case failureSSLRenewal:
		if err := notifier.NotifySSLRenewalFailed(executionID, to, "", runErr.Error(), "", "", nextRunText); err != nil {
			log.Printf("Scheduler: Failed to send ssl_renewal_failed notification: %v", err)
		}
	}

	if err := notifier.NotifySchedulerError(executionID, to, taskID, runErr.Error(), nextRunText); err != nil {
		log.Printf("Scheduler: Failed to send scheduler_error notification: %v", err)
	}
}

// RunNow immediately runs a task by name
func (s *Scheduler) RunNow(name string) error {
	s.mu.RLock()
	task, ok := s.tasks[name]
	s.mu.RUnlock()

	if !ok {
		return nil
	}

	s.runTask(task)
	return nil
}

// GetTasks returns all registered tasks
func (s *Scheduler) GetTasks() []Task {
	s.mu.RLock()
	defer s.mu.RUnlock()

	tasks := make([]Task, 0, len(s.tasks))
	for _, t := range s.tasks {
		tasks = append(tasks, *t)
	}
	return tasks
}

// Health reports whether the scheduler is operating correctly, for the
// checks.scheduler field of /server/healthz (AI.md PART 13). The scheduler
// itself is mandatory and always running (PART 18), so a stopped loop is a
// real fault. A non-skippable task whose most recent run failed is also a
// fault, because those tasks (ssl_renewal, token_cleanup, log_rotation,
// healthcheck_self) cannot be disabled by an operator and their failure means
// the server is degraded. Skippable task failures are reported through the
// scheduler_error notification instead, not through the health endpoint.
func (s *Scheduler) Health() error {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if !s.running {
		return errors.New("scheduler loop is not running")
	}

	for _, t := range s.tasks {
		if t.Skippable || !t.Enabled {
			continue
		}
		if t.LastStatus == "failed" {
			return fmt.Errorf("required task %s last run failed", t.Name)
		}
	}
	return nil
}

// ParseInterval parses interval strings like "hourly", "daily", "weekly"
func ParseInterval(s string) time.Duration {
	switch s {
	case "minutely":
		return time.Minute
	case "hourly":
		return time.Hour
	case "daily":
		return 24 * time.Hour
	case "weekly":
		return 7 * 24 * time.Hour
	case "monthly":
		return 30 * 24 * time.Hour
	default:
		// Fall back to a raw Go duration, then to daily.
		if d, err := time.ParseDuration(s); err == nil {
			return d
		}
		return 24 * time.Hour
	}
}
