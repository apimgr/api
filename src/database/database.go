package database

import (
	"database/sql"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/apimgr/api/src/config"
	"github.com/apimgr/api/src/paths"
	_ "github.com/tursodatabase/libsql-client-go/libsql"
	_ "modernc.org/sqlite"
)

var (
	serverDB *sql.DB
	mu       sync.RWMutex
)

// normalizeDriver maps user-friendly config values to the actual Go
// sql.Open driver name, per AI.md PART 3 "Database Drivers".
func normalizeDriver(driver string) string {
	switch strings.ToLower(driver) {
	case "sqlite", "sqlite2", "sqlite3":
		return "sqlite"
	case "libsql", "turso":
		return "libsql"
	default:
		return driver
	}
}

// validateLibSQL enforces that the libsql driver is only used against a
// remote server URL, per AI.md PART 3 ("libSQL requires a server URL - it
// does NOT support embedded/local mode with CGO_ENABLED=0").
func validateLibSQL(cfg config.DatabaseConfig) error {
	if cfg.URL == "" {
		return fmt.Errorf("libsql driver requires url: use libsql://host?authToken=xxx or https://host with token field")
	}
	return nil
}

// buildLibSQLURL appends a separately configured auth token to the URL
// when the URL does not already carry one, per AI.md PART 3's "Option 2:
// Separate URL and token" configuration.
func buildLibSQLURL(url, token string) string {
	if token == "" || strings.Contains(url, "authToken=") {
		return url
	}
	sep := "?"
	if strings.Contains(url, "?") {
		sep = "&"
	}
	return url + sep + "authToken=" + token
}

// Init initializes the database connection.
// Creates one database per spec: server.db holds resource state (rate
// limits, audit log, scheduler, backups). server.yml is the sole source of
// truth for configuration (see config-rules.md); this project has no user
// accounts, sessions, or admin panel (IDEA.md non-goals, confirmed against
// AI.md's own "no admin web UI" statements), so there is no users.db.
// dbCfg.Driver selects the backend: sqlite (default, local file under
// dataDir) or libsql/turso (remote-only, per AI.md PART 3).
func Init(dbCfg config.DatabaseConfig, dataDir string) error {
	driver := normalizeDriver(dbCfg.Driver)
	if driver == "" {
		driver = "sqlite"
	}

	var dsn, displayPath string
	switch driver {
	case "libsql":
		if err := validateLibSQL(dbCfg); err != nil {
			return err
		}
		dsn = buildLibSQLURL(dbCfg.URL, dbCfg.Token)
		displayPath = dbCfg.URL
	case "sqlite":
		// Ensure database directory exists. Honors the DATABASE_DIR env var
		// per AI.md PART 4 (Environment Variables table), else falls back
		// to {dataDir}/db.
		dbDir := paths.GetDatabaseDir(dataDir)
		if err := os.MkdirAll(dbDir, 0755); err != nil {
			return fmt.Errorf("failed to create database directory: %w", err)
		}
		serverPath := filepath.Join(dbDir, "server.db")
		dsn = serverPath + "?_journal_mode=WAL&_busy_timeout=5000"
		displayPath = serverPath
	default:
		return fmt.Errorf("unsupported database driver: %s", dbCfg.Driver)
	}

	var err error
	serverDB, err = sql.Open(driver, dsn)
	if err != nil {
		return fmt.Errorf("failed to open server database: %w", err)
	}

	// Configure server DB connection pool
	serverDB.SetMaxOpenConns(25)
	serverDB.SetMaxIdleConns(5)

	// Test connection
	if err := serverDB.Ping(); err != nil {
		serverDB.Close()
		return fmt.Errorf("failed to ping server database: %w", err)
	}

	log.Printf("Database: Initialized %s database", driver)
	log.Printf("  Server DB: %s", displayPath)

	// Create schema
	if err := createSchema(); err != nil {
		serverDB.Close()
		return fmt.Errorf("failed to create schema: %w", err)
	}

	return nil
}

// GetServerDB returns the server database connection
func GetServerDB() *sql.DB {
	mu.RLock()
	defer mu.RUnlock()
	return serverDB
}

// Close closes the database connection
func Close() error {
	mu.Lock()
	defer mu.Unlock()

	if serverDB != nil {
		if err := serverDB.Close(); err != nil {
			return fmt.Errorf("server db: %w", err)
		}
	}

	return nil
}

// createSchema creates all required database tables
func createSchema() error {
	// Create server.db schema
	if err := createServerSchema(); err != nil {
		return fmt.Errorf("server schema: %w", err)
	}

	return nil
}

// createServerSchema creates tables in server.db
func createServerSchema() error {
	schema := `
	-- Rate limiting (sliding window counters)
	CREATE TABLE IF NOT EXISTS rate_limits (
		key TEXT PRIMARY KEY,
		count INTEGER NOT NULL DEFAULT 0,
		window_start DATETIME NOT NULL,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);
	CREATE INDEX IF NOT EXISTS idx_rate_limits_window ON rate_limits(window_start);

	-- Audit log (admin actions, config changes, security events)
	CREATE TABLE IF NOT EXISTS audit_log (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		timestamp DATETIME DEFAULT CURRENT_TIMESTAMP,
		event TEXT NOT NULL,
		actor TEXT,
		ip_address TEXT,
		details TEXT,
		request_id TEXT
	);
	CREATE INDEX IF NOT EXISTS idx_audit_timestamp ON audit_log(timestamp);
	CREATE INDEX IF NOT EXISTS idx_audit_actor ON audit_log(actor);

	-- Scheduler tasks
	CREATE TABLE IF NOT EXISTS scheduler_tasks (
		task_id TEXT PRIMARY KEY,
		task_name TEXT NOT NULL,
		schedule TEXT NOT NULL,
		last_run DATETIME,
		last_status TEXT,
		last_error TEXT,
		next_run DATETIME NOT NULL,
		run_count INTEGER DEFAULT 0,
		fail_count INTEGER DEFAULT 0,
		enabled BOOLEAN DEFAULT 1,
		locked_by TEXT,
		locked_at DATETIME
	);
	CREATE INDEX IF NOT EXISTS idx_scheduler_next_run ON scheduler_tasks(next_run, enabled);
	CREATE INDEX IF NOT EXISTS idx_scheduler_locked ON scheduler_tasks(locked_by, locked_at);

	-- Scheduler history
	CREATE TABLE IF NOT EXISTS scheduler_history (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		task_id TEXT NOT NULL,
		started_at DATETIME NOT NULL,
		completed_at DATETIME,
		status TEXT NOT NULL,
		error TEXT,
		duration_ms INTEGER
	);
	CREATE INDEX IF NOT EXISTS idx_scheduler_history_task ON scheduler_history(task_id, started_at);

	-- Backup metadata
	CREATE TABLE IF NOT EXISTS backups (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		filename TEXT NOT NULL,
		path TEXT NOT NULL,
		size_bytes INTEGER NOT NULL,
		encrypted BOOLEAN DEFAULT 0,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		created_by TEXT
	);
	CREATE INDEX IF NOT EXISTS idx_backups_created ON backups(created_at);
	`

	_, err := serverDB.Exec(schema)
	if err != nil {
		return fmt.Errorf("failed to create server schema: %w", err)
	}

	log.Println("Database: Server schema created/verified")
	return nil
}
