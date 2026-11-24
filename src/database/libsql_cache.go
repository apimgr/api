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
	"sort"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

const cacheStateTable = "_api_cache_state"

var (
	sqlTableRewriteOnce  sync.Once
	sqlTableRewriteRules []sqlRewriteRule
)

type sqlRewriteRule struct {
	pattern *regexp.Regexp
	name    string
}

type libSQLCache struct {
	store       *Store
	remote      *sql.DB
	interval    time.Duration
	maxAge      time.Duration
	offlineMode bool
	offlineOnly bool
	restoreSeedPath string
	stop        chan struct{}
	done        chan struct{}
	mu          sync.RWMutex
	lastSync    time.Time
}

var serverCache *libSQLCache

func getServerCache() *libSQLCache {
	mu.RLock()
	defer mu.RUnlock()
	return serverCache
}

func CacheReadOnlyOffline() bool {
	cache := getServerCache()
	if cache == nil || !cache.offlineMode || !cache.fresh(time.Now().UTC()) {
		return false
	}
	cache.mu.RLock()
	offline := cache.offlineOnly
	cache.mu.RUnlock()
	return offline
}

func refreshServerCacheAfterWrite(ctx context.Context, handle interface{}) {
	db, ok := handle.(*sql.DB)
	if !ok || !isServerDatabaseHandle(db) {
		return
	}
	cache := getServerCache()
	if cache == nil {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, 2*cache.interval)
	defer cancel()
	if err := cache.syncAndMark(ctx); err != nil {
		cache.invalidateFreshness(ctx)
		slog.Warn("libsql cache refresh after write failed", "error", err)
	}
}

func openLibSQLCache(path string, readers int, interval, maxAge time.Duration, offlineMode bool) (*libSQLCache, error) {
	if path == "" {
		return nil, errors.New("libsql cache path is empty")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return nil, fmt.Errorf("create libsql cache directory: %w", err)
	}
	store, err := OpenSQLite(path, readers)
	if err != nil {
		return nil, err
	}
	cache := &libSQLCache{
		store:       store,
		interval:    interval,
		maxAge:      maxAge,
		offlineMode: offlineMode,
		stop:        make(chan struct{}),
		done:        make(chan struct{}),
	}
	if _, err := store.writeDB.Exec(`CREATE TABLE IF NOT EXISTS ` + cacheStateTable + ` (id INTEGER PRIMARY KEY CHECK (id = 1), synced_at INTEGER NOT NULL)`); err != nil {
		_ = store.Close()
		return nil, fmt.Errorf("create libsql cache state: %w", err)
	}
	var syncedAt int64
	err = store.readDB.QueryRow(`SELECT synced_at FROM ` + cacheStateTable + ` WHERE id = 1`).Scan(&syncedAt)
	if err == nil {
		cache.lastSync = time.Unix(syncedAt, 0).UTC()
	} else if !errors.Is(err, sql.ErrNoRows) {
		_ = store.Close()
		return nil, fmt.Errorf("read libsql cache state: %w", err)
	}
	return cache, nil
}

func initializeLibSQLCache(path string, readers int, interval, maxAge time.Duration, offlineMode bool, remote *sql.DB) (*libSQLCache, error) {
	cache, err := openLibSQLCache(path, readers, interval, maxAge, offlineMode)
	if err != nil {
		return nil, err
	}
	cache.remote = remote
	if err := cache.store.Write(context.Background(), func(tx *sql.Tx) error {
		return createSchema(tx, "sqlite")
	}); err != nil {
		_ = cache.store.Close()
		return nil, fmt.Errorf("create cache schema: %w", err)
	}
	return cache, nil
}

