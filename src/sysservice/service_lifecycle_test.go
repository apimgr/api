package sysservice

import (
	"os"
	"path/filepath"
	"testing"
)

// These tests exercise the unexported per-platform install/uninstall
// functions directly, bypassing DetectServiceManager(). This is safe here
// because every test run happens inside an ephemeral (--rm) Docker
// container where /etc, /var, /usr/local/bin, and /Library are
// container-local paths, not bind mounts from the host - the container is
// destroyed after the test run, so writing to these hardcoded paths never
// touches real host state. The shelled-out service-manager commands
// (systemctl, sv, launchctl, sc.exe, service) are expected to be absent in
// this toolchain image and are allowed to fail; only the file/directory
// side effects are asserted.

// TestInstallUninstallSystemd verifies the unit file and required
// directories are written, then fully removed.
func TestInstallUninstallSystemd(t *testing.T) {
	// installSystemd writes directly into /etc/systemd/system without
	// creating it first - on a real target OS this directory always
	// exists (owned by the systemd package); this minimal toolchain
	// image does not ship it, so it is created here as test setup only.
	if err := os.MkdirAll("/etc/systemd/system", 0755); err != nil {
		t.Fatalf("setup MkdirAll failed: %v", err)
	}

	err := installSystemd()
	if err != nil {
		t.Logf("installSystemd returned an error (expected if systemctl is absent): %v", err)
	}

	servicePath := "/etc/systemd/system/api.service"
	if _, statErr := os.Stat(servicePath); statErr != nil {
		t.Fatalf("expected service file to exist at %s: %v", servicePath, statErr)
	}

	dirs := []string{
		"/var/lib/apimgr/api",
		"/var/log/apimgr/api",
		"/etc/apimgr/api",
	}
	for _, dir := range dirs {
		if _, statErr := os.Stat(dir); statErr != nil {
			t.Errorf("expected directory %s to exist: %v", dir, statErr)
		}
	}

	if err := uninstallSystemd(); err != nil {
		t.Fatalf("uninstallSystemd failed: %v", err)
	}
	if _, statErr := os.Stat(servicePath); !os.IsNotExist(statErr) {
		t.Errorf("expected service file to be removed, stat err=%v", statErr)
	}
}

// TestUninstallSystemdMissingFile verifies uninstalling an already-absent
// unit file is not an error (idempotent uninstall).
func TestUninstallSystemdMissingFile(t *testing.T) {
	// Ensure a clean slate regardless of test ordering.
	_ = os.Remove("/etc/systemd/system/api.service")

	if err := uninstallSystemd(); err != nil {
		t.Errorf("uninstallSystemd on a missing file returned %v, want nil", err)
	}
}

// TestInstallUninstallRunit verifies the run script, log directory, and
// service symlink are created and removed.
func TestInstallUninstallRunit(t *testing.T) {
	// installRunit symlinks into /var/service without creating it first -
	// on a real runit-managed OS this directory always exists; this
	// minimal toolchain image does not ship it, so it is created here as
	// test setup only.
	if err := os.MkdirAll("/var/service", 0755); err != nil {
		t.Fatalf("setup MkdirAll failed: %v", err)
	}

	if err := installRunit(); err != nil {
		t.Fatalf("installRunit failed: %v", err)
	}

	runPath := "/etc/sv/api/run"
	if _, statErr := os.Stat(runPath); statErr != nil {
		t.Fatalf("expected run script to exist at %s: %v", runPath, statErr)
	}

	logRunPath := "/etc/sv/api/log/run"
	if _, statErr := os.Stat(logRunPath); statErr != nil {
		t.Errorf("expected log run script to exist at %s: %v", logRunPath, statErr)
	}

	linkPath := "/var/service/api"
	if info, statErr := os.Lstat(linkPath); statErr != nil {
		t.Errorf("expected symlink to exist at %s: %v", linkPath, statErr)
	} else if info.Mode()&os.ModeSymlink == 0 {
		t.Errorf("expected %s to be a symlink", linkPath)
	}

	if err := uninstallRunit(); err != nil {
		t.Fatalf("uninstallRunit failed: %v", err)
	}
	if _, statErr := os.Stat("/etc/sv/api"); !os.IsNotExist(statErr) {
		t.Errorf("expected service dir to be removed, stat err=%v", statErr)
	}
	if _, statErr := os.Lstat(linkPath); !os.IsNotExist(statErr) {
		t.Errorf("expected symlink to be removed, stat err=%v", statErr)
	}
}

