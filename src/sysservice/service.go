package sysservice

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"github.com/apimgr/api/src/paths"
)

const (
	appName = "api"
	orgName = "apimgr"
	// plistName is the reverse-DNS bundle identifier used for macOS
	// LaunchDaemon/LaunchAgent files ({plist_name} in the project CLAUDE.md
	// placeholders).
	plistName = "io.github.apimgr.api"
	// windowsServiceAccount is the built-in Virtual Service Account the
	// Windows service runs as. AI.md PART 24 forbids Local System,
	// Administrator, and any logged-in account as the service identity.
	windowsServiceAccount = `NT SERVICE\` + appName
	// windowsDisplayName and windowsDescription follow PART 24's
	// "{project_name}" / "{project_name} service" service metadata.
	windowsDisplayName = appName
	windowsDescription = appName + " service"
)

// ServiceType represents the type of service manager
type ServiceType int

const (
	ServiceUnknown ServiceType = iota
	ServiceSystemd
	ServiceOpenRC
	ServiceSysVinit
	ServiceRunit
	ServiceLaunchd
	ServiceWindows
	ServiceBSDRC
)

// Status reports the current install/run/autostart state of the service,
// along with its PID when known. Populated by GetStatus.
type Status struct {
	Installed bool
	Running   bool
	Enabled   bool
	PID       int
}

// DetectServiceManager detects the system's service manager
func DetectServiceManager() ServiceType {
	switch runtime.GOOS {
	case "linux":
		return detectLinuxServiceManager()
	case "darwin":
		return ServiceLaunchd
	case "windows":
		return ServiceWindows
	case "freebsd", "openbsd", "netbsd":
		return ServiceBSDRC
	default:
		return ServiceUnknown
	}
}

// detectLinuxServiceManager picks the first Linux service manager present,
// in the priority order AI.md PART 23/24 describe: systemd, then OpenRC,
// then runit, then SysVinit (chosen only once systemd and OpenRC are both
// confirmed absent and a working update-rc.d/chkconfig exists).
func detectLinuxServiceManager() ServiceType {
	if hasSystemd() {
		return ServiceSystemd
	}
	if hasOpenRC() {
		return ServiceOpenRC
	}
	if _, err := os.Stat("/run/runit"); err == nil {
		return ServiceRunit
	}
	if hasSysVinit() {
		return ServiceSysVinit
	}
	return ServiceUnknown
}

// hasSystemd reports whether systemd manages this host, either through a
// live /run/systemd/system booted marker, an /etc/systemd tree, or a
// systemctl binary on PATH.
func hasSystemd() bool {
	if fileExists("/run/systemd/system") {
		return true
	}
	if fileExists("/etc/systemd") {
		return true
	}
	return commandExists("systemctl")
}

// hasOpenRC reports whether OpenRC manages this host. AI.md PART 24 keys
// the check on /sbin/openrc-run specifically; a PATH lookup is accepted as
// well for distributions that install it elsewhere.
func hasOpenRC() bool {
	return fileExists("/sbin/openrc-run") || commandExists("openrc-run")
}

// hasSysVinit reports whether this host should use the SysVinit init
// script. Per AI.md PART 24 the SysVinit branch is chosen only once
// /sbin/openrc-run and systemctl are both confirmed absent and /etc/init.d
// exists alongside a working update-rc.d or chkconfig.
func hasSysVinit() bool {
	if hasOpenRC() || hasSystemd() {
		return false
	}
	if !fileExists("/etc/init.d") {
		return false
	}
	return commandExists("update-rc.d") || commandExists("chkconfig")
}

// hasSysVinitRunlevelLink reports whether the SysVinit init script is wired
// into any multi-user runlevel, which is how "enabled" is expressed without
// systemd or OpenRC.
func hasSysVinitRunlevelLink() bool {
	for _, level := range []string{"2", "3", "4", "5"} {
		if fileExists(fmt.Sprintf("/etc/rc%s.d/S01%s", level, appName)) {
			return true
		}
		matches, err := filepath.Glob(fmt.Sprintf("/etc/rc%s.d/S*%s", level, appName))
		if err == nil && len(matches) > 0 {
			return true
		}
	}
	return false
}

// commandExists reports whether name is found on PATH.
func commandExists(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

// fileExists reports whether path exists (any file type).
func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// Install installs, enables, and starts the service for the detected
// service manager. When the process is not elevated (root/Administrator),
// it falls back to a user-level service (systemd --user, launchd
// LaunchAgent). User/group creation and directory setup happen during
// normal binary startup, not here.
func Install() error {
	if err := writeServiceDefinition(); err != nil {
		return err
	}
	return Start()
}

// writeServiceDefinition writes and enables the service definition for the
// detected service manager, without starting it. Install starts it
// afterwards; splitting the two keeps the "install, enable, start" order
// AI.md PART 23 requires explicit and testable.
func writeServiceDefinition() error {
	if !paths.IsElevated() {
		return installUserService()
	}

	switch DetectServiceManager() {
	case ServiceSystemd:
		return installSystemd()
	case ServiceOpenRC:
		return installOpenRC()
	case ServiceSysVinit:
		return installSysVinit()
	case ServiceRunit:
		return installRunit()
	case ServiceLaunchd:
		return installLaunchd()
	case ServiceWindows:
		return installWindows()
	case ServiceBSDRC:
		return installBSDRC()
	default:
		return fmt.Errorf("unsupported service manager")
	}
}

// installUserService installs a per-user service when the caller cannot
// (or does not want to) escalate to root/Administrator.
func installUserService() error {
	switch runtime.GOOS {
	case "linux":
		return installSystemdUser()
	case "darwin":
		return installLaunchdUser()
	default:
		return fmt.Errorf("user-level service installation is not supported on %s; run as root/Administrator to install a system service", runtime.GOOS)
	}
}

// Uninstall stops, disables, and completely removes the service
// definition, then deletes every directory the service owns (config,
// data, cache, log, backup), the PID file, and finally the dedicated
// system user/group. This is destructive and irreversible. When assumeYes
// is false it prompts interactively on stdin and aborts (returning nil)
// unless answered "y"/"yes"; pass assumeYes=true only when the caller
// already obtained confirmation through its own UI.
func Uninstall(assumeYes bool) error {
	if !assumeYes {
		confirmed, err := confirmDestructive("This will delete ALL data, configs, and the system user. Continue? [y/N] ")
		if err != nil {
			return err
		}
		if !confirmed {
			fmt.Println("Uninstall cancelled.")
			return nil
		}
	}

	if err := removeServiceDefinition(); err != nil {
		return err
	}

	for _, dir := range []string{paths.ConfigDir(), paths.DataDir(), paths.CacheDir(), paths.LogDir(), paths.BackupDir()} {
		if err := os.RemoveAll(dir); err != nil {
			return fmt.Errorf("failed to remove %s: %w", dir, err)
		}
	}

	pidPath := paths.DefaultPIDPath()
	if err := os.Remove(pidPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove PID file %s: %w", pidPath, err)
	}

	if err := DeleteServiceUser(); err != nil {
		return fmt.Errorf("failed to delete service user: %w", err)
	}

	fmt.Println("Service uninstalled.")
	fmt.Printf("Delete binary manually: rm %s\n", GetBinaryPath())
	return nil
}

// removeServiceDefinition stops, disables, and deletes the on-disk service
// definition for whichever manager DetectServiceManager selects. Data
// directories and the system user are left alone; Uninstall handles those
// separately.
func removeServiceDefinition() error {
	switch DetectServiceManager() {
	case ServiceSystemd:
		return uninstallSystemd()
	case ServiceOpenRC:
		return uninstallOpenRC()
	case ServiceSysVinit:
		return uninstallSysVinit()
	case ServiceRunit:
		return uninstallRunit()
	case ServiceLaunchd:
		return uninstallLaunchd()
	case ServiceWindows:
		return uninstallWindows()
	case ServiceBSDRC:
		return uninstallBSDRC()
	default:
		return fmt.Errorf("unsupported service manager")
	}
}

// confirmDestructive prints prompt, reads a line from stdin, and reports
// whether the answer was "y" or "yes" (case-insensitive). Any other
// answer, including empty input, is treated as "no".
func confirmDestructive(prompt string) (bool, error) {
	fmt.Print(prompt)
	reader := bufio.NewReader(os.Stdin)
	line, err := reader.ReadString('\n')
	if err != nil && err != io.EOF {
		return false, fmt.Errorf("failed to read confirmation: %w", err)
	}
	answer := strings.ToLower(strings.TrimSpace(line))
	return answer == "y" || answer == "yes", nil
}

// Disable stops the service and removes it from autostart, but keeps the
// service file, config/data/log directories, and system user in place.
// Re-enable with Install.
func Disable() error {
	switch DetectServiceManager() {
	case ServiceSystemd:
		exec.Command("systemctl", systemctlArgs("stop")...).Run()
		return exec.Command("systemctl", systemctlArgs("disable")...).Run()
	case ServiceOpenRC:
		exec.Command("rc-service", appName, "stop").Run()
		return exec.Command("rc-update", "del", appName, "default").Run()
	case ServiceSysVinit:
		exec.Command(fmt.Sprintf("/etc/init.d/%s", appName), "stop").Run()
		if commandExists("update-rc.d") {
			return exec.Command("update-rc.d", "-f", appName, "remove").Run()
		}
		if commandExists("chkconfig") {
			return exec.Command("chkconfig", appName, "off").Run()
		}
		return nil
	case ServiceRunit:
		exec.Command("sv", "stop", appName).Run()
		downFile := fmt.Sprintf("/etc/sv/%s/down", appName)
		return os.WriteFile(downFile, []byte{}, 0644)
	case ServiceLaunchd:
		return exec.Command("launchctl", "unload", "-w", launchdPlistPath()).Run()
	case ServiceWindows:
		return disableWindows()
	case ServiceBSDRC:
		exec.Command("service", appName, "stop").Run()
		return exec.Command("sysrc", fmt.Sprintf("%s_enable=NO", appName)).Run()
	default:
		return fmt.Errorf("unsupported service manager")
	}
}

// Start starts the service
func Start() error {
	switch DetectServiceManager() {
	case ServiceSystemd:
		return exec.Command("systemctl", systemctlArgs("start")...).Run()
	case ServiceOpenRC, ServiceSysVinit:
		return exec.Command("service", appName, "start").Run()
	case ServiceRunit:
		return exec.Command("sv", "start", appName).Run()
	case ServiceLaunchd:
		return exec.Command("launchctl", "load", launchdPlistPath()).Run()
	case ServiceWindows:
		return exec.Command("sc.exe", "start", appName).Run()
	case ServiceBSDRC:
		return exec.Command("service", appName, "start").Run()
	default:
		return fmt.Errorf("unsupported service manager")
	}
}

// Stop stops the service
func Stop() error {
	switch DetectServiceManager() {
	case ServiceSystemd:
		return exec.Command("systemctl", systemctlArgs("stop")...).Run()
	case ServiceOpenRC, ServiceSysVinit:
		return exec.Command("service", appName, "stop").Run()
	case ServiceRunit:
		return exec.Command("sv", "stop", appName).Run()
	case ServiceLaunchd:
		return exec.Command("launchctl", "unload", launchdPlistPath()).Run()
	case ServiceWindows:
		return exec.Command("sc.exe", "stop", appName).Run()
	case ServiceBSDRC:
		return exec.Command("service", appName, "stop").Run()
	default:
		return fmt.Errorf("unsupported service manager")
	}
}

// Restart restarts the service
func Restart() error {
	switch DetectServiceManager() {
	case ServiceSystemd:
		return exec.Command("systemctl", systemctlArgs("restart")...).Run()
	case ServiceOpenRC:
		return exec.Command("rc-service", appName, "restart").Run()
	case ServiceSysVinit:
		return exec.Command(fmt.Sprintf("/etc/init.d/%s", appName), "restart").Run()
	case ServiceRunit:
		return exec.Command("sv", "restart", appName).Run()
	case ServiceLaunchd:
		Stop()
		return Start()
	case ServiceWindows:
		exec.Command("sc.exe", "stop", appName).Run()
		return exec.Command("sc.exe", "start", appName).Run()
	case ServiceBSDRC:
		return exec.Command("service", appName, "restart").Run()
	default:
		return fmt.Errorf("unsupported service manager")
	}
}

// Reload sends a configuration reload to the service where supported,
// falling back to a full Restart otherwise.
func Reload() error {
	switch DetectServiceManager() {
	case ServiceSystemd:
		return exec.Command("systemctl", systemctlArgs("reload")...).Run()
	case ServiceOpenRC, ServiceSysVinit:
		return exec.Command("service", appName, "reload").Run()
	case ServiceRunit:
		return exec.Command("sv", "hup", appName).Run()
	default:
		return Restart()
	}
}

// GetStatus queries the detected service manager for the current service
// state. Failed/absent lookups are reported as false/0 rather than
// errors, since "not installed" is expected, common, and not itself a
// failure.
func GetStatus() Status {
	var st Status

	switch DetectServiceManager() {
	case ServiceSystemd:
		st.Installed = fileExists(fmt.Sprintf("/etc/systemd/system/%s.service", appName))
		st.Running = exec.Command("systemctl", "is-active", "--quiet", appName).Run() == nil
		st.Enabled = exec.Command("systemctl", "is-enabled", "--quiet", appName).Run() == nil
	case ServiceOpenRC:
		st.Installed = fileExists(fmt.Sprintf("/etc/init.d/%s", appName))
		st.Running = exec.Command("service", appName, "status").Run() == nil
		if out, err := exec.Command("rc-update", "show", "default").Output(); err == nil {
			st.Enabled = strings.Contains(string(out), appName)
		}
	case ServiceSysVinit:
		st.Installed = fileExists(fmt.Sprintf("/etc/init.d/%s", appName))
		st.Running = exec.Command("service", appName, "status").Run() == nil
		st.Enabled = hasSysVinitRunlevelLink()
	case ServiceRunit:
		st.Installed = fileExists(fmt.Sprintf("/etc/sv/%s", appName))
		st.Running = exec.Command("sv", "status", appName).Run() == nil
		st.Enabled = st.Installed && !fileExists(fmt.Sprintf("/etc/sv/%s/down", appName))
	case ServiceLaunchd:
		st.Installed = fileExists(fmt.Sprintf("/Library/LaunchDaemons/%s.plist", plistName)) || fileExists(userLaunchAgentPath())
		st.Running = exec.Command("launchctl", "list", plistName).Run() == nil
		st.Enabled = st.Running
	case ServiceWindows:
		out, err := exec.Command("sc.exe", "query", appName).Output()
		st.Installed = err == nil
		st.Running = err == nil && strings.Contains(string(out), "RUNNING")
		if cfg, cfgErr := exec.Command("sc.exe", "qc", appName).Output(); cfgErr == nil {
			st.Enabled = strings.Contains(string(cfg), "AUTO_START")
		}
	case ServiceBSDRC:
		st.Installed = fileExists(fmt.Sprintf("/usr/local/etc/rc.d/%s", appName))
		st.Running = exec.Command("service", appName, "status").Run() == nil
		if out, err := exec.Command("sysrc", "-n", fmt.Sprintf("%s_enable", appName)).Output(); err == nil {
			st.Enabled = strings.EqualFold(strings.TrimSpace(string(out)), "YES")
		}
	}

	if pid, ok := readPIDFile(); ok {
		st.PID = pid
	}
	return st
}

// readPIDFile returns the numeric PID recorded at paths.DefaultPIDPath(),
// if present and parseable.
func readPIDFile() (int, bool) {
	data, err := os.ReadFile(paths.DefaultPIDPath())
	if err != nil {
		return 0, false
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return 0, false
	}
	return pid, true
}

// systemctlArgs builds the systemctl argument list for subcommand sub,
// switching to the "--user" bus when the process is not elevated so it
// targets whichever unit Install/installUserService created.
func systemctlArgs(sub string) []string {
	if !paths.IsElevated() {
		return []string{"--user", sub, appName}
	}
	return []string{sub, appName}
}

// launchdPlistPath returns the LaunchDaemon path when elevated, or the
// current user's LaunchAgent path otherwise, mirroring Install's choice.
func launchdPlistPath() string {
	if paths.IsElevated() {
		return fmt.Sprintf("/Library/LaunchDaemons/%s.plist", plistName)
	}
	return userLaunchAgentPath()
}

// userLaunchAgentPath returns the current user's LaunchAgent plist path.
func userLaunchAgentPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, "Library", "LaunchAgents", plistName+".plist")
}

// GetBinaryPath returns the path where the binary should be installed
func GetBinaryPath() string {
	switch runtime.GOOS {
	case "windows":
		return fmt.Sprintf(`C:\Program Files\%s\%s\%s.exe`, orgName, appName, appName)
	default:
		return fmt.Sprintf("/usr/local/bin/%s", appName)
	}
}

// String returns the human readable name of the service manager, used in
// status output and the --service --help block.
func (s ServiceType) String() string {
	switch s {
	case ServiceSystemd:
		return "systemd"
	case ServiceOpenRC:
		return "openrc"
	case ServiceSysVinit:
		return "sysvinit"
	case ServiceRunit:
		return "runit"
	case ServiceLaunchd:
		return "launchd"
	case ServiceWindows:
		return "windows"
	case ServiceBSDRC:
		return "rc.d"
	default:
		return "unknown"
	}
}

// HelpText renders the --service --help output for binaryName, including the
// live status block AI.md PART 23 requires. binaryName is the actual
// (possibly renamed) binary name so help always reflects how it was invoked.
func HelpText(binaryName string) string {
	st := GetStatus()

	var b strings.Builder
	fmt.Fprintf(&b, "Usage: %s --service <command>\n\n", binaryName)
	b.WriteString("Service control:\n")
	b.WriteString("  start                 Start the service\n")
	b.WriteString("  stop                  Stop the service\n")
	b.WriteString("  restart               Restart the service\n")
	b.WriteString("  reload                Reload the service configuration\n\n")
	b.WriteString("Service management:\n")
	b.WriteString("  --install             Install, enable, and start the service\n")
	b.WriteString("  --disable             Stop and disable the service (keeps data and config)\n")
	b.WriteString("  --uninstall           Remove the service, all data, config, and the system user\n\n")
	b.WriteString("Current status:\n")
	fmt.Fprintf(&b, "  Service manager:      %s\n", DetectServiceManager())
	fmt.Fprintf(&b, "  Installed:            %s\n", yesNo(st.Installed))
	fmt.Fprintf(&b, "  Running:              %s\n", yesNo(st.Running))
	fmt.Fprintf(&b, "  Enabled at boot:      %s\n", yesNo(st.Enabled))
	if st.PID > 0 {
		fmt.Fprintf(&b, "  PID:                  %d\n", st.PID)
	}
	return b.String()
}

// yesNo renders a boolean as the yes/no wording used by the status block.
func yesNo(v bool) string {
	if v {
		return "yes"
	}
	return "no"
}

// copyBinary copies the binary to the destination
func copyBinary(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}

	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}

	return os.WriteFile(dst, data, 0755)
}
