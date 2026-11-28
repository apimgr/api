package database

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"regexp"
	"strings"
	"time"

	"github.com/apimgr/api/src/common/sanitize"
	"github.com/apimgr/api/src/critical"
)

var auditEmailAddress = regexp.MustCompile(`(?i)\b[A-Z0-9._%+-]+@[A-Z0-9.-]+\.[A-Z]{2,}\b`)

// auditQueryTimeout bounds a single audit_log insert.
const auditQueryTimeout = 10 * time.Second

type auditSourceContextKey struct{}

// WithAuditSource associates an audit source with a request context so nested
// in-process work, including MCP-dispatched handlers, retains its origin.
func WithAuditSource(ctx context.Context, source string) context.Context {
	return context.WithValue(ctx, auditSourceContextKey{}, source)
}

// WriteAuditEvent appends one entry to the audit_log table. Callers pass a
// stable dotted event name (for example security.encryption_key_rotated), the
// acting identity, the originating address when one is known, and any extra
// structured context. Details are stored as JSON; secret material is never
// passed in.
func WriteAuditEvent(ctx context.Context, event, actor, ipAddress string, details map[string]any) error {
	return WriteAuditEventWithSource(ctx, event, actor, ipAddress, "", details)
}

// WriteAuditEventWithSource appends an audit event with an optional top-level
// traffic source, such as "api", "mcp", or "scheduler".
func WriteAuditEventWithSource(ctx context.Context, event, actor, ipAddress, source string, details map[string]any) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if !auditFileEnabled() {
		return nil
	}
	if source == "" {
		if inherited, ok := ctx.Value(auditSourceContextKey{}).(string); ok {
			source = inherited
		}
	}
	source = normalizeAuditSource(source, actor, ipAddress)
	record, err := buildAuditRecord(event, actor, ipAddress, source, resultForAuditEvent(event, details), details)
	if err != nil {
		return err
	}
	if err := appendAuditFile(record); err != nil {
		critical.ReportWriteFailure("file_write")
		return err
	}
	mu.RLock()
	db := serverDB
	mu.RUnlock()
	if db == nil {
		return nil
	}

	encoded := ""
	if len(details) > 0 {
		data, err := json.Marshal(sanitizeAuditFields(details))
		if err != nil {
			log.Printf("audit: database mirror details failed to encode: %s", sanitize.SanitizeLogText(err.Error()))
			return nil
		}
		encoded = string(data)
	}

	queryCtx, cancel := context.WithTimeout(ctx, auditQueryTimeout)
	defer cancel()

	_, err = execContext(queryCtx, db,
		`INSERT INTO audit_log (timestamp, event, actor, ip_address, source, details)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		record.Time, sanitize.SanitizeLogText(event), sanitizeAuditText(actor), sanitize.SanitizeLogText(ipAddress), source, encoded)
	if err != nil {
		log.Printf("audit: database mirror failed for %s: %s", sanitize.SanitizeLogText(event), sanitize.SanitizeLogText(err.Error()))
	}
	return nil
}

func buildAuditRecord(event, actor, ipAddress, source, result string, details map[string]any) (auditFileRecord, error) {
	now := time.Now().UTC()
	id, err := generateAuditULID(now)
	if err != nil {
		return auditFileRecord{}, fmt.Errorf("audit: generate id: %w", err)
	}
	event = sanitize.SanitizeLogText(event)
	actor = sanitizeAuditText(actor)
	ipAddress = sanitize.SanitizeLogText(ipAddress)
	details = sanitizeAuditFields(details)
	actorType := "system"
	actorID := ""
	switch source {
	case "web", "api", "mcp":
		if actor == "" || actor == "anonymous" {
			actorType = "anonymous"
		} else {
			actorType, actorID = "owner", actor
		}
	case "cli":
		actorType = "operator"
		if !isAuditPlaceholder(actor) {
			actorID = actor
		}
	case "scheduler":
		actorType = "system"
	default:
		if ipAddress != "" {
			if actor != "" && !isAuditPlaceholder(actor) {
				actorType, actorID = "owner", actor
			} else {
				actorType = "anonymous"
			}
		} else if !isAuditPlaceholder(actor) && actor != "" {
			actorType, actorID = "operator", actor
		}
	}
	entry := auditFileRecord{
		ID:       id,
		Time:     now.Format("2006-01-02T15:04:05.000Z"),
		Event:    event,
		Category: auditCategory(event),
		Severity: auditSeverity(event),
		Actor:    auditActor{Type: actorType, ID: actorID},
		Details:  details,
		Result:   result,
		Source:   source,
	}
	if ipAddress != "" {
		entry.Actor.IP = ipAddress
	}
	if reason, ok := details["reason"].(string); ok && reason != "" {
		entry.Reason = reason
	}
	if target, ok := details["target"].(map[string]any); ok {
		entry.Target = target
		delete(details, "target")
	}
	return entry, nil
}

func sanitizeAuditText(value string) string {
	return auditEmailAddress.ReplaceAllString(sanitize.SanitizeLogText(value), sanitize.RedactedPlaceholder)
}

func sanitizeAuditFields(fields map[string]any) map[string]any {
	sanitized := sanitize.SanitizeLogFields(fields)
	for key, value := range sanitized {
		normalized := strings.ToLower(strings.ReplaceAll(strings.ReplaceAll(key, "-", "_"), ".", "_"))
		if strings.Contains(normalized, "email") || normalized == "recipient" || normalized == "recipients" {
			sanitized[key] = sanitize.RedactedPlaceholder
			continue
		}
		sanitized[key] = sanitizeAuditValue(value)
	}
	return sanitized
}

func sanitizeAuditValue(value any) any {
	switch typed := value.(type) {
	case string:
		return sanitizeAuditText(typed)
	case map[string]interface{}:
		return sanitizeAuditFields(typed)
	case []interface{}:
		items := make([]interface{}, len(typed))
		for i, item := range typed {
			items[i] = sanitizeAuditValue(item)
		}
		return items
	case []string:
		items := make([]string, len(typed))
		for i, item := range typed {
			items[i] = sanitizeAuditText(item)
		}
		return items
	default:
		return typed
	}
}

func normalizeAuditSource(source, actor, ipAddress string) string {
	switch strings.ToLower(strings.TrimSpace(source)) {
	case "web", "api", "mcp", "cli", "scheduler":
		return strings.ToLower(strings.TrimSpace(source))
	case "startup", "system":
		return ""
	case "maintenance_cli", "email_cli":
		return "cli"
	}
	switch strings.ToLower(strings.TrimSpace(actor)) {
	case "scheduler":
		return "scheduler"
	case "cli", "root":
		return "cli"
	}
	if ipAddress != "" {
		return "api"
	}
	return ""
}

func isAuditPlaceholder(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "system", "scheduler", "anonymous", "server", "operator":
		return true
	default:
		return false
	}
}

func auditCategory(event string) string {
	if category, _, ok := strings.Cut(event, "."); ok && category != "" {
		return category
	}
	return "system"
}

func auditSeverity(event string) string {
	lower := strings.ToLower(event)
	if strings.Contains(lower, "critical") || strings.Contains(lower, "audit_chain_broken") || strings.Contains(lower, "brute_force") {
		return "critical"
	}
	if strings.Contains(lower, "failed") || strings.Contains(lower, "failure") || strings.Contains(lower, "invalid") || strings.Contains(lower, "rejected") || strings.Contains(lower, "blocked") {
		return "warn"
	}
	return "info"
}

func resultForAuditEvent(event string, details map[string]any) string {
	if result, ok := details["result"].(string); ok && strings.EqualFold(result, "failure") {
		return "failure"
	}
	lower := strings.ToLower(event)
	for _, marker := range []string{"failed", "failure", "invalid", "rejected", "auth_failed", "verification_failed"} {
		if strings.Contains(lower, marker) {
			return "failure"
		}
	}
	return "success"
}
