package sysservice

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/apimgr/api/src/paths"
)

// installSystemd creates the systemd unit file, enables it, and copies the
// binary into place. Directory creation follows AI.md PART 24's hardened
// template: ProtectSystem=strict, ProtectHome=yes, PrivateTmp=yes, with a
// dedicated ReadWritePaths line per writable directory. No User=/Group=
// line is set since the binary drops its own privileges after bind.
func installSystemd() error {
	binaryPath := GetBinaryPath()

	serviceContent := fmt.Sprintf(`[Unit]
Description=%s service
Documentation=https://%s.github.io/%s
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=%s
# Reload() dispatches to systemctl reload, which requires an ExecReload verb
ExecReload=/bin/kill -HUP $MAINPID
Restart=on-failure
RestartSec=5
StandardOutput=journal
StandardError=journal

# Security hardening (binary drops privileges after port binding)
ProtectSystem=strict
ProtectHome=yes
PrivateTmp=yes
ReadWritePaths=%s
ReadWritePaths=%s
ReadWritePaths=%s
ReadWritePaths=%s

[Install]
WantedBy=multi-user.target
`, appName, orgName, appName, binaryPath, paths.ConfigDir(), paths.DataDir(), paths.CacheDir(), paths.LogDir())

	servicePath := fmt.Sprintf("/etc/systemd/system/%s.service", appName)

	for _, dir := range []string{paths.ConfigDir(), paths.DataDir(), paths.CacheDir(), paths.LogDir()} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("failed to create directory %s: %w", dir, err)
		}
	}

	if err := os.WriteFile(servicePath, []byte(serviceContent), 0644); err != nil {
		return fmt.Errorf("failed to write service file: %w", err)
	}

	if exePath, err := os.Executable(); err == nil && exePath != binaryPath {
		if err := copyBinary(exePath, binaryPath); err != nil {
			return fmt.Errorf("failed to copy binary: %w", err)
		}
	}

	if err := exec.Command("systemctl", "daemon-reload").Run(); err != nil {
		return fmt.Errorf("failed to reload systemd: %w", err)
	}
	if err := exec.Command("systemctl", "enable", appName).Run(); err != nil {
		return fmt.Errorf("failed to enable service: %w", err)
	}

	fmt.Printf("Service installed at: %s\n", servicePath)
	fmt.Printf("Binary installed at: %s\n", binaryPath)

	return nil
}

// uninstallSystemd stops, disables, and removes the systemd unit file.
func uninstallSystemd() error {
	servicePath := fmt.Sprintf("/etc/systemd/system/%s.service", appName)

	exec.Command("systemctl", "stop", appName).Run()
	exec.Command("systemctl", "disable", appName).Run()

	if err := os.Remove(servicePath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove service file: %w", err)
	}

	exec.Command("systemctl", "daemon-reload").Run()

	fmt.Printf("Service uninstalled: %s\n", servicePath)
	return nil
}

// installSystemdUser installs a systemd --user unit for the invoking,
// non-elevated user, per AI.md PART 23's user-mode service fallback.
func installSystemdUser() error {
	binaryPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to resolve current executable: %w", err)
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to resolve home directory: %w", err)
	}
	unitDir := filepath.Join(home, ".config", "systemd", "user")
	if err := os.MkdirAll(unitDir, 0755); err != nil {
		return fmt.Errorf("failed to create unit directory: %w", err)
	}

	serviceContent := fmt.Sprintf(`[Unit]
Description=API Manager Server (user service)
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=%s
ExecReload=/bin/kill -HUP $MAINPID
Restart=on-failure
RestartSec=5s

[Install]
WantedBy=default.target
`, binaryPath)

	unitPath := filepath.Join(unitDir, appName+".service")
	if err := os.WriteFile(unitPath, []byte(serviceContent), 0644); err != nil {
		return fmt.Errorf("failed to write user unit file: %w", err)
	}

	exec.Command("systemctl", "--user", "daemon-reload").Run()
	if err := exec.Command("systemctl", "--user", "enable", appName).Run(); err != nil {
		return fmt.Errorf("failed to enable user service: %w", err)
	}

	fmt.Printf("User service installed at: %s\n", unitPath)

	return nil
}

