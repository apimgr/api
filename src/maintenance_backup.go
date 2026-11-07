package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"time"

	"github.com/apimgr/api/src/backup"
	"github.com/apimgr/api/src/config"
	"github.com/apimgr/api/src/database"
	"github.com/apimgr/api/src/paths"
	"github.com/apimgr/api/src/sysservice"
	"golang.org/x/term"
)

// handleBackup implements `--maintenance backup [filename]` per AI.md PART 21.
// It drives backup.Manager so the archive format, manifest, SHA-256 checksum,
// post-creation verification, retention sweep and audit events are identical
// to the scheduled run.
func handleBackup(name string, binaryName string) {
	cfg, err := config.Load()
	if err != nil {
		cprintf("❌ Failed to load configuration: %v\n", err)
		os.Exit(exConfig)
	}

	if err := database.Init(cfg.Server.Database, paths.DataDir()); err != nil {
		cprintf("❌ Failed to initialize database: %v\n", err)
		os.Exit(exUnavailable)
	}
	defer database.Close()

	password, err := resolveBackupPassword(cfg, "Enter backup password: ")
	if err != nil {
		cprintf("❌ %v\n", err)
		os.Exit(1)
	}

	manager, err := cliBackupManager(cfg, password)
	if err != nil {
		cprintf("❌ %v\n", err)
		os.Exit(1)
	}

	cprintf("📦 Creating backup in: %s\n", manager.Dir())

	path, err := manager.CreateManual(name, time.Now())
	if err != nil {
		if errors.Is(err, backup.ErrEncryptionRequired) {
			cprintln("❌ Compliance mode requires an encrypted backup; set server.backup.encryption_password first")
			os.Exit(1)
		}
		cprintf("❌ Backup failed: %v\n", err)
		os.Exit(1)
	}

	cprintln("✅ Backup created and verified")
	cprintf("   Archive: %s\n", path)
	cprintf("   Encrypted: %t\n", manager.Encrypted())

	if _, err := manager.Cleanup(); err != nil {
		cprintf("⚠️ Retention sweep failed: %v\n", err)
	}
}

// handleRestore implements `--maintenance restore <backup-file>` per AI.md
// PART 21. It applies the restore authorization matrix, prompts for the backup
// password when the archive is encrypted (never a flag), and only unpacks an
// archive that passes every verification check.
func handleRestore(restorePath string, binaryName string) {
	cfg, err := config.Load()
	if err != nil {
		cprintf("❌ Failed to load configuration: %v\n", err)
		os.Exit(exConfig)
	}

	// The first-run check reads the database location before Init, since Init
	// itself creates an empty server.db and would erase the distinction.
	firstRun := databaseIsEmpty(cfg)

	if err := database.Init(cfg.Server.Database, paths.DataDir()); err != nil {
		cprintf("❌ Failed to initialize database: %v\n", err)
		os.Exit(exUnavailable)
	}
	defer database.Close()

	caller, err := restoreCaller(cfg, restorePath, firstRun)
	if err != nil {
		cprintf("❌ %v\n", err)
		os.Exit(1)
	}

	password := ""
	if backup.RequiresPassword(restorePath) {
		password, err = promptPassword("Enter backup password: ")
		if err != nil {
			cprintf("❌ %v\n", err)
			os.Exit(1)
		}
	}

	manager, err := cliBackupManager(cfg, password)
	if err != nil {
		cprintf("❌ %v\n", err)
		os.Exit(1)
	}

	cprintf("📥 Restoring from: %s\n", restorePath)
	cprintln("   Verifying backup integrity...")

	if err := manager.Restore(caller, restorePath, password); err != nil {
		switch {
		case errors.Is(err, backup.ErrRestoreDenied), errors.Is(err, backup.ErrOperatorTokenUnset):
			cprintf("❌ %v\n", err)
			os.Exit(exUsage)
		case errors.Is(err, backup.ErrRestoreNotConfirmed):
			cprintln("Aborted; nothing was restored.")
			os.Exit(0)
		default:
			cprintf("❌ Restore failed: %v\n", err)
			os.Exit(1)
		}
	}

	cprintln("✅ Restore completed")
	cprintln("   Restart the service to apply changes")
}

