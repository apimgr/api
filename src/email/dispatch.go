package email

import "log"

// Package-level operator dispatch helpers (AI.md PART 17 "Operator
// Notifications"). Subsystems such as backup and SSL raise operator events
// from code paths that must never fail because of a notification problem, so
// every helper here is best effort: it resolves the process-wide notifier via
// DefaultNotifier(), no-ops when no notifier or no SMTP client is registered,
// and logs - never returns - a delivery error.
//
// The structured, leveled log line AI.md PART 17 mandates for every operator
// event stays the caller's responsibility: logging is unconditional, email is
// only an addition when SMTP is configured and the per-event toggle is on.

// dispatch runs a notifier call, swallowing any delivery error after logging
// it so the caller's operation result is never changed by email trouble.
func dispatch(event string, fn func(n *Notifier) error) {
	n := DefaultNotifier()
	if n == nil {
		return
	}

	if err := fn(n); err != nil {
		log.Printf("Notify: Failed to send %s notification: %v", event, err)
	}
}

// OperatorSecurityAlert raises the security_alert operator event.
func OperatorSecurityAlert(event, ip, details string) {
	dispatch("security_alert", func(n *Notifier) error {
		return n.NotifySecurityAlert(nil, event, ip, details)
	})
}

// OperatorBackupComplete raises the backup_complete operator event, which is
// routine success and therefore disabled by default.
func OperatorBackupComplete(filename, size string) {
	dispatch("backup_complete", func(n *Notifier) error {
		return n.NotifyBackupComplete(nil, filename, size)
	})
}

// OperatorBackupFailed raises the backup_failed operator event and marks the
// execution so the scheduler's scheduler_error notification is suppressed for
// the same run - one notification, not two.
func OperatorBackupFailed(executionID, filename, size, errMsg string) {
	dispatch("backup_failed", func(n *Notifier) error {
		return n.NotifyBackupFailed(executionID, nil, filename, size, errMsg)
	})
}

// OperatorUpdateInstalled raises the update_installed operator event. The
// scheduled update_check task sends this itself through its own notifier; this
// helper covers the manual "--update yes" path, which has no notifier of its
// own but still owes the operator a record that the binary changed version.
func OperatorUpdateInstalled(previousVersion, newVersion string) {
	dispatch("update_installed", func(n *Notifier) error {
		return n.NotifyUpdateInstalled(nil, previousVersion, newVersion)
	})
}

// OperatorSSLExpiring raises the ssl_expiring operator event, which is only
// emailed inside the urgent 7/3/1 day window.
func OperatorSSLExpiring(fqdn, expiresIn, expiryDate string) {
	dispatch("ssl_expiring", func(n *Notifier) error {
		return n.NotifySSLExpiring(nil, fqdn, expiresIn, expiryDate)
	})
}

// OperatorSSLRenewed raises the ssl_renewed operator event.
func OperatorSSLRenewed(fqdn, validUntil string) {
	dispatch("ssl_renewed", func(n *Notifier) error {
		return n.NotifySSLRenewed(nil, fqdn, validUntil)
	})
}

// OperatorSSLRenewalFailed raises the ssl_renewal_failed operator event and
// marks the execution so scheduler_error is suppressed for the same run.
func OperatorSSLRenewalFailed(executionID, fqdn, errMsg, expiresIn, expiryDate, nextRetry string) {
	dispatch("ssl_renewal_failed", func(n *Notifier) error {
		return n.NotifySSLRenewalFailed(executionID, nil, fqdn, errMsg, expiresIn, expiryDate, nextRetry)
	})
}
