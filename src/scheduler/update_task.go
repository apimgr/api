package scheduler

import (
	"context"
	"log"
	"time"

	"github.com/apimgr/api/src/config"
	"github.com/apimgr/api/src/database"
	"github.com/apimgr/api/src/update"
)

// lastNotifiedKey is the config key holding the version most recently
// announced by the update_check task, so the update_available notification
// fires once per newly-seen eligible version instead of on every run
// (AI.md PART 22 "Update Visibility").
const lastNotifiedKey = "scheduler.update_check.last_notified_version"

// updateCheckTask implements AI.md PART 18's update_check task: it runs the
// defer_days-gated equivalent of `--update check` and, depending on
// update.auto_install, either notifies the operator or performs the full
// install. Update availability is operator-only information - it is logged
// and emailed, never surfaced on any public endpoint.
func (s *Scheduler) updateCheckTask() error {
	cfg, err := config.Load()
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
		log.Printf("Scheduler: Update available on the %s channel: %s (current: %s)", branch, release.TagName, s.opts.Version)
		s.notifyUpdateAvailable(s.opts.Version, release.TagName, branch)
		return nil
	}

	log.Printf("Scheduler: Installing update %s from the %s channel", release.TagName, branch)
	previous := s.opts.Version
	if err := update.Install(ctx, release); err != nil {
		log.Printf("Scheduler: Update install failed: %v", err)
		return err
	}

	log.Printf("Scheduler: Update %s installed, restarting service", release.TagName)
	if s.opts.Notifier != nil && len(s.opts.NotifyTo) > 0 {
		if nerr := s.opts.Notifier.NotifyUpdateInstalled(s.opts.NotifyTo, previous, release.TagName); nerr != nil {
			log.Printf("Scheduler: Failed to send update_installed notification: %v", nerr)
		}
	}

	if err := update.RestartService(); err != nil {
		log.Printf("Scheduler: Service restart after update failed: %v", err)
		return err
	}

	return nil
}

// notifyUpdateAvailable sends the update_available notification exactly once
// per newly-seen version, remembering the announced version in the config
// table so a restart does not re-announce it.
func (s *Scheduler) notifyUpdateAvailable(currentVersion, newVersion, branch string) {
	if lastNotifiedVersion() == newVersion {
		return
	}

	if s.opts.Notifier != nil && len(s.opts.NotifyTo) > 0 {
		if err := s.opts.Notifier.NotifyUpdateAvailable(s.opts.NotifyTo, currentVersion, newVersion, branch); err != nil {
			log.Printf("Scheduler: Failed to send update_available notification: %v", err)
			return
		}
	}

	setLastNotifiedVersion(newVersion)
}

// lastNotifiedVersion reads the most recently announced update version.
func lastNotifiedVersion() string {
	db := database.GetServerDB()
	if db == nil {
		return ""
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var value string
	if err := db.QueryRowContext(ctx, `SELECT value FROM config WHERE key = ?`, lastNotifiedKey).Scan(&value); err != nil {
		return ""
	}
	return value
}

// setLastNotifiedVersion records the announced update version.
func setLastNotifiedVersion(version string) {
	db := database.GetServerDB()
	if db == nil {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if _, err := db.ExecContext(ctx, `
		INSERT INTO config (key, value, type, updated_at) VALUES (?, ?, 'string', strftime('%s', 'now'))
		ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`,
		lastNotifiedKey, version); err != nil {
		log.Printf("Scheduler: Failed to record last notified update version: %v", err)
	}
}
