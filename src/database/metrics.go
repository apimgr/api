package database

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"log/slog"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/apimgr/api/src/config"
	"github.com/apimgr/api/src/metrics"
	"github.com/apimgr/api/src/mode"
)

// connStatsInterval is how often the pool gauges are refreshed from
// sql.DB.Stats(), per AI.md PART 20's db_connections_open/in_use gauges.
const connStatsInterval = 15 * time.Second

// Fixed operation label values. PART 20 restricts operation to a small,
// low-cardinality set; opOther covers DDL and maintenance statements.
const (
	opSelect = "select"
	opInsert = "insert"
	opUpdate = "update"
	opDelete = "delete"
	opOther  = "other"
)

// Fixed table label fallbacks used when a statement touches no known table
// or touches several of them (for example the schema bootstrap blob).
const (
	tableUnknown  = "unknown"
	tableMultiple = "multiple"
)

// Fixed error_type label values, per PART 20's error classification table.
const (
	errConnection = "connection"
	errTimeout    = "timeout"
	errConstraint = "constraint"
	errDuplicate  = "duplicate"
	errOther      = "other"
)

// knownTables is the closed set of table names this package queries. Only
// names from this list ever become a metric label value, which is what keeps
// the table label bounded no matter what a statement contains.
var knownTables = []string{
	"scheduler_history",
	"scheduler_tasks",
	"config_meta",
	"app_secrets",
	"api_tokens",
	"rate_limits",
	"audit_log",
	"backups",
	"config",
}

// tableMatchers holds one word-boundary matcher per known table, built once.
var tableMatchers = buildTableMatchers()

// leadingNoise strips SQL line comments and leading whitespace so the first
// real keyword of a statement can be identified.
var leadingNoise = regexp.MustCompile(`(?m)^\s*--.*$`)

// connStats guards the connection-pool sampler goroutine lifecycle.
var connStats struct {
	mu   sync.Mutex
	stop chan struct{}
	done chan struct{}
}

// buildTableMatchers compiles a word-boundary regexp for every known table.
func buildTableMatchers() map[string]*regexp.Regexp {
	matchers := make(map[string]*regexp.Regexp, len(knownTables))
	for _, name := range knownTables {
		matchers[name] = regexp.MustCompile(`\b` + name + `\b`)
	}
	return matchers
}

// queryLabels derives the low-cardinality operation and table labels for a
// statement. Only literal SQL written in this package is passed in, and only
// values from the fixed constant sets above are ever returned, so no user
// input, identifier, or raw SQL text can reach a metric label.
func queryLabels(query string) (string, string) {
	cleaned := strings.ToLower(leadingNoise.ReplaceAllString(query, ""))
	cleaned = strings.TrimSpace(cleaned)

	operation := opOther
	switch {
	case strings.HasPrefix(cleaned, "select"):
		operation = opSelect
	case strings.HasPrefix(cleaned, "insert"), strings.HasPrefix(cleaned, "replace"):
		operation = opInsert
	case strings.HasPrefix(cleaned, "update"):
		operation = opUpdate
	case strings.HasPrefix(cleaned, "delete"):
		operation = opDelete
	}

	table := tableUnknown
	matched := 0
	for _, name := range knownTables {
		if tableMatchers[name].MatchString(cleaned) {
			matched++
			if matched == 1 {
				table = name
			}
		}
	}
	if matched > 1 {
		table = tableMultiple
	}

	return operation, table
}

// classifyDBError maps a driver error onto one of PART 20's fixed error_type
// label values. The error string itself is never used as a label.
func classifyDBError(err error) string {
	if errors.Is(err, context.DeadlineExceeded) {
		return errTimeout
	}
	if errors.Is(err, context.Canceled) {
		return errConnection
	}
	if errors.Is(err, sql.ErrConnDone) || errors.Is(err, driver.ErrBadConn) {
		return errConnection
	}

	text := strings.ToLower(err.Error())
	switch {
	case strings.Contains(text, "unique constraint"), strings.Contains(text, "duplicate"):
		return errDuplicate
	case strings.Contains(text, "constraint"):
		return errConstraint
	case strings.Contains(text, "timeout"), strings.Contains(text, "deadline exceeded"), strings.Contains(text, "database is locked"):
		return errTimeout
	case strings.Contains(text, "connection"), strings.Contains(text, "connect"), strings.Contains(text, "network"), strings.Contains(text, "broken pipe"):
		return errConnection
	}
	return errOther
}

