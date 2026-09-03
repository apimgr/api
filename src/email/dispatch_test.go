package email

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestDispatch_NoNotifierRegistered proves the operator helpers are safe to
// call from any subsystem before SMTP is configured (AI.md PART 17: never
// attempt to send without a working SMTP connection).
func TestDispatch_NoNotifierRegistered(t *testing.T) {
	original := DefaultNotifier()
	defer SetDefaultNotifier(original)

	SetDefaultNotifier(nil)
	assert.NotPanics(t, func() {
		OperatorSecurityAlert("Disk space low", "", "free 1 MB")
		OperatorBackupComplete("api_backup.tar.gz", "1.0 MB")
		OperatorBackupFailed("", "api_backup.tar.gz", "0 B", "disk full")
		OperatorSSLExpiring("example.com", "3 days", "2031-01-04T00:00:00Z")
		OperatorSSLRenewed("example.com", "2031-04-01T00:00:00Z")
		OperatorSSLRenewalFailed("", "example.com", "acme failure", "3 days", "2031-01-04T00:00:00Z", "tomorrow")
	})
}

// TestDispatch_RegisteredNotifierIsUsed proves the helpers route through the
// registered notifier, including the suppression mark backup_failed sets so
// scheduler_error is not sent twice for the same execution.
func TestDispatch_RegisteredNotifierIsUsed(t *testing.T) {
	original := DefaultNotifier()
	defer SetDefaultNotifier(original)

	n := newTestNotifier(t)
	SetDefaultNotifier(n)

	OperatorBackupFailed("exec-dispatch", "api_backup.tar.gz", "0 B", "disk full")
	assert.True(t, n.isSuppressed("exec-dispatch"))

	OperatorSSLRenewalFailed("exec-ssl-dispatch", "example.com", "acme failure", "3 days", "2031-01-04T00:00:00Z", "tomorrow")
	assert.True(t, n.isSuppressed("exec-ssl-dispatch"))
}

// TestDispatch_BlankExecutionIDLeavesNoSuppression proves a subsystem event
// not tied to a scheduled run never suppresses an unrelated scheduler_error.
func TestDispatch_BlankExecutionIDLeavesNoSuppression(t *testing.T) {
	original := DefaultNotifier()
	defer SetDefaultNotifier(original)

	n := newTestNotifier(t)
	SetDefaultNotifier(n)

	OperatorBackupFailed("", "api_backup.tar.gz", "0 B", "disk full")
	assert.Empty(t, n.suppressed)
}
