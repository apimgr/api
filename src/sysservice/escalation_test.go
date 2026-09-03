package sysservice

import (
	"os"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Every EscalationMethod must render a distinct, non-empty label and the
// zero value must render "none".
func TestEscalationMethodString(t *testing.T) {
	assert.Equal(t, "none", EscalationNone.String())

	seen := map[string]bool{}
	for _, m := range []EscalationMethod{
		EscalationAlreadyRoot, EscalationSudo, EscalationSu, EscalationPkexec,
		EscalationDoas, EscalationOsascript, EscalationUAC, EscalationRunas,
	} {
		label := m.String()
		assert.NotEmpty(t, label)
		assert.NotEqual(t, "none", label)
		assert.False(t, seen[label], "duplicate EscalationMethod label %q", label)
		seen[label] = true
	}
}

// DetectEscalationMethod must report already-root whenever the process runs
// elevated, and must return an error (never a bogus method) when the user
// genuinely cannot escalate.
func TestDetectEscalationMethodMatchesElevation(t *testing.T) {
	method, err := DetectEscalationMethod()

	if isElevatedNow() {
		require.NoError(t, err)
		assert.Equal(t, EscalationAlreadyRoot, method)
		return
	}
	if err != nil {
		assert.Equal(t, EscalationNone, method)
		return
	}
	assert.NotEqual(t, EscalationNone, method)
}

// CanEscalate must agree with DetectEscalationMethod's error result.
func TestCanEscalateAgreesWithDetection(t *testing.T) {
	_, err := DetectEscalationMethod()
	assert.Equal(t, err == nil, CanEscalate())
}

// ElevatedCommand must reject an empty argv rather than producing a
// half-formed escalation command line.
func TestElevatedCommandRejectsEmptyArgv(t *testing.T) {
	_, err := ElevatedCommand(nil)
	assert.Error(t, err)
}

// When already elevated, ElevatedCommand must return the argv unchanged -
// no escalation wrapper and no credential prompt.
func TestElevatedCommandAlreadyRootIsPassthrough(t *testing.T) {
	if !isElevatedNow() {
		t.Skip("test process is not elevated")
	}
	argv := []string{"/usr/local/bin/api", "--service", "start"}

	got, err := ElevatedCommand(argv)
	require.NoError(t, err)
	assert.Equal(t, argv, got)
}

// When escalation is possible but not yet applied, the returned command
// must be wrapped by a known escalation helper and must still carry the
// original binary somewhere in its arguments.
func TestElevatedCommandWrapsWhenNotRoot(t *testing.T) {
	if isElevatedNow() {
		t.Skip("test process is already elevated")
	}
	if !CanEscalate() {
		t.Skip("this host offers no escalation method")
	}

	got, err := ElevatedCommand([]string{"/usr/local/bin/api", "--service", "start"})
	require.NoError(t, err)
	require.NotEmpty(t, got)
	assert.Contains(t, []string{"sudo", "su", "doas", "pkexec", "osascript", "powershell", "runas"}, got[0])
	assert.Contains(t, strings.Join(got, " "), "/usr/local/bin/api")
}

// shellQuote must single-quote every argument and escape embedded single
// quotes so an argument containing shell metacharacters cannot break out.
func TestShellQuote(t *testing.T) {
	assert.Equal(t, `'a' 'b'`, shellQuote([]string{"a", "b"}))
	assert.Equal(t, `'a b'`, shellQuote([]string{"a b"}))
	assert.Equal(t, `';rm -rf /'`, shellQuote([]string{";rm -rf /"}))
	assert.Equal(t, `'it'\''s'`, shellQuote([]string{"it's"}))
	assert.Equal(t, "", shellQuote(nil))
}

// isElevatedNow must agree with the effective UID on Unix-like systems.
func TestIsElevatedNowMatchesEUID(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("euid comparison is Unix-only")
	}
	assert.Equal(t, os.Geteuid() == 0, isElevatedNow())
}

// hasInteractiveDesktop must never claim a GUI prompt is available inside
// an SSH session, where no such prompt could ever be displayed.
func TestHasInteractiveDesktopRejectsSSH(t *testing.T) {
	t.Setenv("SSH_CONNECTION", "10.0.0.1 22 10.0.0.2 22")
	assert.False(t, hasInteractiveDesktop())

	t.Setenv("SSH_CONNECTION", "")
	t.Setenv("SSH_TTY", "/dev/pts/0")
	assert.False(t, hasInteractiveDesktop())
}

// inEscalationGroup must return false for group names that do not exist
// rather than erroring or panicking.
func TestInEscalationGroupUnknownGroup(t *testing.T) {
	assert.False(t, inEscalationGroup("definitely-not-a-real-group-name"))
}

// Escalate must actually run the command, forwarding the current process's
// streams. Running it elevated exercises the passthrough path without ever
// triggering a credential prompt.
func TestEscalateRunsCommand(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses a POSIX no-op command")
	}
	if !isElevatedNow() {
		t.Skip("test process is not elevated; escalating would prompt for credentials")
	}
	assert.NoError(t, Escalate([]string{"true"}))
	assert.Error(t, Escalate([]string{"false"}))
}

// The per-platform detectors must either return a usable method or an
// explanatory error, never a usable-looking EscalationNone.
func TestPlatformEscalationDetectors(t *testing.T) {
	detectors := map[string]func() (EscalationMethod, error){
		"linux":  detectLinuxEscalation,
		"darwin": detectDarwinEscalation,
		"bsd":    detectBSDEscalation,
	}
	if runtime.GOOS == "windows" {
		detectors["windows"] = detectWindowsEscalation
	}

	for name, detect := range detectors {
		method, err := detect()
		if err != nil {
			assert.Equal(t, EscalationNone, method, "%s detector", name)
			assert.NotEmpty(t, err.Error(), "%s detector must explain why", name)
			continue
		}
		assert.NotEqual(t, EscalationNone, method, "%s detector", name)
	}
}
