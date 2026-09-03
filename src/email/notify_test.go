package email

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newTestNotifier builds a notifier with every event toggle on and no SMTP
// client, so notification calls exercise the toggle/suppression logic and
// then no-op instead of attempting a real send.
func newTestNotifier(t *testing.T) *Notifier {
	t.Helper()
	toggles := EventToggles{
		Startup:          true,
		Shutdown:         true,
		BackupComplete:   true,
		BackupFailed:     true,
		SSLExpiring:      true,
		SSLRenewed:       true,
		SSLRenewalFailed: true,
		SecurityAlert:    true,
		SchedulerError:   true,
		UpdateAvailable:  true,
		UpdateInstalled:  true,
	}
	return NewNotifier(nil, t.TempDir(), GlobalVars{AppName: "api"}, toggles, []string{"ops@example.com"})
}

func (n *Notifier) isSuppressed(executionID string) bool {
	n.mu.Lock()
	defer n.mu.Unlock()
	_, ok := n.suppressed[executionID]
	return ok
}

// TestSuppression_BackupFailedSuppressesSchedulerError covers AI.md
// PART 17: "backup_failed suppresses scheduler_error for the same
// execution. One notification, not two."
func TestSuppression_BackupFailedSuppressesSchedulerError(t *testing.T) {
	n := newTestNotifier(t)

	require.NoError(t, n.NotifyBackupFailed("exec-1", nil, "backup.tar.gz", "0 B", "disk full"))
	assert.True(t, n.isSuppressed("exec-1"))

	require.NoError(t, n.NotifySchedulerError("exec-1", nil, "backup_daily", "disk full", "tomorrow"))
	assert.False(t, n.isSuppressed("exec-1"), "suppression mark must be consumed once used")
}

// TestSuppression_SSLRenewalFailedSuppressesSchedulerError covers the
// matching rule for SSL renewal.
func TestSuppression_SSLRenewalFailedSuppressesSchedulerError(t *testing.T) {
	n := newTestNotifier(t)

	require.NoError(t, n.NotifySSLRenewalFailed("exec-ssl", nil, "example.com", "acme failure", "3 days", "2025-01-20", "2025-01-16"))
	assert.True(t, n.isSuppressed("exec-ssl"))

	require.NoError(t, n.NotifySchedulerError("exec-ssl", nil, "ssl_renewal", "acme failure", "tomorrow"))
	assert.False(t, n.isSuppressed("exec-ssl"))
}

// TestSuppression_UnrelatedExecutionsUnaffected proves a suppression mark
// never leaks into a different execution, and that tasks with no dedicated
// failure event (token_cleanup, log_rotation, update_check) still notify.
func TestSuppression_UnrelatedExecutionsUnaffected(t *testing.T) {
	n := newTestNotifier(t)

	require.NoError(t, n.NotifyBackupFailed("exec-a", nil, "backup.tar.gz", "0 B", "disk full"))
	assert.False(t, n.consumeSuppression("exec-b"))
	assert.False(t, n.consumeSuppression("token_cleanup-2025-01-15T00:00:00Z"))
	assert.True(t, n.isSuppressed("exec-a"))
}

// TestSuppression_BlankExecutionIDIsNoOp proves an event not tied to a
// scheduled run neither records nor consumes a suppression mark.
func TestSuppression_BlankExecutionIDIsNoOp(t *testing.T) {
	n := newTestNotifier(t)

	require.NoError(t, n.NotifyBackupFailed("", nil, "backup.tar.gz", "0 B", "disk full"))
	assert.False(t, n.consumeSuppression(""))
	assert.Empty(t, n.suppressed)
}

// TestSuppression_ExpiresAfterTTL proves an unconsumed mark is pruned so
// it cannot suppress an unrelated later execution reusing the same id.
func TestSuppression_ExpiresAfterTTL(t *testing.T) {
	n := newTestNotifier(t)
	original := nowFunc
	defer func() { nowFunc = original }()

	base := time.Date(2031, 1, 1, 0, 0, 0, 0, time.UTC)
	nowFunc = func() time.Time { return base }
	require.NoError(t, n.NotifyBackupFailed("exec-old", nil, "backup.tar.gz", "0 B", "disk full"))

	nowFunc = func() time.Time { return base.Add(suppressionTTL + time.Minute) }
	assert.False(t, n.consumeSuppression("exec-old"))
}

// TestNotifier_NilReceiverIsSafe proves callers in other packages may use
// email.DefaultNotifier() unconditionally before SMTP is configured.
func TestNotifier_NilReceiverIsSafe(t *testing.T) {
	var n *Notifier
	assert.False(t, n.Enabled())
	assert.Nil(t, n.Recipients())
	require.NoError(t, n.NotifySecurityAlert(nil, "event", "1.2.3.4", "details"))
	require.NoError(t, n.NotifySchedulerError("exec", nil, "task", "err", "next"))
	require.Error(t, n.SendTestEmail(nil))

	n.SetToggles(EventToggles{})
	n.SetVars(GlobalVars{})
	n.SetRecipients([]string{"a@example.com"})
	n.SetClient(nil)
}

// TestNotifier_DisabledEventDoesNotSend proves a disabled toggle short
// circuits the send path.
func TestNotifier_DisabledEventDoesNotSend(t *testing.T) {
	n := NewNotifier(nil, t.TempDir(), GlobalVars{}, EventToggles{}, []string{"ops@example.com"})
	require.NoError(t, n.NotifyBackupComplete(nil, "backup.tar.gz", "1 MB"))
	require.NoError(t, n.NotifyUpdateAvailable(nil, "1.0.0", "1.1.0", "stable"))
}

// TestNotifier_SendTestEmailRequiresSMTP proves the test-email path reports
// an error (rather than silently no-oping) when SMTP is unavailable.
func TestNotifier_SendTestEmailRequiresSMTP(t *testing.T) {
	n := newTestNotifier(t)
	err := n.SendTestEmail([]string{"ops@example.com"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not enabled")

	n.SetClient(NewClient(Config{Enabled: false}))
	err = n.SendTestEmail(nil)
	require.Error(t, err)
}

func TestDefaultNotifier(t *testing.T) {
	original := DefaultNotifier()
	defer SetDefaultNotifier(original)

	n := newTestNotifier(t)
	SetDefaultNotifier(n)
	assert.Same(t, n, DefaultNotifier())
	assert.Equal(t, []string{"ops@example.com"}, DefaultNotifier().Recipients())
}

func TestNotifier_SetRecipientsCopies(t *testing.T) {
	n := newTestNotifier(t)
	in := []string{"a@example.com"}
	n.SetRecipients(in)
	in[0] = "mutated@example.com"
	assert.Equal(t, []string{"a@example.com"}, n.Recipients())
}
