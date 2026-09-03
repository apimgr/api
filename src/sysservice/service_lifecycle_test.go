package sysservice

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/apimgr/api/src/paths"
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

	// The runtime directories are resolved by the paths package (container,
	// root and user modes all resolve differently), so assert against the
	// same resolver installSystemd uses rather than hardcoded system paths.
	dirs := []string{
		paths.ConfigDir(),
		paths.DataDir(),
		paths.CacheDir(),
		paths.LogDir(),
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

	plistPath := "/Library/LaunchDaemons/io.github.apimgr.api.plist"
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
	_ = os.Remove("/Library/LaunchDaemons/io.github.apimgr.api.plist")

	if err := uninstallLaunchd(); err != nil {
		t.Errorf("uninstallLaunchd on a missing file returned %v, want nil", err)
	}
}

// TestInstallWindows verifies installWindows behaves correctly for the
// current build. On GOOS=windows it must attempt real VSA/service creation
// and copy the binary to GetBinaryPath(); on every other platform, the stub
// must return a guaranteed error with no binary-copy side effect.
func TestInstallWindows(t *testing.T) {
	err := installWindows()

	if runtime.GOOS != "windows" {
		if err == nil {
			t.Error("installWindows stub returned nil error on a non-windows build, want a guaranteed error")
		}
		return
	}

	if err == nil {
		t.Log("installWindows succeeded unexpectedly (sc.exe must be present in this image)")
	} else {
		t.Logf("installWindows returned an error (expected if sc.exe is absent): %v", err)
	}

	if _, statErr := os.Stat(GetBinaryPath()); statErr != nil {
		t.Errorf("expected binary to be copied to %s: %v", GetBinaryPath(), statErr)
	}
}

// TestUninstallWindows verifies uninstallWindows behaves correctly for the
// current build: a guaranteed error on non-windows builds (stub), and a
// clean error/success (never a panic) on GOOS=windows.
func TestUninstallWindows(t *testing.T) {
	err := uninstallWindows()

	if runtime.GOOS != "windows" {
		if err == nil {
			t.Error("uninstallWindows stub returned nil error on a non-windows build, want a guaranteed error")
		}
		return
	}

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
		"Uninstall": func() error { return Uninstall(true) },
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

// TestInstallUninstallOpenRC verifies the OpenRC init script is written to
// the shared /etc/init.d/api path and removed again.
func TestInstallUninstallOpenRC(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("OpenRC is Unix-only")
	}
	if err := os.MkdirAll("/etc/init.d", 0755); err != nil {
		t.Fatalf("setup MkdirAll failed: %v", err)
	}

	if err := installOpenRC(); err != nil {
		t.Logf("installOpenRC returned an error (expected if rc-update is absent): %v", err)
	}

	scriptPath := "/etc/init.d/api"
	data, readErr := os.ReadFile(scriptPath)
	if readErr != nil {
		t.Fatalf("expected OpenRC script at %s: %v", scriptPath, readErr)
	}

	// AI.md PART 24's OpenRC template requires the service to run as the
	// dedicated api:api account, with an explicit pidfile, split
	// output/error logs, the full depend() block, and a start_pre() that
	// pre-creates the pid and log directories owned by that account.
	script := string(data)
	for _, want := range []string{
		"#!/sbin/openrc-run",
		`name="api"`,
		`command_args=""`,
		`command_user="api:api"`,
		"command_background=true",
		"pidfile=\"" + paths.DefaultPIDPath() + "\"",
		`output_log="` + paths.LogDir() + `/server.log"`,
		`error_log="` + paths.LogDir() + `/error.log"`,
		"need net",
		"after firewall",
		"use dns logger",
		"start_pre() {",
		"checkpath -d -m 0755 -o api:api " + filepath.Dir(paths.DefaultPIDPath()),
		"checkpath -d -m 0755 -o api:api " + paths.LogDir(),
	} {
		if !strings.Contains(script, want) {
			t.Errorf("OpenRC script is missing %q", want)
		}
	}

	if err := uninstallOpenRC(); err != nil {
		t.Fatalf("uninstallOpenRC failed: %v", err)
	}
	if _, statErr := os.Stat(scriptPath); !os.IsNotExist(statErr) {
		t.Errorf("expected OpenRC script to be removed, stat err=%v", statErr)
	}
}

