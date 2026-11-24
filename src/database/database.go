package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/apimgr/api/src/config"
	"github.com/apimgr/api/src/paths"
	_ "github.com/tursodatabase/libsql-client-go/libsql"
	_ "modernc.org/sqlite"
)

var (
	serverDB     *sql.DB
	serverStore  *Store
	serverDriver string
	mu           sync.RWMutex
)

// TableName maps a fixed application table or index name to the configured
// database-specific name. Remote libSQL names carry the srv_ prefix.
func TableName(name string) string {
	mu.RLock()
	driver := serverDriver
	mu.RUnlock()
	return tableNameForDriver(driver, name)
}

func tableNameForDriver(driver, name string) string {
	if normalizeDriver(driver) == "libsql" {
		return "srv_" + name
	}
	return name
}

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
	var store *Store
	var err error
	pool, err := resolveDatabasePool(dbCfg.Pool)
	if err != nil {
		return err
	}
	switch driver {
	case "libsql":
		if err := validateLibSQL(dbCfg); err != nil {
			return err
		}
		dsn = buildLibSQLURL(dbCfg.URL, dbCfg.Token)
		displayPath = dbCfg.URL
	case "sqlite":
		if dbCfg.Cache.Enabled {
			return errors.New("server.database.cache.enabled requires the libsql database driver")
		}
		// Ensure database directory exists. Honors the DATABASE_DIR env var
		// per AI.md PART 4 (Environment Variables table), else falls back
		// to {dataDir}/db.
		dbDir := paths.GetDatabaseDir(dataDir)
		if err := os.MkdirAll(dbDir, 0755); err != nil {
			return fmt.Errorf("failed to create database directory: %w", err)
		}
		serverPath := filepath.Join(dbDir, "server.db")
		store, err = OpenSQLite(serverPath, pool.maxOpen)
		if err != nil {
			return fmt.Errorf("failed to open server database: %w", err)
		}
		store.readDB.SetMaxIdleConns(pool.maxIdle)
		store.readDB.SetConnMaxLifetime(pool.maxLifetime)
		store.readDB.SetConnMaxIdleTime(pool.maxIdleTime)
		displayPath = serverPath
	default:
		return fmt.Errorf("unsupported database driver: %s", dbCfg.Driver)
	}

	// Keep the remote driver on its configured pool; local SQLite uses
	// separate query-only readers and a one-connection immediate writer.
	var db *sql.DB
	if driver == "sqlite" {
		db = store.writeDB
	} else {
		db, err = sql.Open(driver, dsn)
		if err != nil {
			return fmt.Errorf("failed to open server database: %w", err)
		}
		db.SetMaxOpenConns(pool.maxOpen)
		db.SetMaxIdleConns(pool.maxIdle)
		db.SetConnMaxLifetime(pool.maxLifetime)
		db.SetConnMaxIdleTime(pool.maxIdleTime)
	}

	var cache *libSQLCache
	var cachePath string
	var cacheInterval, cacheMaxAge time.Duration
	var cacheOpenErr error
	var cacheOfflineMode bool
	if driver == "libsql" && dbCfg.Cache.Enabled {
		normalized, warnings := dbCfg.Cache.Normalized()
		for _, warning := range warnings {
			slog.Warn(warning)
		}
		cacheInterval, _ = time.ParseDuration(normalized.SyncInterval)
		cacheMaxAge, _ = time.ParseDuration(normalized.MaxAge)
		cacheOfflineMode = normalized.OfflineMode
		cachePath = normalized.Path
		if !filepath.IsAbs(cachePath) {
			cachePath = filepath.Join(dataDir, cachePath)
		}
		cache, cacheOpenErr = initializeLibSQLCache(cachePath, pool.maxOpen, cacheInterval, cacheMaxAge, cacheOfflineMode, db)
	}

	// Test the remote connection. A recent cache can serve read-only data
	// while an enabled offline cache or restored database waits for connectivity.
	if err := db.Ping(); err != nil {
		var restoreCacheErr error
		if cache != nil && cache.offlineMode && cache.fresh(time.Now().UTC()) {
			cache.offlineOnly = true
			mu.Lock()
			serverDB = db
			serverStore = nil
			serverDriver = driver
			serverCache = cache
			mu.Unlock()
			startConnStatsSampler(db)
			go cache.run()
			slog.Warn("database: remote unavailable; serving fresh SQLite cache read-only", "error", err)
			return nil
		}
		restorePath := filepath.Join(paths.GetDatabaseDir(dataDir), "server.db")
		if cacheOfflineMode && cachePath != "" {
			if cache != nil {
				_ = cache.store.Close()
				cache = nil
			}
			restoreCacheErr = installRestoredSQLiteCache(restorePath, cachePath)
			if restoreCacheErr == nil {
				cache, cacheOpenErr = initializeLibSQLCache(cachePath, pool.maxOpen, cacheInterval, cacheMaxAge, cacheOfflineMode, db)
				if cacheOpenErr == nil {
					cache.restoreSeedPath = restorePath
					cache.offlineOnly = true
					if markErr := cache.markSynced(context.Background(), time.Now().UTC()); markErr == nil && cache.fresh(time.Now().UTC()) {
						mu.Lock()
						serverDB = db
						serverStore = nil
						serverDriver = driver
						serverCache = cache
						mu.Unlock()
						startConnStatsSampler(db)
						go cache.run()
						slog.Warn("database: remote unavailable; serving restored SQLite backup read-only", "error", err)
						return nil
					} else if markErr != nil {
						restoreCacheErr = fmt.Errorf("mark restored cache fresh: %w", markErr)
					}
					_ = cache.store.Close()
					cache = nil
				}
			}
		}
		if cache != nil {
			_ = cache.store.Close()
		}
		_ = db.Close()
		if cacheOpenErr != nil {
			return fmt.Errorf("failed to ping server database: %w (libsql cache unavailable: %v)", err, cacheOpenErr)
		}
		if restoreCacheErr != nil {
			return fmt.Errorf("failed to ping server database: %w (restored cache unavailable: %v)", err, restoreCacheErr)
		}
		return fmt.Errorf("failed to ping server database: %w", err)
	}
	if dbCfg.Cache.Enabled && cache == nil {
		if cacheOpenErr != nil {
			if resetErr := resetSQLiteCacheFiles(cachePath); resetErr != nil {
				_ = db.Close()
				return fmt.Errorf("libsql cache is unusable and could not be reset: %w (initial error: %v)", resetErr, cacheOpenErr)
			}
		}
		cache, cacheOpenErr = initializeLibSQLCache(cachePath, pool.maxOpen, cacheInterval, cacheMaxAge, cacheOfflineMode, db)
		if cacheOpenErr != nil {
			_ = db.Close()
			return fmt.Errorf("failed to initialize libsql cache: %w", cacheOpenErr)
		}
	}

	slog.Info("database: initialized", "driver", driver, "path", displayPath)

	// Create schema
	if store != nil {
		if err := store.Write(context.Background(), func(tx *sql.Tx) error {
			return createSchema(tx, driver)
		}); err != nil {
			_ = store.Close()
			return fmt.Errorf("failed to create schema: %w", err)
		}
	} else if err := createSchema(db, driver); err != nil {
		if cache != nil {
			_ = cache.store.Close()
		}
		_ = db.Close()
		return fmt.Errorf("failed to create schema: %w", err)
	}
	if driver == "libsql" {
		seedPath := filepath.Join(paths.GetDatabaseDir(dataDir), "server.db")
		if err := seedRestoredSQLite(context.Background(), db, seedPath); err != nil {
			if cache != nil {
				_ = cache.store.Close()
			}
			_ = db.Close()
			return fmt.Errorf("failed to seed empty remote database from restored server.db: %w", err)
		}
	}
	if cache != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 2*cache.interval)
		err := cache.syncAndMark(ctx)
		cancel()
		if err != nil {
			_ = cache.store.Close()
			_ = db.Close()
			return fmt.Errorf("failed to initialize libsql cache: %w", err)
		}
	}

	mu.Lock()
	serverDB = db
	serverStore = store
	serverDriver = driver
	serverCache = cache
	mu.Unlock()

	// Publish the connection-pool gauges required by AI.md PART 20. The
	// sampler is owned by this package and stopped in Close().
	if store != nil {
		startStoreConnStatsSampler(store)
	} else {
		startConnStatsSampler(db)
	}
	startOwnerTokenUsageFlusher(db)
	if cache != nil {
		go cache.run()
	}

	return nil
}

