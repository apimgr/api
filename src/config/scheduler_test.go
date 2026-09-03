package config

import (
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

// TestDefaultSchedulerConfig checks the compiled-in defaults against the
// AI.md PART 18 "Task Configuration" and "Retry Policy" tables.
func TestDefaultSchedulerConfig(t *testing.T) {
	cfg := DefaultSchedulerConfig()

	if cfg.Timezone != "America/New_York" {
		t.Errorf("timezone = %q, want America/New_York", cfg.Timezone)
	}
	if cfg.CatchUpWindow != "1h" {
		t.Errorf("catch_up_window = %q, want 1h", cfg.CatchUpWindow)
	}
	if cfg.MaxRetries != 3 {
		t.Errorf("max_retries = %d, want 3", cfg.MaxRetries)
	}
	if cfg.RetryDelay != "5m" {
		t.Errorf("retry_delay = %q, want 5m", cfg.RetryDelay)
	}
	if cfg.Backoff != BackoffExponential {
		t.Errorf("backoff = %q, want exponential", cfg.Backoff)
	}

	tests := []struct {
		id       string
		schedule string
		enabled  bool
	}{
		{"ssl_renewal", "0 3 * * *", true},
		{"geoip_update", "0 3 * * 0", true},
		{"blocklist_update", "0 4 * * *", true},
		{"cve_update", "0 5 * * *", true},
		{"update_check", "0 6 * * *", true},
		{"token_cleanup", "@every 15m", true},
		{"log_rotation", "0 0 * * *", true},
		{"backup_daily", "0 2 * * *", true},
		{"backup_hourly", "@hourly", false},
		{"healthcheck_self", "@every 5m", true},
		{"tor_health", "@every 10m", true},
	}

	for _, tc := range tests {
		t.Run(tc.id, func(t *testing.T) {
			task, ok := cfg.Tasks[tc.id]
			if !ok {
				t.Fatalf("task %q missing from defaults", tc.id)
			}
			if task.Schedule != tc.schedule {
				t.Errorf("schedule = %q, want %q", task.Schedule, tc.schedule)
			}
			if !task.Enabled.Set {
				t.Fatalf("enabled not set for %q", tc.id)
			}
			if task.Enabled.Value != tc.enabled {
				t.Errorf("enabled = %v, want %v", task.Enabled.Value, tc.enabled)
			}
		})
	}

	if len(cfg.Tasks) != len(tests) {
		t.Errorf("task count = %d, want %d", len(cfg.Tasks), len(tests))
	}
}

// TestDefaultSchedulerTaskRetryOverrides checks the per-task retry and restart
// defaults AI.md PART 18 sets on blocklist_update, cve_update and tor_health.
func TestDefaultSchedulerTaskRetryOverrides(t *testing.T) {
	cfg := DefaultSchedulerConfig()

	for _, id := range []string{"blocklist_update", "cve_update"} {
		task := cfg.Tasks[id]
		if !task.RetryOnFail.Set || !task.RetryOnFail.Value {
			t.Errorf("%s: retry_on_fail = %+v, want set true", id, task.RetryOnFail)
		}
		delay, ok := cfg.TaskRetryDelay(id)
		if !ok || delay != time.Hour {
			t.Errorf("%s: retry_delay = %s (ok=%v), want 1h", id, delay, ok)
		}
	}

	tor := cfg.Tasks["tor_health"]
	if !tor.RestartOnFail.Set || !tor.RestartOnFail.Value {
		t.Errorf("tor_health: restart_on_fail = %+v, want set true", tor.RestartOnFail)
	}
}

// TestSchedulerConfigNormalized checks that invalid scalars fall back to their
// PART 18 defaults instead of failing, per the PART 5 config-validation rule.
func TestSchedulerConfigNormalized(t *testing.T) {
	def := DefaultSchedulerConfig()

	tests := []struct {
		name  string
		in    SchedulerConfig
		check func(t *testing.T, got SchedulerConfig)
	}{
		{
			name: "empty config takes every default",
			in:   SchedulerConfig{},
			check: func(t *testing.T, got SchedulerConfig) {
				if got.Timezone != def.Timezone || got.CatchUpWindow != def.CatchUpWindow {
					t.Errorf("got %+v, want defaults", got)
				}
				if got.MaxRetries != def.MaxRetries || got.RetryDelay != def.RetryDelay {
					t.Errorf("retry policy = %d/%s, want %d/%s", got.MaxRetries, got.RetryDelay, def.MaxRetries, def.RetryDelay)
				}
				if len(got.Tasks) != len(def.Tasks) {
					t.Errorf("task count = %d, want %d", len(got.Tasks), len(def.Tasks))
				}
			},
		},
		{
			name: "unknown timezone falls back",
			in:   SchedulerConfig{Timezone: "Mars/Olympus_Mons"},
			check: func(t *testing.T, got SchedulerConfig) {
				if got.Timezone != def.Timezone {
					t.Errorf("timezone = %q, want %q", got.Timezone, def.Timezone)
				}
			},
		},
		{
			name: "valid timezone is preserved",
			in:   SchedulerConfig{Timezone: "UTC"},
			check: func(t *testing.T, got SchedulerConfig) {
				if got.Timezone != "UTC" {
					t.Errorf("timezone = %q, want UTC", got.Timezone)
				}
			},
		},
		{
			name: "unparsable catch_up_window falls back",
			in:   SchedulerConfig{CatchUpWindow: "one hour"},
			check: func(t *testing.T, got SchedulerConfig) {
				if got.CatchUpWindowDuration() != time.Hour {
					t.Errorf("catch_up_window = %s, want 1h", got.CatchUpWindowDuration())
				}
			},
		},
		{
			name: "non-positive catch_up_window falls back",
			in:   SchedulerConfig{CatchUpWindow: "-5m"},
			check: func(t *testing.T, got SchedulerConfig) {
				if got.CatchUpWindow != def.CatchUpWindow {
					t.Errorf("catch_up_window = %q, want %q", got.CatchUpWindow, def.CatchUpWindow)
				}
			},
		},
		{
			name: "custom catch_up_window is preserved",
			in:   SchedulerConfig{CatchUpWindow: "30m"},
			check: func(t *testing.T, got SchedulerConfig) {
				if got.CatchUpWindowDuration() != 30*time.Minute {
					t.Errorf("catch_up_window = %s, want 30m", got.CatchUpWindowDuration())
				}
			},
		},
		{
			name: "negative max_retries falls back",
			in:   SchedulerConfig{MaxRetries: -1},
			check: func(t *testing.T, got SchedulerConfig) {
				if got.MaxRetries != def.MaxRetries {
					t.Errorf("max_retries = %d, want %d", got.MaxRetries, def.MaxRetries)
				}
			},
		},
		{
			name: "unsupported backoff falls back to exponential",
			in:   SchedulerConfig{Backoff: "fibonacci"},
			check: func(t *testing.T, got SchedulerConfig) {
				if got.Backoff != BackoffExponential {
					t.Errorf("backoff = %q, want exponential", got.Backoff)
				}
			},
		},
		{
			name: "invalid per-task retry_delay is dropped",
			in: SchedulerConfig{
				Tasks: map[string]SchedulerTaskConfig{
					"cve_update": {Schedule: "0 5 * * *", RetryDelay: "soon"},
				},
			},
			check: func(t *testing.T, got SchedulerConfig) {
				if _, ok := got.TaskRetryDelay("cve_update"); ok {
					t.Error("invalid per-task retry_delay was kept")
				}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tc.check(t, tc.in.Normalized())
		})
	}
}

// TestSchedulerConfigNormalizedMergesTaskOverrides verifies that naming one
// task in server.yml overrides only that task and leaves the other ten at
// their PART 18 defaults.
func TestSchedulerConfigNormalizedMergesTaskOverrides(t *testing.T) {
	cfg := SchedulerConfig{
		Tasks: map[string]SchedulerTaskConfig{
			"backup_hourly": {Schedule: "@every 30m", Enabled: NewOptionalBool(true)},
		},
	}

	got := cfg.Normalized()

	hourly := got.Tasks["backup_hourly"]
	if hourly.Schedule != "@every 30m" {
		t.Errorf("backup_hourly schedule = %q, want @every 30m", hourly.Schedule)
	}
	if !hourly.Enabled.Set || !hourly.Enabled.Value {
		t.Errorf("backup_hourly enabled = %+v, want set true", hourly.Enabled)
	}

	daily := got.Tasks["backup_daily"]
	if daily.Schedule != "0 2 * * *" {
		t.Errorf("backup_daily schedule = %q, want the default 0 2 * * *", daily.Schedule)
	}
	if len(got.Tasks) != len(DefaultSchedulerConfig().Tasks) {
		t.Errorf("task count = %d, want %d", len(got.Tasks), len(DefaultSchedulerConfig().Tasks))
	}
}

// TestSchedulerConfigRetryBackoff checks the ladder AI.md PART 18 documents:
// 5m, 10m, 20m at the defaults, rebased on a custom retry_delay.
func TestSchedulerConfigRetryBackoff(t *testing.T) {
	tests := []struct {
		name string
		in   SchedulerConfig
		want []time.Duration
	}{
		{
			name: "spec defaults",
			in:   SchedulerConfig{},
			want: []time.Duration{5 * time.Minute, 10 * time.Minute, 20 * time.Minute},
		},
		{
			name: "custom base delay",
			in:   SchedulerConfig{RetryDelay: "2m"},
			want: []time.Duration{2 * time.Minute, 4 * time.Minute, 8 * time.Minute},
		},
		{
			name: "custom retry count",
			in:   SchedulerConfig{MaxRetries: 2},
			want: []time.Duration{5 * time.Minute, 10 * time.Minute},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.in.Normalized().RetryBackoff()
			if len(got) != len(tc.want) {
				t.Fatalf("ladder = %v, want %v", got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Errorf("ladder[%d] = %s, want %s", i, got[i], tc.want[i])
				}
			}
		})
	}
}

