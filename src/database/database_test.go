package database

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/apimgr/api/src/config"
)

func execTestWrite(t *testing.T, query string, args ...any) {
	t.Helper()
	store := GetStore()
	require.NotNil(t, store)
	require.NoError(t, store.Write(context.Background(), func(tx *sql.Tx) error {
		_, err := tx.ExecContext(context.Background(), query, args...)
		return err
	}))
}

// TestMain initializes a single shared SQLite instance under a temp
// directory for the whole package test run, since Init/GetServerDB
// operate on package-level globals rather than an injectable handle.
func TestMain(m *testing.M) {
	baseDir := filepath.Join(os.TempDir(), "apimgr")
	if err := os.MkdirAll(baseDir, 0755); err != nil {
		panic(err)
	}
	dataDir, err := os.MkdirTemp(baseDir, "api-database-test-")
	if err != nil {
		panic(err)
	}
	if err := os.Setenv("DATABASE_DIR", filepath.Join(dataDir, "db")); err != nil {
		panic(err)
	}
	if err := InitDatabase(config.DatabaseConfig{Driver: "sqlite"}, dataDir); err != nil {
		_ = os.RemoveAll(dataDir)
		panic(err)
	}

	code := m.Run()

	if err := Close(); err != nil && code == 0 {
		code = 1
	}
	if err := os.RemoveAll(dataDir); err != nil && code == 0 {
		code = 1
	}
	os.Exit(code)
}

// Init must create the db subdirectory and the SQLite file, and
// GetServerDB must return a non-nil, pingable connection.
func TestInitCreatesDatabases(t *testing.T) {
	serverDBConn := GetServerDB()
	require.NotNil(t, serverDBConn)

	assert.NoError(t, serverDBConn.Ping())
}

func TestProbeServerDBWriteLeavesNoProbeRows(t *testing.T) {
	require.NoError(t, ProbeServerDBWrite(context.Background()))
	var count int
	require.NoError(t, GetServerDB().QueryRow(`SELECT COUNT(*) FROM app_secrets WHERE name LIKE '__maintenance_probe_%'`).Scan(&count))
	assert.Zero(t, count)
}

func TestProbeServerDBChecksReadAndWriteHandles(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	require.NoError(t, ProbeServerDB(ctx))
	require.NoError(t, (ServerDBProbe{}).PingContext(ctx))
}

