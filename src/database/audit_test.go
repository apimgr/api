package database

import (
	"context"
	"database/sql"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestWriteAuditEventWithSourceStoresTopLevelSource(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	require.NoError(t, err)
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, createSchema(db))

	mu.Lock()
	previous := serverDB
	previousStore := serverStore
	serverDB = db
	serverStore = nil
	mu.Unlock()
	t.Cleanup(func() {
		mu.Lock()
		serverDB = previous
		serverStore = previousStore
		mu.Unlock()
	})

	require.NoError(t, WriteAuditEventWithSource(context.Background(), "mcp.request", "owner-1", "192.0.2.1", "mcp", map[string]any{"method": "tools/call"}))
	var source, details, timestamp string
	require.NoError(t, db.QueryRow(`SELECT source, details, timestamp FROM audit_log WHERE event = 'mcp.request'`).Scan(&source, &details, &timestamp))
	require.Equal(t, "mcp", source)
	require.Contains(t, details, `"method":"tools/call"`)
	require.Regexp(t, `^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d{3}Z$`, timestamp)
	require.NoError(t, WriteAuditEvent(WithAuditSource(context.Background(), "mcp"), "mcp.dispatched", "owner-1", "192.0.2.1", nil))
	require.NoError(t, db.QueryRow(`SELECT source FROM audit_log WHERE event = 'mcp.dispatched'`).Scan(&source))
	require.Equal(t, "mcp", source)
}