// TestOptionalBoolUnmarshalYAML verifies the tri-state boolean accepts the
// full truthy/falsey word set and leaves itself unset for absent or bad input.
func TestOptionalBoolUnmarshalYAML(t *testing.T) {
	tests := []struct {
		name  string
		yaml  string
		set   bool
		value bool
	}{
		{name: "native true", yaml: "enabled: true", set: true, value: true},
		{name: "native false", yaml: "enabled: false", set: true, value: false},
		{name: "word yes", yaml: "enabled: yes", set: true, value: true},
		{name: "word off", yaml: "enabled: off", set: true, value: false},
		{name: "word disabled", yaml: "enabled: disabled", set: true, value: false},
		{name: "absent key", yaml: "schedule: \"@hourly\"", set: false},
		{name: "explicit null", yaml: "enabled: null", set: false},
		{name: "invalid word ignored", yaml: "enabled: maybe", set: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var task SchedulerTaskConfig
			if err := yaml.Unmarshal([]byte(tc.yaml), &task); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if task.Enabled.Set != tc.set {
				t.Fatalf("set = %v, want %v", task.Enabled.Set, tc.set)
			}
			if tc.set && task.Enabled.Value != tc.value {
				t.Errorf("value = %v, want %v", task.Enabled.Value, tc.value)
			}
		})
	}
}

