package database

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMain initializes a single shared SQLite instance under a temp
// directory for the whole package test run, since Init/GetServerDB
// operate on package-level globals rather than an injectable handle.
func TestMain(m *testing.M) {
	dataDir, err := os.MkdirTemp("", "database-test-*")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(dataDir)

	if err := Init(dataDir); err != nil {
		panic(err)
	}

	code := m.Run()

	Close()
	os.RemoveAll(dataDir)
	os.Exit(code)
}

// Init must create the db subdirectory and the SQLite file, and
// GetServerDB must return a non-nil, pingable connection.
func TestInitCreatesDatabases(t *testing.T) {
	serverDBConn := GetServerDB()
	require.NotNil(t, serverDBConn)

	assert.NoError(t, serverDBConn.Ping())
}

// Init must fail cleanly (rather than panic) when it cannot create the
// database directory, e.g. because a file already occupies that path.
func TestInitDirectoryCreationFailure(t *testing.T) {
	tmp := t.TempDir()
	blocker := filepath.Join(tmp, "blocked")
	require.NoError(t, os.WriteFile(blocker, []byte("x"), 0644))

	// blocker is a file, so MkdirAll(blocker/db, ...) must fail.
	err := Init(blocker)
	assert.Error(t, err)
}

// The server schema must contain every table the spec requires. This
// project has no user accounts, sessions, or admin panel (IDEA.md
// non-goals, confirmed against AI.md's own "no admin web UI" statements),
// and config lives solely in server.yml (config-rules.md), so there is no
// config/config_meta/sessions/admins/users table.
func TestServerSchemaTables(t *testing.T) {
	want := []string{"rate_limits", "audit_log", "scheduler_tasks", "scheduler_history", "backups"}
	assertTablesExist(t, GetServerDB(), want)
}

// assertTablesExist verifies each table in want is present in db's
// sqlite_master catalog.
func assertTablesExist(t *testing.T, db *sql.DB, want []string) {
	t.Helper()

	rows, err := db.Query(`SELECT name FROM sqlite_master WHERE type = 'table'`)
	require.NoError(t, err)
	defer rows.Close()

	present := map[string]bool{}
	for rows.Next() {
		var name string
		require.NoError(t, rows.Scan(&name))
		present[name] = true
	}
	require.NoError(t, rows.Err())

	for _, name := range want {
		assert.True(t, present[name], "expected table %q to exist", name)
	}
}