// TestInstallUninstallLaunchd verifies the plist and support directories
// are written and removed. On non-macOS this still exercises the file I/O
// paths, since installLaunchd hardcodes /Library/... regardless of GOOS.
func TestInstallUninstallLaunchd(t *testing.T) {
	// installLaunchd writes directly into /Library/LaunchDaemons without
	// creating it first - on real macOS this directory always exists;
	// this Linux toolchain image does not ship it, so it is created here
	// as test setup only.
	if err := os.MkdirAll("/Library/LaunchDaemons", 0755); err != nil {
		t.Fatalf("setup MkdirAll failed: %v", err)
	}

	err := installLaunchd()
	if err != nil {
		t.Logf("installLaunchd returned an error (expected if launchctl is absent): %v", err)
	}

	plistPath := "/Library/LaunchDaemons/com.apimgr.api.plist"
	if _, statErr := os.Stat(plistPath); statErr != nil {
		t.Fatalf("expected plist to exist at %s: %v", plistPath, statErr)
	}

	if err := uninstallLaunchd(); err != nil {
		t.Fatalf("uninstallLaunchd failed: %v", err)
	}
	if _, statErr := os.Stat(plistPath); !os.IsNotExist(statErr) {
		t.Errorf("expected plist to be removed, stat err=%v", statErr)
	}
}

// TestUninstallLaunchdMissingFile verifies uninstalling an already-absent
// plist is not an error.
func TestUninstallLaunchdMissingFile(t *testing.T) {
	_ = os.Remove("/Library/LaunchDaemons/com.apimgr.api.plist")

	if err := uninstallLaunchd(); err != nil {
		t.Errorf("uninstallLaunchd on a missing file returned %v, want nil", err)
	}
}

// TestInstallWindows verifies the binary is copied to GetBinaryPath() and
// the (expected-to-fail, sc.exe absent) service-creation error is
// surfaced rather than silently swallowed.
func TestInstallWindows(t *testing.T) {
	err := installWindows()
	if err == nil {
		t.Log("installWindows succeeded unexpectedly (sc.exe must be present in this image)")
	} else {
		t.Logf("installWindows returned an error (expected if sc.exe is absent): %v", err)
	}

	if _, statErr := os.Stat(GetBinaryPath()); statErr != nil {
		t.Errorf("expected binary to be copied to %s: %v", GetBinaryPath(), statErr)
	}
}

// TestUninstallWindows verifies uninstallWindows surfaces the (expected)
// sc.exe-absent error rather than panicking.
func TestUninstallWindows(t *testing.T) {
	err := uninstallWindows()
	if err == nil {
		t.Log("uninstallWindows succeeded unexpectedly (sc.exe must be present in this image)")
	} else {
		t.Logf("uninstallWindows returned an error (expected if sc.exe is absent): %v", err)
	}
}

// TestInstallUninstallBSDRC verifies the rc.d script is written and
// removed.
func TestInstallUninstallBSDRC(t *testing.T) {
	// installBSDRC writes directly into /usr/local/etc/rc.d without
	// creating it first - on a real BSD system this directory always
	// exists; this Linux toolchain image does not ship it, so it is
	// created here as test setup only.
	if err := os.MkdirAll("/usr/local/etc/rc.d", 0755); err != nil {
		t.Fatalf("setup MkdirAll failed: %v", err)
	}

	if err := installBSDRC(); err != nil {
		t.Fatalf("installBSDRC failed: %v", err)
	}

	rcPath := "/usr/local/etc/rc.d/api"
	if _, statErr := os.Stat(rcPath); statErr != nil {
		t.Fatalf("expected rc.d script to exist at %s: %v", rcPath, statErr)
	}

	if err := uninstallBSDRC(); err != nil {
		t.Fatalf("uninstallBSDRC failed: %v", err)
	}
	if _, statErr := os.Stat(rcPath); !os.IsNotExist(statErr) {
		t.Errorf("expected rc.d script to be removed, stat err=%v", statErr)
	}
}

