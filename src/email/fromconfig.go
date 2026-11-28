package email

import (
	"github.com/apimgr/api/src/config"
)

// EventTogglesFromConfig converts the parsed
// server.notifications.email.events.* block into the event-toggle type
// this package works with (AI.md PART 17 "Configuration"). It exists so
// callers wiring up a Notifier do not have to restate the mapping, while
// src/config stays free of any dependency on src/email.
func EventTogglesFromConfig(events config.EmailEventsConfig) EventToggles {
	return EventToggles{
		Startup:          events.Startup,
		Shutdown:         events.Shutdown,
		BackupComplete:   events.BackupComplete,
		BackupFailed:     events.BackupFailed,
		SSLExpiring:      events.SSLExpiring,
		SSLRenewed:       events.SSLRenewed,
		SSLRenewalFailed: events.SSLRenewalFailed,
		SecurityAlert:    events.SecurityAlert,
		SchedulerError:   events.SchedulerError,
		DiskLow:          events.DiskLow,
		DatabaseError:    events.DatabaseError,
		UpdateAvailable:  events.UpdateAvailable,
		UpdateInstalled:  events.UpdateInstalled,
		UpdateFailed:     events.UpdateFailed,
		UpdateStaged:     events.UpdateStaged,
	}
}

// ClientConfigFromConfig builds the SMTP client configuration from the
// parsed server.notifications.email.* block. The caller supplies the
// already-resolved app title and FQDN because src/email deliberately does
// no FQDN resolution of its own; they fill in the AI.md PART 17 "Default
// Sender" fallbacks (From Name = app title, From Address =
// no-reply@{fqdn}) whenever the operator left those fields empty. If no FQDN
// is available, email is disabled instead of using a localhost sender.
// SMTP_* env overrides are applied earlier, when src/config loads the
// file, so the values arriving here are already final.
func ClientConfigFromConfig(email config.EmailConfig, appTitle, fqdn string, enabled bool) EmailConfig {
	fromName := email.From.Name
	if fromName == "" {
		fromName = appTitle
	}
	fromEmail := email.From.Email
	if fromEmail == "" {
		if fqdn != "" {
			fromEmail = "no-reply@" + fqdn
		}
	}
	// A stable FQDN is required for the default sender and Message-ID domain.
	// PART 17 explicitly disables email when the FQDN is unavailable; never
	// manufacture a localhost identity as a substitute.
	enabled = enabled && fqdn != ""
	return EmailConfig{
		Enabled:   enabled,
		SMTPHost:  email.SMTP.Host,
		SMTPPort:  email.SMTP.Port,
		Username:  email.SMTP.Username,
		Password:  email.SMTP.Password,
		FromName:  fromName,
		FromEmail: fromEmail,
		TLS:       email.SMTP.TLS,
		ReplyTo:   email.ReplyTo,
	}
}
