package sysservice

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// DetectServiceManager must return one of the known ServiceType values for
// the current platform and never panic. The exact value on Linux depends
// on what's present in the sandbox (systemd files, runit), so we only
// assert it lands in the valid enum and matches non-Linux platforms
// exactly (those branches are unconditional).
func TestDetectServiceManager(t *testing.T) {
	got := DetectServiceManager()

	switch runtime.GOOS {
	case "darwin":
		assert.Equal(t, ServiceLaunchd, got)
	case "windows":
		assert.Equal(t, ServiceWindows, got)
	case "freebsd", "openbsd", "netbsd":
		assert.Equal(t, ServiceBSDRC, got)
	case "linux":
		assert.Contains(t, []ServiceType{ServiceSystemd, ServiceOpenRC, ServiceSysVinit, ServiceRunit, ServiceUnknown}, got)
	default:
		assert.Equal(t, ServiceUnknown, got)
	}
}

// The ServiceType enum values must be distinct and ServiceUnknown must be
// the zero value (callers rely on this as the "nothing detected" default).
func TestServiceTypeConstants(t *testing.T) {
	assert.Equal(t, ServiceType(0), ServiceUnknown)
	values := []ServiceType{ServiceUnknown, ServiceSystemd, ServiceOpenRC, ServiceSysVinit, ServiceRunit, ServiceLaunchd, ServiceWindows, ServiceBSDRC}
	seen := map[ServiceType]bool{}
	for _, v := range values {
		assert.False(t, seen[v], "duplicate ServiceType value %d", v)
		seen[v] = true
	}
}

// GetBinaryPath must return the platform-appropriate install location for
// the "api" binary under the "apimgr" org.
func TestGetBinaryPath(t *testing.T) {
	got := GetBinaryPath()

	switch runtime.GOOS {
	case "windows":
		assert.Equal(t, `C:\Program Files\apimgr\api\api.exe`, got)
	default:
		assert.Equal(t, "/usr/local/bin/api", got)
	}
}

// copyBinary must copy file contents byte-for-byte, create missing parent
// directories, and mark the destination executable (0755).
func TestCopyBinary(t *testing.T) {
	tmp := t.TempDir()
	src := filepath.Join(tmp, "srcbin")
	content := []byte("#!/bin/sh\necho hi\n")
	require.NoError(t, os.WriteFile(src, content, 0644))

	dst := filepath.Join(tmp, "nested", "subdir", "dstbin")
	require.NoError(t, copyBinary(src, dst))

	got, err := os.ReadFile(dst)
	require.NoError(t, err)
	assert.Equal(t, content, got)

	info, err := os.Stat(dst)
	require.NoError(t, err)
	if runtime.GOOS != "windows" {
		assert.Equal(t, os.FileMode(0755), info.Mode().Perm())
	}
}

// copyBinary must return an error when the source file does not exist.
func TestCopyBinaryMissingSource(t *testing.T) {
	tmp := t.TempDir()
	err := copyBinary(filepath.Join(tmp, "missing"), filepath.Join(tmp, "dst"))
	assert.Error(t, err)
}

// Each ServiceType must render a distinct, non-empty label and the zero
// value must render "unknown".
func TestServiceTypeString(t *testing.T) {
	assert.Equal(t, "unknown", ServiceUnknown.String())

	seen := map[string]bool{}
	for _, v := range []ServiceType{ServiceSystemd, ServiceOpenRC, ServiceSysVinit, ServiceRunit, ServiceLaunchd, ServiceWindows, ServiceBSDRC} {
		label := v.String()
		assert.NotEmpty(t, label)
		assert.NotEqual(t, "unknown", label)
		assert.False(t, seen[label], "duplicate ServiceType label %q", label)
		seen[label] = true
	}
}

// hasSysVinit must never claim SysVinit on a host where systemd or OpenRC
// is present, since AI.md PART 24 installs exactly one init script per host
// at /etc/init.d/api.
func TestSysVinitDetectionIsExclusive(t *testing.T) {
	if hasSystemd() || hasOpenRC() {
		assert.False(t, hasSysVinit())
	}
	if hasSysVinit() {
		assert.False(t, hasSystemd())
		assert.False(t, hasOpenRC())
		assert.True(t, commandExists("update-rc.d") || commandExists("chkconfig"))
	}
}

// detectLinuxServiceManager must prefer systemd over every other manager
// when systemd is present.
func TestDetectLinuxServiceManagerPrefersSystemd(t *testing.T) {
	if !hasSystemd() {
		t.Skip("systemd not present in this environment")
	}
	assert.Equal(t, ServiceSystemd, detectLinuxServiceManager())
}

// HelpText must document every --service command AI.md PART 23 defines and
// must end with the live status block.
func TestHelpText(t *testing.T) {
	out := HelpText("api")

	for _, want := range []string{
		"Usage: api --service <command>",
		"start", "stop", "restart", "reload",
		"--install", "--disable", "--uninstall",
		"Current status:",
		"Service manager:",
		"Installed:",
		"Running:",
		"Enabled at boot:",
	} {
		assert.Contains(t, out, want)
	}
}

// HelpText must reflect the actual (possibly renamed) binary name it is
// given, per the PART 8 renamed-binary rule.
func TestHelpTextUsesGivenBinaryName(t *testing.T) {
	assert.Contains(t, HelpText("apictl"), "Usage: apictl --service <command>")
}

// yesNo renders the booleans in the status block.
func TestYesNo(t *testing.T) {
	assert.Equal(t, "yes", yesNo(true))
	assert.Equal(t, "no", yesNo(false))
}

// Install/Uninstall/Start/Stop/Restart/Disable/Reload all operate on
// hardcoded, real system paths (/etc/systemd/system, /usr/local/bin, sc.exe,
// launchctl, etc.) and shell out to the live service manager. They are not
// scoped to a temp directory, so exercising them here would mutate real
// system state (or require root/an actual service manager) with no safe
// way to undo it in this sandbox/CI environment.
func TestServiceLifecycleOperationsRequireLiveSystem(t *testing.T) {
	t.Skip("Install/Uninstall/Start/Stop/Restart/Disable/Reload write to hardcoded system paths and shell out to systemctl/sc.exe/launchctl; unsafe to exercise outside a real, disposable host")
}
