package app

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
func handleBackup(name string, binaryName string, includeSSL, includeData bool) {
	cfg, err := config.Load(nil)
	if err != nil {
		cprintf("❌ Failed to load configuration: %v\n", err)
		os.Exit(exConfig)
	}
	if cfg.Server.Backup.EncryptionPasswordUnreadable {
		cprintln("❌ Configured backup encryption password cannot be decrypted; restore server.security.encryption_key before creating backups")
		os.Exit(1)
	}

	if err := database.InitDatabase(cfg.Server.Database, paths.DataDir()); err != nil {
		cprintf("❌ Failed to initialize database: %v\n", err)
		os.Exit(exUnavailable)
	}
	defer func() { _ = database.Close() }()

	password, err := resolveBackupPassword(cfg, "Enter backup password: ")
	if err != nil {
		cprintf("❌ %v\n", err)
		os.Exit(1)
	}

	manager, err := cliBackupManager(cfg, password, includeSSL, includeData)
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
	cfg, err := config.Load(nil)
	if err != nil {
		cprintf("❌ Failed to load configuration: %v\n", err)
		os.Exit(exConfig)
	}

	// The first-run check reads the database location before Init, since Init
	// itself creates an empty server.db and would erase the distinction.
	firstRun := databaseIsEmpty(cfg)

	if err := database.InitDatabase(cfg.Server.Database, paths.DataDir()); err != nil {
		cprintf("❌ Failed to initialize database: %v\n", err)
		os.Exit(exUnavailable)
	}
	defer func() { _ = database.Close() }()

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

	manager, err := cliBackupManager(cfg, password, false, false)
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

// handleBackupTest verifies an archive without touching live database or
// configuration data, including the backed-up security PGP keypair.
func handleBackupTest(args []string, binaryName string) {
	if len(args) > 1 {
		cprintf("❌ Usage: %s --maintenance backup test [file]\n", binaryName)
		os.Exit(exUsage)
	}
	backupDir := paths.BackupDir()
	files, err := backup.ListBackups(backupDir)
	if err != nil {
		cprintf("❌ Failed to list backups: %v\n", err)
		os.Exit(exUnavailable)
	}
	password := ""
	if len(args) == 1 {
		if backup.IsEncryptedName(args[0]) {
			password, err = promptPassword("Enter backup password: ")
			if err != nil {
				cprintf("❌ %v\n", err)
				os.Exit(exUsage)
			}
		}
		if err := verifyBackupWithBase(args[0], files, password); err != nil {
			cprintf("❌ Backup test failed: %v\n", err)
			os.Exit(1)
		}
		cprintln("✅ Backup verified: archive integrity, checksum, database, and PGP keypair (when present)")
		return
	}
	if len(files) == 0 {
		cprintln("❌ No backup archives were found")
		os.Exit(exUnavailable)
	}
	for _, file := range files {
		if file.Encrypted {
			password, err = promptPassword("Enter backup password: ")
			if err != nil {
				cprintf("❌ %v\n", err)
				os.Exit(exUsage)
			}
			break
		}
	}
	var newest backup.File
	var newestManifest *backup.Manifest
	for _, candidate := range files {
		manifest, verifyErr := backup.Verify(candidate.Path, password)
		if verifyErr != nil {
			cprintf("❌ Cannot select newest backup; %s failed verification: %v\n", candidate.Name, verifyErr)
			os.Exit(1)
		}
		if newestManifest == nil || manifest.CreatedAt.After(newestManifest.CreatedAt) {
			newest = candidate
			newestManifest = manifest
		}
	}
	if err := verifyBackupWithBase(newest.Path, files, password); err != nil {
		cprintf("❌ Backup test failed for %s: %v\n", newest.Name, err)
		os.Exit(1)
	}
	cprintf("✅ Newest backup verified (%s, snapshot %s): archive integrity, checksum, database, and PGP keypair (when present)\n",
		newest.Name, newestManifest.CreatedAt.UTC().Format(time.RFC3339))
}

func verifyBackupWithBase(path string, files []backup.File, password string) error {
	manifest, err := backup.Verify(path, password)
	if err != nil {
		return err
	}
	if manifest.BaseBackup == "" {
		return nil
	}
	for _, candidate := range files {
		if candidate.Name == manifest.BaseBackup {
			if _, err := backup.Verify(candidate.Path, password); err != nil {
				return fmt.Errorf("incremental base %s failed verification: %w", candidate.Name, err)
			}
			return nil
		}
	}
	return fmt.Errorf("incremental base %s is missing from the backup directory", manifest.BaseBackup)
}

// restoreCaller assembles the identity evidence PART 21's authorization matrix
// evaluates. Root is asked to confirm; every other non-first-run caller is
// asked for the operator token, which is compared in constant time inside the
// backup package.
func restoreCaller(cfg *config.ServerConfig, restorePath string, firstRun bool) (backup.Caller, error) {
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
func cliBackupManager(cfg *config.ServerConfig, password string, includeSSL, includeData bool) (*backup.Manager, error) {
	sources, supplementalSources, err := backup.SourcesWithConfig(cfg, paths.ConfigDir(), paths.DataDir(), includeSSL, includeData)
	if err != nil {
		return nil, fmt.Errorf("resolve backup sources: %w", err)
	}
	var sqliteSourcePath string
	var snapshotSQLite func(context.Context, string) error
	if strings.EqualFold(cfg.Server.Database.Driver, "sqlite") || strings.EqualFold(cfg.Server.Database.Driver, "sqlite3") || strings.EqualFold(cfg.Server.Database.Driver, "sqlite2") {
		sqliteSourcePath = filepath.Join(paths.GetDatabaseDir(paths.DataDir()), "server.db")
		snapshotSQLite = database.SnapshotServerSQLite
	}
	restoreLimits, err := backupRestoreLimits(cfg.Server.Restore)
	if err != nil {
		return nil, fmt.Errorf("invalid server.restore configuration: %w", err)
	}

	backupDir := paths.BackupDir()
	if err := os.MkdirAll(backupDir, 0o750); err != nil {
		return nil, fmt.Errorf("failed to create %s: %w", backupDir, err)
	}

	return backup.NewManager(backup.BackupManagerOptions{
		Dir:                                backupDir,
		Sources:                            sources,
		SupplementalSources:                supplementalSources,
		SQLiteSourcePath:                   sqliteSourcePath,
		SnapshotSQLite:                     snapshotSQLite,
		ExportAppSecrets:                   database.ExportAppSecretsSnapshot,
		ExportAppSecretsFromSQLiteSnapshot: database.ExportAppSecretsFromSQLiteSnapshot,
		RestoreAppSecrets: func(ctx context.Context, data []byte) error {
			remote := cfg.Server.Database.Driver == "libsql" || cfg.Server.Database.Driver == "turso"
			if !remote {
				if err := database.Close(); err != nil {
					return fmt.Errorf("close pre-restore database: %w", err)
				}
				if err := database.InitDatabase(cfg.Server.Database, paths.DataDir()); err != nil {
					return fmt.Errorf("open restored database: %w", err)
				}
			}
			return database.RestoreAppSecretsSnapshot(ctx, data, remote)
		},
		RollbackAppSecrets: func(context.Context) error {
			remote := cfg.Server.Database.Driver == "libsql" || cfg.Server.Database.Driver == "turso"
			if remote {
				return nil
			}
			_ = database.Close()
			return database.InitDatabase(cfg.Server.Database, paths.DataDir())
		},
		RestoreRoots:      backup.RestoreRoots(paths.ConfigDir(), paths.DataDir(), includeSSL, includeData),
		Password:          password,
		Retention:         cfg.Server.Backup.Retention,
		RestoreLimits:     restoreLimits,
		ComplianceEnabled: cfg.Server.Compliance.AnyEnabled(),
		AppVersion:        Version,
		Audit:             cliBackupAudit(),
	}), nil
}

func backupRestoreLimits(cfg config.RestoreConfig) (backup.RestoreLimits, error) {
	maxArchiveSize, err := config.ParseByteSize(cfg.MaxArchiveSize)
	if err != nil {
		return backup.RestoreLimits{}, err
	}
	maxExpandedSize, err := config.ParseByteSize(cfg.MaxExpandedSize)
	if err != nil {
		return backup.RestoreLimits{}, err
	}
	maxFileSize, err := config.ParseByteSize(cfg.MaxFileSize)
	if err != nil {
		return backup.RestoreLimits{}, err
	}
	return backup.NormalizeRestoreLimits(backup.RestoreLimits{
		MaxArchiveSize:  maxArchiveSize,
		MaxExpandedSize: maxExpandedSize,
		MaxFiles:        cfg.MaxFiles,
		MaxFileSize:     maxFileSize,
		MaxRatio:        cfg.MaxRatio,
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
func resolveBackupPassword(cfg *config.ServerConfig, prompt string) (string, error) {
	if cfg != nil && cfg.Server.Backup.EncryptionPasswordUnreadable {
		return "", config.ErrBackupPasswordUnreadable
	}
	if cfg.Server.Backup.EncryptionPassword != "" {
		return cfg.Server.Backup.EncryptionPassword, nil
	}
	if !cfg.Server.Backup.Encryption.Enabled && !cfg.Server.Compliance.AnyEnabled() {
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
func databaseIsEmpty(cfg *config.ServerConfig) bool {
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