// TestSchedulerConfigYAMLRoundTrip parses the server.scheduler block exactly as
// AI.md PART 18 prints it and checks the resulting values.
func TestSchedulerConfigYAMLRoundTrip(t *testing.T) {
	const doc = `
timezone: America/Chicago
catch_up_window: 2h
max_retries: 5
retry_delay: 10m
backoff: exponential
tasks:
  ssl_renewal:
    schedule: "0 3 * * *"
    enabled: true
  blocklist_update:
    schedule: "0 4 * * *"
    enabled: true
    retry_on_fail: true
    retry_delay: 1h
  backup_hourly:
    schedule: "@hourly"
    enabled: false
  tor_health:
    schedule: "@every 10m"
    enabled: true
    restart_on_fail: false
`

	var cfg SchedulerConfig
	if err := yaml.Unmarshal([]byte(doc), &cfg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	got := cfg.Normalized()

	if got.Timezone != "America/Chicago" {
		t.Errorf("timezone = %q, want America/Chicago", got.Timezone)
	}
	if got.CatchUpWindowDuration() != 2*time.Hour {
		t.Errorf("catch_up_window = %s, want 2h", got.CatchUpWindowDuration())
	}
	if got.MaxRetries != 5 {
		t.Errorf("max_retries = %d, want 5", got.MaxRetries)
	}
	if got.RetryDelayDuration() != 10*time.Minute {
		t.Errorf("retry_delay = %s, want 10m", got.RetryDelayDuration())
	}
	if delay, ok := got.TaskRetryDelay("blocklist_update"); !ok || delay != time.Hour {
		t.Errorf("blocklist_update retry_delay = %s (ok=%v), want 1h", delay, ok)
	}
	if hourly := got.Tasks["backup_hourly"]; !hourly.Enabled.Set || hourly.Enabled.Value {
		t.Errorf("backup_hourly enabled = %+v, want set false", hourly.Enabled)
	}
	if tor := got.Tasks["tor_health"]; !tor.RestartOnFail.Set || tor.RestartOnFail.Value {
		t.Errorf("tor_health restart_on_fail = %+v, want set false", tor.RestartOnFail)
	}
}