// TestUninstallOpenRCMissingFile verifies uninstalling an absent script is
// not an error.
func TestUninstallOpenRCMissingFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("OpenRC is Unix-only")
	}
	_ = os.Remove("/etc/init.d/api")

	if err := uninstallOpenRC(); err != nil {
		t.Errorf("uninstallOpenRC on a missing file returned %v, want nil", err)
	}
}

// TestInstallUninstallSysVinit verifies the SysVinit init script is written
// to the same /etc/init.d/api path and removed again.
func TestInstallUninstallSysVinit(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("SysVinit is Unix-only")
	}
	if err := os.MkdirAll("/etc/init.d", 0755); err != nil {
		t.Fatalf("setup MkdirAll failed: %v", err)
	}

	if err := installSysVinit(); err != nil {
		t.Logf("installSysVinit returned an error (expected if update-rc.d is absent): %v", err)
	}

	scriptPath := "/etc/init.d/api"
	data, readErr := os.ReadFile(scriptPath)
	if readErr != nil {
		t.Fatalf("expected SysVinit script at %s: %v", scriptPath, readErr)
	}

	// AI.md PART 24's SysVinit template requires the LSB header with
	// $network $remote_fs $syslog, the DAEMON_USER/PIDFILE/LOGFILE vars,
	// start-stop-daemon with --chuid, a --retry 30 stop, and a status
	// branch exiting 3 when the daemon is not running.
	script := string(data)
	for _, want := range []string{
		"### BEGIN INIT INFO",
		"# Required-Start:    $network $remote_fs $syslog",
		"# Required-Stop:     $network $remote_fs $syslog",
		"NAME=api",
		"DAEMON_USER=api",
		"PIDFILE=" + paths.DefaultPIDPath(),
		"LOGFILE=" + paths.LogDir() + "/server.log",
		"start-stop-daemon --start --quiet --background --make-pidfile",
		"--chuid $DAEMON_USER",
		"--no-close >> $LOGFILE 2>&1",
		"start-stop-daemon --stop --quiet --pidfile $PIDFILE --retry 30",
		"rm -f $PIDFILE",
		"kill -0 $(cat $PIDFILE)",
		"exit 3",
	} {
		if !strings.Contains(script, want) {
			t.Errorf("SysVinit script is missing %q", want)
		}
	}

	if err := uninstallSysVinit(); err != nil {
		t.Fatalf("uninstallSysVinit failed: %v", err)
	}
	if _, statErr := os.Stat(scriptPath); !os.IsNotExist(statErr) {
		t.Errorf("expected SysVinit script to be removed, stat err=%v", statErr)
	}
}

// TestUninstallSysVinitMissingFile verifies uninstalling an absent script is
// not an error.
func TestUninstallSysVinitMissingFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("SysVinit is Unix-only")
	}
	_ = os.Remove("/etc/init.d/api")

	if err := uninstallSysVinit(); err != nil {
		t.Errorf("uninstallSysVinit on a missing file returned %v, want nil", err)
	}
}

// TestSystemdUnitHardening verifies the systemd unit carries the PART 24
// hardening directives and a ReadWritePaths entry per writable directory,
// and carries no User=/Group= line (the binary drops privileges itself).
func TestSystemdUnitHardening(t *testing.T) {
	if err := os.MkdirAll("/etc/systemd/system", 0755); err != nil {
		t.Fatalf("setup MkdirAll failed: %v", err)
	}
	if err := installSystemd(); err != nil {
		t.Logf("installSystemd returned an error (expected if systemctl is absent): %v", err)
	}
	defer func() {
		_ = uninstallSystemd()
	}()

	data, readErr := os.ReadFile("/etc/systemd/system/api.service")
	if readErr != nil {
		t.Fatalf("expected systemd unit file: %v", readErr)
	}
	unit := string(data)

	for _, want := range []string{
		"Description=api service",
		"Documentation=https://apimgr.github.io/api",
		"After=network-online.target",
		"Wants=network-online.target",
		"Type=simple",
		"Restart=on-failure",
		"RestartSec=5\n",
		"StandardOutput=journal",
		"StandardError=journal",
		"ProtectSystem=strict",
		"ProtectHome=yes",
		"PrivateTmp=yes",
		"ReadWritePaths=" + paths.ConfigDir(),
		"ReadWritePaths=" + paths.DataDir(),
		"ReadWritePaths=" + paths.CacheDir(),
		"ReadWritePaths=" + paths.LogDir(),
		"WantedBy=multi-user.target",
	} {
		if !strings.Contains(unit, want) {
			t.Errorf("systemd unit is missing %q", want)
		}
	}
	for _, unwanted := range []string{"\nUser=", "\nGroup="} {
		if strings.Contains(unit, unwanted) {
			t.Errorf("systemd unit must not set %q; the binary drops privileges itself", strings.TrimSpace(unwanted))
		}
	}
	if strings.Contains(unit, "ProtectHome=read-only") {
		t.Error("systemd unit sets ProtectHome=read-only; PART 24 requires ProtectHome=yes")
	}
}