// restoreCaller assembles the identity evidence PART 21's authorization matrix
// evaluates. Root is asked to confirm; every other non-first-run caller is
// asked for the operator token, which is compared in constant time inside the
// backup package.
func restoreCaller(cfg *config.Config, restorePath string, firstRun bool) (backup.Caller, error) {
	caller := backup.Caller{
		FirstRun:      firstRun,
		Elevated:      paths.IsElevated(),
		ExpectedToken: cfg.Server.Token,
	}

	if caller.FirstRun {
		return caller, nil
	}

	if caller.Elevated {
		caller.Confirmed = confirmRestore(restorePath)
		return caller, nil
	}
	current, err := user.Current()
	if err != nil {
		return caller, fmt.Errorf("cannot verify restore caller: %w", err)
	}
	service, err := user.Lookup(sysservice.ServiceUserName)
	if err != nil || current.Uid != service.Uid {
		return caller, backup.ErrRestoreDenied
	}

	if cfg.Server.Token == "" {
		return caller, backup.ErrOperatorTokenUnset
	}

	token, err := promptPassword("Operator token: ")
	if err != nil {
		return caller, err
	}
	caller.SuppliedToken = token

	if !confirmRestore(restorePath) {
		return caller, fmt.Errorf("%w", backup.ErrRestoreNotConfirmed)
	}
	return caller, nil
}

// confirmRestore requires an explicit yes before a destructive restore.
func confirmRestore(restorePath string) bool {
	cprintf("Restoring %s overwrites the current configuration and database. Continue? [y/N]: ",
		filepath.Base(restorePath))
	var answer string
	if _, err := fmt.Scanln(&answer); err != nil {
		return false
	}
	answer = strings.ToLower(strings.TrimSpace(answer))
	return answer == "y" || answer == "yes"
}

// cliBackupManager builds the Manager the maintenance commands drive. The
// backup directory is resolved once here and cached by the Manager, matching
// the scheduler's wiring so both paths write and prune the same directory.
func cliBackupManager(cfg *config.Config, password string) (*backup.Manager, error) {
	sources := []string{
		filepath.Join(paths.DataDir(), "db"),
		filepath.Join(paths.ConfigDir(), "server.yml"),
	}
	for _, optional := range []string{"template", "theme"} {
		path := filepath.Join(paths.ConfigDir(), optional)
		if _, err := os.Stat(path); err == nil {
			sources = append(sources, path)
		}
	}

	backupDir := paths.BackupDir()
	if err := os.MkdirAll(backupDir, 0o750); err != nil {
		return nil, fmt.Errorf("failed to create %s: %w", backupDir, err)
	}

	return backup.NewManager(backup.Options{
		Dir:               backupDir,
		Sources:           sources,
		Password:          password,
		Retention:         cfg.Server.Backup.Retention,
		ComplianceEnabled: cfg.Server.Compliance.Enabled,
		AppVersion:        Version,
		Audit:             cliBackupAudit(),
	}), nil
}

// cliBackupAudit records every PART 21 backup audit event to audit_log with
// the invoking identity as the actor.
func cliBackupAudit() backup.AuditFunc {
	actor := cliActor()
	return func(event string, details map[string]interface{}) {
		payload := make(map[string]any, len(details)+1)
		for key, value := range details {
			payload[key] = value
		}
		payload["source"] = "maintenance_cli"
		if err := database.WriteAuditEvent(context.Background(), event, actor, "", payload); err != nil {
			cprintf("⚠️ Audit entry for %s failed: %v\n", event, err)
		}
	}
}

// cliActor names the identity running the maintenance command.
func cliActor() string {
	if paths.IsElevated() {
		return "root"
	}
	if current, err := user.Current(); err == nil && current.Username != "" {
		return current.Username
	}
	return "unknown"
}

// resolveBackupPassword returns the configured encryption password, prompting
// interactively when encryption is required but no password is stored. PART 21
// forbids a password CLI flag, so the prompt is the only interactive source.
func resolveBackupPassword(cfg *config.Config, prompt string) (string, error) {
	if cfg.Server.Backup.EncryptionPassword != "" {
		return cfg.Server.Backup.EncryptionPassword, nil
	}
	if !cfg.Server.Backup.Encryption.Enabled && !cfg.Server.Compliance.Enabled {
		return "", nil
	}
	return promptPassword(prompt)
}

// promptPassword reads a secret from the terminal without echoing it.
func promptPassword(prompt string) (string, error) {
	cprintf("%s", prompt)
	entered, err := term.ReadPassword(int(os.Stdin.Fd()))
	cprintln("")
	if err != nil {
		return "", fmt.Errorf("failed to read the password: %w", err)
	}
	return string(entered), nil
}

// databaseIsEmpty reports the PART 21 "database empty (first-run)" condition,
// which authorizes a restore with no further credential. Only a local SQLite
// database can be judged from disk; a remote database is never assumed empty.
func databaseIsEmpty(cfg *config.Config) bool {
	driver := strings.ToLower(strings.TrimSpace(cfg.Server.Database.Driver))
	switch driver {
	case "", "sqlite", "sqlite2", "sqlite3":
	default:
		return false
	}

	info, err := os.Stat(filepath.Join(paths.GetDatabaseDir(paths.DataDir()), "server.db"))
	if err != nil {
		return os.IsNotExist(err)
	}
	return info.Size() == 0
}
