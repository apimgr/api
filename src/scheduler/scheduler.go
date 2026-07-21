package scheduler

import (
	"log"
	"sort"
	"sync"
	"time"

	"github.com/apimgr/api/src/database"
)

// catchUpWindow bounds how far in the past a missed next_run may be and
// still be run immediately on startup, per AI.md PART 18 startup flow.
const catchUpWindow = 1 * time.Hour

// Retry policy per AI.md PART 18: max 3 retries with exponential backoff.
const maxRetries = 3

var retryBackoff = []time.Duration{5 * time.Minute, 10 * time.Minute, 20 * time.Minute}

// Task represents a scheduled task
type Task struct {
	Name     string
	Schedule string
	Func     func() error
	LastRun  time.Time
	NextRun  time.Time
	Enabled  bool

	sched   schedule
	retries int
}

// Scheduler manages periodic tasks
type Scheduler struct {
	tasks   map[string]*Task
	stop    chan struct{}
	wg      sync.WaitGroup
	running bool
	mu      sync.RWMutex
}

// New creates a new scheduler
func New() *Scheduler {
	return &Scheduler{
		tasks: make(map[string]*Task),
		stop:  make(chan struct{}),
	}
}

// AddTask adds a new task to the scheduler.
// schedule: 5-field cron expression, @hourly, @daily, @weekly, @monthly, or
// @every X. Persisted state (next_run, enabled, last_run) is restored from
// the database if a row already exists for this task, so schedules survive
// restarts per AI.md PART 18.
func (s *Scheduler) AddTask(name string, sched string, fn func() error, enabledDefault bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	parsed, err := parseSchedule(sched)
	if err != nil {
		log.Printf("Scheduler: Failed to parse schedule '%s' for task '%s': %v", sched, name, err)
		parsed, _ = parseSchedule("@daily")
	}

	now := time.Now()
	task := &Task{
		Name:     name,
		Schedule: sched,
		Func:     fn,
		NextRun:  parsed.next(now),
		Enabled:  enabledDefault,
		sched:    parsed,
	}

	if persisted, perr := database.GetSchedulerTask(name); perr == nil && persisted != nil {
		task.NextRun = persisted.NextRun
		task.Enabled = persisted.Enabled
		if persisted.LastRun.Valid {
			task.LastRun = persisted.LastRun.Time
		}
	}

	s.tasks[name] = task

	if err := database.UpsertSchedulerTask(name, name, sched, task.NextRun, task.Enabled); err != nil {
		log.Printf("Scheduler: Failed to persist task '%s': %v", name, err)
	}

	log.Printf("Scheduler: Added task '%s' (schedule: %s, next run: %s, enabled: %v)",
		name, sched, task.NextRun.Format(time.RFC3339), task.Enabled)
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
	task.NextRun = task.sched.next(time.Now())
	if err := database.UpsertSchedulerTask(name, name, task.Schedule, task.NextRun, true); err != nil {
		log.Printf("Scheduler: Failed to persist task '%s': %v", name, err)
	}
}

// DisableTask disables a task
func (s *Scheduler) DisableTask(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	task, ok := s.tasks[name]
	if !ok {
		return
	}
	task.Enabled = false
	if err := database.UpsertSchedulerTask(name, name, task.Schedule, task.NextRun, false); err != nil {
		log.Printf("Scheduler: Failed to persist task '%s': %v", name, err)
	}
}

// Start begins the scheduler loop. Before entering the normal polling loop
// it runs catch-up: any enabled task whose persisted next_run already
// elapsed, but by no more than catchUpWindow, is run immediately (in order
// of original scheduled time); tasks missed by more than the window have
// their missed run skipped and next_run recomputed, per AI.md PART 18.
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
	log.Printf("Scheduler: Started with %d tasks", taskCount)

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
	now := time.Now()

	s.mu.Lock()
	var overdue []*Task
	for _, task := range s.tasks {
		if task.Enabled && now.After(task.NextRun) {
			overdue = append(overdue, task)
		}
	}
	sort.Slice(overdue, func(i, j int) bool { return overdue[i].NextRun.Before(overdue[j].NextRun) })

	var toRun []*Task
	for _, task := range overdue {
		if now.Sub(task.NextRun) <= catchUpWindow {
			toRun = append(toRun, task)
		} else {
			log.Printf("Scheduler: Task '%s' missed run at %s (outside %s catch-up window), skipping to next occurrence",
				task.Name, task.NextRun.Format(time.RFC3339), catchUpWindow)
			task.NextRun = task.sched.next(now)
			if err := database.UpsertSchedulerTask(task.Name, task.Name, task.Schedule, task.NextRun, task.Enabled); err != nil {
				log.Printf("Scheduler: Failed to persist task '%s': %v", task.Name, err)
			}
		}
	}
	s.mu.Unlock()

	for _, task := range toRun {
		log.Printf("Scheduler: Catching up missed run of task '%s' (was due %s)", task.Name, task.NextRun.Format(time.RFC3339))
		s.runTask(task)
	}
}

// Stop stops the scheduler and waits for in-flight tasks to finish, up to
// 30 seconds, per AI.md PART 18 graceful shutdown.
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
		log.Println("Scheduler: Shutdown wait timed out after 30s, some tasks may be interrupted")
	}
}

// runDueTasks executes tasks that are due
func (s *Scheduler) runDueTasks() {
	s.mu.Lock()
	now := time.Now()
	dueTasks := make([]*Task, 0)

	for _, task := range s.tasks {
		if task.Enabled && now.After(task.NextRun) {
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

		log.Printf("Scheduler: Running task '%s'", t.Name)
		started := time.Now()
		runErr := t.Func()
		completed := time.Now()

		s.mu.Lock()
		t.LastRun = completed

		status := "success"
		if runErr != nil {
			status = "failed"
			log.Printf("Scheduler: Task '%s' failed: %v", t.Name, runErr)

			if t.retries < maxRetries {
				delay := retryBackoff[t.retries]
				t.retries++
				t.NextRun = completed.Add(delay)
				log.Printf("Scheduler: Task '%s' will retry in %s (attempt %d/%d)", t.Name, delay, t.retries, maxRetries)
			} else {
				log.Printf("Scheduler: Task '%s' exhausted %d retries, resuming normal schedule", t.Name, maxRetries)
				t.retries = 0
				t.NextRun = t.sched.next(completed)
			}
		} else {
			log.Printf("Scheduler: Task '%s' completed", t.Name)
			t.retries = 0
			t.NextRun = t.sched.next(completed)
		}

		nextRun := t.NextRun
		s.mu.Unlock()

		database.RecordSchedulerRun(t.Name, started, completed, status, runErr, nextRun)
	}()
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
		// Try to parse as duration
		if d, err := time.ParseDuration(s); err == nil {
			return d
		}
		return 24 * time.Hour // Default to daily
	}
}
