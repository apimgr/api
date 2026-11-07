package backup

import (
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"strings"
)

// Actor labels recorded in the backup.restored audit event, one per row of
// PART 21's restore authorization matrix.
const (
	// ActorFirstRun is an empty/first-run installation, which PART 21 allows
	// to restore without any credential.
	ActorFirstRun = "first_run"
	// ActorRoot is a confirmed root/Administrator restore.
	ActorRoot = "root"
	// ActorOperator is the service user presenting a valid server.token.
	ActorOperator = "operator"
)

// ErrRestoreDenied is returned when the caller matches PART 21's "random user"
// row: not root, and without a valid operator token.
var ErrRestoreDenied = errors.New("restore denied: run as root or supply the operator token (server.token)")

// ErrRestoreNotConfirmed is returned when a root restore was not confirmed.
// PART 21 requires confirmation because restore is destructive.
var ErrRestoreNotConfirmed = errors.New("restore not confirmed")

// ErrRestorePasswordRequired is returned when an encrypted archive is restored
// without a password. The API surface maps it to a 400 VALIDATION_FAILED
// response; the CLI prompts instead of failing.
var ErrRestorePasswordRequired = errors.New("encrypted backup requires password")

// ErrOperatorTokenUnset is returned when a non-root caller supplies a token but
// the server has none configured, so nothing can authorize the restore.
var ErrOperatorTokenUnset = errors.New("restore denied: server.token is not set; run this command as root")

// Caller carries the identity evidence PART 21's restore authorization matrix
// evaluates. It is assembled by the call site (CLI, API handler) rather than
// probed here, so the same matrix serves every entry point.
type Caller struct {
	// FirstRun reports an empty database / first-run installation, which is
	// allowed to restore unconditionally.
	FirstRun bool
	// Elevated reports that the process runs as root or Administrator.
	Elevated bool
	// Confirmed records that an elevated caller confirmed the destructive
	// operation. It is ignored for every other row of the matrix.
	Confirmed bool
	// SuppliedToken is the operator token the caller presented, if any.
	SuppliedToken string
	// ExpectedToken is the configured server.token to compare against.
	ExpectedToken string
}

// AuthorizeRestore applies PART 21's restore authorization matrix and returns
// the audit actor label for the authorized caller.
func AuthorizeRestore(caller Caller) (string, error) {
	if caller.FirstRun {
		return ActorFirstRun, nil
	}

	if caller.Elevated {
		if !caller.Confirmed {
			return "", ErrRestoreNotConfirmed
		}
		return ActorRoot, nil
	}

	supplied := strings.TrimSpace(caller.SuppliedToken)
	if supplied == "" {
		return "", ErrRestoreDenied
	}
	if caller.ExpectedToken == "" {
		return "", ErrOperatorTokenUnset
	}

	suppliedSum := sha256.Sum256([]byte(supplied))
	expectedSum := sha256.Sum256([]byte(caller.ExpectedToken))
	if subtle.ConstantTimeCompare(suppliedSum[:], expectedSum[:]) != 1 {
		return "", ErrRestoreDenied
	}
	return ActorOperator, nil
}

// RequiresPassword reports whether an archive is encrypted and therefore needs
// a restore password. The caller uses it to prompt (CLI), open a dialog
// (WebUI) or answer 400 VALIDATION_FAILED (API) before any restore starts.
func RequiresPassword(path string) bool {
	return IsEncryptedName(path)
}
