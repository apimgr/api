package scheduler

import (
	"log"
	"time"

	"github.com/apimgr/api/src/config"
	"github.com/apimgr/api/src/email"
)

// DefaultTimezone is AI.md PART 18's default scheduler timezone
// (server.scheduler.timezone).
const DefaultTimezone = "America/New_York"

// DefaultCatchUpWindow bounds how far in the past a missed next_run may be
// and still be run immediately on startup (server.scheduler.catch_up_window).
const DefaultCatchUpWindow = 1 * time.Hour

// maxRetries is AI.md PART 18's default retry ceiling.
const maxRetries = 3

// retryBackoff is PART 18's exponential retry ladder (5m, 10m, 20m).
var retryBackoff = []time.Duration{5 * time.Minute, 10 * time.Minute, 20 * time.Minute}

// TaskOverride carries operator overrides for one built-in task, mirroring
// the server.scheduler.tasks.<id> block in AI.md PART 18. A nil Enabled
// leaves the built-in default in place; a blank Schedule keeps the built-in
// schedule.
type TaskOverride struct {
	// Schedule replaces the built-in cron/interval expression.
	Schedule string
	// Enabled toggles the task; nil keeps the built-in default.
	Enabled *bool
	// RetryOnFail turns the retry ladder off for this task when explicitly
	// false, so a failed run resumes its normal schedule immediately.
	RetryOnFail *bool
	// RetryDelay replaces the global backoff ladder with a fixed delay for
	// this task alone. Zero keeps the ladder.
	RetryDelay time.Duration
	// RestartOnFail lets a health-check task restart the component it
	// watches; PART 18 defines it for tor_health.
	RestartOnFail *bool
}

// Options configures a Scheduler. Every field has a spec default, so the
// zero value produces a fully compliant scheduler. There is deliberately no
// "enabled" field: PART 18 requires the scheduler itself to always run and
// only allows individual tasks to be toggled.
type Options struct {
	// Timezone is the IANA zone cron schedules are evaluated in.
	Timezone string
	// CatchUpWindow bounds startup catch-up of missed runs.
	CatchUpWindow time.Duration
	// MaxRetries is the per-task retry ceiling before the normal schedule
	// resumes.
	MaxRetries int
	// RetryBackoff is the delay ladder applied to consecutive retries. The
	// last entry repeats once the ladder is exhausted.
	RetryBackoff []time.Duration
	// Overrides maps a task id to its operator overrides.
	Overrides map[string]TaskOverride
	// Notifier dispatches operator failure emails. Nil disables email
	// notification; the structured log line and audit entry still happen.
	Notifier *email.Notifier
	// NotifyTo is the recipient list for failure notifications, normally
	// the resolved server.contact.admin address.
	NotifyTo []string
	// Audit receives one entry per task execution in addition to the
	// database audit_log row. Nil is allowed - it is an extra sink, not the
	// only one.
	Audit func(event string, details map[string]interface{})
	// Version is the running server version, used by the update_check task.
	Version string
	// BuildEpoch is the embedded build timestamp (Unix seconds), used by
	// update_check to compare dated pre-release channels.
	BuildEpoch int64
}

// normalized fills in every unset field with its AI.md PART 18 default and
// resolves the configured timezone. An unknown timezone warns and falls back
// to the default zone rather than failing, per the config-validation rule in
// PART 5 ("never fail startup on invalid config").
func (o Options) normalized() (Options, *time.Location) {
	if o.Timezone == "" {
		o.Timezone = DefaultTimezone
	}
	if o.CatchUpWindow <= 0 {
		o.CatchUpWindow = DefaultCatchUpWindow
	}
	if o.MaxRetries <= 0 {
		o.MaxRetries = maxRetries
	}
	if len(o.RetryBackoff) == 0 {
		o.RetryBackoff = retryBackoff
	}
	if o.Version == "" {
		o.Version = "dev"
	}

	loc, err := time.LoadLocation(o.Timezone)
	if err != nil {
		log.Printf("Scheduler: Unknown timezone %q, falling back to %s: %v", o.Timezone, DefaultTimezone, err)
		loc, err = time.LoadLocation(DefaultTimezone)
		if err != nil {
			loc = time.UTC
		}
		o.Timezone = DefaultTimezone
	}

	return o, loc
}

// backoffFor returns the retry delay for attempt number n (0-based),
// repeating the final ladder entry once the ladder is exhausted.
func (o Options) backoffFor(attempt int) time.Duration {
	if attempt < 0 {
		attempt = 0
	}
	if attempt >= len(o.RetryBackoff) {
		return o.RetryBackoff[len(o.RetryBackoff)-1]
	}
	return o.RetryBackoff[attempt]
}

// overrideFor returns the operator override for a task id, if any.
func (o Options) overrideFor(id string) (TaskOverride, bool) {
	if o.Overrides == nil {
		return TaskOverride{}, false
	}
	override, ok := o.Overrides[id]
	return override, ok
}

// retryPolicyFor resolves the retry behaviour for one task: whether the task
// retries at all, and the delay for the given 0-based attempt. A per-task
// retry_delay replaces the global ladder; retry_on_fail: false disables
// retrying entirely for that task.
func (o Options) retryPolicyFor(id string, attempt int) (time.Duration, bool) {
	override, ok := o.overrideFor(id)
	if !ok {
		return o.backoffFor(attempt), true
	}
	if override.RetryOnFail != nil && !*override.RetryOnFail {
		return 0, false
	}
	if override.RetryDelay > 0 {
		return override.RetryDelay, true
	}
	return o.backoffFor(attempt), true
}

// restartOnFail reports whether a health-check task may restart the component
// it watches. Absent configuration keeps AI.md PART 18's default of true for
// tor_health.
func (o Options) restartOnFail(id string) bool {
	override, ok := o.overrideFor(id)
	if !ok || override.RestartOnFail == nil {
		return true
	}
	return *override.RestartOnFail
}

// OptionsFromConfig maps the server.scheduler tree (AI.md PART 18) onto the
// scheduler's Options. The config is normalized first, so an invalid timezone,
// window, retry count or backoff strategy degrades to its documented default
// rather than failing startup. Only the scheduling knobs come from config: the
// notifier, audit hook and version fields stay caller-supplied.
func OptionsFromConfig(cfg config.SchedulerConfig) Options {
	normalized := cfg.Normalized()

	overrides := make(map[string]TaskOverride, len(normalized.Tasks))
	for id, task := range normalized.Tasks {
		override := TaskOverride{Schedule: task.Schedule}
		if task.Enabled.Set {
			enabled := task.Enabled.Value
			override.Enabled = &enabled
		}
		if task.RetryOnFail.Set {
			retry := task.RetryOnFail.Value
			override.RetryOnFail = &retry
		}
		if task.RestartOnFail.Set {
			restart := task.RestartOnFail.Value
			override.RestartOnFail = &restart
		}
		if delay, ok := normalized.TaskRetryDelay(id); ok {
			override.RetryDelay = delay
		}
		overrides[id] = override
	}

	return Options{
		Timezone:      normalized.Timezone,
		CatchUpWindow: normalized.CatchUpWindowDuration(),
		MaxRetries:    normalized.MaxRetries,
		RetryBackoff:  normalized.RetryBackoff(),
		Overrides:     overrides,
	}
}
