package sysservice

import (
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"runtime"
	"strings"
)

// EscalationMethod identifies how the current process could gain (or
// already has) elevated privileges.
type EscalationMethod int

const (
	EscalationNone EscalationMethod = iota
	EscalationAlreadyRoot
	EscalationSudo
	EscalationSu
	EscalationPkexec
	EscalationDoas
	EscalationOsascript
	EscalationUAC
	EscalationRunas
)

// String returns a human-readable label for the escalation method,
// suitable for CLI/log output.
func (m EscalationMethod) String() string {
	switch m {
	case EscalationAlreadyRoot:
		return "already-root"
	case EscalationSudo:
		return "sudo"
	case EscalationSu:
		return "su"
	case EscalationPkexec:
		return "pkexec"
	case EscalationDoas:
		return "doas"
	case EscalationOsascript:
		return "osascript"
	case EscalationUAC:
		return "uac"
	case EscalationRunas:
		return "runas"
	default:
		return "none"
	}
}

// DetectEscalationMethod reports how the current process can reach
// root/Administrator, checking in the OS-specific order AI.md PART 23
// requires: Linux root->sudo->su->pkexec->doas, macOS root->sudo->osascript,
// BSD root->doas->sudo->su, Windows Administrator->UAC->runas. It returns
// an error, rather than a method, when the current user genuinely cannot
// escalate on this host - callers must show that error instead of
// prompting for credentials that will never succeed.
func DetectEscalationMethod() (EscalationMethod, error) {
	if isElevatedNow() {
		return EscalationAlreadyRoot, nil
	}

	switch runtime.GOOS {
	case "linux":
		return detectLinuxEscalation()
	case "darwin":
		return detectDarwinEscalation()
	case "freebsd", "openbsd", "netbsd":
		return detectBSDEscalation()
	case "windows":
		return detectWindowsEscalation()
	default:
		return EscalationNone, fmt.Errorf("privilege escalation is not supported on %s", runtime.GOOS)
	}
}

// CanEscalate reports whether the current process is elevated already or
// could become elevated on this host. Callers use it to decide between
// running a privileged action and printing an informative error, never to
// decide whether to prompt for credentials.
func CanEscalate() bool {
	_, err := DetectEscalationMethod()
	return err == nil
}

// ElevatedCommand builds the argv that runs the given command elevated with
// the detected escalation method. It returns the detection error unchanged
// when escalation is impossible, so the caller can surface the reason.
func ElevatedCommand(argv []string) ([]string, error) {
	if len(argv) == 0 {
		return nil, fmt.Errorf("no command given to elevate")
	}

	method, err := DetectEscalationMethod()
	if err != nil {
		return nil, err
	}

	switch method {
	case EscalationAlreadyRoot:
		return argv, nil
	case EscalationSudo:
		return append([]string{"sudo", "--"}, argv...), nil
	case EscalationDoas:
		return append([]string{"doas", "--"}, argv...), nil
	case EscalationPkexec:
		return append([]string{"pkexec"}, argv...), nil
	case EscalationSu:
		return []string{"su", "-c", shellQuote(argv)}, nil
	case EscalationOsascript:
		return []string{"osascript", "-e", fmt.Sprintf("do shell script %q with administrator privileges", shellQuote(argv))}, nil
	case EscalationUAC:
		return []string{"powershell", "-NoProfile", "-Command", fmt.Sprintf("Start-Process -FilePath %q -ArgumentList %q -Verb RunAs -Wait", argv[0], strings.Join(argv[1:], " "))}, nil
	case EscalationRunas:
		return []string{"runas", "/user:Administrator", shellQuote(argv)}, nil
	default:
		return nil, fmt.Errorf("cannot escalate privileges on this host")
	}
}