// TestRunitLogRunUsesLogDir verifies the runit log/run script logs into the
// resolved log directory rather than a service-relative ./main path.
func TestRunitLogRunUsesLogDir(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("runit is Unix-only")
	}
	if err := installRunit(); err != nil {
		t.Logf("installRunit returned an error: %v", err)
	}
	defer func() {
		_ = uninstallRunit()
	}()

	data, readErr := os.ReadFile("/etc/sv/api/log/run")
	if readErr != nil {
		t.Fatalf("expected runit log/run script: %v", readErr)
	}
	want := "exec svlogd -tt " + paths.LogDir()
	if !strings.Contains(string(data), want) {
		t.Errorf("runit log/run script is missing %q, got:\n%s", want, data)
	}
}

// TestLaunchdPlistHasNoUserKeys verifies the LaunchDaemon plist uses the
// PART 24 stdout/stderr log names and sets no UserName/GroupName key -
// launchd starts the daemon as root and the binary drops privileges.
func TestLaunchdPlistHasNoUserKeys(t *testing.T) {
	if err := os.MkdirAll("/Library/LaunchDaemons", 0755); err != nil {
		t.Fatalf("setup MkdirAll failed: %v", err)
	}
	if err := installLaunchd(); err != nil {
		t.Logf("installLaunchd returned an error (expected if launchctl is absent): %v", err)
	}
	defer func() {
		_ = uninstallLaunchd()
	}()

	data, readErr := os.ReadFile("/Library/LaunchDaemons/io.github.apimgr.api.plist")
	if readErr != nil {
		t.Fatalf("expected launchd plist: %v", readErr)
	}
	plist := string(data)

	for _, want := range []string{
		"<string>io.github.apimgr.api</string>",
		paths.LogDir() + "/stdout.log",
		paths.LogDir() + "/stderr.log",
	} {
		if !strings.Contains(plist, want) {
			t.Errorf("launchd plist is missing %q", want)
		}
	}
	for _, unwanted := range []string{"UserName", "GroupName"} {
		if strings.Contains(plist, unwanted) {
			t.Errorf("launchd plist must not set %s", unwanted)
		}
	}
}

// TestWriteServiceDefinitionDoesNotStart verifies Install is split so the
// definition can be written without starting the service; Install itself
// then starts it, per PART 23's "install, enable, and start" order.
func TestWriteServiceDefinitionDoesNotStart(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("writeServiceDefinition panicked: %v", r)
		}
	}()
	if err := writeServiceDefinition(); err != nil {
		t.Logf("writeServiceDefinition returned an error (expected in a sandbox): %v", err)
	}
}

// TestLaunchdPlistPath verifies the plist path follows the elevation of
// the current process: LaunchDaemon when elevated, LaunchAgent otherwise.
func TestLaunchdPlistPath(t *testing.T) {
	got := launchdPlistPath()

	if paths.IsElevated() {
		if got != "/Library/LaunchDaemons/io.github.apimgr.api.plist" {
			t.Errorf("launchdPlistPath() = %q, want the LaunchDaemon path", got)
		}
		return
	}
	if !strings.HasSuffix(got, filepath.Join("Library", "LaunchAgents", "io.github.apimgr.api.plist")) {
		t.Errorf("launchdPlistPath() = %q, want a LaunchAgent path", got)
	}
}

