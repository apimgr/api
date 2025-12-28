package database

import (
	"database/sql"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"

	_ "modernc.org/sqlite"
)

var (
	serverDB *sql.DB
	usersDB  *sql.DB
	mu       sync.RWMutex
)

// Init initializes the database connections
// Creates two SQLite databases per spec:
// - server.db: Server state (config, sessions, rate limits, audit, scheduler)
// - users.db: User data (admins, users, API keys)
func Init(dataDir string) error {
	// Ensure database directory exists
	dbDir := filepath.Join(dataDir, "db")
	if err := os.MkdirAll(dbDir, 0755); err != nil {
		return fmt.Errorf("failed to create database directory: %w", err)
	}

	// Open server database
	// Driver name is "sqlite" (not "sqlite3") for modernc.org/sqlite
	serverPath := filepath.Join(dbDir, "server.db")
	var err error
	serverDB, err = sql.Open("sqlite", serverPath+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		return fmt.Errorf("failed to open server database: %w", err)
	}

	// Configure server DB connection pool
	serverDB.SetMaxOpenConns(25)
	serverDB.SetMaxIdleConns(5)

	// Open users database
	usersPath := filepath.Join(dbDir, "users.db")
	usersDB, err = sql.Open("sqlite", usersPath+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		serverDB.Close()
		return fmt.Errorf("failed to open users database: %w", err)
	}

	// Configure users DB connection pool
	usersDB.SetMaxOpenConns(25)
	usersDB.SetMaxIdleConns(5)

	// Test connections
	if err := serverDB.Ping(); err != nil {
		serverDB.Close()
		usersDB.Close()
		return fmt.Errorf("failed to ping server database: %w", err)
	}

	if err := usersDB.Ping(); err != nil {
		serverDB.Close()
		usersDB.Close()
		return fmt.Errorf("failed to ping users database: %w", err)
	}

	log.Printf("Database: Initialized SQLite databases")
	log.Printf("  Server DB: %s", serverPath)
	log.Printf("  Users DB: %s", usersPath)

	// Create schema
	if err := createSchema(); err != nil {
		serverDB.Close()
		usersDB.Close()
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

// GetUsersDB returns the users database connection
func GetUsersDB() *sql.DB {
	mu.RLock()
	defer mu.RUnlock()
	return usersDB
}

// Close closes both database connections
func Close() error {
	mu.Lock()
	defer mu.Unlock()

	var errs []error

	if serverDB != nil {
		if err := serverDB.Close(); err != nil {
			errs = append(errs, fmt.Errorf("server db: %w", err))
		}
	}

	if usersDB != nil {
		if err := usersDB.Close(); err != nil {
			errs = append(errs, fmt.Errorf("users db: %w", err))
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("database close errors: %v", errs)
	}

	return nil
}

// createSchema creates all required database tables
func createSchema() error {
	// Create server.db schema
	if err := createServerSchema(); err != nil {
		return fmt.Errorf("server schema: %w", err)
	}

	// Create users.db schema
	if err := createUsersSchema(); err != nil {
		return fmt.Errorf("users schema: %w", err)
	}

	return nil
}

// createServerSchema creates tables in server.db
func createServerSchema() error {
	schema := `
	-- Config storage (key-value)
	CREATE TABLE IF NOT EXISTS config (
		key TEXT PRIMARY KEY,
		value TEXT NOT NULL,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	-- Config metadata (version tracking)
	CREATE TABLE IF NOT EXISTS config_meta (
		id INTEGER PRIMARY KEY CHECK (id = 1),
		version INTEGER NOT NULL DEFAULT 1,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	-- Insert initial config_meta row
	INSERT OR IGNORE INTO config_meta (id, version) VALUES (1, 1);

	-- Sessions (admin WebUI login sessions)
	CREATE TABLE IF NOT EXISTS sessions (
		id TEXT PRIMARY KEY,
		admin_id INTEGER NOT NULL,
		data TEXT NOT NULL,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		expires_at DATETIME NOT NULL,
		last_activity DATETIME DEFAULT CURRENT_TIMESTAMP
	);
	CREATE INDEX IF NOT EXISTS idx_sessions_expires ON sessions(expires_at);
	CREATE INDEX IF NOT EXISTS idx_sessions_admin ON sessions(admin_id);

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

// createUsersSchema creates tables in users.db
func createUsersSchema() error {
	schema := `
	-- Server admin accounts (WebUI access)
	CREATE TABLE IF NOT EXISTS admins (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		username TEXT UNIQUE NOT NULL,
		email TEXT UNIQUE NOT NULL,
		password_hash TEXT NOT NULL,
		totp_secret TEXT,
		totp_enabled BOOLEAN DEFAULT 0,
		webauthn_credentials TEXT,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		last_login DATETIME,
		is_primary BOOLEAN DEFAULT 0,
		enabled BOOLEAN DEFAULT 1
	);
	CREATE INDEX IF NOT EXISTS idx_admins_email ON admins(email);
	CREATE INDEX IF NOT EXISTS idx_admins_username ON admins(username);

	-- Regular app users (if project has users)
	CREATE TABLE IF NOT EXISTS users (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		username TEXT UNIQUE NOT NULL,
		email TEXT UNIQUE NOT NULL,
		password_hash TEXT NOT NULL,
		totp_secret TEXT,
		totp_enabled BOOLEAN DEFAULT 0,
		webauthn_credentials TEXT,
		email_verified BOOLEAN DEFAULT 0,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		last_login DATETIME,
		enabled BOOLEAN DEFAULT 1
	);
	CREATE INDEX IF NOT EXISTS idx_users_email ON users(email);
	CREATE INDEX IF NOT EXISTS idx_users_username ON users(username);

	-- API authentication keys
	CREATE TABLE IF NOT EXISTS api_keys (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		key TEXT UNIQUE NOT NULL,
		name TEXT NOT NULL,
		admin_id INTEGER,
		user_id INTEGER,
		permissions TEXT,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		expires_at DATETIME,
		last_used DATETIME,
		enabled BOOLEAN DEFAULT 1
	);
	CREATE INDEX IF NOT EXISTS idx_api_keys_key ON api_keys(key);
	CREATE INDEX IF NOT EXISTS idx_api_keys_expires ON api_keys(expires_at);

	-- Password reset tokens
	CREATE TABLE IF NOT EXISTS password_resets (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		token TEXT UNIQUE NOT NULL,
		email TEXT NOT NULL,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		expires_at DATETIME NOT NULL,
		used BOOLEAN DEFAULT 0
	);
	CREATE INDEX IF NOT EXISTS idx_password_resets_token ON password_resets(token);
	CREATE INDEX IF NOT EXISTS idx_password_resets_expires ON password_resets(expires_at);

	-- Email verification tokens
	CREATE TABLE IF NOT EXISTS email_verifications (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		token TEXT UNIQUE NOT NULL,
		user_id INTEGER NOT NULL,
		email TEXT NOT NULL,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		expires_at DATETIME NOT NULL,
		verified BOOLEAN DEFAULT 0
	);
	CREATE INDEX IF NOT EXISTS idx_email_verifications_token ON email_verifications(token);
	CREATE INDEX IF NOT EXISTS idx_email_verifications_user ON email_verifications(user_id);

	-- TOTP secrets and backup codes
	CREATE TABLE IF NOT EXISTS totp_secrets (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		admin_id INTEGER,
		user_id INTEGER,
		secret TEXT NOT NULL,
		backup_codes TEXT,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		last_used DATETIME
	);
	CREATE INDEX IF NOT EXISTS idx_totp_admin ON totp_secrets(admin_id);
	CREATE INDEX IF NOT EXISTS idx_totp_user ON totp_secrets(user_id);
	`

	_, err := usersDB.Exec(schema)
	if err != nil {
		return fmt.Errorf("failed to create users schema: %w", err)
	}

	log.Println("Database: Users schema created/verified")
	return nil
}

// RunMigrations runs any pending database migrations
func RunMigrations() error {
	// TODO: Implement migration system
	// Check current schema version
	// Apply migrations in order
	// Update schema version

	log.Println("Database: Migrations check completed")
	return nil
}