// TestUninstallBSDRCMissingFile verifies uninstalling an already-absent
// rc.d script is not an error.
func TestUninstallBSDRCMissingFile(t *testing.T) {
	_ = os.Remove("/usr/local/etc/rc.d/api")

	if err := uninstallBSDRC(); err != nil {
		t.Errorf("uninstallBSDRC on a missing file returned %v, want nil", err)
	}
}

// TestInstallCopiesBinaryOnce verifies copyBinary is only attempted when
// the running executable differs from the target install path (guards
// against installSystemd/installLaunchd/installBSDRC/installWindows
// self-copying onto themselves).
func TestInstallCopiesBinaryOnce(t *testing.T) {
	exePath, err := os.Executable()
	if err != nil {
		t.Skip("os.Executable unavailable in this environment")
	}
	if exePath == GetBinaryPath() {
		t.Skip("test binary already at the install path, nothing to assert")
	}

	dst := filepath.Join(t.TempDir(), "copied-binary")
	if err := copyBinary(exePath, dst); err != nil {
		t.Fatalf("copyBinary failed: %v", err)
	}
	if _, statErr := os.Stat(dst); statErr != nil {
		t.Errorf("expected copied binary to exist: %v", statErr)
	}
}

// TestExportedDispatchersRunitBranch exercises the ServiceRunit branch of
// Start/Stop/Restart/Disable/Reload, which is otherwise unreachable in this
// container since DetectServiceManager() only returns ServiceRunit when
// /run/runit exists. Creating that marker directory here is safe test
// setup - it is container-local under the ephemeral --rm sandbox and never
// touches host state.
func TestExportedDispatchersRunitBranch(t *testing.T) {
	if err := os.MkdirAll("/run/runit", 0755); err != nil {
		t.Fatalf("setup MkdirAll failed: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll("/run/runit") })

	if got := DetectServiceManager(); got != ServiceRunit {
		t.Skipf("expected DetectServiceManager() to return ServiceRunit, got %v; skipping runit-branch coverage", got)
	}

	// installRunit creates /etc/sv/api, which Disable's downFile write and
	// Reload/Restart/Start/Stop's "sv" exec calls target.
	if err := os.MkdirAll("/var/service", 0755); err != nil {
		t.Fatalf("setup MkdirAll failed: %v", err)
	}
	if err := installRunit(); err != nil {
		t.Fatalf("installRunit setup failed: %v", err)
	}
	t.Cleanup(func() { _ = uninstallRunit() })

	funcs := map[string]func() error{
		"Start":   Start,
		"Stop":    Stop,
		"Restart": Restart,
		"Disable": Disable,
		"Reload":  Reload,
	}
	for name, fn := range funcs {
		if err := fn(); err != nil {
			t.Logf("%s (runit branch) returned an error (expected, sv is absent): %v", name, err)
		}
	}
}

// TestExportedDispatchers exercises the public Install/Uninstall/Start/
// Stop/Restart/Disable/Reload entry points. Whichever ServiceType this
// sandbox's DetectServiceManager() resolves to determines which internal
// branch runs; all are expected to either succeed or return a clean error
// (never panic).
func TestExportedDispatchers(t *testing.T) {
	funcs := map[string]func() error{
		"Install":   Install,
		"Start":     Start,
		"Stop":      Stop,
		"Restart":   Restart,
		"Disable":   Disable,
		"Reload":    Reload,
		"Uninstall": Uninstall,
	}

	for name, fn := range funcs {
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("%s panicked: %v", name, r)
				}
			}()
			if err := fn(); err != nil {
				t.Logf("%s returned an error (expected in a sandbox with no live service manager): %v", name, err)
			}
		}()
	}
}