// installOpenRC creates an OpenRC init script and adds it to the default
// runlevel.
func installOpenRC() error {
	binaryPath := GetBinaryPath()

	scriptContent := fmt.Sprintf(`#!/sbin/openrc-run
# Service identity comes from the internal name so config and data paths stay
# stable across binary renames.

name="%s"
description="API Manager Server"
# actual binary (may differ from the internal name after a rename)
command="%s"
command_args=""
command_user="%s:%s"
pidfile="%s"
command_background=true
output_log="%s/server.log"
error_log="%s/error.log"

depend() {
	need net
	after firewall
	use dns logger
}

start_pre() {
	checkpath -d -m 0755 -o %s:%s %s
	checkpath -d -m 0755 -o %s:%s %s
}
`, appName, binaryPath,
		ServiceUserName, ServiceGroupName,
		paths.DefaultPIDPath(),
		paths.LogDir(), paths.LogDir(),
		ServiceUserName, ServiceGroupName, filepath.Dir(paths.DefaultPIDPath()),
		ServiceUserName, ServiceGroupName, paths.LogDir())

	scriptPath := fmt.Sprintf("/etc/init.d/%s", appName)

	for _, dir := range []string{paths.ConfigDir(), paths.DataDir(), paths.CacheDir(), paths.LogDir()} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("failed to create directory %s: %w", dir, err)
		}
	}

	if err := os.WriteFile(scriptPath, []byte(scriptContent), 0755); err != nil {
		return fmt.Errorf("failed to write OpenRC script: %w", err)
	}

	if exePath, err := os.Executable(); err == nil && exePath != binaryPath {
		if err := copyBinary(exePath, binaryPath); err != nil {
			return fmt.Errorf("failed to copy binary: %w", err)
		}
	}

	if err := exec.Command("rc-update", "add", appName, "default").Run(); err != nil {
		return fmt.Errorf("failed to add service to default runlevel: %w", err)
	}

	fmt.Printf("OpenRC service installed at: %s\n", scriptPath)

	return nil
}

// uninstallOpenRC stops, removes from the default runlevel, and deletes
// the OpenRC init script.
func uninstallOpenRC() error {
	scriptPath := fmt.Sprintf("/etc/init.d/%s", appName)

	exec.Command("rc-service", appName, "stop").Run()
	exec.Command("rc-update", "del", appName, "default").Run()

	if err := os.Remove(scriptPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove OpenRC script: %w", err)
	}

	fmt.Printf("OpenRC service uninstalled: %s\n", scriptPath)
	return nil
}

// installSysVinit creates a SysVinit-compatible init script and registers
// it via update-rc.d (Debian/Ubuntu) or chkconfig (RHEL/CentOS), whichever
// is present.
func installSysVinit() error {
	binaryPath := GetBinaryPath()

	scriptContent := fmt.Sprintf(`#!/bin/sh
### BEGIN INIT INFO
# Provides:          %s
# Required-Start:    $network $remote_fs $syslog
# Required-Stop:     $network $remote_fs $syslog
# Default-Start:     2 3 4 5
# Default-Stop:      0 1 6
# Short-Description: API Manager Server
# Description:       API Manager Server daemon
### END INIT INFO

NAME=%s
DAEMON=%s
DAEMON_USER=%s
PIDFILE=%s
LOGFILE=%s/server.log

case "$1" in
    start)
        echo "Starting $NAME..."
        mkdir -p $(dirname $PIDFILE) $(dirname $LOGFILE)
        chown -R $DAEMON_USER:$DAEMON_USER $(dirname $PIDFILE) $(dirname $LOGFILE)
        start-stop-daemon --start --quiet --background --make-pidfile \
            --pidfile $PIDFILE --chuid $DAEMON_USER --exec $DAEMON \
            --no-close >> $LOGFILE 2>&1
        ;;
    stop)
        echo "Stopping $NAME..."
        start-stop-daemon --stop --quiet --pidfile $PIDFILE --retry 30
        rm -f $PIDFILE
        ;;
    restart)
        $0 stop
        sleep 1
        $0 start
        ;;
    status)
        if [ -f $PIDFILE ] && kill -0 $(cat $PIDFILE) 2>/dev/null; then
            echo "$NAME is running (pid $(cat $PIDFILE))"
            exit 0
        else
            echo "$NAME is stopped"
            exit 3
        fi
        ;;
    *)
        echo "Usage: $0 {start|stop|restart|status}"
        exit 1
        ;;
esac
exit 0
`, appName, appName, binaryPath, ServiceUserName, paths.DefaultPIDPath(), paths.LogDir())

	scriptPath := fmt.Sprintf("/etc/init.d/%s", appName)

	for _, dir := range []string{paths.ConfigDir(), paths.DataDir(), paths.CacheDir(), paths.LogDir()} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("failed to create directory %s: %w", dir, err)
		}
	}

	if err := os.WriteFile(scriptPath, []byte(scriptContent), 0755); err != nil {
		return fmt.Errorf("failed to write SysVinit script: %w", err)
	}

	if exePath, err := os.Executable(); err == nil && exePath != binaryPath {
		if err := copyBinary(exePath, binaryPath); err != nil {
			return fmt.Errorf("failed to copy binary: %w", err)
		}
	}

	if commandExists("update-rc.d") {
		if err := exec.Command("update-rc.d", appName, "defaults").Run(); err != nil {
			return fmt.Errorf("failed to register service with update-rc.d: %w", err)
		}
	} else if commandExists("chkconfig") {
		if err := exec.Command("chkconfig", "--add", appName).Run(); err != nil {
			return fmt.Errorf("failed to register service with chkconfig: %w", err)
		}
	}

	fmt.Printf("SysVinit script installed at: %s\n", scriptPath)

	return nil
}

