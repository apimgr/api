package app

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/apimgr/api/src/backup"
	"github.com/apimgr/api/src/config"
	"github.com/apimgr/api/src/database"
	"github.com/apimgr/api/src/paths"
	"github.com/apimgr/api/src/server"
)

func handleComplianceMaintenance(command string, args []string, binaryName string) int {
	if len(args) == 0 {
		printDataComplianceHelp(binaryName)
		return exUsage
	}
	if strings.EqualFold(args[0], "help") || args[0] == "--help" {
		printDataComplianceHelp(binaryName)
		return 0
	}
	if (strings.EqualFold(args[0], "export") || strings.EqualFold(args[0], "delete") || strings.EqualFold(args[0], "report")) && len(args) == 2 && strings.EqualFold(args[1], "--help") {
		printDataComplianceHelp(binaryName)
		return 0
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
	auditConfig := cfg.Server.Logs.Audit
	auditConfig.Keep = cfg.Server.Compliance.EffectiveAuditRetention(auditConfig.Keep)
	if err := database.ConfigureAuditFile(auditConfig); err != nil {
		fmt.Fprintln(os.Stderr, "Failed to configure the audit log")
		return exUnavailable
	}
	if err := server.InitLogger(&cfg.Server.Logs); err != nil {
		fmt.Fprintln(os.Stderr, "Failed to configure logging")
		return exUnavailable
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	switch strings.ToLower(command) {
	case "data":
		if strings.EqualFold(args[0], "--help") || strings.EqualFold(args[0], "help") {
			printDataComplianceHelp(binaryName)
			return 0
		}
		switch strings.ToLower(args[0]) {
		case "export":
			if len(args) == 2 && strings.EqualFold(args[1], "--help") {
				printDataComplianceHelp(binaryName)
				return 0
			}
			return handleDataExport(ctx, args[1:], binaryName)
		case "delete":
			if len(args) == 2 && strings.EqualFold(args[1], "--help") {
				printDataComplianceHelp(binaryName)
				return 0
			}
			return handleDataDelete(ctx, args[1:], binaryName)
		default:
			printDataComplianceHelp(binaryName)
			return exUsage
		}
	case "compliance":
		if strings.EqualFold(args[0], "--help") || strings.EqualFold(args[0], "help") {
			printDataComplianceHelp(binaryName)
			return 0
		}
		if len(args) == 2 && strings.EqualFold(args[1], "--help") {
			printDataComplianceHelp(binaryName)
			return 0
		}
		if strings.ToLower(args[0]) != "report" || len(args) != 1 {
			printDataComplianceHelp(binaryName)
			return exUsage
		}
		return printComplianceReport(ctx, cfg)
	}
	return 0
}

func parseOwnerCommandArgs(args []string, allowOutput, allowYes bool) (string, string, bool, error) {
	prefix, output := "", ""
	yes := false
	for index := 0; index < len(args); index++ {
		switch args[index] {
		case "--owner":
			if prefix != "" || index+1 == len(args) || strings.HasPrefix(args[index+1], "--") {
				return "", "", false, errors.New("--owner requires one token prefix")
			}
			prefix = strings.TrimSpace(args[index+1])
			index++
		case "--output":
			if !allowOutput || output != "" || index+1 == len(args) {
				return "", "", false, errors.New("invalid --output option")
			}
			output = args[index+1]
			index++
		case "--yes":
			if !allowYes || yes {
				return "", "", false, errors.New("unexpected --yes option")
			}
			yes = true
		default:
			return "", "", false, fmt.Errorf("unexpected argument %q", args[index])
		}
	}
	if prefix == "" {
		return "", "", false, errors.New("--owner is required")
	}
	return prefix, output, yes, nil
}

func handleDataExport(ctx context.Context, args []string, binaryName string) int {
	prefix, output, _, err := parseOwnerCommandArgs(args, true, false)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Usage: %s --maintenance data export --owner <prefix> [--output FILE]\n", binaryName)
		return exUsage
	}
	db := database.GetServerDB()
	data, err := database.ExportOwnerData(ctx, db, prefix)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Owner token prefix was not found or could not be resolved")
		return exUnavailable
	}
	requestID, err := newComplianceRequestID()
	if err != nil {
		fmt.Fprintln(os.Stderr, "Could not create request identifier")
		return exUnavailable
	}
	if err := database.WriteAuditEventWithSource(ctx, "compliance.data_export_requested", "operator", "", "cli", map[string]any{"owner_id": data.Owner.ID, "request_id": requestID}); err != nil {
		fmt.Fprintln(os.Stderr, "Could not record the export request")
		return exUnavailable
	}
	encoded, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		fmt.Fprintln(os.Stderr, "Could not encode export data")
		return exUnavailable
	}
	if output == "-" {
		if _, err := os.Stdout.Write(append(encoded, '\n')); err != nil {
			fmt.Fprintln(os.Stderr, "Could not write export data")
			return exUnavailable
		}
	} else {
		if output == "" {
			output = fmt.Sprintf("%s-export-%s-%s.json", paths.ProjectName, data.Owner.ID, time.Now().UTC().Format("20060102"))
		}
		file, err := os.OpenFile(filepath.Clean(output), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			fmt.Fprintln(os.Stderr, "Could not create export file")
			return exCantCreat
		}
		if _, err = file.Write(append(encoded, '\n')); err == nil {
			err = file.Sync()
		}
		closeErr := file.Close()
		if err == nil {
			err = closeErr
		}
		if err != nil {
			_ = os.Remove(output)
			fmt.Fprintln(os.Stderr, "Could not write export file")
			return exUnavailable
		}
	}
	if err := database.WriteAuditEventWithSource(ctx, "compliance.data_export_completed", "operator", "", "cli", map[string]any{"owner_id": data.Owner.ID, "request_id": requestID, "file_size": len(encoded) + 1}); err != nil {
		fmt.Fprintln(os.Stderr, "Export completed, but the completion event could not be recorded")
		return exUnavailable
	}
	if output != "-" {
		cprintf("Export written: %s\n", output)
	}
	return 0
}

