package main

import (
	"context"
	"net/mail"
	"os"
	"strings"

	"github.com/apimgr/api/src/config"
	"github.com/apimgr/api/src/database"
	"github.com/apimgr/api/src/email"
	"github.com/apimgr/api/src/paths"
)

// handleEmailCommand implements the positional `email` command documented in
// AI.md PART 17 ("Use {project_name} email test to send a test email and
// verify configuration"). Only the `test` subcommand exists: it renders the
// test template with sample data, prefixes the subject with [TEST], sends a
// real message through the configured SMTP server and records the delivery in
// the audit log.
func handleEmailCommand(args []string, binaryName string) {
	if len(args) == 0 || strings.ToLower(args[0]) != "test" {
		printEmailHelp(binaryName)
		os.Exit(exUsage)
	}

	recipient := ""
	if len(args) > 1 {
		recipient = strings.TrimSpace(args[1])
	}
	if recipient != "" {
		if _, err := mail.ParseAddress(recipient); err != nil {
			cprintf("❌ Invalid recipient address: %s\n", recipient)
			os.Exit(exUsage)
		}
	}

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

	// Reuse the server's own SMTP bring-up so the test exercises exactly what
	// the running server would use: auto-detection when no host is configured,
	// a live connection test when one is, and email disabled when neither
	// works. Nothing is ever queued for a later retry (PART 17).
	initSMTP(cfg)

	// The dialog described in PART 17 "Send Test Email" defaults the recipient
	// to server.notifications.email.reply_to; with neither an argument nor a
	// reply-to address the notifier falls back to server.contact.admin.
	var to []string
	switch {
	case recipient != "":
		to = []string{recipient}
	case cfg.Server.Notifications.Email.ReplyTo != "":
		to = []string{cfg.Server.Notifications.Email.ReplyTo}
	}

	result, err := email.DefaultNotifier().SendTest(to)
	if err != nil {
		cprintf("❌ Test email not sent: %v\n", err)
		os.Exit(exUnavailable)
	}

	if err := database.WriteAuditEvent(context.Background(), "email.test_sent", "cli", "", map[string]any{
		"recipients": result.Recipients,
		"subject":    result.Subject,
		"template":   "test",
		"source":     "email_cli",
	}); err != nil {
		cprintf("⚠️ Test email sent but the audit entry failed: %v\n", err)
	}

	cprintf("✅ Test email sent to %s\n", strings.Join(result.Recipients, ", "))
	cprintf("   Subject: %s\n", result.Subject)
}

// printEmailHelp documents the email command surface.
func printEmailHelp(binaryName string) {
	cprintf(`%s email - Email configuration commands

Usage: %s email test [recipient]

Commands:
  test [recipient]        Send a real test email using sample data

Notes:
  Requires a working SMTP server; nothing is queued when SMTP is unavailable.
  The recipient defaults to server.notifications.email.reply_to, then to
  server.contact.admin.email. The subject is prefixed with [TEST] and the
  delivery is recorded in the audit log.
`, binaryName, binaryName)
}