// uninstallSysVinit stops, unregisters, and removes the SysVinit init
// script.
func uninstallSysVinit() error {
	scriptPath := fmt.Sprintf("/etc/init.d/%s", appName)

	exec.Command(scriptPath, "stop").Run()

	if commandExists("update-rc.d") {
		exec.Command("update-rc.d", "-f", appName, "remove").Run()
	} else if commandExists("chkconfig") {
		exec.Command("chkconfig", "--del", appName).Run()
	}

	if err := os.Remove(scriptPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove SysVinit script: %w", err)
	}

	fmt.Printf("SysVinit script uninstalled: %s\n", scriptPath)
	return nil
}

// installRunit creates a runit service directory with run/log scripts and
// symlinks it into the active service directory.
func installRunit() error {
	svDir := fmt.Sprintf("/etc/sv/%s", appName)
	binaryPath := GetBinaryPath()

	if err := os.MkdirAll(svDir, 0755); err != nil {
		return fmt.Errorf("failed to create service directory: %w", err)
	}

	for _, dir := range []string{paths.ConfigDir(), paths.DataDir(), paths.CacheDir(), paths.LogDir()} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("failed to create directory %s: %w", dir, err)
		}
	}

	if exePath, err := os.Executable(); err == nil && exePath != binaryPath {
		if err := copyBinary(exePath, binaryPath); err != nil {
			return fmt.Errorf("failed to copy binary: %w", err)
		}
	}

	runScript := fmt.Sprintf(`#!/bin/sh
exec %s 2>&1
`, binaryPath)

	runPath := filepath.Join(svDir, "run")
	if err := os.WriteFile(runPath, []byte(runScript), 0755); err != nil {
		return fmt.Errorf("failed to write run script: %w", err)
	}

	logDir := filepath.Join(svDir, "log")
	if err := os.MkdirAll(logDir, 0755); err != nil {
		return fmt.Errorf("failed to create log directory: %w", err)
	}

	logRunScript := fmt.Sprintf(`#!/bin/sh
exec svlogd -tt %s
`, paths.LogDir())
	logRunPath := filepath.Join(logDir, "run")
	if err := os.WriteFile(logRunPath, []byte(logRunScript), 0755); err != nil {
		return fmt.Errorf("failed to write log run script: %w", err)
	}

	linkPath := fmt.Sprintf("/var/service/%s", appName)
	if err := os.Symlink(svDir, linkPath); err != nil && !os.IsExist(err) {
		return fmt.Errorf("failed to link service directory: %w", err)
	}

	fmt.Printf("Runit service installed at: %s\n", svDir)
	return nil
}

// uninstallRunit stops the service, removes the active symlink, and
// deletes the service directory.
func uninstallRunit() error {
	svDir := fmt.Sprintf("/etc/sv/%s", appName)
	linkPath := fmt.Sprintf("/var/service/%s", appName)

	exec.Command("sv", "stop", appName).Run()
	os.Remove(linkPath)
	os.RemoveAll(svDir)

	fmt.Println("Runit service uninstalled")
	return nil
}

