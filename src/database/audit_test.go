package database

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/apimgr/api/src/config"
	"github.com/apimgr/api/src/paths"
	"github.com/stretchr/testify/require"
)

func useTestAuditFile(t *testing.T, rotate, keep string) string {
	t.Helper()
	dir := t.TempDir()
	previousLogDir := paths.LogDir()
	paths.SetApplicationPathOverrides("", "", dir)
	require.NoError(t, ConfigureAuditFile(config.AuditLogConfig{
		Enabled: true, Filename: "audit.log", Rotate: rotate, Keep: keep,
	}))
	t.Cleanup(func() {
		paths.SetApplicationPathOverrides("", "", previousLogDir)
		_ = ConfigureAuditFile(config.AuditLogConfig{Enabled: true, Filename: "audit.log", Rotate: "never", Keep: "forever"})
	})
	return filepath.Join(dir, "audit.log")
}

func TestAuditFileWritesJSONLHashChainAndActorMetadata(t *testing.T) {
	path := useTestAuditFile(t, "never", "forever")
	require.NoError(t, WriteAuditEventWithSource(context.Background(), "mcp.request", "owner-1", "192.0.2.10", "mcp", map[string]any{
		"method": "tools/call", "api_token": "must-not-be-written", "contact_email": "person@example.com",
	}))
	require.NoError(t, WriteAuditEventWithSource(context.Background(), "mcp.request_failed", "", "192.0.2.11", "mcp", nil))

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	lines := strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
	require.Len(t, lines, 2)
	var first, second map[string]any
	require.NoError(t, json.Unmarshal([]byte(lines[0]), &first))
	require.NoError(t, json.Unmarshal([]byte(lines[1]), &second))
	require.Regexp(t, `^[0-7][0-9A-HJKMNP-TV-Z]{25}$`, first["id"])
	require.Regexp(t, `^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d{3}Z$`, first["time"])
	require.Equal(t, "mcp", first["source"])
	require.Equal(t, "success", first["result"])
	actor := first["actor"].(map[string]any)
	require.Equal(t, "owner", actor["type"])
	require.Equal(t, "owner-1", actor["id"])
	require.NotContains(t, lines[0], "must-not-be-written")
	require.NotContains(t, lines[0], "person@example.com")
	firstHash := sha256.Sum256([]byte(lines[0]))
	require.Equal(t, hex.EncodeToString(firstHash[:]), second["prev"])
	require.Equal(t, "failure", second["result"])
	secondActor := second["actor"].(map[string]any)
	require.Equal(t, "anonymous", secondActor["type"])
	require.NotContains(t, secondActor, "id")
}

func TestAuditFileOwnerOnlyPermissionsByDefault(t *testing.T) {
	path := useTestAuditFile(t, "never", "none")
	require.NoError(t, WriteAuditEvent(context.Background(), "test.permissions", "", "", nil))

	info, err := os.Stat(path)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0600), info.Mode().Perm())
	dirInfo, err := os.Stat(filepath.Dir(path))
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0700), dirInfo.Mode().Perm())
}

func TestAuditFileDetectsActiveTailTampering(t *testing.T) {
	path := useTestAuditFile(t, "never", "forever")
	require.NoError(t, WriteAuditEvent(context.Background(), "test.first", "operator", "", nil))
	require.NoError(t, WriteAuditEvent(context.Background(), "test.second", "operator", "", nil))

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	tampered := strings.Replace(string(data), `"event":"test.second"`, `"event":"changed"`, 1)
	require.NotEqual(t, string(data), tampered)
	require.NoError(t, os.WriteFile(path, []byte(tampered), 0600))
	require.NoError(t, WriteAuditEvent(context.Background(), "test.third", "operator", "", nil))

	data, err = os.ReadFile(path)
	require.NoError(t, err)
	lines := strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
	require.Len(t, lines, 2)
	var warning map[string]any
	require.NoError(t, json.Unmarshal([]byte(lines[0]), &warning))
	require.Equal(t, "security.audit_chain_broken", warning["event"])
	require.Equal(t, "critical", warning["severity"])
	require.Equal(t, "failure", warning["result"])
	details := warning["details"].(map[string]any)
	require.Equal(t, float64(2), details["line"])
	entries, err := os.ReadDir(filepath.Dir(path))
	require.NoError(t, err)
	var archived bool
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), "audit.log.") {
			archived = true
		}
	}
	require.True(t, archived)
}

func TestAuditRotationWritesAnchorBeforePruning(t *testing.T) {
	path := useTestAuditFile(t, "daily", "forever")
	require.NoError(t, WriteAuditEvent(context.Background(), "test.before_rotation", "operator", "", nil))
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	line := strings.TrimSuffix(string(data), "\n")
	old := time.Now().Add(-48 * time.Hour)
	require.NoError(t, os.Chtimes(path, old, old))
	require.NoError(t, WriteAuditEvent(context.Background(), "test.after_rotation", "operator", "", nil))

	anchorPath := filepath.Join(filepath.Dir(path), "audit.anchor")
	anchor, err := os.ReadFile(anchorPath)
	require.NoError(t, err)
	hash := sha256.Sum256([]byte(line))
	require.Equal(t, hex.EncodeToString(hash[:]), strings.TrimSpace(string(anchor)))
	anchorInfo, err := os.Stat(anchorPath)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0600), anchorInfo.Mode().Perm())
	entries, err := os.ReadDir(filepath.Dir(path))
	require.NoError(t, err)
	rotated := false
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), "audit.log.") {
			rotated = true
		}
	}
	require.True(t, rotated)
	current, err := os.ReadFile(path)
	require.NoError(t, err)
	var second map[string]any
	require.NoError(t, json.Unmarshal([]byte(strings.TrimSpace(string(current))), &second))
	require.Equal(t, hex.EncodeToString(hash[:]), second["prev"])
}

func TestWriteAuditEventWithSourceStoresTopLevelSource(t *testing.T) {
	useTestAuditFile(t, "never", "forever")
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
	parsedTimestamp, parseErr := time.Parse(time.RFC3339Nano, timestamp)
	require.NoError(t, parseErr)
	require.Equal(t, time.UTC, parsedTimestamp.Location())
	require.NoError(t, WriteAuditEvent(WithAuditSource(context.Background(), "mcp"), "mcp.dispatched", "owner-1", "192.0.2.1", nil))
	require.NoError(t, db.QueryRow(`SELECT source FROM audit_log WHERE event = 'mcp.dispatched'`).Scan(&source))
	require.Equal(t, "mcp", source)
}
