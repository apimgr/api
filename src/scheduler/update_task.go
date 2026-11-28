package scheduler

import (
	"context"
	"log"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/apimgr/api/src/config"
	"github.com/apimgr/api/src/database"
	"github.com/apimgr/api/src/paths"
	"github.com/apimgr/api/src/sysservice"
	"github.com/apimgr/api/src/update"
)

// lastNotifiedKey is the config key holding the version most recently
// announced by the update_check task, so the update_available notification
// fires once per newly-seen eligible version instead of on every run
// (AI.md PART 22 "Update Visibility").
const lastNotifiedKey = "scheduler.update_check.last_notified_version"
const lastFailedKey = "scheduler.update_check.last_failed_version"
const lastStagedKey = "scheduler.update_check.last_staged_version"

// updateCheckTask implements AI.md PART 18's update_check task: it runs the
// defer_days-gated equivalent of `--update check` and, depending on
// update.auto_install, either notifies the operator or performs the full
// install. Update availability is operator-only information - it is logged
// and emailed, never surfaced on any public endpoint.
func (s *Scheduler) updateCheckTask() error {
	cfg, err := config.Load(nil)
	if err != nil {
		log.Printf("Scheduler: Update check skipped, failed to load config: %v", err)
		return err
	}

	branch := cfg.Server.Update.Branch
	if !update.ValidBranch(branch) {
		branch = update.BranchStable
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	release, err := update.CheckEligible(ctx, s.opts.Version, branch, s.opts.BuildEpoch, cfg.Server.Update.DeferDays)
	if err != nil {
		log.Printf("Scheduler: Update check failed: %v", err)
		return err
	}
	if release == nil {
		log.Println("Scheduler: Update check completed, no eligible update available")
		return nil
	}
	if !cfg.Server.Update.AutoInstall {
		s.notifyUpdateAvailable(s.opts.Version, release.TagName, branch)
		return nil
	}
	if lastFailedUpdateVersion() == release.TagName {
		log.Printf("Scheduler: Update %s previously failed; waiting for the next release", release.TagName)
		return nil
	}
	if lastStagedUpdateVersion() == release.TagName {
		return nil
	}
	if !paths.IsElevated() || runtime.GOOS == "windows" {
		if s.notifyUpdateAvailable(s.opts.Version, release.TagName, branch) {
			s.auditUpdate("update.install_skipped", map[string]interface{}{"reason": "not_elevated", "version": release.TagName})
		}
		return nil
	}

	log.Printf("Scheduler: Staging update %s from the %s channel", release.TagName, branch)
	if err := update.Stage(ctx, release, paths.DataDir()); err != nil {
		log.Printf("Scheduler: Update staging failed: %v", err)
		s.auditUpdate("update.failed", map[string]interface{}{"version": release.TagName})
		setLastFailedUpdateVersion(release.TagName)
		if s.opts.Notifier != nil && len(s.opts.NotifyTo) > 0 {
			if nerr := s.opts.Notifier.NotifyUpdateFailed(s.opts.NotifyTo, release.TagName, update.SanitizeUpdateFailure(err)); nerr != nil {
				log.Printf("Scheduler: Failed to send update_failed notification: %v", nerr)
			}
		}
		return err
	}
	setLastFailedUpdateVersion("")
	setLastStagedUpdateVersion(release.TagName)
	s.auditUpdate("update.staged", map[string]interface{}{"version": release.TagName})
	if supervisorRestartsAfterTempFail() {
		log.Printf("Scheduler: Update %s staged; shutting down for privileged restart", release.TagName)
		update.RequestRestart()
		process, err := os.FindProcess(os.Getpid())
		if err != nil {
			return err
		}
		if err := process.Signal(os.Interrupt); err != nil {
			return err
		}
	} else {
		log.Printf("Scheduler: Update %s staged and will be applied on the next service start", release.TagName)
		if s.opts.Notifier != nil && len(s.opts.NotifyTo) > 0 {
			if nerr := s.opts.Notifier.NotifyUpdateStaged(s.opts.NotifyTo, release.TagName); nerr != nil {
				log.Printf("Scheduler: Failed to send update_staged notification: %v", nerr)
			}
		}
	}
	return nil
}

func lastFailedUpdateVersion() string {
	value, err := database.GetConfigValue(context.Background(), lastFailedKey)
	if err != nil {
		log.Printf("Scheduler: Failed to read last failed update version: %v", err)
		return ""
	}
	return value
}

func setLastFailedUpdateVersion(version string) {
	if err := database.SetConfigValue(context.Background(), lastFailedKey, version); err != nil {
		log.Printf("Scheduler: Failed to record failed update version: %v", err)
	}
}

func lastStagedUpdateVersion() string {
	value, err := database.GetConfigValue(context.Background(), lastStagedKey)
	if err != nil {
		log.Printf("Scheduler: Failed to read last staged update version: %v", err)
		return ""
	}
	return value
}

func setLastStagedUpdateVersion(version string) {
	if err := database.SetConfigValue(context.Background(), lastStagedKey, version); err != nil {
		log.Printf("Scheduler: Failed to record staged update version: %v", err)
	}
}

func supervisorRestartsAfterTempFail() bool {
	switch sysservice.DetectServiceManager() {
	case sysservice.ServiceSystemd:
		return os.Getenv("INVOCATION_ID") != ""
	case sysservice.ServiceRunit:
		return hasLinuxSupervisorAncestor("runsv", "runsvdir")
	case sysservice.ServiceS6:
		return hasLinuxSupervisorAncestor("s6-supervise", "s6-svscan")
	case sysservice.ServiceLaunchd:
		return os.Getenv("LAUNCH_JOBKEY_LABEL") != "" || os.Getenv("XPC_SERVICE_NAME") != ""
	case sysservice.ServiceBSDRC:
		return true
	default:
		return false
	}
}

func hasLinuxSupervisorAncestor(names ...string) bool {
	if runtime.GOOS != "linux" {
		return false
	}
	pid := os.Getpid()
	for depth := 0; depth < 16 && pid > 1; depth++ {
		base := "/proc/" + strconv.Itoa(pid)
		comm, err := os.ReadFile(base + "/comm")
		if err == nil {
			name := strings.TrimSpace(string(comm))
			for _, candidate := range names {
				if name == candidate {
					return true
				}
			}
		}
		stat, err := os.ReadFile(base + "/stat")
		if err != nil {
			return false
		}
		end := strings.LastIndexByte(string(stat), ')')
		if end < 0 {
			return false
		}
		fields := strings.Fields(string(stat[end+1:]))
		if len(fields) < 2 {
			return false
		}
		parent, err := strconv.Atoi(fields[1])
		if err != nil || parent <= 0 || parent == pid {
			return false
		}
		pid = parent
	}
	return false
}

func (s *Scheduler) auditUpdate(event string, details map[string]interface{}) {
	if err := database.WriteAuditEventWithSource(context.Background(), event, "system", "", "scheduler", details); err != nil {
		log.Printf("Scheduler: Failed to write %s audit event: %v", event, err)
	}
}

// notifyUpdateAvailable sends the update_available notification exactly once
// per newly-seen version, remembering the announced version in the config
// table so a restart does not re-announce it.
func (s *Scheduler) notifyUpdateAvailable(currentVersion, newVersion, branch string) bool {
	if lastNotifiedVersion() == newVersion {
		return false
	}
	setLastNotifiedVersion(newVersion)
	log.Printf("Scheduler: [WARN] Update available on the %s channel: %s (current: %s)", branch, newVersion, currentVersion)

	if s.opts.Notifier != nil && len(s.opts.NotifyTo) > 0 {
		if err := s.opts.Notifier.NotifyUpdateAvailable(s.opts.NotifyTo, currentVersion, newVersion, branch); err != nil {
			log.Printf("Scheduler: Failed to send update_available notification: %v", err)
		}
	}
	return true
}

// lastNotifiedVersion reads the most recently announced update version. A
// read failure is treated as "never announced" so a transient database error
// can only ever cause a duplicate notification, never a missed one.
func lastNotifiedVersion() string {
	value, err := database.GetConfigValue(context.Background(), lastNotifiedKey)
	if err != nil {
		log.Printf("Scheduler: Failed to read last notified update version: %v", err)
		return ""
	}
	return value
}

// setLastNotifiedVersion records the announced update version.
func setLastNotifiedVersion(version string) {
	if err := database.SetConfigValue(context.Background(), lastNotifiedKey, version); err != nil {
		log.Printf("Scheduler: Failed to record last notified update version: %v", err)
	}
}