// installLaunchd creates a macOS LaunchDaemon plist keyed off plistName
// and copies the binary into place. Per AI.md PART 24 the plist carries no
// UserName/GroupName key: launchd starts the daemon as root so privileged
// ports can be bound, and the binary drops privileges itself afterwards.
func installLaunchd() error {
	binaryPath := GetBinaryPath()
	plistPath := fmt.Sprintf("/Library/LaunchDaemons/%s.plist", plistName)

	plistContent := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>%s</string>
    <key>ProgramArguments</key>
    <array>
        <string>%s</string>
    </array>
    <key>RunAtLoad</key>
    <true/>
    <key>KeepAlive</key>
    <true/>
    <key>StandardOutPath</key>
    <string>%s/stdout.log</string>
    <key>StandardErrorPath</key>
    <string>%s/stderr.log</string>
</dict>
</plist>
`, plistName, binaryPath, paths.LogDir(), paths.LogDir())

	for _, dir := range []string{paths.ConfigDir(), paths.DataDir(), paths.LogDir()} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("failed to create directory %s: %w", dir, err)
		}
	}

	if err := os.WriteFile(plistPath, []byte(plistContent), 0644); err != nil {
		return fmt.Errorf("failed to write plist file: %w", err)
	}

	if exePath, err := os.Executable(); err == nil && exePath != binaryPath {
		if err := copyBinary(exePath, binaryPath); err != nil {
			return fmt.Errorf("failed to copy binary: %w", err)
		}
	}

	fmt.Printf("LaunchDaemon installed at: %s\n", plistPath)
	fmt.Println()
	fmt.Println("To load the service:")
	fmt.Printf("  launchctl load %s\n", plistPath)

	return nil
}

// uninstallLaunchd unloads and removes the macOS LaunchDaemon plist.
func uninstallLaunchd() error {
	plistPath := fmt.Sprintf("/Library/LaunchDaemons/%s.plist", plistName)

	exec.Command("launchctl", "unload", plistPath).Run()

	if err := os.Remove(plistPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove plist file: %w", err)
	}

	fmt.Println("LaunchDaemon uninstalled")
	return nil
}

// installLaunchdUser installs a per-user macOS LaunchAgent for the
// invoking, non-elevated user.
func installLaunchdUser() error {
	binaryPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to resolve current executable: %w", err)
	}

	plistPath := userLaunchAgentPath()
	if plistPath == "" {
		return fmt.Errorf("failed to resolve home directory")
	}
	if err := os.MkdirAll(filepath.Dir(plistPath), 0755); err != nil {
		return fmt.Errorf("failed to create LaunchAgents directory: %w", err)
	}

	plistContent := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>%s</string>
    <key>ProgramArguments</key>
    <array>
        <string>%s</string>
    </array>
    <key>RunAtLoad</key>
    <true/>
    <key>KeepAlive</key>
    <true/>
    <key>StandardOutPath</key>
    <string>%s/stdout.log</string>
    <key>StandardErrorPath</key>
    <string>%s/stderr.log</string>
</dict>
</plist>
`, plistName, binaryPath, paths.LogDir(), paths.LogDir())

	if err := os.WriteFile(plistPath, []byte(plistContent), 0644); err != nil {
		return fmt.Errorf("failed to write LaunchAgent plist: %w", err)
	}

	fmt.Printf("LaunchAgent installed at: %s\n", plistPath)
	fmt.Println()
	fmt.Println("To load the service:")
	fmt.Printf("  launchctl load %s\n", plistPath)

	return nil
}

// installBSDRC creates a BSD rc.d script and copies the binary into
// place.
func installBSDRC() error {
	binaryPath := GetBinaryPath()
	rcPath := fmt.Sprintf("/usr/local/etc/rc.d/%s", appName)

	rcContent := fmt.Sprintf(`#!/bin/sh

# PROVIDE: %s
# REQUIRE: NETWORKING
# KEYWORD: shutdown

. /etc/rc.subr

name="%s"
rcvar="%s_enable"
command="%s"
pidfile="/var/run/%s.pid"

load_rc_config $name
: ${%s_enable:="NO"}

run_rc_command "$1"
`, appName, appName, appName, binaryPath, appName, appName)

	if err := os.WriteFile(rcPath, []byte(rcContent), 0755); err != nil {
		return fmt.Errorf("failed to write rc.d script: %w", err)
	}

	if exePath, err := os.Executable(); err == nil && exePath != binaryPath {
		if err := copyBinary(exePath, binaryPath); err != nil {
			return fmt.Errorf("failed to copy binary: %w", err)
		}
	}

	fmt.Printf("BSD rc.d script installed at: %s\n", rcPath)
	fmt.Println()
	fmt.Printf("Add '%s_enable=\"YES\"' to /etc/rc.conf\n", appName)

	return nil
}

// uninstallBSDRC stops the service and removes the rc.d script.
func uninstallBSDRC() error {
	rcPath := fmt.Sprintf("/usr/local/etc/rc.d/%s", appName)

	exec.Command("service", appName, "stop").Run()

	if err := os.Remove(rcPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove rc.d script: %w", err)
	}

	fmt.Println("BSD rc.d script uninstalled")
	return nil
}