// observeQuery records one completed statement: always its duration, plus a
// classified error when the statement failed. sql.ErrNoRows is an ordinary
// empty result, not a database error, so it is not counted.
func observeQuery(query string, start time.Time, err error) {
	operation, table := queryLabels(query)
	m := metrics.Get()
	duration := time.Since(start)
	m.RecordDBQuery(operation, table, duration)
	cfg := config.Get()
	if mode.IsDebugEnabled() && cfg != nil && cfg.Server.Debug.LogQueries {
		attributes := []any{"query", mode.Sanitize(query), "duration_ms", duration.Milliseconds()}
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			attributes = append(attributes, "error", mode.Sanitize(classifyDBError(err)))
		}
		slog.Debug("database query", attributes...)
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		m.RecordDBError(operation, classifyDBError(err))
	}
}

// recordQueryError records an error that only surfaces after the statement
// itself returned, such as a *sql.Row.Scan failure or a *sql.Rows iteration
// error. The duration was already observed when the statement ran.
func recordQueryError(query string, err error) {
	if err == nil || errors.Is(err, sql.ErrNoRows) {
		return
	}
	operation, _ := queryLabels(query)
	metrics.Get().RecordDBError(operation, classifyDBError(err))
}

// execContext runs an INSERT/UPDATE/DELETE/DDL statement with metrics.
func execContext(ctx context.Context, db interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}, query string, args ...any) (sql.Result, error) {
	query = rewriteSQLTables(query)
	start := time.Now()
	defer recordRequestDatabaseTiming(ctx, start)
	var result sql.Result
	var err error
	if handle, ok := db.(*sql.DB); ok && isServerDatabaseHandle(handle) && GetStore() != nil {
		err = GetStore().Write(ctx, func(tx *sql.Tx) error {
			var execErr error
			result, execErr = tx.Exec(query, args...)
			return execErr
		})
	} else {
		result, err = db.ExecContext(ctx, query, args...)
	}
	observeQuery(query, start, err)
	if err == nil {
		refreshServerCacheAfterWrite(ctx, db)
	}
	return result, err
}

// queryContext runs a multi-row SELECT with metrics. Callers must still pass
// any rows.Err() to recordQueryError once iteration finishes.
type timedRows struct {
	*sql.Rows
	cancel  context.CancelFunc
	ctx     context.Context
	started time.Time
	finish  sync.Once
}

func (rows *timedRows) Close() error {
	defer rows.cancel()
	rows.recordTiming()
	return rows.Rows.Close()
}

func (rows *timedRows) Next() bool {
	if rows.Rows.Next() {
		return true
	}
	rows.cancel()
	rows.recordTiming()
	return false
}

func (rows *timedRows) recordTiming() {
	rows.finish.Do(func() {
		recordRequestDatabaseTiming(rows.ctx, rows.started)
	})
}

func queryContext(ctx context.Context, db interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}, query string, args ...any) (*timedRows, error) {
	query = rewriteSQLTables(query)
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	start := time.Now()
	if handle, ok := db.(*sql.DB); ok && isServerDatabaseHandle(handle) {
		cache := getServerCache()
		if cache != nil && cache.fresh(time.Now().UTC()) {
			rows, err := cache.store.readDB.QueryContext(ctx, sqliteCacheSQL(query), args...)
			if err == nil {
				observeQuery(query, start, nil)
				return &timedRows{Rows: rows, cancel: cancel, ctx: ctx, started: start}, nil
			}
		}
		if store := GetStore(); store != nil {
			db = store.readDB
		}
	}
	rows, err := db.QueryContext(ctx, query, args...)
	observeQuery(query, start, err)
	if err != nil {
		if handle, ok := db.(*sql.DB); ok && isServerDatabaseHandle(handle) {
			cache := getServerCache()
			if cache != nil && cache.offlineMode && cache.fresh(time.Now().UTC()) {
				rows, cacheErr := cache.store.readDB.QueryContext(ctx, sqliteCacheSQL(query), args...)
				if cacheErr == nil {
					return &timedRows{Rows: rows, cancel: cancel, ctx: ctx, started: start}, nil
				}
			}
		}
		recordRequestDatabaseTiming(ctx, start)
		cancel()
		return nil, err
	}
	return &timedRows{Rows: rows, cancel: cancel, ctx: ctx, started: start}, nil
}

