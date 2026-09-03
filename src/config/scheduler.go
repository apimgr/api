package config

import (
	"log"
	"time"

	"gopkg.in/yaml.v3"
)

// DefaultSchedulerTimezone is the IANA zone cron fields are evaluated in when
// the operator sets nothing (AI.md PART 18 "Task Configuration").
const DefaultSchedulerTimezone = "America/New_York"

// DefaultSchedulerCatchUpWindow is how far in the past a missed run may be and
// still be executed on startup (AI.md PART 18 "Startup Behavior").
const DefaultSchedulerCatchUpWindow = "1h"

// DefaultSchedulerMaxRetries is the per-task retry ceiling from AI.md PART 18
// "Retry Policy".
const DefaultSchedulerMaxRetries = 3

// DefaultSchedulerRetryDelay is the base delay of the retry ladder from AI.md
// PART 18 "Retry Policy".
const DefaultSchedulerRetryDelay = "5m"

// BackoffExponential is the only backoff strategy AI.md PART 18 defines: each
// retry doubles the previous delay, producing 5m, 10m, 20m at the defaults.
const BackoffExponential = "exponential"

// OptionalBool is a tri-state YAML boolean. An absent or unparsable key leaves
// Set false so the compiled-in default survives, which is what AI.md PART 5
// requires of invalid config ("warn and substitute the default"). Values are
// read through ParseBool, so the full truthy/falsey word set is accepted, not
// just true/false.
type OptionalBool struct {
	// Set reports whether the operator supplied a value at all.
	Set bool
	// Value is the parsed boolean, meaningful only when Set is true.
	Value bool
}

// NewOptionalBool builds an explicitly-set OptionalBool, used for the
// compiled-in per-task defaults.
func NewOptionalBool(value bool) OptionalBool {
	return OptionalBool{Set: true, Value: value}
}

// UnmarshalYAML reads any scalar form (true, "yes", "on", "disabled", ...) via
// ParseBool. An invalid word warns and stays unset rather than failing config
// load.
func (b *OptionalBool) UnmarshalYAML(node *yaml.Node) error {
	if node.Tag == "!!null" || node.Value == "" {
		return nil
	}
	parsed, err := ParseBool(node.Value, false)
	if err != nil {
		log.Printf("Config: server.scheduler: %v, ignoring and keeping the built-in default", err)
		return nil
	}
	b.Set = true
	b.Value = parsed
	return nil
}

// MarshalYAML writes the plain boolean when set and null when the operator
// never expressed a preference.
func (b OptionalBool) MarshalYAML() (interface{}, error) {
	if !b.Set {
		return nil, nil
	}
	return b.Value, nil
}

// SchedulerTaskConfig holds the server.scheduler.tasks.<id> block from AI.md
// PART 18. Every field is an override: an unset field leaves the built-in
// value for that task in place.
type SchedulerTaskConfig struct {
	// Schedule is a 5-field cron expression or an @hourly/@daily/@weekly/
	// @monthly/@every X shorthand.
	Schedule string `yaml:"schedule"`
	// Enabled toggles the task. Tasks AI.md PART 18 marks not skippable
	// ignore a false value and keep running.
	Enabled OptionalBool `yaml:"enabled"`
	// RetryOnFail turns the retry ladder on or off for this task alone. When
	// explicitly false a failed run resumes the normal schedule immediately.
	RetryOnFail OptionalBool `yaml:"retry_on_fail"`
	// RetryDelay replaces the global retry ladder with a fixed delay for this
	// task, e.g. "1h" for blocklist_update and cve_update.
	RetryDelay string `yaml:"retry_delay"`
	// RestartOnFail lets a health-check task restart the component it
	// watches; PART 18 defines it for tor_health.
	RestartOnFail OptionalBool `yaml:"restart_on_fail"`
}