func installRestoredSQLiteCache(sourcePath, cachePath string) error {
	info, err := os.Stat(sourcePath)
	if err != nil {
		return fmt.Errorf("restored server.db unavailable: %w", err)
	}
	if info.IsDir() {
		return fmt.Errorf("restored server.db %q is a directory", sourcePath)
	}
	same, err := sameFilePath(sourcePath, cachePath)
	if err != nil {
		return fmt.Errorf("resolve restored cache paths: %w", err)
	}
	if same {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(cachePath), 0755); err != nil {
		return fmt.Errorf("create cache directory: %w", err)
	}
	source, err := sql.Open("sqlite", "file:"+filepath.ToSlash(sourcePath)+"?mode=ro")
	if err != nil {
		return fmt.Errorf("open restored server.db: %w", err)
	}
	defer func() { _ = source.Close() }()
	if err := source.Ping(); err != nil {
		return fmt.Errorf("read restored server.db: %w", err)
	}
	tables, err := sqliteTableNames(context.Background(), source)
	if err != nil {
		return fmt.Errorf("inspect restored server.db: %w", err)
	}
	if len(tables) == 0 {
		return errors.New("restored server.db contains no tables")
	}
	temporary, err := os.CreateTemp(filepath.Dir(cachePath), ".cache-restore-*.db")
	if err != nil {
		return fmt.Errorf("create restored cache snapshot: %w", err)
	}
	temporaryPath := temporary.Name()
	if err := temporary.Close(); err != nil {
		_ = os.Remove(temporaryPath)
		return err
	}
	if err := os.Remove(temporaryPath); err != nil {
		return err
	}
	if _, err := source.Exec(`VACUUM INTO ?`, temporaryPath); err != nil {
		_ = os.Remove(temporaryPath)
		return fmt.Errorf("snapshot restored server.db: %w", err)
	}
	if err := os.Chmod(temporaryPath, 0600); err != nil {
		_ = os.Remove(temporaryPath)
		return fmt.Errorf("secure restored cache snapshot: %w", err)
	}
	if err := resetSQLiteCacheFiles(cachePath); err != nil {
		_ = os.Remove(temporaryPath)
		return fmt.Errorf("replace unusable cache: %w", err)
	}
	if err := os.Rename(temporaryPath, cachePath); err != nil {
		_ = os.Remove(temporaryPath)
		return fmt.Errorf("install restored cache snapshot: %w", err)
	}
	return nil
}

func sameFilePath(left, right string) (bool, error) {
	leftAbs, err := filepath.Abs(left)
	if err != nil {
		return false, err
	}
	rightAbs, err := filepath.Abs(right)
	if err != nil {
		return false, err
	}
	return filepath.Clean(leftAbs) == filepath.Clean(rightAbs), nil
}

func resetSQLiteCacheFiles(path string) error {
	for _, candidate := range []string{path, path + "-wal", path + "-shm"} {
		if err := os.Remove(candidate); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}

func (cache *libSQLCache) fresh(now time.Time) bool {
	if cache == nil {
		return false
	}
	cache.mu.RLock()
	last := cache.lastSync
	cache.mu.RUnlock()
	return !last.IsZero() && now.Sub(last) <= cache.maxAge
}

func (cache *libSQLCache) markSynced(ctx context.Context, at time.Time) error {
	if _, err := cache.store.writeDB.ExecContext(ctx, `INSERT INTO `+cacheStateTable+` (id, synced_at) VALUES (1, ?) ON CONFLICT(id) DO UPDATE SET synced_at = excluded.synced_at`, at.Unix()); err != nil {
		return err
	}
	cache.mu.Lock()
	cache.lastSync = at
	cache.mu.Unlock()
	return nil
}

func (cache *libSQLCache) invalidateFreshness(ctx context.Context) {
	cache.mu.Lock()
	cache.lastSync = time.Time{}
	cache.mu.Unlock()
	if _, err := cache.store.writeDB.ExecContext(ctx, `DELETE FROM `+cacheStateTable+` WHERE id = 1`); err != nil {
		slog.Warn("libsql cache freshness could not be invalidated", "error", err)
	}
}

func (cache *libSQLCache) sync(ctx context.Context) error {
	if cache == nil || cache.remote == nil || cache.store == nil {
		return errors.New("libsql cache is unavailable")
	}
	ctx, cancel := context.WithTimeout(ctx, 2*cache.interval)
	defer cancel()
	if cache.restoreSeedPath != "" {
		if err := seedRestoredSQLite(ctx, cache.remote, cache.restoreSeedPath); err != nil {
			return fmt.Errorf("seed restored database into empty remote: %w", err)
		}
	}
	remoteTx, err := cache.remote.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return err
	}
	defer func() { _ = remoteTx.Rollback() }()
	tables, err := remoteApplicationTables(ctx, remoteTx)
	if err != nil {
		return err
	}
	if err := cache.store.Write(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `PRAGMA defer_foreign_keys = ON`); err != nil {
			return err
		}
		for _, table := range tables {
			localTable := localTableName(table)
			if err := ensureCacheTableSchema(ctx, tx, remoteTx, table, localTable); err != nil {
				return fmt.Errorf("sync cache schema %s: %w", table, err)
			}
			if err := copyRemoteTable(ctx, tx, remoteTx, table, localTable); err != nil {
				return fmt.Errorf("sync cache table %s: %w", table, err)
			}
		}
		return nil
	}); err != nil {
		return err
	}
	return remoteTx.Commit()
}

