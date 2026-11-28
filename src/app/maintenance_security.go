package app

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/apimgr/api/src/config"
	"github.com/apimgr/api/src/database"
	"github.com/apimgr/api/src/paths"
	"github.com/apimgr/api/src/server"
)

func handleSecurityMaintenance(args []string, binaryName string) {
	if len(args) == 0 {
		printSecurityMaintenanceHelp(binaryName)
		os.Exit(exUsage)
	}
	cfg, err := config.Load(nil)
	if err != nil {
		cprintf("❌ Failed to load configuration: %v\n", err)
		os.Exit(exConfig)
	}
	actor, err := authorizeSensitiveOperation(cfg)
	if err != nil {
		cprintf("❌ %v\n", err)
		os.Exit(exUsage)
	}
	if err := database.InitDatabase(cfg.Server.Database, paths.DataDir()); err != nil {
		cprintf("❌ Failed to initialize database: %v\n", err)
		os.Exit(exUnavailable)
	}
	defer func() { _ = database.Close() }()
	if err := server.InitLogger(&cfg.Server.Logs); err != nil {
		cprintf("❌ Failed to initialize security log: %v\n", err)
		os.Exit(exUnavailable)
	}

	ctx := context.Background()
	switch strings.ToLower(args[0]) {
	case "list":
		status, err := parseSecurityListArgs(args[1:])
		if err != nil {
			cprintf("❌ %v\n", err)
			os.Exit(exUsage)
		}
		reports, err := database.ListSecurityReports(ctx, status)
		if err != nil {
			cprintf("❌ Failed to list security reports: %v\n", err)
			os.Exit(exUnavailable)
		}
		cprintf("%-23s %-12s %-15s %-24s %-20s %-20s\n", "TRACKING ID", "STATUS", "SEVERITY", "COMPONENT", "CREATED", "UPDATED")
		for _, report := range reports {
			cprintf("%-23s %-12s %-15s %-24s %-20s %-20s\n", report.TrackingID, report.Status, report.Severity, report.Component, report.CreatedAt, report.UpdatedAt)
		}
	case "show":
		if len(args) != 2 {
			cprintf("❌ Usage: %s --maintenance security show TRACKING_ID\n", binaryName)
			os.Exit(exUsage)
		}
		report, err := database.GetSecurityReport(ctx, args[1])
		if err != nil {
			cprintln("❌ Security report not found")
			os.Exit(exUnavailable)
		}
		cprintf("Tracking ID: %s\nStatus: %s\nSeverity: %s\nComponent: %s\nCredit: %s\nCVE requested: %t\nCreated: %s\nUpdated: %s\nClosed: %s\nResearcher comment: %s\nDisclosure date: %s\n",
			report.TrackingID, report.Status, report.Severity, report.Component, report.CreditPref, report.CVERequested, report.CreatedAt, report.UpdatedAt, report.ClosedAt, report.ResearcherComment, report.DisclosureDate)
		cprintln("Report content is not displayed. Read the encrypted maintainer notification or export its ciphertext.")
	case "export":
		if len(args) != 2 && len(args) != 4 {
			cprintf("❌ Usage: %s --maintenance security export TRACKING_ID [--output FILE]\n", binaryName)
			os.Exit(exUsage)
		}
		output := args[1] + ".asc"
		if len(args) == 4 {
			if args[2] != "--output" || strings.TrimSpace(args[3]) == "" {
				cprintln("❌ Expected --output FILE")
				os.Exit(exUsage)
			}
			output = args[3]
		}
		body, algorithm, err := database.GetSecurityReportEncryptedBody(ctx, args[1])
		if err != nil {
			cprintln("❌ Security report not found")
			os.Exit(exUnavailable)
		}
		if output == "-" {
			if _, err := os.Stdout.Write(body); err != nil {
				cprintf("❌ Failed to write encrypted report: %v\n", err)
				os.Exit(exUnavailable)
			}
		} else {
			file, err := os.OpenFile(output, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
			if err != nil {
				cprintf("❌ Failed to create output file: %v\n", err)
				os.Exit(exUnavailable)
			}
			if _, err = file.Write(body); err == nil {
				err = file.Sync()
			}
			closeErr := file.Close()
			if err == nil {
				err = closeErr
			}
			if err != nil {
				_ = os.Remove(output)
				cprintf("❌ Failed to write encrypted report: %v\n", err)
				os.Exit(exUnavailable)
			}
		}
		if logger := server.GetLogger(); logger != nil {
			logger.LogSecurity("security.report_exported", "local", map[string]interface{}{"tracking_id": args[1], "algorithm": algorithm, "actor": actor})
		}
		if output == "-" {
			return
		}
		cprintf("✅ Encrypted security report exported to %s\n", output)
	case "update":
		if len(args) < 2 {
			cprintf("❌ Usage: %s --maintenance security update TRACKING_ID [--status STATUS] [--comment TEXT] [--disclosure-date YYYY-MM-DD] [--severity LEVEL]\n", binaryName)
			os.Exit(exUsage)
		}
		update, err := parseSecurityUpdateArgs(args[2:])
		if err != nil {
			cprintf("❌ %v\n", err)
			os.Exit(exUsage)
		}
		if update.Status == "" && update.Comment == "" && update.DisclosureDate == "" && update.Severity == "" {
			cprintln("❌ At least one update field is required")
			os.Exit(exUsage)
		}
		before, err := database.GetSecurityReport(ctx, args[1])
		if err != nil {
			cprintln("❌ Security report not found")
			os.Exit(exUnavailable)
		}
		after, err := database.UpdateSecurityReport(ctx, args[1], update, time.Now().UTC())
		if err != nil {
			cprintf("❌ Failed to update security report: %v\n", err)
			os.Exit(exUsage)
		}
		if logger := server.GetLogger(); logger != nil {
			logger.LogSecurity("security.report_updated", "local", map[string]interface{}{
				"tracking_id": args[1], "actor": actor, "old_status": before.Status, "new_status": after.Status,
			})
		}
		if before.Status != after.Status {
			if sent, notifyErr := server.SendSecurityStatusNotification(cfg, ctx, after.TrackingID, after); notifyErr != nil {
				cprintf("⚠️ Status saved, but researcher notification failed: %v\n", notifyErr)
			} else if sent {
				cprintln("📧 Researcher status notification sent")
			}
		}
		cprintf("✅ Security report %s updated (%s → %s)\n", after.TrackingID, before.Status, after.Status)
	default:
		printSecurityMaintenanceHelp(binaryName)
		os.Exit(exUsage)
	}
}

func parseSecurityListArgs(args []string) (string, error) {
	status := ""
	for i := 0; i < len(args); i++ {
		if args[i] != "--status" || i+1 >= len(args) || status != "" {
			return "", fmt.Errorf("unexpected or incomplete list option %q", args[i])
		}
		status = strings.ToLower(args[i+1])
		i++
	}
	return status, nil
}

func parseSecurityUpdateArgs(args []string) (database.SecurityReportUpdate, error) {
	var update database.SecurityReportUpdate
	for i := 0; i < len(args); i++ {
		if i+1 >= len(args) {
			return update, fmt.Errorf("missing value for %s", args[i])
		}
		value := args[i+1]
		switch args[i] {
		case "--status":
			update.Status = strings.ToLower(value)
		case "--comment":
			update.Comment = value
		case "--disclosure-date":
			if _, err := time.Parse("2006-01-02", value); err != nil {
				return update, fmt.Errorf("disclosure date must use YYYY-MM-DD")
			}
			update.DisclosureDate = value
		case "--severity":
			update.Severity = strings.ToLower(value)
		default:
			return update, fmt.Errorf("unknown option %q", args[i])
		}
		i++
	}
	return update, nil
}

func printSecurityMaintenanceHelp(binaryName string) {
	cprintf("Security report maintenance\n\n  %s --maintenance security list [--status STATUS]\n  %s --maintenance security show TRACKING_ID\n  %s --maintenance security export TRACKING_ID [--output FILE]\n  %s --maintenance security update TRACKING_ID [--status STATUS] [--comment TEXT] [--disclosure-date YYYY-MM-DD] [--severity LEVEL]\n",
		binaryName, binaryName, binaryName, binaryName)
}