// GetServerDB returns the server database query handle. Local SQLite returns
// the query-only pool; mutations must go through database operations.
func GetServerDB() *sql.DB {
	mu.RLock()
	defer mu.RUnlock()
	if serverStore != nil {
		return serverStore.readDB
	}
	return serverDB
}

// ServerDBProbe implements the health-check probe without exposing writable
// database handles to callers.
type ServerDBProbe struct{}

func (ServerDBProbe) PingContext(ctx context.Context) error {
	return ProbeServerDB(ctx)
}

// ProbeServerDB runs a trivial query against the write and read handles, as
// required by the PART 13 health check and PART 17 database notification.
func ProbeServerDB(ctx context.Context) error {
	mu.RLock()
	store, db := serverStore, serverDB
	mu.RUnlock()
	probe := func(handle *sql.DB) error {
		if handle == nil {
			return errors.New("database handle is unavailable")
		}
		probeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		var result int
		if err := handle.QueryRowContext(probeCtx, "SELECT 1").Scan(&result); err != nil {
			return err
		}
		if result != 1 {
			return errors.New("database probe returned an unexpected result")
		}
		return nil
	}
	if store != nil {
		if err := probe(store.writeDB); err != nil {
			return fmt.Errorf("write handle: %w", err)
		}
		if err := probe(store.readDB); err != nil {
			return fmt.Errorf("read handle: %w", err)
		}
		return nil
	}
	if err := probe(db); err != nil {
		return fmt.Errorf("remote handle: %w", err)
	}
	return probe(db)
}