func ensureCacheTableSchema(ctx context.Context, target *sql.Tx, source interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, table, localTable string) error {
	var createStatement sql.NullString
	if err := source.QueryRowContext(ctx, `SELECT sql FROM sqlite_master WHERE type = 'table' AND name = ?`, table).Scan(&createStatement); err != nil {
		return err
	}
	var exists int
	if err := target.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?`, localTable).Scan(&exists); err != nil {
		return err
	}
	if exists == 0 {
		if !createStatement.Valid || strings.TrimSpace(createStatement.String) == "" {
			return fmt.Errorf("remote schema for %s is unavailable", table)
		}
		if _, err := target.ExecContext(ctx, sqliteCacheSQL(createStatement.String)); err != nil {
			return err
		}
	}
	indexes, err := source.QueryContext(ctx, `SELECT name, sql FROM sqlite_master WHERE type = 'index' AND tbl_name = ? AND sql IS NOT NULL ORDER BY name`, table)
	if err != nil {
		return err
	}
	defer func() { _ = indexes.Close() }()
	for indexes.Next() {
		var name, statement string
		if err := indexes.Scan(&name, &statement); err != nil {
			return err
		}
		localIndex := localTableName(name)
		if !safeIdentifier(name) {
			return fmt.Errorf("invalid remote index name %q", name)
		}
		var count int
		if err := target.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type = 'index' AND name = ?`, localIndex).Scan(&count); err != nil {
			return err
		}
		if count == 0 {
			if _, err := target.ExecContext(ctx, sqliteCacheSQL(statement)); err != nil {
				return err
			}
		}
	}
	return indexes.Err()
}

func (cache *libSQLCache) syncAndMark(ctx context.Context) error {
	if err := cache.sync(ctx); err != nil {
		return err
	}
	if err := cache.markSynced(ctx, time.Now().UTC()); err != nil {
		return err
	}
	cache.mu.Lock()
	cache.offlineOnly = false
	cache.mu.Unlock()
	return nil
}

func (cache *libSQLCache) run() {
	ticker := time.NewTicker(cache.interval)
	defer ticker.Stop()
	defer close(cache.done)
	for {
		select {
		case <-cache.stop:
			return
		case <-ticker.C:
			ctx, cancel := context.WithTimeout(context.Background(), 2*cache.interval)
			if err := cache.syncAndMark(ctx); err != nil {
				slog.Warn("libsql cache synchronization failed", "error", err)
			}
			cancel()
		}
	}
}

func (cache *libSQLCache) close() error {
	if cache == nil {
		return nil
	}
	close(cache.stop)
	<-cache.done
	return cache.store.Close()
}

