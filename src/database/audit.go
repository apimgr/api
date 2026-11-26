package database

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

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
	if serverDB == nil {
		return fmt.Errorf("audit: database not initialized")
	}
	if source == "" {
		if inherited, ok := ctx.Value(auditSourceContextKey{}).(string); ok {
			source = inherited
		}
	}

	encoded := ""
	if len(details) > 0 {
		data, err := json.Marshal(details)
		if err != nil {
			return fmt.Errorf("audit: encode details: %w", err)
		}
		encoded = string(data)
	}

	queryCtx, cancel := context.WithTimeout(ctx, auditQueryTimeout)
	defer cancel()

	_, err := execContext(queryCtx, serverDB,
		`INSERT INTO audit_log (timestamp, event, actor, ip_address, source, details)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		time.Now().UTC().Format("2006-01-02T15:04:05.000Z"), event, actor, ipAddress, source, encoded)
	if err != nil {
		return fmt.Errorf("audit: write %s: %w", event, err)
	}
	return nil
}
