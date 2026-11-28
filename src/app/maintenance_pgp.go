package app

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"time"

	"github.com/apimgr/api/src/config"
	"github.com/apimgr/api/src/database"
	"github.com/apimgr/api/src/paths"
	"github.com/apimgr/api/src/pgp"
	"github.com/apimgr/api/src/server"
	"golang.org/x/term"
)

func handlePGPCommand(args []string, binaryName string) {
	if len(args) == 0 {
		printPGPHelp(binaryName)
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
	if err := database.EnsureAppSecrets(context.Background()); err != nil {
		cprintf("❌ Failed to initialize application secrets: %v\n", err)
		os.Exit(exUnavailable)
	}
	if err := server.InitLogger(&cfg.Server.Logs); err != nil {
		cprintf("❌ Failed to initialize audit log: %v\n", err)
		os.Exit(exUnavailable)
	}

	ctx := context.Background()
	switch strings.ToLower(args[0]) {
	case "generate":
		if len(args) != 1 {
			printPGPHelp(binaryName)
			os.Exit(exUsage)
		}
		metadata, err := pgp.Generate(ctx, cfg)
		if err != nil {
			pgpFail("generate keypair", err)
		}
		if err := database.WriteAuditEventWithSource(ctx, "security.pgp_generated", actor, "", "cli", map[string]any{"fingerprint": metadata.Fingerprint}); err != nil {
			cprintf("⚠️ Key generated but audit entry failed: %v\n", err)
		}
		if err := pgp.Publish(ctx, cfg); err != nil {
			cprintf("⚠️ Key generated; keyserver publishing will retry: %v\n", err)
		}
		cprintf("✅ Security key generated: %s (expires %s)\n", metadata.Fingerprint, metadata.ExpiresAt.Format(time.RFC3339))
	case "rotate":
		if len(args) != 1 {
			printPGPHelp(binaryName)
			os.Exit(exUsage)
		}
		if !confirmPGPAction("rotate") {
			cprintln("Aborted; no key was rotated.")
			return
		}
		metadata, err := pgp.Rotate(ctx, cfg)
		if err != nil {
			pgpFail("rotate keypair", err)
		}
		if err := database.WriteAuditEventWithSource(ctx, "security.pgp_rotated", actor, "", "cli", map[string]any{"fingerprint": metadata.Fingerprint}); err != nil {
			cprintf("⚠️ Key rotated but audit entry failed: %v\n", err)
		}
		if err := pgp.Publish(ctx, cfg); err != nil {
			cprintf("⚠️ Key rotated; keyserver publishing will retry: %v\n", err)
		}
		cprintf("✅ Security key rotated: %s (expires %s)\n", metadata.Fingerprint, metadata.ExpiresAt.Format(time.RFC3339))
	case "publish":
		if len(args) != 1 {
			printPGPHelp(binaryName)
			os.Exit(exUsage)
		}
		if err := pgp.Publish(ctx, cfg); err != nil {
			pgpFail("publish public key", err)
		}
		if err := database.WriteAuditEventWithSource(ctx, "security.pgp_published", actor, "", "cli", nil); err != nil {
			cprintf("⚠️ Publish succeeded but audit entry failed: %v\n", err)
		}
		cprintln("✅ Public key submitted to configured keyservers")
	case "export":
		handlePGPExport(ctx, args[1:], cfg, actor, binaryName)
	case "import":
		handlePGPImport(ctx, args[1:], cfg, actor, binaryName)
	case "delete":
		if len(args) != 1 {
			printPGPHelp(binaryName)
			os.Exit(exUsage)
		}
		cprintln("Deleting the key makes PGP-encrypted reports permanently unreadable.")
		if !confirmPGPAction("delete") {
			cprintln("Aborted; no key was deleted.")
			return
		}
		metadata, metadataErr := database.GetSecurityPGPMetadata(ctx)
		if err := pgp.Delete(ctx, cfg); err != nil {
			pgpFail("delete keypair", err)
		}
		if metadataErr == nil {
			if err := database.WriteAuditEventWithSource(ctx, "security.pgp_deleted", actor, "", "cli", map[string]any{"fingerprint": metadata.Fingerprint}); err != nil {
				cprintf("⚠️ Key deleted but audit entry failed: %v\n", err)
			}
		}
		cprintln("✅ Security key deleted; public-key publishing disabled")
	case "metadata":
		if len(args) != 1 {
			printPGPHelp(binaryName)
			os.Exit(exUsage)
		}
		metadata, err := database.GetSecurityPGPMetadata(ctx)
		if err != nil {
			pgpFail("read key metadata", err)
		}
		cprintf("Fingerprint: %s\nCreated: %s\nExpires: %s\nRevoked: %t\n",
			metadata.Fingerprint, metadata.CreatedAt.Format(time.RFC3339), metadata.ExpiresAt.Format(time.RFC3339), metadata.Revoked)
		if metadata.LastRotatedAt != nil {
			cprintf("Last rotated: %s\n", metadata.LastRotatedAt.Format(time.RFC3339))
		}
		for keyserver, published := range metadata.KeyserversPublished {
			cprintf("Published to %s: %s\n", keyserver, published)
		}
	default:
		cprintf("❌ Unknown PGP command: %s\n", args[0])
		printPGPHelp(binaryName)
		os.Exit(exUsage)
	}
}

func handlePGPExport(ctx context.Context, args []string, cfg *config.ServerConfig, actor, binaryName string) {
	if len(args) < 1 || len(args) > 2 {
		printPGPHelp(binaryName)
		os.Exit(exUsage)
	}
	switch strings.ToLower(args[0]) {
	case "public":
		data, err := pgp.ReadPublic()
		if err != nil {
			pgpFail("read public key", err)
		}
		if len(args) == 1 || args[1] == "-" {
			if _, err := os.Stdout.Write(data); err != nil {
				pgpFail("write public key", err)
			}
			return
		}
		if err := writePGPExport(args[1], data, 0644); err != nil {
			pgpFail("write public key", err)
		}
		cprintf("✅ Public key exported to %s\n", args[1])
	case "private":
		if len(args) != 2 || strings.TrimSpace(args[1]) == "" || args[1] == "-" {
			cprintf("❌ Usage: %s --maintenance pgp export private <path>\n", binaryName)
			os.Exit(exUsage)
		}
		confirmedActor, err := authorizeSensitiveOperation(cfg)
		if err != nil {
			cprintf("❌ Second operator authorization failed: %v\n", err)
			os.Exit(exUsage)
		}
		if confirmedActor != actor {
			cprintln("❌ Operator identity changed during private-key export confirmation")
			os.Exit(exUsage)
		}
		if !confirmPGPAction("export-private") {
			cprintln("Aborted; no private key was exported.")
			return
		}
		if err := enforcePGPExportRateLimit(ctx, actor); err != nil {
			cprintf("❌ %v\n", err)
			os.Exit(exUnavailable)
		}
		reason, err := readPGPReason()
		if err != nil {
			pgpFail("read export reason", err)
		}
		private, err := pgp.ExportPrivate(ctx)
		if err != nil {
			pgpFail("decrypt private key", err)
		}
		if err := writePGPExport(args[1], []byte(private), 0600); err != nil {
			pgpFail("write private key", err)
		}
		if err := database.WriteAuditEventWithSource(ctx, "security.private_key_exported", actor, localOperatorIP(), "cli", map[string]any{"reason": reason, "path": args[1]}); err != nil {
			cprintf("⚠️ Private key exported but audit entry failed: %v\n", err)
		}
		cprintf("✅ Private key exported to %s\n", args[1])
	default:
		printPGPHelp(binaryName)
		os.Exit(exUsage)
	}
}

func localOperatorIP() string {
	for _, variable := range []string{"SSH_CONNECTION", "SSH_CLIENT"} {
		fields := strings.Fields(os.Getenv(variable))
		if len(fields) == 0 {
			continue
		}
		if address := net.ParseIP(fields[0]); address != nil {
			return address.String()
		}
	}
	return "127.0.0.1"
}

func handlePGPImport(ctx context.Context, args []string, cfg *config.ServerConfig, actor, binaryName string) {
	if len(args) < 1 || len(args) > 2 || strings.TrimSpace(args[0]) == "" || (len(args) == 2 && args[1] != "--allow-identity-mismatch") {
		printPGPHelp(binaryName)
		os.Exit(exUsage)
	}
	if !confirmPGPAction("import") {
		cprintln("Aborted; no key was imported.")
		return
	}
	data, err := os.ReadFile(args[0])
	if err != nil {
		pgpFail("read private key", err)
	}
	allowMismatch := false
	if len(args) == 2 {
		if !confirmPGPAction("identity-mismatch") {
			cprintln("Aborted; key identity was not overridden.")
			return
		}
		allowMismatch = true
	}
	metadata, err := pgp.ImportPrivate(ctx, cfg, string(data), allowMismatch)
	if err != nil {
		pgpFail("import private key", err)
	}
	if err := database.WriteAuditEventWithSource(ctx, "security.pgp_imported", actor, "", "cli", map[string]any{"fingerprint": metadata.Fingerprint, "identity_override": allowMismatch}); err != nil {
		cprintf("⚠️ Key imported but audit entry failed: %v\n", err)
	}
	if err := pgp.Publish(ctx, cfg); err != nil {
		cprintf("⚠️ Key imported; keyserver publishing will retry: %v\n", err)
	}
	cprintf("✅ Security key imported: %s\n", metadata.Fingerprint)
}

func enforcePGPExportRateLimit(ctx context.Context, actor string) error {
	allowed, err := database.ClaimSecurityPGPExport(ctx, actor, time.Now().UTC())
	if err != nil {
		return err
	}
	if !allowed {
		return fmt.Errorf("private-key export is limited to once per hour per operator")
	}
	return nil
}

func readPGPReason() (string, error) {
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return "", fmt.Errorf("private-key export requires an interactive terminal for the required reason")
	}
	cprintf("Reason for exporting the private key: ")
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil && err != io.EOF {
		return "", err
	}
	line = strings.TrimSpace(line)
	if line == "" || len(line) > 1000 {
		return "", fmt.Errorf("export reason must contain 1 to 1000 characters")
	}
	return line, nil
}

func writePGPExport(path string, data []byte, mode os.FileMode) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return err
	}
	if err := file.Chmod(mode); err != nil {
		_ = file.Close()
		_ = os.Remove(path)
		return err
	}
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		_ = os.Remove(path)
		return err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		_ = os.Remove(path)
		return err
	}
	return file.Close()
}

func confirmPGPAction(action string) bool {
	cprintf("Type %q to confirm: ", action)
	var typed string
	if _, err := fmt.Scanln(&typed); err != nil {
		return false
	}
	return strings.TrimSpace(typed) == action
}

func pgpFail(action string, err error) {
	cprintf("❌ Failed to %s: %v\n", action, err)
	os.Exit(exUnavailable)
}

func printPGPHelp(binaryName string) {
	cprintf(`Security PGP key management

  %s --maintenance pgp generate
  %s --maintenance pgp rotate
  %s --maintenance pgp publish
  %s --maintenance pgp export public [path]
  %s --maintenance pgp export private <path>
  %s --maintenance pgp import <file> [--allow-identity-mismatch]
  %s --maintenance pgp delete
  %s --maintenance pgp metadata

Requires root or the operator token (server.token).
`, binaryName, binaryName, binaryName, binaryName, binaryName, binaryName, binaryName, binaryName)
}
