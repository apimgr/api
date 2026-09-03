package config

// EmailEventsConfig holds server.notifications.email.events.* per-event
// toggles (AI.md PART 17 "Configuration"). Defaults match the exact table
// there; log output always happens regardless of these toggles - they gate
// email delivery only, never the structured log line.
type EmailEventsConfig struct {
	Startup          bool `yaml:"startup"`
	Shutdown         bool `yaml:"shutdown"`
	BackupComplete   bool `yaml:"backup_complete"`
	BackupFailed     bool `yaml:"backup_failed"`
	SSLExpiring      bool `yaml:"ssl_expiring"`
	SSLRenewed       bool `yaml:"ssl_renewed"`
	SSLRenewalFailed bool `yaml:"ssl_renewal_failed"`
	SecurityAlert    bool `yaml:"security_alert"`
	SchedulerError   bool `yaml:"scheduler_error"`
	UpdateAvailable  bool `yaml:"update_available"`
	UpdateInstalled  bool `yaml:"update_installed"`
}

// DefaultEmailEventsConfig returns the AI.md PART 17 "Configuration"
// defaults for defaultConfig() to embed.
func DefaultEmailEventsConfig() EmailEventsConfig {
	return EmailEventsConfig{
		Startup:          false,
		Shutdown:         false,
		BackupComplete:   false,
		BackupFailed:     true,
		SSLExpiring:      true,
		SSLRenewed:       false,
		SSLRenewalFailed: true,
		SecurityAlert:    true,
		SchedulerError:   true,
		UpdateAvailable:  false,
		UpdateInstalled:  true,
	}
}

// WebUINotificationsConfig holds server.notifications.webui.* settings
// (AI.md PART 17 "Configuration") - client-side toast placement for
// visitor-facing notifications only; never used for operator events.
type WebUINotificationsConfig struct {
	Position string `yaml:"position"`
	Duration int    `yaml:"duration"`
}

// DefaultWebUINotificationsConfig returns the AI.md PART 17 defaults.
func DefaultWebUINotificationsConfig() WebUINotificationsConfig {
	return WebUINotificationsConfig{
		Position: "top-right",
		Duration: 5,
	}
}