// Escalate re-runs the given command with elevated privileges, forwarding
// the current process's standard streams so any credential prompt from
// sudo/su/doas is visible to the user.
func Escalate(argv []string) error {
	elevated, err := ElevatedCommand(argv)
	if err != nil {
		return err
	}

	cmd := exec.Command(elevated[0], elevated[1:]...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// shellQuote joins argv into a single single-quoted shell string for the
// escalation helpers that take a command line rather than an argv slice
// (su -c, osascript, runas).
func shellQuote(argv []string) string {
	quoted := make([]string, 0, len(argv))
	for _, arg := range argv {
		quoted = append(quoted, "'"+strings.ReplaceAll(arg, "'", `'\''`)+"'")
	}
	return strings.Join(quoted, " ")
}

// isElevatedNow reports whether the current process already runs with
// root/Administrator privileges.
func isElevatedNow() bool {
	if runtime.GOOS == "windows" {
		return isWindowsAdmin()
	}
	return os.Geteuid() == 0
}

// detectLinuxEscalation checks sudo, then su, then pkexec, then doas.
func detectLinuxEscalation() (EscalationMethod, error) {
	if inEscalationGroup("sudo", "wheel") && commandExists("sudo") {
		return EscalationSudo, nil
	}
	if commandExists("su") {
		return EscalationSu, nil
	}
	if commandExists("pkexec") && hasInteractiveDesktop() {
		return EscalationPkexec, nil
	}
	if commandExists("doas") {
		return EscalationDoas, nil
	}
	return EscalationNone, fmt.Errorf("cannot escalate privileges: user is not in sudo/wheel and no su/pkexec/doas is usable")
}

// detectDarwinEscalation checks sudo, then osascript (GUI authorization
// prompt).
func detectDarwinEscalation() (EscalationMethod, error) {
	if inEscalationGroup("admin") && commandExists("sudo") {
		return EscalationSudo, nil
	}
	if commandExists("osascript") && hasInteractiveDesktop() {
		return EscalationOsascript, nil
	}
	return EscalationNone, fmt.Errorf("cannot escalate privileges: user is not in the admin group and no GUI authorization prompt is available")
}

// detectBSDEscalation checks doas, then sudo, then su.
func detectBSDEscalation() (EscalationMethod, error) {
	if commandExists("doas") {
		return EscalationDoas, nil
	}
	if inEscalationGroup("wheel") && commandExists("sudo") {
		return EscalationSudo, nil
	}
	if commandExists("su") {
		return EscalationSu, nil
	}
	return EscalationNone, fmt.Errorf("cannot escalate privileges: user is not in wheel and no doas/sudo/su is usable")
}

// detectWindowsEscalation checks whether the process is already elevated
// (handled earlier by DetectEscalationMethod), otherwise offers UAC
// (interactive desktop) or runas (any session).
func detectWindowsEscalation() (EscalationMethod, error) {
	if hasInteractiveDesktop() {
		return EscalationUAC, nil
	}
	return EscalationRunas, nil
}

// inEscalationGroup reports whether the current user belongs to any of
// the named Unix groups (e.g. "sudo", "wheel", "admin").
func inEscalationGroup(names ...string) bool {
	u, err := user.Current()
	if err != nil {
		return false
	}
	groupIDs, err := u.GroupIds()
	if err != nil {
		return false
	}

	wanted := make(map[string]bool, len(names))
	for _, n := range names {
		if g, err := user.LookupGroup(n); err == nil {
			wanted[g.Gid] = true
		}
	}

	for _, gid := range groupIDs {
		if wanted[gid] {
			return true
		}
	}
	return false
}

// hasInteractiveDesktop reports whether a graphical session is available
// to host a GUI authorization prompt (pkexec, osascript, UAC), rather
// than a headless/SSH context where such a prompt could never be shown.
func hasInteractiveDesktop() bool {
	if os.Getenv("SSH_TTY") != "" || os.Getenv("SSH_CONNECTION") != "" {
		return false
	}
	switch runtime.GOOS {
	case "linux":
		return os.Getenv("DISPLAY") != "" || os.Getenv("WAYLAND_DISPLAY") != ""
	case "darwin":
		return true
	case "windows":
		return true
	default:
		return false
	}
}
