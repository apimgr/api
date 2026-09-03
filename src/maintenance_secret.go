package main

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"fmt"
	"os"
	"strings"

	"github.com/apimgr/api/src/config"
	"github.com/apimgr/api/src/database"
	"github.com/apimgr/api/src/paths"
	"golang.org/x/term"
)

// manualRotationSecrets maps the secret names that AI.md PART 11 allows to be
// rotated by hand to the audit event each rotation records. cookie_signing_key
// and csrf_token_secret are auto-rotated only and are rejected here.
var manualRotationSecrets = map[string]string{
	database.SecretInstallationSecret: "security.installation_secret_rotated",
	"encryption_key":                  "security.encryption_key_rotated",
}

// handleSecretCommand implements `--maintenance secret rotate <name>`, the
// only supported path for manual secret rotation (AI.md PART 11: there is no
// web UI and no admin API route). Authorization follows PART 5's sensitive
// operations flow: root, or a re-prompt for the operator token.
func handleSecretCommand(args []string, binaryName string) {
	if len(args) < 2 || strings.ToLower(args[0]) != "rotate" {
		printSecretHelp(binaryName)
		os.Exit(exUsage)
	}

	name := strings.ToLower(args[1])
	event, ok := manualRotationSecrets[name]
	if !ok {
		switch name {
		case database.SecretCookieSigningKey, database.SecretCSRFTokenSecret:
			cprintf("❌ %s is rotated automatically and cannot be rotated by hand\n", name)
		default:
			cprintf("❌ Unknown secret: %s\n", name)
		}
		printSecretHelp(binaryName)
		os.Exit(exUsage)
	}

	cfg, err := config.Load()
	if err != nil {
		cprintf("❌ Failed to load configuration: %v\n", err)
		os.Exit(exConfig)
	}

	actor, err := authorizeSensitiveOperation(cfg)
	if err != nil {
		cprintf("❌ %v\n", err)
		os.Exit(exUsage)
	}

	if !confirmSecretRotation(name) {
		cprintln("Aborted; no secret was rotated.")
		os.Exit(0)
	}

	if err := database.Init(cfg.Server.Database, paths.DataDir()); err != nil {
		cprintf("❌ Failed to initialize database: %v\n", err)
		os.Exit(exUnavailable)
	}
	defer database.Close()

	ctx := context.Background()
	if name == database.SecretInstallationSecret {
		if err := database.RotateSecret(ctx, database.SecretInstallationSecret); err != nil {
			cprintf("❌ Failed to rotate %s: %v\n", name, err)
			os.Exit(1)
		}
		cprintln("✅ installation_secret rotated; the previous value stays valid for 7 days")
	} else {
		if err := config.RotateEncryptionKey(cfg); err != nil {
			cprintf("❌ Failed to rotate %s: %v\n", name, err)
			os.Exit(1)
		}
		cprintln("✅ encryption_key rotated; the previous key stays valid for 30 days")
	}

	if err := database.WriteAuditEvent(ctx, event, actor, "", map[string]any{
		"secret": name,
		"source": "maintenance_cli",
	}); err != nil {
		cprintf("⚠️ Rotation succeeded but the audit entry failed: %v\n", err)
	}
}

// authorizeSensitiveOperation authorizes a sensitive maintenance operation.
// Running as root is sufficient; otherwise the operator token is re-prompted
// and compared in constant time against the configured server.token.
func authorizeSensitiveOperation(cfg *config.Config) (string, error) {
	if os.Geteuid() == 0 {
		return "root", nil
	}
	if cfg.Server.Token == "" {
		return "", fmt.Errorf("server.token is not set; run this command as root")
	}

	cprintf("Operator token: ")
	entered, err := term.ReadPassword(int(os.Stdin.Fd()))
	cprintln("")
	if err != nil {
		return "", fmt.Errorf("failed to read the operator token: %w", err)
	}

	supplied := sha256.Sum256([]byte(strings.TrimSpace(string(entered))))
	expected := sha256.Sum256([]byte(cfg.Server.Token))
	if subtle.ConstantTimeCompare(supplied[:], expected[:]) != 1 {
		return "", fmt.Errorf("operator token rejected")
	}
	return "operator", nil
}

// confirmSecretRotation requires the operator to type the secret name back,
// since rotation is irreversible for anything already past its grace window.
func confirmSecretRotation(name string) bool {
	cprintf("Rotating %s is irreversible. Type the secret name to confirm: ", name)
	var typed string
	if _, err := fmt.Scanln(&typed); err != nil {
		return false
	}
	return strings.TrimSpace(typed) == name
}

// printSecretHelp documents the secret subcommand surface.
func printSecretHelp(binaryName string) {
	cprintf(`Secret Rotation

  %s --maintenance secret rotate installation_secret
  %s --maintenance secret rotate encryption_key

Requires root or the operator token (server.token).
cookie_signing_key and csrf_token_secret rotate automatically and cannot be
rotated by hand.
`, binaryName, binaryName)
}