// queryRowContext runs a single-row SELECT with metrics. A *sql.Row defers
// its error to Scan, so callers must pass that error to recordQueryError.
// Row is the scan-only result of a bounded single-row query.
type Row interface {
	Scan(...any) error
}

type timedRow struct {
	row      *sql.Row
	fallback *sql.DB
	query    string
	args     []any
	ctx      context.Context
	cancel   context.CancelFunc
	started  time.Time
}

func (row timedRow) Scan(dest ...any) error {
	defer row.cancel()
	defer recordRequestDatabaseTiming(row.ctx, row.started)
	err := row.row.Scan(dest...)
	if err == nil || row.fallback == nil || errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if fallbackErr := row.fallback.QueryRowContext(row.ctx, row.query, row.args...).Scan(dest...); fallbackErr == nil {
		return nil
	}
	return err
}

func queryRowContext(ctx context.Context, db interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, query string, args ...any) Row {
	query = rewriteSQLTables(query)
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	start := time.Now()
	var fallback *sql.DB
	if handle, ok := db.(*sql.DB); ok && isServerDatabaseHandle(handle) {
		cache := getServerCache()
		if cache != nil && cache.fresh(time.Now().UTC()) {
			row := cache.store.readDB.QueryRowContext(ctx, sqliteCacheSQL(query), args...)
			observeQuery(query, start, nil)
			return timedRow{row: row, fallback: handle, query: query, args: append([]any(nil), args...), ctx: ctx, cancel: cancel, started: start}
		}
		if store := GetStore(); store != nil {
			db = store.readDB
		}
		fallback = handle
	}
	row := db.QueryRowContext(ctx, query, args...)
	observeQuery(query, start, nil)
	return timedRow{row: row, fallback: fallback, query: query, args: append([]any(nil), args...), ctx: ctx, cancel: cancel, started: start}
}

// startConnStatsSampler begins refreshing the connection-pool gauges from
// sql.DB.Stats() on a ticker. Calling it while a sampler is already running
// is a no-op, so repeated Init calls never leak a goroutine.
func startConnStatsSampler(db *sql.DB) {
	startConnStatsSamplerWithSample(func() {
		sampleConnStats(db)
	})
}

func startStoreConnStatsSampler(store *Store) {
	startConnStatsSamplerWithSample(func() {
		write := store.writeDB.Stats()
		read := store.readDB.Stats()
		metrics.Get().SetDBConnections(write.OpenConnections+read.OpenConnections, write.InUse+read.InUse)
	})
}

func startConnStatsSamplerWithSample(sample func()) {
	connStats.mu.Lock()
	defer connStats.mu.Unlock()

	if connStats.stop != nil {
		return
	}

	stop := make(chan struct{})
	done := make(chan struct{})
	connStats.stop = stop
	connStats.done = done

	go func() {
		defer close(done)
		ticker := time.NewTicker(connStatsInterval)
		defer ticker.Stop()

		sample()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				sample()
			}
		}
	}()
}

// stopConnStatsSampler stops the sampler goroutine and waits for it to exit.
func stopConnStatsSampler() {
	connStats.mu.Lock()
	stop := connStats.stop
	done := connStats.done
	connStats.stop = nil
	connStats.done = nil
	connStats.mu.Unlock()

	if stop == nil {
		return
	}
	close(stop)
	<-done
}

// sampleConnStats copies the current pool counters into the metrics gauges.
func sampleConnStats(db *sql.DB) {
	stats := db.Stats()
	metrics.Get().SetDBConnections(stats.OpenConnections, stats.InUse)
}
