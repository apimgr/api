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

	"github.com/apimgr/api/src/common/i18n"
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
	ServiceS6
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
// in the priority order AI.md PART 23/24 describe: systemd, OpenRC, runit,
// s6, then SysVinit (chosen only when the earlier managers are absent).
func detectLinuxServiceManager() ServiceType {
	if hasSystemd() {
		return ServiceSystemd
	}
	if hasOpenRC() {
		return ServiceOpenRC
	}
	if fileExists("/etc/runit") || commandExists("sv") {
		return ServiceRunit
	}
	if commandExists("s6-svc") && hasRunningS6SVScan() {
		return ServiceS6
	}
	if hasSysVinit() {
		return ServiceSysVinit
	}
	return ServiceUnknown
}

// hasSystemd reports whether systemd is the running host init.
func hasSystemd() bool {
	return fileExists("/run/systemd/system")
}

// hasOpenRC reports whether OpenRC is installed and running on this host.
func hasOpenRC() bool {
	return fileExists("/sbin/openrc-run") && fileExists("/run/openrc")
}

// hasSysVinit reports whether this host should use the SysVinit init
// script. Per AI.md PART 24 the SysVinit branch is chosen only once
// /sbin/openrc-run and systemctl are both confirmed absent and /etc/init.d
// exists alongside a working update-rc.d or chkconfig.
func hasSysVinit() bool {
	if fileExists("/sbin/openrc-run") || commandExists("systemctl") {
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

	manager := DetectServiceManager()
	if manager == ServiceUnknown {
		return fmt.Errorf("unsupported service manager")
	}
	if runtime.GOOS != "windows" {
		uid, gid, err := RootSetup()
		if err != nil {
			return fmt.Errorf("prepare service account and runtime directories: %w", err)
		}
		if err := ClaimRuntimeDirs(uid, gid); err != nil {
			return fmt.Errorf("set runtime directory ownership for service account: %w", err)
		}
	}

	switch manager {
	case ServiceSystemd:
		return installSystemd()
	case ServiceOpenRC:
		return installOpenRC()
	case ServiceSysVinit:
		return installSysVinit()
	case ServiceRunit:
		return installRunit()
	case ServiceS6:
		return installS6()
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
	case ServiceS6:
		return uninstallS6()
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
		if err := exec.Command("systemctl", systemctlArgs("stop")...).Run(); err != nil {
			return err
		}
		return exec.Command("systemctl", systemctlArgs("disable")...).Run()
	case ServiceOpenRC:
		if err := exec.Command("rc-service", appName, "stop").Run(); err != nil {
			return err
		}
		return exec.Command("rc-update", "del", appName, "default").Run()
	case ServiceSysVinit:
		if err := exec.Command(fmt.Sprintf("/etc/init.d/%s", appName), "stop").Run(); err != nil {
			return err
		}
		if commandExists("update-rc.d") {
			return exec.Command("update-rc.d", "-f", appName, "remove").Run()
		}
		if commandExists("chkconfig") {
			return exec.Command("chkconfig", appName, "off").Run()
		}
		return nil
	case ServiceRunit:
		if GetStatus().Running {
			if err := exec.Command("sv", "stop", appName).Run(); err != nil {
				return err
			}
		}
		downFile := fmt.Sprintf("/etc/sv/%s/down", appName)
		return os.WriteFile(downFile, []byte{}, 0644)
	case ServiceS6:
		if err := exec.Command("s6-svc", "-d", s6ServicePath()).Run(); err != nil {
			return err
		}
		if err := os.Remove(s6ServiceLink()); err != nil && !os.IsNotExist(err) {
			return err
		}
		return exec.Command("s6-svscanctl", "-a", s6ScanDir()).Run()
	case ServiceLaunchd:
		return exec.Command("launchctl", "unload", "-w", launchdPlistPath()).Run()
	case ServiceWindows:
		return disableWindows()
	case ServiceBSDRC:
		if GetStatus().Running {
			if err := exec.Command("service", appName, "stop").Run(); err != nil {
				return err
			}
		}
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
	case ServiceOpenRC:
		return exec.Command("rc-service", appName, "start").Run()
	case ServiceSysVinit:
		return exec.Command(fmt.Sprintf("/etc/init.d/%s", appName), "start").Run()
	case ServiceRunit:
		return exec.Command("sv", "start", appName).Run()
	case ServiceS6:
		return exec.Command("s6-svc", "-u", s6ServicePath()).Run()
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
	case ServiceOpenRC:
		return exec.Command("rc-service", appName, "stop").Run()
	case ServiceSysVinit:
		return exec.Command(fmt.Sprintf("/etc/init.d/%s", appName), "stop").Run()
	case ServiceRunit:
		return exec.Command("sv", "stop", appName).Run()
	case ServiceS6:
		return exec.Command("s6-svc", "-d", s6ServicePath()).Run()
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
		output, err := exec.Command("rc-service", appName, "restart").CombinedOutput()
		if err != nil {
			return fmt.Errorf("rc-service %s restart failed: %w: %s", appName, err, strings.TrimSpace(string(output)))
		}
		return nil
	case ServiceSysVinit:
		return exec.Command(fmt.Sprintf("/etc/init.d/%s", appName), "restart").Run()
	case ServiceRunit:
		return exec.Command("sv", "restart", appName).Run()
	case ServiceS6:
		return exec.Command("s6-svc", "-r", s6ServicePath()).Run()
	case ServiceLaunchd:
		if err := Stop(); err != nil {
			return err
		}
		return Start()
	case ServiceWindows:
		if err := exec.Command("sc.exe", "stop", appName).Run(); err != nil {
			return err
		}
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
	case ServiceOpenRC:
		return exec.Command("rc-service", appName, "reload").Run()
	case ServiceSysVinit:
		return exec.Command(fmt.Sprintf("/etc/init.d/%s", appName), "reload").Run()
	case ServiceRunit:
		return exec.Command("sv", "hup", appName).Run()
	case ServiceS6:
		return Restart()
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
		st.Running = exec.Command("rc-service", appName, "status").Run() == nil
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
	case ServiceS6:
		st.Installed = fileExists(s6ServiceDefinition())
		st.Enabled = fileExists(s6ServiceLink())
		if st.Enabled {
			out, err := exec.Command("s6-svstat", s6ServicePath()).Output()
			st.Running = err == nil && strings.HasPrefix(strings.TrimSpace(string(out)), "up ")
		}
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
	case ServiceS6:
		return "s6"
	default:
		return "unknown"
	}
}

// HelpText renders the --service --help output for binaryName, including the
// live status block AI.md PART 23 requires. binaryName is the actual
// (possibly renamed) binary name so help always reflects how it was invoked.
func HelpText(binaryName, lang string) string {
	st := GetStatus()
	values := map[string]string{
		"binary_name":     binaryName,
		"service_manager": fmt.Sprint(DetectServiceManager()),
		"installed":       i18n.Translate(lang, boolTranslationKey(st.Installed)),
		"running":         i18n.Translate(lang, boolTranslationKey(st.Running)),
		"enabled":         i18n.Translate(lang, boolTranslationKey(st.Enabled)),
		"pid_line":        "",
	}
	if st.PID > 0 {
		values["pid_line"] = i18n.TranslateFormat(lang, "cli.service_pid", map[string]string{"pid": strconv.Itoa(st.PID)})
	}
	return i18n.TranslateFormat(lang, "cli.service_help", values)
}

func boolTranslationKey(value bool) string {
	if value {
		return "cli.status_yes"
	}
	return "cli.status_no"
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