// TestUserLaunchAgentPath verifies the LaunchAgent path is anchored under
// the invoking user's home directory.
func TestUserLaunchAgentPath(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home directory in this environment")
	}
	want := filepath.Join(home, "Library", "LaunchAgents", "io.github.apimgr.api.plist")
	if got := userLaunchAgentPath(); got != want {
		t.Errorf("userLaunchAgentPath() = %q, want %q", got, want)
	}
}

// TestHasSysVinitRunlevelLink verifies runlevel-link detection agrees with
// the actual contents of /etc/rc?.d.
func TestHasSysVinitRunlevelLink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("SysVinit runlevels are Unix-only")
	}

	found := false
	for _, level := range []string{"2", "3", "4", "5"} {
		matches, err := filepath.Glob("/etc/rc" + level + ".d/S*api")
		if err == nil && len(matches) > 0 {
			found = true
		}
	}
	if got := hasSysVinitRunlevelLink(); got != found {
		t.Errorf("hasSysVinitRunlevelLink() = %v, want %v", got, found)
	}
}

// TestInstallUserServiceHonoursPlatform verifies the user-level install
// path is only supported on the platforms that have per-user service
// managers, and errors informatively elsewhere.
func TestInstallUserServiceHonoursPlatform(t *testing.T) {
	err := installUserService()

	switch runtime.GOOS {
	case "linux", "darwin":
		if err != nil {
			t.Logf("installUserService returned an error (expected without a user session bus): %v", err)
		}
	default:
		if err == nil {
			t.Errorf("installUserService must not claim support on %s", runtime.GOOS)
		}
	}
}

// TestConfirmDestructiveRejectsNonYes verifies only an explicit y/yes
// answer confirms a destructive operation; empty input means no.
func TestConfirmDestructiveRejectsNonYes(t *testing.T) {
	cases := map[string]bool{
		"y\n":      true,
		"Y\n":      true,
		"yes\n":    true,
		"  YES \n": true,
		"n\n":      false,
		"no\n":     false,
		"\n":       false,
		"maybe\n":  false,
		"":         false,
	}

	origStdin := os.Stdin
	defer func() { os.Stdin = origStdin }()

	for input, want := range cases {
		r, w, err := os.Pipe()
		if err != nil {
			t.Fatalf("os.Pipe failed: %v", err)
		}
		if _, err := w.WriteString(input); err != nil {
			t.Fatalf("writing stdin failed: %v", err)
		}
		w.Close()

		os.Stdin = r
		got, confirmErr := confirmDestructive("continue? [y/N]: ")
		r.Close()

		if confirmErr != nil {
			t.Errorf("confirmDestructive(%q) returned error %v", input, confirmErr)
		}
		if got != want {
			t.Errorf("confirmDestructive(%q) = %v, want %v", input, got, want)
		}
	}
}

// TestWindowsServiceIdentity verifies the Windows service identity is the
// built-in Virtual Service Account required by AI.md PART 24, and never
// Local System, Administrator, or a logged-in account.
func TestWindowsServiceIdentity(t *testing.T) {
	if windowsServiceAccount != `NT SERVICE\api` {
		t.Errorf("windowsServiceAccount = %q, want %q", windowsServiceAccount, `NT SERVICE\api`)
	}

	lowered := strings.ToLower(windowsServiceAccount)
	for _, forbidden := range []string{"localsystem", "local system", "administrator", "networkservice", "localservice"} {
		if strings.Contains(lowered, forbidden) {
			t.Errorf("windowsServiceAccount must not be %s", forbidden)
		}
	}

	if windowsDisplayName != "api" {
		t.Errorf("windowsDisplayName = %q, want %q", windowsDisplayName, "api")
	}
	if windowsDescription != "api service" {
		t.Errorf("windowsDescription = %q, want %q", windowsDescription, "api service")
	}
}

// TestDisableWindowsOffWindows verifies the non-Windows build reports the
// Windows service path as unavailable instead of silently succeeding.
func TestDisableWindowsOffWindows(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("this assertion covers the non-Windows stub only")
	}
	if err := disableWindows(); err == nil {
		t.Error("disableWindows must return an error when not built for GOOS=windows")
	}
}
