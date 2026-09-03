package database

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/apimgr/api/src/metrics"
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
	m.RecordDBQuery(operation, table, time.Since(start))
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
func execContext(ctx context.Context, db *sql.DB, query string, args ...any) (sql.Result, error) {
	start := time.Now()
	result, err := db.ExecContext(ctx, query, args...)
	observeQuery(query, start, err)
	return result, err
}

// queryContext runs a multi-row SELECT with metrics. Callers must still pass
// any rows.Err() to recordQueryError once iteration finishes.
func queryContext(ctx context.Context, db *sql.DB, query string, args ...any) (*sql.Rows, error) {
	start := time.Now()
	rows, err := db.QueryContext(ctx, query, args...)
	observeQuery(query, start, err)
	return rows, err
}

// queryRowContext runs a single-row SELECT with metrics. A *sql.Row defers
// its error to Scan, so callers must pass that error to recordQueryError.
func queryRowContext(ctx context.Context, db *sql.DB, query string, args ...any) *sql.Row {
	start := time.Now()
	row := db.QueryRowContext(ctx, query, args...)
	observeQuery(query, start, nil)
	return row
}

// startConnStatsSampler begins refreshing the connection-pool gauges from
// sql.DB.Stats() on a ticker. Calling it while a sampler is already running
// is a no-op, so repeated Init calls never leak a goroutine.
func startConnStatsSampler(db *sql.DB) {
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

		sampleConnStats(db)
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				sampleConnStats(db)
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
