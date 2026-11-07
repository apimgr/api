package backup

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestAuthorizeRestoreFirstRun covers the "database empty (first-run)" row of
// PART 21's matrix, which is allowed with no credential at all.
func TestAuthorizeRestoreFirstRun(t *testing.T) {
	actor, err := AuthorizeRestore(Caller{FirstRun: true})
	require.NoError(t, err)
	require.Equal(t, ActorFirstRun, actor)
}

// TestAuthorizeRestoreRoot covers the "running as root" row: allowed only once
// the destructive operation has been confirmed.
func TestAuthorizeRestoreRoot(t *testing.T) {
	actor, err := AuthorizeRestore(Caller{Elevated: true, Confirmed: true})
	require.NoError(t, err)
	require.Equal(t, ActorRoot, actor)

	_, err = AuthorizeRestore(Caller{Elevated: true})
	require.ErrorIs(t, err, ErrRestoreNotConfirmed)
}

// TestAuthorizeRestoreOperator covers the "running as service user" row, which
// requires a matching operator token.
func TestAuthorizeRestoreOperator(t *testing.T) {
	actor, err := AuthorizeRestore(Caller{SuppliedToken: "s3cret", ExpectedToken: "s3cret"})
	require.NoError(t, err)
	require.Equal(t, ActorOperator, actor)

	_, err = AuthorizeRestore(Caller{SuppliedToken: "wrong", ExpectedToken: "s3cret"})
	require.ErrorIs(t, err, ErrRestoreDenied)

	_, err = AuthorizeRestore(Caller{SuppliedToken: "s3cret"})
	require.ErrorIs(t, err, ErrOperatorTokenUnset)
}

// TestAuthorizeRestoreRandomUser covers the "random user" row: denied.
func TestAuthorizeRestoreRandomUser(t *testing.T) {
	_, err := AuthorizeRestore(Caller{ExpectedToken: "s3cret"})
	require.ErrorIs(t, err, ErrRestoreDenied)

	_, err = AuthorizeRestore(Caller{SuppliedToken: "   ", ExpectedToken: "s3cret"})
	require.ErrorIs(t, err, ErrRestoreDenied)
}

// TestRequiresPassword checks the encrypted-archive detection that drives the
// CLI prompt, the WebUI dialog and the API's 400 VALIDATION_FAILED response.
func TestRequiresPassword(t *testing.T) {
	require.True(t, RequiresPassword("/backups/api_backup_2026-01-15.tar.gz.enc"))
	require.False(t, RequiresPassword("/backups/api_backup_2026-01-15.tar.gz"))
}

// TestRestorePasswordRequired verifies Restore refuses an encrypted archive
// when no password was supplied, before any decryption is attempted.
func TestRestorePasswordRequired(t *testing.T) {
	dir := t.TempDir()
	manager := NewManager(Options{Dir: dir, Sources: []string{dir}})

	err := manager.Restore(Caller{FirstRun: true}, "api_backup_2026-01-15.tar.gz.enc", "")
	require.ErrorIs(t, err, ErrRestorePasswordRequired)
}