// SchedulerConfig holds the server.scheduler tree from AI.md PART 18. There is
// deliberately no enable/disable key for the scheduler itself: PART 18 requires
// it to run from application start until shutdown, and only individual tasks
// carry an enabled flag.
type SchedulerConfig struct {
	// Timezone is the IANA zone cron schedules are evaluated in.
	Timezone string `yaml:"timezone"`
	// CatchUpWindow is a Go duration bounding startup catch-up of missed runs.
	CatchUpWindow string `yaml:"catch_up_window"`
	// MaxRetries is the retry ceiling before a failed task resumes its normal
	// schedule.
	MaxRetries int `yaml:"max_retries"`
	// RetryDelay is the base delay of the retry ladder.
	RetryDelay string `yaml:"retry_delay"`
	// Backoff is the ladder growth strategy; "exponential" is the only value
	// AI.md PART 18 defines.
	Backoff string `yaml:"backoff"`
	// Tasks maps a built-in task id to its operator overrides.
	Tasks map[string]SchedulerTaskConfig `yaml:"tasks"`
}

// defaultSchedulerConfig returns the AI.md PART 18 defaults verbatim: the
// timezone, catch-up window and retry policy from the settings tables, plus
// the per-task schedule/enabled values from the "Task Configuration" YAML.
func defaultSchedulerConfig() SchedulerConfig {
	return SchedulerConfig{
		Timezone:      DefaultSchedulerTimezone,
		CatchUpWindow: DefaultSchedulerCatchUpWindow,
		MaxRetries:    DefaultSchedulerMaxRetries,
		RetryDelay:    DefaultSchedulerRetryDelay,
		Backoff:       BackoffExponential,
		Tasks: map[string]SchedulerTaskConfig{
			"ssl_renewal": {
				Schedule: "0 3 * * *",
				Enabled:  NewOptionalBool(true),
			},
			"geoip_update": {
				Schedule: "0 3 * * 0",
				Enabled:  NewOptionalBool(true),
			},
			"blocklist_update": {
				Schedule:    "0 4 * * *",
				Enabled:     NewOptionalBool(true),
				RetryOnFail: NewOptionalBool(true),
				RetryDelay:  "1h",
			},
			"cve_update": {
				Schedule:    "0 5 * * *",
				Enabled:     NewOptionalBool(true),
				RetryOnFail: NewOptionalBool(true),
				RetryDelay:  "1h",
			},
			"update_check": {
				Schedule: "0 6 * * *",
				Enabled:  NewOptionalBool(true),
			},
			"token_cleanup": {
				Schedule: "@every 15m",
				Enabled:  NewOptionalBool(true),
			},
			"log_rotation": {
				Schedule: "0 0 * * *",
				Enabled:  NewOptionalBool(true),
			},
			"backup_daily": {
				Schedule: "0 2 * * *",
				Enabled:  NewOptionalBool(true),
			},
			"backup_hourly": {
				Schedule: "@hourly",
				Enabled:  NewOptionalBool(false),
			},
			"healthcheck_self": {
				Schedule: "@every 5m",
				Enabled:  NewOptionalBool(true),
			},
			"tor_health": {
				Schedule:      "@every 10m",
				Enabled:       NewOptionalBool(true),
				RestartOnFail: NewOptionalBool(true),
			},
		},
	}
}

// DefaultSchedulerConfig exposes the PART 18 defaults to callers outside the
// config package, which need them when building a scheduler without a loaded
// config file.
func DefaultSchedulerConfig() SchedulerConfig {
	return defaultSchedulerConfig()
}