func isServerDatabaseHandle(db *sql.DB) bool {
	mu.RLock()
	defer mu.RUnlock()
	return db != nil && (db == serverDB || (serverStore != nil && db == serverStore.readDB) || (serverCache != nil && (db == serverCache.store.readDB || db == serverCache.store.writeDB)))
}

// GetStore returns the database store for operations that require explicit read/write routing.
func GetStore() *Store {
	mu.RLock()
	defer mu.RUnlock()
	return serverStore
}

// Close closes the database connection
func Close() error {
	usageErr := stopOwnerTokenUsageFlusher()
	// Stop the pool sampler before taking the lock so the goroutine can
	// never be blocked on a database call while Close waits for it.
	stopConnStatsSampler()

	mu.Lock()
	defer mu.Unlock()

	if serverDB != nil {
		db := serverDB
		serverDB = nil
		store := serverStore
		serverStore = nil
		cache := serverCache
		serverCache = nil
		serverDriver = ""
		var err error
		if cache != nil {
			if cacheErr := cache.close(); cacheErr != nil {
				err = fmt.Errorf("libsql cache: %w", cacheErr)
			}
		}
		if store != nil {
			if closeErr := store.Close(); closeErr != nil {
				err = errors.Join(err, closeErr)
			}
		} else {
			if closeErr := db.Close(); closeErr != nil {
				err = errors.Join(err, closeErr)
			}
		}
		if err != nil {
			if usageErr != nil {
				return fmt.Errorf("server db: %v; owner token usage flush: %w", err, usageErr)
			}
			return fmt.Errorf("server db: %w", err)
		}
	}
	if usageErr != nil {
		return fmt.Errorf("owner token usage flush: %w", usageErr)
	}
	return nil
}

// createSchema creates all required database tables. It takes the handle
// explicitly because it runs before the package-level connection is
// published, so it cannot read it back through GetServerDB.
func createSchema(db interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, driver ...string) error {
	dbDriver := "sqlite"
	if len(driver) > 0 && driver[0] != "" {
		dbDriver = normalizeDriver(driver[0])
	}
	// Create server.db schema
	if err := createServerSchema(db, dbDriver); err != nil {
		return fmt.Errorf("server schema: %w", err)
	}

	return nil
}