func handleDataDelete(ctx context.Context, args []string, binaryName string) int {
	prefix, _, yes, err := parseOwnerCommandArgs(args, false, true)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Usage: %s --maintenance data delete --owner <prefix> [--yes]\n", binaryName)
		return exUsage
	}
	data, err := database.ExportOwnerData(ctx, database.GetServerDB(), prefix)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Owner token prefix was not found or could not be resolved")
		return exUnavailable
	}
	if !yes && !confirmOwnerDeletion(data.Owner.ID) {
		cprintln("Deletion cancelled; no data was changed")
		return 0
	}
	requestID, err := newComplianceRequestID()
	if err != nil {
		fmt.Fprintln(os.Stderr, "Could not create request identifier")
		return exUnavailable
	}
	if err := database.WriteAuditEventWithSource(ctx, "compliance.data_deletion_requested", "operator", "", "cli", map[string]any{"owner_id": data.Owner.ID, "request_id": requestID}); err != nil {
		fmt.Fprintln(os.Stderr, "Could not record the deletion request")
		return exUnavailable
	}
	ownerID, deletedTokens, deletedResources, err := database.DeleteOwnerByTokenPrefix(ctx, database.GetServerDB(), prefix)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Owner data could not be deleted")
		return exUnavailable
	}
	if err := database.WriteAuditEventWithSource(ctx, "compliance.data_deletion_completed", "operator", "", "cli", map[string]any{
		"owner_id": ownerID, "request_id": requestID, "deleted_count": deletedTokens + deletedResources + 1, "anonymized_count": 0,
	}); err != nil {
		fmt.Fprintln(os.Stderr, "Deletion completed, but the completion event could not be recorded")
		return exUnavailable
	}
	cprintf("Deleted %d items; anonymized 0 items\n", deletedTokens+deletedResources+1)
	return 0
}

func confirmOwnerDeletion(ownerID string) bool {
	cprintf("Type owner id %s to confirm deletion: ", ownerID)
	var typed string
	if _, err := fmt.Scanln(&typed); err != nil {
		return false
	}
	return strings.TrimSpace(typed) == ownerID
}

func newComplianceRequestID() (string, error) {
	var bytes [16]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes[:]), nil
}

