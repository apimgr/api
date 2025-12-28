package scheduler

import (
	"log"
	"sync"
	"time"
)

// Task represents a scheduled task
type Task struct {
	Name     string
	Interval time.Duration
	Func     func() error
	LastRun  time.Time
	NextRun  time.Time
	Enabled  bool
}

// Scheduler manages periodic tasks
type Scheduler struct {
	tasks   map[string]*Task
	stop    chan struct{}
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

// AddTask adds a new task to the scheduler
// schedule: cron expression, @hourly, @daily, @weekly, @every Xm
func (s *Scheduler) AddTask(name string, schedule string, fn func() error, enabled bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	interval, err := parseScheduleExpression(schedule)
	if err != nil {
		log.Printf("Scheduler: Failed to parse schedule '%s' for task '%s': %v", schedule, name, err)
		interval = 24 * time.Hour // Default to daily
	}

	s.tasks[name] = &Task{
		Name:     name,
		Interval: interval,
		Func:     fn,
		NextRun:  time.Now().Add(interval),
		Enabled:  enabled,
	}
	log.Printf("Scheduler: Added task '%s' (schedule: %s, interval: %v, enabled: %v)", name, schedule, interval, enabled)
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
	if task, ok := s.tasks[name]; ok {
		task.Enabled = true
		task.NextRun = time.Now().Add(task.Interval)
	}
}

// DisableTask disables a task
func (s *Scheduler) DisableTask(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if task, ok := s.tasks[name]; ok {
		task.Enabled = false
	}
}

// Start begins the scheduler loop
func (s *Scheduler) Start() {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return
	}
	s.running = true
	s.stop = make(chan struct{})
	s.mu.Unlock()

	log.Printf("Scheduler: Started with %d tasks", len(s.tasks))

	go func() {
		ticker := time.NewTicker(30 * time.Second) // Check every 30 seconds
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

// Stop stops the scheduler
func (s *Scheduler) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.running {
		return
	}

	close(s.stop)
	s.running = false
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
		go func(t *Task) {
			log.Printf("Scheduler: Running task '%s'", t.Name)
			if err := t.Func(); err != nil {
				log.Printf("Scheduler: Task '%s' failed: %v", t.Name, err)
			} else {
				log.Printf("Scheduler: Task '%s' completed", t.Name)
			}

			s.mu.Lock()
			t.LastRun = time.Now()
			t.NextRun = t.LastRun.Add(t.Interval)
			s.mu.Unlock()
		}(task)
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

	log.Printf("Scheduler: Running task '%s' immediately", name)
	err := task.Func()

	s.mu.Lock()
	task.LastRun = time.Now()
	task.NextRun = task.LastRun.Add(task.Interval)
	s.mu.Unlock()

	return err
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