func TestGetServerDBExposesReadOnlyPool(t *testing.T) {
	db := GetServerDB()
	require.NotNil(t, db)
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS external_write_probe (id INTEGER PRIMARY KEY)`)
	assert.Error(t, err)
}

// Init must fail cleanly (rather than panic) when it cannot create the
// database directory, e.g. because a file already occupies that path.
func TestInitDirectoryCreationFailure(t *testing.T) {
	tmp := t.TempDir()
	blocker := filepath.Join(tmp, "blocked")
	require.NoError(t, os.WriteFile(blocker, []byte("x"), 0644))

	// blocker is a file, so MkdirAll(blocker/db, ...) must fail.
	t.Setenv("DATABASE_DIR", filepath.Join(blocker, "db"))
	err := InitDatabase(config.DatabaseConfig{Driver: "sqlite"}, blocker)
	assert.Error(t, err)
}

// Init must reject the libsql driver when no url is configured, per AI.md
// PART 3 ("libSQL requires a server URL - it does NOT support
// embedded/local mode").
func TestInitLibSQLRequiresURL(t *testing.T) {
	err := InitDatabase(config.DatabaseConfig{Driver: "libsql"}, t.TempDir())
	assert.Error(t, err)
}

// Init must reject an unrecognized driver name rather than silently
// falling back to sqlite.
func TestInitUnsupportedDriver(t *testing.T) {
	err := InitDatabase(config.DatabaseConfig{Driver: "postgres"}, t.TempDir())
	assert.Error(t, err)
}

// The server schema must contain every table the spec requires. This
// project has no user accounts, sessions, or admin panel (IDEA.md
// non-goals, confirmed against AI.md's own "no admin web UI" statements),
// and config lives solely in server.yml (config-rules.md), so there is no
// config/config_meta/sessions/admins/users table.
func TestServerSchemaTables(t *testing.T) {
	want := []string{"rate_limits", "audit_log", "scheduler_tasks", "scheduler_history", "backups", "server_stats", "server_request_hours"}
	assertTablesExist(t, GetServerDB(), want)
}

func TestSchedulerSchemaMigratesLegacyColumnsAndTimestamps(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")
	db, err := sql.Open("sqlite", path)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	_, err = db.Exec(`CREATE TABLE scheduler_tasks (
		task_id TEXT PRIMARY KEY, task_name TEXT NOT NULL, schedule TEXT NOT NULL,
		last_run DATETIME, last_status TEXT, last_error TEXT, next_run DATETIME NOT NULL,
		run_count INTEGER DEFAULT 0, fail_count INTEGER DEFAULT 0, enabled BOOLEAN DEFAULT 1
	);
	CREATE TABLE scheduler_history (
		id TEXT PRIMARY KEY, task_id TEXT NOT NULL, ran_at DATETIME NOT NULL,
		completed_at DATETIME, status TEXT NOT NULL, error TEXT, duration_ms INTEGER
	);`)
	require.NoError(t, err)
	lastRun := time.Date(2025, time.January, 2, 3, 4, 5, 0, time.UTC)
	nextRun := lastRun.Add(time.Hour)
	finishedAt := lastRun.Add(time.Minute)
	_, err = db.Exec(`INSERT INTO scheduler_tasks (task_id, task_name, schedule, last_run, next_run, run_count, enabled)
		VALUES (?, ?, ?, ?, ?, ?, ?)`, "legacy-task", "Legacy Task", "@hourly", lastRun.Format(time.RFC3339), nextRun.Format(time.RFC3339), 7, true)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO scheduler_history (id, task_id, ran_at, completed_at, status)
		VALUES (?, ?, ?, ?, ?)`, "opaque-history-id", "legacy-task", lastRun.Format(time.RFC3339), finishedAt.Format(time.RFC3339), "success")
	require.NoError(t, err)
	require.NoError(t, createSchema(db, "sqlite"))

	taskColumns, err := databaseColumns(context.Background(), db, "scheduler_tasks")
	require.NoError(t, err)
	assert.True(t, taskColumns["id"])
	assert.True(t, taskColumns["name"])
	assert.False(t, taskColumns["task_id"])
	assert.False(t, taskColumns["task_name"])
	historyColumns, err := databaseColumns(context.Background(), db, "scheduler_history")
	require.NoError(t, err)
	assert.True(t, historyColumns["started_at"])
	assert.True(t, historyColumns["finished_at"])
	assert.False(t, historyColumns["ran_at"])
	assert.False(t, historyColumns["completed_at"])

	var gotID, gotName string
	var gotLastRun, gotNextRun int64
	var gotRunCount int
	require.NoError(t, db.QueryRow(`SELECT id, name, last_run, next_run, run_count FROM scheduler_tasks WHERE id = ?`, "legacy-task").Scan(&gotID, &gotName, &gotLastRun, &gotNextRun, &gotRunCount))
	assert.Equal(t, "legacy-task", gotID)
	assert.Equal(t, "Legacy Task", gotName)
	assert.Equal(t, lastRun.Unix(), gotLastRun)
	assert.Equal(t, nextRun.Unix(), gotNextRun)
	assert.Equal(t, 7, gotRunCount)
	var opaqueID string
	var gotStarted, gotFinished int64
	require.NoError(t, db.QueryRow(`SELECT id, started_at, finished_at FROM scheduler_history WHERE task_id = ?`, "legacy-task").Scan(&opaqueID, &gotStarted, &gotFinished))
	assert.Equal(t, "opaque-history-id", opaqueID)
	assert.Equal(t, lastRun.Unix(), gotStarted)
	assert.Equal(t, finishedAt.Unix(), gotFinished)
}

// normalizeDriver must map every documented config alias to its Go
// sql.Open driver name and pass unknown values through unchanged.
func TestNormalizeDriver(t *testing.T) {
	assert.Equal(t, "sqlite", normalizeDriver("sqlite"))
	assert.Equal(t, "sqlite", normalizeDriver("sqlite2"))
	assert.Equal(t, "sqlite", normalizeDriver("sqlite3"))
	assert.Equal(t, "sqlite", normalizeDriver("SQLite3"))
	assert.Equal(t, "libsql", normalizeDriver("libsql"))
	assert.Equal(t, "libsql", normalizeDriver("turso"))
	assert.Equal(t, "postgres", normalizeDriver("postgres"))
}

// validateLibSQL must require a non-empty URL and accept any non-empty URL.
func TestValidateLibSQL(t *testing.T) {
	assert.Error(t, validateLibSQL(config.DatabaseConfig{}))
	assert.NoError(t, validateLibSQL(config.DatabaseConfig{URL: "libsql://db.turso.io?authToken=xxx"}))
}

// buildLibSQLURL must append a separately configured token as authToken
// only when the URL doesn't already carry one, respecting existing query
// strings.
func TestBuildLibSQLURL(t *testing.T) {
	assert.Equal(t, "https://db.turso.io?authToken=abc", buildLibSQLURL("https://db.turso.io", "abc"))
	assert.Equal(t, "https://db.turso.io?x=1&authToken=abc", buildLibSQLURL("https://db.turso.io?x=1", "abc"))
	assert.Equal(t, "libsql://db.turso.io?authToken=already", buildLibSQLURL("libsql://db.turso.io?authToken=already", "unused"))
	assert.Equal(t, "https://db.turso.io", buildLibSQLURL("https://db.turso.io", ""))
}

// assertTablesExist verifies each table in want is present in db's
// sqlite_master catalog.
func assertTablesExist(t *testing.T, db *sql.DB, want []string) {
	t.Helper()

	rows, err := db.Query(`SELECT name FROM sqlite_master WHERE type = 'table'`)
	require.NoError(t, err)
	defer func() { _ = rows.Close() }()

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
