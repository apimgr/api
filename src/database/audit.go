package database

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// auditQueryTimeout bounds a single audit_log insert.
const auditQueryTimeout = 10 * time.Second

// WriteAuditEvent appends one entry to the audit_log table. Callers pass a
// stable dotted event name (for example security.encryption_key_rotated), the
// acting identity, the originating address when one is known, and any extra
// structured context. Details are stored as JSON; secret material is never
// passed in.
func WriteAuditEvent(ctx context.Context, event, actor, ipAddress string, details map[string]any) error {
	if serverDB == nil {
		return fmt.Errorf("audit: database not initialized")
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
		`INSERT INTO audit_log (timestamp, event, actor, ip_address, details)
		 VALUES (?, ?, ?, ?, ?)`,
		time.Now().UTC().Format(time.RFC3339), event, actor, ipAddress, encoded)
	if err != nil {
		return fmt.Errorf("audit: write %s: %w", event, err)
	}
	return nil
}