func printComplianceReport(ctx context.Context, cfg *config.ServerConfig) int {
	db := database.GetServerDB()
	summary, err := database.GetComplianceSummary(ctx, db, time.Now().UTC())
	if err != nil {
		fmt.Fprintln(os.Stderr, "Could not collect compliance summary")
		return exUnavailable
	}
	integrity, auditEntries, err := database.AuditIntegritySummary()
	if err != nil {
		fmt.Fprintln(os.Stderr, "Could not verify audit log integrity")
		return exUnavailable
	}
	backups, backupErr := backup.ListBackups(paths.BackupDir())
	if backupErr != nil {
		fmt.Fprintln(os.Stderr, "Could not list backup archives")
		return exUnavailable
	}
	lastBackup := "none"
	for _, item := range backups {
		if lastBackup == "none" || item.Date.After(parseReportTime(lastBackup)) {
			lastBackup = item.Date.UTC().Format(time.RFC3339)
		}
	}
	cprintln("Compliance report")
	cprintf("Standards: %s\n", complianceSettingsSummary(cfg.Server.Compliance))
	cprintf("Compliance mode: %t (backup encryption required: %t)\n", cfg.Server.Compliance.AnyEnabled(), cfg.Server.Compliance.AnyEnabled())
	cprintf("Retention periods: audit=%s (minimum=%s), security_reports=%dd, owner_idle_purge=%dd, backup=max %d archives / weekly=%d monthly=%d yearly=%d\n",
		cfg.Server.Compliance.EffectiveAuditRetention(cfg.Server.Logs.Audit.Keep), formatRetentionDays(cfg.Server.Compliance.MinimumAuditRetentionDays()),
		cfg.Server.Security.Reports.RetentionDays, cfg.Server.Tokens.IdlePurgeDays,
		cfg.Server.Backup.Retention.MaxBackups, cfg.Server.Backup.Retention.KeepWeekly,
		cfg.Server.Backup.Retention.KeepMonthly, cfg.Server.Backup.Retention.KeepYearly)
	cprintf("Owners: %d\nResources: %d\nActive tokens: %d\nOpen security reports: %d\n", summary.Owners, summary.Resources, summary.ActiveTokens, summary.OpenReports)
	cprintf("Compliance events (last 30 days): %d\nLast backup: %s\n", summary.EventsLast30Days, lastBackup)
	if strings.HasPrefix(integrity, "broken at ") {
		cprintf("Audit integrity: %s\n", integrity)
	} else {
		cprintf("Audit integrity: intact (%d entries)\n", auditEntries)
	}
	if err := database.WriteAuditEventWithSource(ctx, "compliance.report_generated", "operator", "", "cli", map[string]any{"report_type": "compliance", "generated_by": "operator"}); err != nil {
		fmt.Fprintln(os.Stderr, "Could not record report generation")
		return exUnavailable
	}
	return 0
}

func parseReportTime(value string) time.Time {
	parsed, _ := time.Parse(time.RFC3339, value)
	return parsed
}

func complianceSettingsSummary(cfg config.ComplianceConfig) string {
	values := make([]string, 0, 11)
	standards := []struct {
		name string
		on   bool
	}{
		{"gdpr", cfg.GDPR}, {"ccpa", cfg.CCPA}, {"hipaa", cfg.HIPAA}, {"soc2", cfg.SOC2},
		{"pci_dss", cfg.PCIDSS}, {"iso27001", cfg.ISO27001}, {"fedramp", cfg.FedRAMP}, {"lgpd", cfg.LGPD},
		{"pipeda", cfg.PIPEDA}, {"appi", cfg.APPI}, {"pdpa", cfg.PDPA},
	}
	for _, standard := range standards {
		values = append(values, fmt.Sprintf("%s=%t", standard.name, standard.on))
	}
	return strings.Join(values, ", ")
}

func formatRetentionDays(days int) string {
	if days == 0 {
		return "none"
	}
	return fmt.Sprintf("%dd", days)
}

func printDataComplianceHelp(binaryName string) {
	cprintf("Data and compliance maintenance\n\n  %s --maintenance data export --owner <prefix> [--output FILE]\n  %s --maintenance data delete --owner <prefix> [--yes]\n  %s --maintenance compliance report\n\nRequires root or the operator token (server.token).\n", binaryName, binaryName, binaryName)
}