// Normalized returns the config with every missing or invalid value replaced by
// its AI.md PART 18 default, so an operator typo degrades to the documented
// behaviour instead of failing startup (PART 5 config-validation rule). Tasks
// the operator did not mention keep their default block.
func (s SchedulerConfig) Normalized() SchedulerConfig {
	def := defaultSchedulerConfig()

	if s.Timezone == "" {
		s.Timezone = def.Timezone
	} else if _, err := time.LoadLocation(s.Timezone); err != nil {
		log.Printf("Config: server.scheduler.timezone %q is not a known IANA zone, using %s", s.Timezone, def.Timezone)
		s.Timezone = def.Timezone
	}

	if d, err := time.ParseDuration(s.CatchUpWindow); err != nil || d <= 0 {
		if s.CatchUpWindow != "" {
			log.Printf("Config: server.scheduler.catch_up_window %q is not a positive duration, using %s", s.CatchUpWindow, def.CatchUpWindow)
		}
		s.CatchUpWindow = def.CatchUpWindow
	}

	if s.MaxRetries < 1 {
		s.MaxRetries = def.MaxRetries
	}

	if d, err := time.ParseDuration(s.RetryDelay); err != nil || d <= 0 {
		if s.RetryDelay != "" {
			log.Printf("Config: server.scheduler.retry_delay %q is not a positive duration, using %s", s.RetryDelay, def.RetryDelay)
		}
		s.RetryDelay = def.RetryDelay
	}

	if s.Backoff != BackoffExponential {
		if s.Backoff != "" {
			log.Printf("Config: server.scheduler.backoff %q is not supported, using %s", s.Backoff, BackoffExponential)
		}
		s.Backoff = BackoffExponential
	}

	tasks := make(map[string]SchedulerTaskConfig, len(def.Tasks)+len(s.Tasks))
	for id, task := range def.Tasks {
		tasks[id] = task
	}
	for id, task := range s.Tasks {
		if task.RetryDelay != "" {
			if d, err := time.ParseDuration(task.RetryDelay); err != nil || d <= 0 {
				log.Printf("Config: server.scheduler.tasks.%s.retry_delay %q is not a positive duration, ignoring", id, task.RetryDelay)
				task.RetryDelay = ""
			}
		}
		tasks[id] = task
	}
	s.Tasks = tasks

	return s
}

// CatchUpWindowDuration returns the parsed catch-up window. It is only safe on
// a Normalized config, and falls back to the default if it is ever called on a
// raw one.
func (s SchedulerConfig) CatchUpWindowDuration() time.Duration {
	if d, err := time.ParseDuration(s.CatchUpWindow); err == nil && d > 0 {
		return d
	}
	d, _ := time.ParseDuration(DefaultSchedulerCatchUpWindow)
	return d
}

// RetryDelayDuration returns the parsed base retry delay, with the same
// fallback contract as CatchUpWindowDuration.
func (s SchedulerConfig) RetryDelayDuration() time.Duration {
	if d, err := time.ParseDuration(s.RetryDelay); err == nil && d > 0 {
		return d
	}
	d, _ := time.ParseDuration(DefaultSchedulerRetryDelay)
	return d
}

// RetryBackoff builds the retry delay ladder AI.md PART 18 documents: the base
// retry_delay doubled once per attempt, capped at max_retries entries, which
// yields 5m, 10m, 20m at the spec defaults.
func (s SchedulerConfig) RetryBackoff() []time.Duration {
	attempts := s.MaxRetries
	if attempts < 1 {
		attempts = DefaultSchedulerMaxRetries
	}

	delay := s.RetryDelayDuration()
	ladder := make([]time.Duration, 0, attempts)
	for i := 0; i < attempts; i++ {
		ladder = append(ladder, delay)
		delay *= 2
	}
	return ladder
}

// TaskRetryDelay returns the fixed per-task retry delay for a task id and
// whether one was configured. When set it replaces the global ladder for that
// task alone.
func (s SchedulerConfig) TaskRetryDelay(id string) (time.Duration, bool) {
	task, ok := s.Tasks[id]
	if !ok || task.RetryDelay == "" {
		return 0, false
	}
	d, err := time.ParseDuration(task.RetryDelay)
	if err != nil || d <= 0 {
		return 0, false
	}
	return d, true
}