// createServerSchema creates tables in server.db
func createServerSchema(db interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, driver string) error {
	schema := `
	-- Config key-value storage (mirrors the YAML structure as flat dot-notation keys)
	CREATE TABLE IF NOT EXISTS config (
		key TEXT PRIMARY KEY,
		value TEXT NOT NULL,
		type TEXT NOT NULL DEFAULT 'string',
		updated_at INTEGER NOT NULL DEFAULT (strftime('%s', 'now'))
	);
	CREATE INDEX IF NOT EXISTS idx_config_key_prefix ON config(key);

	-- Config metadata for change detection (single row)
	CREATE TABLE IF NOT EXISTS config_meta (
		id INTEGER PRIMARY KEY CHECK (id = 1),
		version INTEGER NOT NULL DEFAULT 1,
		updated_at INTEGER NOT NULL DEFAULT (strftime('%s', 'now'))
	);
	INSERT OR IGNORE INTO config_meta (id, version) VALUES (1, 1);

	-- Bump the config version on every config mutation so watchers detect changes
	CREATE TRIGGER IF NOT EXISTS config_version_bump_insert
	AFTER INSERT ON config
	BEGIN
		UPDATE config_meta SET version = version + 1, updated_at = strftime('%s', 'now') WHERE id = 1;
	END;
	CREATE TRIGGER IF NOT EXISTS config_version_bump_update
	AFTER UPDATE ON config
	BEGIN
		UPDATE config_meta SET version = version + 1, updated_at = strftime('%s', 'now') WHERE id = 1;
	END;
	CREATE TRIGGER IF NOT EXISTS config_version_bump_delete
	AFTER DELETE ON config
	BEGIN
		UPDATE config_meta SET version = version + 1, updated_at = strftime('%s', 'now') WHERE id = 1;
	END;

	-- Project-level cryptographic secrets, base64-encoded, never logged
	CREATE TABLE IF NOT EXISTS app_secrets (
		name TEXT PRIMARY KEY,
		value TEXT NOT NULL,
		previous_value TEXT,
		previous_expires_at INTEGER,
		created_at INTEGER NOT NULL DEFAULT (strftime('%s', 'now')),
		rotated_at INTEGER
	);

	-- Anonymous resource owners; there are no profiles, logins or passwords.
	CREATE TABLE IF NOT EXISTS owners (
		id TEXT PRIMARY KEY,
		created_at INTEGER NOT NULL DEFAULT (strftime('%s', 'now')),
		last_seen_at INTEGER NOT NULL DEFAULT (strftime('%s', 'now'))
	);

	-- Owner and linked credentials. The operator server.token is never stored here.
	CREATE TABLE IF NOT EXISTS api_tokens (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		owner_id TEXT NOT NULL REFERENCES owners(id) ON DELETE CASCADE,
		kind TEXT NOT NULL CHECK (kind IN ('primary', 'linked')),
		name TEXT,
		token_hash TEXT NOT NULL UNIQUE,
		token_prefix TEXT NOT NULL,
		token_last4 TEXT NOT NULL DEFAULT '',
		token_enc TEXT NOT NULL DEFAULT '',
		created_at INTEGER NOT NULL DEFAULT (strftime('%s', 'now')),
		rotated_at INTEGER,
		expires_at INTEGER,
		last_used_at INTEGER,
		revoked_at INTEGER,
		revoked_reason TEXT
	);

	-- Audit log (admin actions, config changes, security events)
	CREATE TABLE IF NOT EXISTS audit_log (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		timestamp DATETIME DEFAULT CURRENT_TIMESTAMP,
		event TEXT NOT NULL,
		actor TEXT,
		ip_address TEXT,
		source TEXT NOT NULL DEFAULT '',
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

	-- Coordinated vulnerability disclosure metadata and encrypted content.
	CREATE TABLE IF NOT EXISTS security_reports (
		tracking_id TEXT PRIMARY KEY,
		id_window INTEGER NOT NULL,
		severity TEXT NOT NULL,
		component TEXT NOT NULL,
		status TEXT NOT NULL DEFAULT 'received',
		body_enc BLOB NOT NULL,
		body_alg TEXT NOT NULL,
		contact_enc BLOB,
		credit_pref TEXT NOT NULL DEFAULT 'no',
		credit_name TEXT,
		researcher_comment TEXT NOT NULL DEFAULT '',
		disclosure_date TEXT,
		cve_requested BOOLEAN NOT NULL DEFAULT FALSE,
		token_hash TEXT NOT NULL,
		token_last_used TEXT,
		created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
		updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
		closed_at TEXT
	);
	CREATE INDEX IF NOT EXISTS idx_security_reports_status ON security_reports(status, created_at);
	CREATE TABLE IF NOT EXISTS security_thanks (
		tracking_id TEXT PRIMARY KEY,
		credit TEXT NOT NULL,
		severity TEXT NOT NULL,
		disclosed_on TEXT NOT NULL
	);
	`
	if normalizeDriver(driver) == "libsql" {
		schema = prefixServerSchema(schema)
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeoutSchema)
	defer cancel()

	_, err := execContext(ctx, db, schema)
	if err != nil {
		return fmt.Errorf("failed to create server schema: %w", err)
	}
	rateLimitsTable := tableNameForDriver(driver, "rate_limits")
	rateLimitsIndex := tableNameForDriver(driver, "idx_rate_limits_window")
	rateLimitSchema := fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
		key TEXT PRIMARY KEY,
		count INTEGER NOT NULL DEFAULT 1,
		prev_count INTEGER NOT NULL DEFAULT 0,
		window_start INTEGER NOT NULL DEFAULT (strftime('%%s', 'now')),
		updated_at INTEGER NOT NULL DEFAULT (strftime('%%s', 'now'))
	);
	CREATE INDEX IF NOT EXISTS %s ON %s(window_start);`, rateLimitsTable, rateLimitsIndex, rateLimitsTable)
	if _, err := execContext(ctx, db, rateLimitSchema); err != nil {
		return fmt.Errorf("failed to create rate-limit schema: %w", err)
	}
	if err := migrateOwnerTokenSchema(ctx, db, driver); err != nil {
		return fmt.Errorf("failed to migrate owner token schema: %w", err)
	}
	if err := migrateAuditSourceColumn(ctx, db, driver); err != nil {
		return fmt.Errorf("failed to migrate audit source column: %w", err)
	}

	slog.Info("database: server schema created/verified")
	return nil
}

func prefixServerSchema(schema string) string {
	objects := []string{
		"config", "config_meta", "app_secrets", "owners", "api_tokens", "audit_log",
		"scheduler_tasks", "scheduler_history", "backups", "security_reports", "security_thanks",
		"idx_config_key_prefix", "config_version_bump_insert", "config_version_bump_update", "config_version_bump_delete",
		"idx_audit_timestamp", "idx_audit_actor", "idx_scheduler_next_run", "idx_scheduler_locked",
		"idx_scheduler_history_task", "idx_backups_created", "idx_security_reports_status",
	}
	for _, object := range objects {
		pattern := regexp.MustCompile(`\b` + regexp.QuoteMeta(object) + `\b`)
		schema = pattern.ReplaceAllString(schema, tableNameForDriver("libsql", object))
	}
	return schema
}

// migrateOwnerTokenSchema adds the owner-token columns to databases created
// before anonymous owners were introduced. Unattributed legacy credentials
// are revoked because their old resource binding cannot safely establish an
// owner identity.
func migrateOwnerTokenSchema(ctx context.Context, db interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, driver string) error {
	tokensTable := tableNameForDriver(driver, "api_tokens")
	ownersTable := tableNameForDriver(driver, "owners")
	rows, err := queryContext(ctx, db, "PRAGMA table_info("+tokensTable+")")
	if err != nil {
		return err
	}
	columns := make(map[string]bool)
	for rows.Next() {
		var cid, notNull, primaryKey int
		var name, columnType string
		var defaultValue sql.NullString
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
			return errors.Join(err, rows.Close())
		}
		columns[name] = true
	}
	if err := rows.Err(); err != nil {
		return errors.Join(err, rows.Close())
	}
	if err := rows.Close(); err != nil {
		return err
	}
	if !columns["owner_id"] {
		var legacyRows int
		if err := queryRowContext(ctx, db, `SELECT COUNT(*) FROM `+tokensTable).Scan(&legacyRows); err != nil {
			return err
		}
		if legacyRows > 0 {
			return fmt.Errorf("legacy API token rows have no owner identity and cannot be migrated safely")
		}
		if _, err := execContext(ctx, db, `DROP TABLE `+tokensTable); err != nil {
			return err
		}
		if _, err := execContext(ctx, db, `CREATE TABLE `+tokensTable+` (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			owner_id TEXT NOT NULL REFERENCES `+ownersTable+`(id) ON DELETE CASCADE,
			kind TEXT NOT NULL CHECK (kind IN ('primary', 'linked')),
			name TEXT,
			token_hash TEXT NOT NULL UNIQUE,
			token_prefix TEXT NOT NULL,
			token_last4 TEXT NOT NULL,
			token_enc TEXT NOT NULL,
			created_at INTEGER NOT NULL DEFAULT (strftime('%s', 'now')),
			rotated_at INTEGER,
			expires_at INTEGER,
			last_used_at INTEGER,
			revoked_at INTEGER,
			revoked_reason TEXT
		)`); err != nil {
			return err
		}
		columns = map[string]bool{"owner_id": true, "kind": true, "name": true, "token_last4": true, "token_enc": true, "rotated_at": true}
	}
	additions := []struct{ name, definition string }{
		{"kind", "TEXT NOT NULL DEFAULT 'primary'"},
		{"name", "TEXT"},
		{"token_last4", "TEXT NOT NULL DEFAULT ''"},
		{"token_enc", "TEXT NOT NULL DEFAULT ''"},
		{"rotated_at", "INTEGER"},
	}
	for _, column := range additions {
		if columns[column.name] {
			continue
		}
		if _, err := execContext(ctx, db, "ALTER TABLE "+tokensTable+" ADD COLUMN "+column.name+" "+column.definition); err != nil {
			return err
		}
	}
	if _, err := execContext(ctx, db, "UPDATE "+tokensTable+" SET revoked_at = COALESCE(revoked_at, strftime('%s','now')) WHERE owner_id IS NULL OR token_enc = ''"); err != nil {
		return err
	}
	indexes := []string{
		"CREATE INDEX IF NOT EXISTS " + tableNameForDriver(driver, "idx_api_tokens_hash") + " ON " + tokensTable + "(token_hash)",
		"CREATE INDEX IF NOT EXISTS " + tableNameForDriver(driver, "idx_api_tokens_prefix") + " ON " + tokensTable + "(token_prefix)",
		"CREATE INDEX IF NOT EXISTS " + tableNameForDriver(driver, "idx_api_tokens_owner") + " ON " + tokensTable + "(owner_id)",
		"CREATE INDEX IF NOT EXISTS " + tableNameForDriver(driver, "idx_api_tokens_active") + " ON " + tokensTable + "(revoked_at) WHERE revoked_at IS NULL",
		"CREATE UNIQUE INDEX IF NOT EXISTS " + tableNameForDriver(driver, "idx_api_tokens_primary") + " ON " + tokensTable + "(owner_id) WHERE kind = 'primary' AND revoked_at IS NULL",
		"CREATE UNIQUE INDEX IF NOT EXISTS " + tableNameForDriver(driver, "idx_api_tokens_owner_name") + " ON " + tokensTable + "(owner_id, name) WHERE kind = 'linked'",
	}
	for _, statement := range indexes {
		if _, err := execContext(ctx, db, statement); err != nil {
			return err
		}
	}
	return nil
}

// migrateAuditSourceColumn adds the top-level source discriminator used by
// API, MCP, scheduler, and operator audit events.
func migrateAuditSourceColumn(ctx context.Context, db interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, driver string) error {
	auditTable := tableNameForDriver(driver, "audit_log")
	rows, err := queryContext(ctx, db, "PRAGMA table_info("+auditTable+")")
	if err != nil {
		return err
	}
	found := false
	for rows.Next() {
		var cid, notNull, primaryKey int
		var name, columnType string
		var defaultValue sql.NullString
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
			return errors.Join(err, rows.Close())
		}
		if name == "source" {
			found = true
		}
	}
	if err := rows.Err(); err != nil {
		return errors.Join(err, rows.Close())
	}
	if err := rows.Close(); err != nil {
		return err
	}
	if found {
		return nil
	}
	_, err = execContext(ctx, db, "ALTER TABLE "+auditTable+" ADD COLUMN source TEXT NOT NULL DEFAULT ''")
	return err
}
