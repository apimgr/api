package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/apimgr/api/src/common/i18n"
	"github.com/apimgr/api/src/config"
	"github.com/apimgr/api/src/database"
	"github.com/apimgr/api/src/email"
	appmode "github.com/apimgr/api/src/mode"
	"github.com/apimgr/api/src/paths"
	"github.com/apimgr/api/src/scheduler"
	"github.com/apimgr/api/src/server"
)

func handleSchedulerMaintenance(args []string, binaryName string) int {
	if len(args) == 0 || strings.EqualFold(args[0], "help") || args[0] == "--help" || args[0] == "-h" {
		return printSchedulerHelp(binaryName)
	}
	if len(args) == 2 && args[1] == "--help" {
		return printSchedulerHelp(binaryName)
	}

	cfg, err := config.Load(nil)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Failed to load configuration")
		return exConfig
	}
	if _, err := authorizeSensitiveOperationWithWriter(cfg, os.Stderr); err != nil {
		fmt.Fprintf(os.Stderr, "Authorization failed: %v\n", err)
		return 77
	}
	if err := database.InitDatabase(cfg.Server.Database, paths.DataDir()); err != nil {
		fmt.Fprintln(os.Stderr, "Failed to initialize the database")
		return exUnavailable
	}
	defer func() { _ = database.Close() }()
	if err := database.ConfigureAuditFile(cfg.Server.Logs.Audit); err != nil {
		fmt.Fprintln(os.Stderr, "Failed to configure the audit log")
		return exUnavailable
	}
	if err := server.InitLogger(&cfg.Server.Logs); err != nil {
		fmt.Fprintln(os.Stderr, "Failed to configure logging")
		return exUnavailable
	}
	config.Set(cfg)

	initCLINotifier()
	options := scheduler.SchedulerOptionsFromConfig(cfg.Server.Scheduler)
	options.Version = Version
	options.BuildEpoch = buildEpoch()
	if notifier := email.DefaultNotifier(); notifier != nil {
		options.Notifier = notifier
		options.NotifyTo = notifier.Recipients()
	}
	sched := scheduler.NewWithOptions(options)
	sched.RegisterDefaultTasks()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	switch strings.ToLower(args[0]) {
	case "list":
		if len(args) != 1 {
			return schedulerUsage(binaryName)
		}
		if _, err := fmt.Fprintf(os.Stdout, "%-22s %-22s %-24s %-10s %-12s %-20s\n", "TASK", "SCHEDULE", "LAST RUN", "STATUS", "NEXT RUN", "RUNS/FAILURES"); err != nil {
			return exUnavailable
		}
		for _, task := range sched.ListTasks() {
			lastRun, nextRun := "-", "-"
			if !task.LastRun.IsZero() {
				lastRun = task.LastRun.Local().Format("2006-01-02 15:04")
			}
			if !task.NextRun.IsZero() {
				nextRun = task.NextRun.Local().Format("2006-01-02 15:04")
			}
			status := task.LastStatus
			if status == "" {
				status = "pending"
			}
			if !task.Enabled {
				status = "disabled"
			}
			if _, err := fmt.Fprintf(os.Stdout, "%-22s %-22s %-24s %-10s %-20s %d/%d\n", task.ID, task.Schedule, lastRun, status, nextRun, task.RunCount, task.FailCount); err != nil {
				return exUnavailable
			}
		}
		return 0

	case "show":
		if len(args) != 2 {
			return schedulerUsage(binaryName)
		}
		task, err := sched.ShowTask(args[1])
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return exUnavailable
		}
		lastRun, nextRun := "-", "-"
		if !task.LastRun.IsZero() {
			lastRun = task.LastRun.Local().Format(time.RFC3339)
		}
		if !task.NextRun.IsZero() {
			nextRun = task.NextRun.Local().Format(time.RFC3339)
		}
		if _, err := fmt.Fprintf(os.Stdout, "Task: %s\nName: %s\nEnabled: %t\nSkippable: %t\nSchedule: %s\nLast run: %s\nLast status: %s\nLast error: %s\nNext run: %s\nSuccessful runs: %d\nFailed runs: %d\nRunning: %t\n",
			task.ID, task.Name, task.Enabled, task.Skippable, task.Schedule, lastRun, task.LastStatus, task.LastError, nextRun, task.RunCount, task.FailCount, task.Running); err != nil {
			return exUnavailable
		}
		return 0

	case "history":
		if len(args) != 2 {
			return schedulerUsage(binaryName)
		}
		entries, err := sched.TaskHistory(args[1], 50)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return exUnavailable
		}
		if _, err := fmt.Fprintf(os.Stdout, "%-22s %-22s %-10s %-12s %s\n", "STARTED", "FINISHED", "STATUS", "DURATION", "ERROR"); err != nil {
			return exUnavailable
		}
		for _, entry := range entries {
			if _, err := fmt.Fprintf(os.Stdout, "%-22s %-22s %-10s %-12s %s\n", entry.StartedAt.Local().Format("2006-01-02 15:04:05"), entry.FinishedAt.Local().Format("2006-01-02 15:04:05"), entry.Status, time.Duration(entry.DurationMS)*time.Millisecond, entry.Error); err != nil {
				return exUnavailable
			}
		}
		return 0

	case "run":
		if len(args) != 2 {
			return schedulerUsage(binaryName)
		}
		task, err := sched.ShowTask(args[1])
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return exUnavailable
		}
		if err := database.WriteAuditEventWithSource(ctx, "scheduler.task_manual_run", "operator", "", "cli", map[string]any{"task_name": task.Name, "task_id": task.ID, "source": "cli"}); err != nil {
			fmt.Fprintln(os.Stderr, "Failed to audit manual task run")
			return exUnavailable
		}
		if err := sched.RunTaskByID(task.ID); err != nil {
			fmt.Fprintf(os.Stderr, "Task %s failed: %v\n", task.ID, err)
			return exUnavailable
		}
		if _, err := fmt.Fprintf(os.Stdout, "Task %s completed successfully\n", task.ID); err != nil {
			return exUnavailable
		}
		return 0

	case "enable", "disable":
		if len(args) != 2 {
			return schedulerUsage(binaryName)
		}
		task, err := sched.ShowTask(args[1])
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return exUnavailable
		}
		enabled := strings.EqualFold(args[0], "enable")
		if task.Enabled == enabled {
			state := "disabled"
			if enabled {
				state = "enabled"
			}
			if _, err := fmt.Fprintf(os.Stdout, "Task %s is already %s\n", task.ID, state); err != nil {
				return exUnavailable
			}
			return 0
		}
		if !enabled && !task.Skippable {
			fmt.Fprintf(os.Stderr, "Task %s is required and cannot be disabled\n", task.ID)
			return exUsage
		}
		if cfg.Server.Scheduler.Tasks == nil {
			cfg.Server.Scheduler.Tasks = make(map[string]config.SchedulerTaskConfig)
		}
		taskConfig := cfg.Server.Scheduler.Tasks[task.ID]
		taskConfig.Enabled = config.NewOptionalBool(enabled)
		cfg.Server.Scheduler.Tasks[task.ID] = taskConfig
		if err := config.Save(cfg); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to save scheduler configuration: %v\n", err)
			return exUnavailable
		}
		if err := sched.SetTaskEnabled(task.ID, enabled); err != nil {
			fmt.Fprintln(os.Stderr, err)
			if errors.Is(err, scheduler.ErrTaskRequired) {
				return exUsage
			}
			return exUnavailable
		}
		event := "scheduler.task_disabled"
		if enabled {
			event = "scheduler.task_enabled"
		}
		if err := database.WriteAuditEventWithSource(ctx, event, "operator", "", "cli", map[string]any{
			"task_name": task.Name, "task_id": task.ID, "source": "cli", "old_enabled": task.Enabled, "new_enabled": enabled,
		}); err != nil {
			fmt.Fprintln(os.Stderr, "Task state changed, but its audit event failed")
			return exUnavailable
		}
		if _, err := fmt.Fprintf(os.Stdout, "Task %s %s\n", task.ID, args[0]); err != nil {
			return exUnavailable
		}
		return 0

	default:
		return schedulerUsage(binaryName)
	}
}

func schedulerUsage(binaryName string) int {
	fmt.Fprintf(os.Stderr, "Usage: %s --maintenance scheduler {list|show <id>|run <id>|enable <id>|disable <id>|history <id>}\n", binaryName)
	return exUsage
}

func printSchedulerHelp(binaryName string) int {
	if _, err := fmt.Fprint(os.Stdout, i18n.TranslateFormat(appmode.GetLang(), "cli.scheduler_help", map[string]string{"binary_name": binaryName})); err != nil {
		return exUnavailable
	}
	return 0
}