func remoteApplicationTables(ctx context.Context, remote interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}) ([]string, error) {
	rows, err := remote.QueryContext(ctx, `SELECT name FROM sqlite_master WHERE type = 'table' AND substr(name, 1, 4) = 'srv_' ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		if !safeIdentifier(name) {
			return nil, fmt.Errorf("unexpected remote table name %q", name)
		}
		names = append(names, name)
	}
	return names, rows.Err()
}

func copyRemoteTable(ctx context.Context, target *sql.Tx, source interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}, table, localTable string) error {
	columns, err := tableColumns(ctx, source, table)
	if err != nil {
		return err
	}
	if len(columns) == 0 {
		return nil
	}
	quotedTable := quoteIdentifier(table)
	quotedLocalTable := quoteIdentifier(localTable)
	quotedColumns := make([]string, len(columns))
	placeholders := make([]string, len(columns))
	for i, column := range columns {
		quotedColumns[i] = quoteIdentifier(column)
		placeholders[i] = "?"
	}
	if _, err := target.ExecContext(ctx, `DELETE FROM `+quotedLocalTable); err != nil {
		return err
	}
	rows, err := source.QueryContext(ctx, `SELECT `+strings.Join(quotedColumns, ",")+` FROM `+quotedTable)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	statement, err := target.PrepareContext(ctx, `INSERT INTO `+quotedLocalTable+` (`+strings.Join(quotedColumns, ",")+") VALUES ("+strings.Join(placeholders, ",")+")")
	if err != nil {
		return err
	}
	defer func() { _ = statement.Close() }()
	for rows.Next() {
		values := make([]any, len(columns))
		dest := make([]any, len(columns))
		for i := range values {
			dest[i] = &values[i]
		}
		if err := rows.Scan(dest...); err != nil {
			return err
		}
		for i, value := range values {
			if bytes, ok := value.([]byte); ok {
				values[i] = append([]byte(nil), bytes...)
			}
		}
		if _, err := statement.ExecContext(ctx, values...); err != nil {
			return err
		}
	}
	return rows.Err()
}

func tableColumns(ctx context.Context, db interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}, table string) ([]string, error) {
	if !safeIdentifier(table) {
		return nil, fmt.Errorf("invalid table name %q", table)
	}
	rows, err := db.QueryContext(ctx, `PRAGMA table_info(`+quoteIdentifier(table)+`)`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var columns []string
	for rows.Next() {
		var cid, notNull, primaryKey int
		var name, columnType string
		var defaultValue sql.NullString
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
			return nil, err
		}
		if !safeIdentifier(name) {
			return nil, fmt.Errorf("invalid column name %q in %s", name, table)
		}
		columns = append(columns, name)
	}
	return columns, rows.Err()
}

func seedRestoredSQLite(ctx context.Context, remote *sql.DB, sourcePath string) error {
	info, err := os.Stat(sourcePath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	if info.IsDir() {
		return fmt.Errorf("restore seed source %q is a directory", sourcePath)
	}
	empty, err := remoteDatabaseEmpty(ctx, remote)
	if err != nil || !empty {
		return err
	}
	source, err := sql.Open("sqlite", "file:"+filepath.ToSlash(sourcePath)+"?mode=ro")
	if err != nil {
		return fmt.Errorf("open restore seed source: %w", err)
	}
	defer func() { _ = source.Close() }()
	if err := source.PingContext(ctx); err != nil {
		return fmt.Errorf("read restore seed source: %w", err)
	}
	sourceTables, err := sqliteTableNames(ctx, source)
	if err != nil {
		return err
	}
	sort.SliceStable(sourceTables, func(i, j int) bool {
		return restoreTableOrder(sourceTables[i]) < restoreTableOrder(sourceTables[j])
	})
	return withWriteTransaction(ctx, remote, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `PRAGMA defer_foreign_keys = ON`); err != nil {
			return err
		}
		for _, name := range sourceTables {
			if strings.HasPrefix(name, "sqlite_") || name == cacheStateTable {
				continue
			}
			target := name
			if !strings.HasPrefix(target, "srv_") {
				target = tableNameForDriver("libsql", target)
			}
			if err := ensureRestoredRemoteTable(ctx, tx, target); err != nil {
				return fmt.Errorf("prepare restored table %s: %w", target, err)
			}
			exists, err := remoteTableExists(ctx, tx, target)
			if err != nil {
				return err
			}
			if !exists {
				continue
			}
			if err := seedTable(ctx, tx, source, name, target); err != nil {
				return fmt.Errorf("seed remote table %s: %w", target, err)
			}
		}
		return nil
	})
}

func ensureRestoredRemoteTable(ctx context.Context, tx *sql.Tx, table string) error {
	if !safeIdentifier(table) || !strings.HasPrefix(table, "srv_") {
		return fmt.Errorf("invalid restored remote table %q", table)
	}
	switch strings.TrimPrefix(table, "srv_") {
	case "captcha_challenges":
		if _, err := tx.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS srv_captcha_challenges (
			id VARCHAR(32) PRIMARY KEY,
			answer_hash VARCHAR(64) NOT NULL,
			expires_at TIMESTAMP NOT NULL
		)`); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `CREATE INDEX IF NOT EXISTS srv_idx_captcha_challenges_expires ON srv_captcha_challenges(expires_at)`)
		return err
	case "security_pgp_metadata":
		_, err := tx.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS srv_security_pgp_metadata (
			singleton INTEGER PRIMARY KEY CHECK (singleton = 1),
			fingerprint TEXT NOT NULL,
			created_at TEXT NOT NULL,
			expires_at TEXT NOT NULL,
			last_rotated_at TEXT,
			keyservers_published TEXT NOT NULL DEFAULT '{}',
			revoked BOOLEAN NOT NULL DEFAULT FALSE
		)`)
		return err
	case "security_pgp_exports":
		_, err := tx.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS srv_security_pgp_exports (
			actor TEXT PRIMARY KEY,
			last_export_at TEXT NOT NULL
		)`)
		return err
	default:
		return nil
	}
}

func restoreTableOrder(name string) int {
	switch strings.TrimPrefix(name, "srv_") {
	case "owners":
		return 0
	case "api_tokens":
		return 1
	default:
		return 2
	}
}

func remoteDatabaseEmpty(ctx context.Context, remote *sql.DB) (bool, error) {
	tables, err := remoteApplicationTables(ctx, remote)
	if err != nil {
		return false, err
	}
	for _, table := range tables {
		if table == "srv_config_meta" {
			continue
		}
		var count int64
		if err := remote.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+quoteIdentifier(table)).Scan(&count); err != nil {
			return false, err
		}
		if count != 0 {
			return false, nil
		}
	}
	return true, nil
}

func sqliteTableNames(ctx context.Context, db *sql.DB) ([]string, error) {
	rows, err := db.QueryContext(ctx, `SELECT name FROM sqlite_master WHERE type = 'table' ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		if !safeIdentifier(name) {
			return nil, fmt.Errorf("invalid SQLite table name %q", name)
		}
		names = append(names, name)
	}
	return names, rows.Err()
}

func seedTable(ctx context.Context, target *sql.Tx, source *sql.DB, sourceName, targetName string) error {
	sourceColumns, err := tableColumns(ctx, source, sourceName)
	if err != nil {
		return err
	}
	targetColumns, err := tableColumns(ctx, target, targetName)
	if err != nil {
		return err
	}
	targetSet := make(map[string]bool, len(targetColumns))
	for _, column := range targetColumns {
		targetSet[column] = true
	}
	var columns []string
	for _, column := range sourceColumns {
		if targetSet[column] {
			columns = append(columns, column)
		}
	}
	if len(columns) == 0 {
		return nil
	}
	quoted := make([]string, len(columns))
	placeholders := make([]string, len(columns))
	for i, column := range columns {
		quoted[i], placeholders[i] = quoteIdentifier(column), "?"
	}
	rows, err := source.QueryContext(ctx, `SELECT `+strings.Join(quoted, ",")+` FROM `+quoteIdentifier(sourceName))
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	statement, err := target.PrepareContext(ctx, `INSERT OR IGNORE INTO `+quoteIdentifier(targetName)+` (`+strings.Join(quoted, ",")+") VALUES ("+strings.Join(placeholders, ",")+")")
	if err != nil {
		return err
	}
	defer func() { _ = statement.Close() }()
	for rows.Next() {
		values := make([]any, len(columns))
		dest := make([]any, len(columns))
		for i := range values {
			dest[i] = &values[i]
		}
		if err := rows.Scan(dest...); err != nil {
			return err
		}
		if _, err := statement.ExecContext(ctx, values...); err != nil {
			return err
		}
	}
	return rows.Err()
}

func remoteTableExists(ctx context.Context, remote interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, name string) (bool, error) {
	var count int
	if err := remote.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?`, name).Scan(&count); err != nil {
		return false, err
	}
	return count == 1, nil
}

func safeIdentifier(value string) bool {
	if value == "" {
		return false
	}
	for _, char := range value {
		valid := char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' || char >= '0' && char <= '9' || char == '_'
		if !valid {
			return false
		}
	}
	return true
}

func localTableName(remoteName string) string {
	return strings.TrimPrefix(remoteName, "srv_")
}

func sqliteCacheSQL(query string) string {
	var rewritten strings.Builder
	rewritten.Grow(len(query))
	for i := 0; i < len(query); {
		if query[i] == '\'' {
			end := scanSQLQuoted(query, i, '\'')
			rewritten.WriteString(query[i:end])
			i = end
			continue
		}
		if i+1 < len(query) && query[i] == '-' && query[i+1] == '-' {
			end := i + 2
			for end < len(query) && query[end] != '\n' {
				end++
			}
			rewritten.WriteString(query[i:end])
			i = end
			continue
		}
		if i+1 < len(query) && query[i] == '/' && query[i+1] == '*' {
			end := strings.Index(query[i+2:], "*/")
			if end < 0 {
				rewritten.WriteString(query[i:])
				break
			}
			end += i + 4
			rewritten.WriteString(query[i:end])
			i = end
			continue
		}
		if closing, ok := sqlIdentifierQuote(query[i]); ok {
			end := scanSQLQuoted(query, i, closing)
			quoted := query[i:end]
			if len(quoted) >= 2 && strings.HasPrefix(quoted[1:len(quoted)-1], "srv_") {
				quoted = quoted[:1] + quoted[5:]
			}
			rewritten.WriteString(quoted)
			i = end
			continue
		}
		if isSQLIdentifierStart(query[i]) {
			end := i + 1
			for end < len(query) && isSQLIdentifierPart(query[end]) {
				end++
			}
			identifier := query[i:end]
			rewritten.WriteString(strings.TrimPrefix(identifier, "srv_"))
			i = end
			continue
		}
		rewritten.WriteByte(query[i])
		i++
	}
	return rewritten.String()
}

func scanSQLQuoted(query string, start int, closing byte) int {
	for i := start + 1; i < len(query); i++ {
		if query[i] != closing {
			continue
		}
		if i+1 < len(query) && query[i+1] == closing {
			i++
			continue
		}
		return i + 1
	}
	return len(query)
}

func sqlIdentifierQuote(char byte) (byte, bool) {
	switch char {
	case '"', '`':
		return char, true
	case '[':
		return ']', true
	default:
		return 0, false
	}
}

func isSQLIdentifierStart(char byte) bool {
	return char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' || char == '_'
}

func isSQLIdentifierPart(char byte) bool {
	return isSQLIdentifierStart(char) || char >= '0' && char <= '9'
}

func quoteIdentifier(value string) string {
	return `"` + strings.ReplaceAll(value, `"`, `""`) + `"`
}

func rewriteSQLTables(query string) string {
	mu.RLock()
	remote := serverDriver == "libsql"
	mu.RUnlock()
	if !remote {
		return query
	}
	sqlTableRewriteOnce.Do(func() {
		tables := []string{
			"config", "config_meta", "app_secrets", "owners", "api_tokens", "audit_log",
			"scheduler_tasks", "scheduler_history", "backups", "security_reports", "security_thanks",
			"rate_limits", "captcha_challenges", "security_pgp_metadata", "security_pgp_exports",
		}
		for _, table := range tables {
			quoted := regexp.QuoteMeta(table)
			for _, verb := range []string{"FROM", "JOIN", "INTO", "UPDATE", "TABLE", "ON", "REFERENCES"} {
				pattern := regexp.MustCompile(`(?i)(\b` + verb + `\s+)([` + "\"" + `]?)(` + quoted + `)\b`)
				sqlTableRewriteRules = append(sqlTableRewriteRules, sqlRewriteRule{pattern: pattern, name: table})
			}
			pattern := regexp.MustCompile(`(?i)(\btable_info\s*\(\s*)([` + "\"" + `]?)(` + quoted + `)\b`)
			sqlTableRewriteRules = append(sqlTableRewriteRules, sqlRewriteRule{pattern: pattern, name: table})
		}
	})
	for _, rule := range sqlTableRewriteRules {
		query = rule.pattern.ReplaceAllString(query, `${1}${2}`+tableNameForDriver("libsql", rule.name))
	}
	query = rewriteSQLObjectName(query, `(?i)(\bCREATE\s+(?:UNIQUE\s+)?INDEX\s+(?:IF\s+NOT\s+EXISTS\s+)?)([A-Za-z_][A-Za-z0-9_]*)`)
	query = rewriteSQLObjectName(query, `(?i)(\bDROP\s+INDEX\s+(?:IF\s+EXISTS\s+)?)([A-Za-z_][A-Za-z0-9_]*)`)
	query = rewriteSQLObjectName(query, `(?i)(\bCREATE\s+TRIGGER\s+(?:IF\s+NOT\s+EXISTS\s+)?)([A-Za-z_][A-Za-z0-9_]*)`)
	return query
}

func rewriteSQLObjectName(query, expression string) string {
	pattern := regexp.MustCompile(expression)
	return pattern.ReplaceAllStringFunc(query, func(match string) string {
		parts := pattern.FindStringSubmatch(match)
		if len(parts) != 3 || strings.HasPrefix(parts[2], "srv_") {
			return match
		}
		return parts[1] + tableNameForDriver("libsql", parts[2])
	})
}

func execTxContext(ctx context.Context, tx *sql.Tx, query string, args ...any) (sql.Result, error) {
	return tx.ExecContext(ctx, rewriteSQLTables(query), args...)
}

func queryTxContext(ctx context.Context, tx *sql.Tx, query string, args ...any) (*sql.Rows, error) {
	return tx.QueryContext(ctx, rewriteSQLTables(query), args...)
}

func queryRowTxContext(ctx context.Context, tx *sql.Tx, query string, args ...any) *sql.Row {
	return tx.QueryRowContext(ctx, rewriteSQLTables(query), args...)
}
