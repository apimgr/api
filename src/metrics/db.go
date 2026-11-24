package metrics

import "time"

// RecordDBQuery records a completed database query. operation is a
// low-cardinality verb (select, insert, update, delete); table is the
// target table name, lowercase.
func (m *Metrics) RecordDBQuery(operation, table string, duration time.Duration) {
	m.dbQueriesTotal.WithLabelValues(operation, table).Inc()
	m.dbQueryDuration.WithLabelValues(operation, table).Observe(duration.Seconds())
}

// RecordDBError records a failed database operation. errorType must be one
// of connection, timeout, constraint, duplicate, other - never the raw
// error string, per PART 20 label cardinality rules.
func (m *Metrics) RecordDBError(operation, errorType string) {
	m.dbErrorsTotal.WithLabelValues(operation, errorType).Inc()
}

// SetDBConnections records the current connection pool state, typically
// sourced from sql.DB.Stats().
func (m *Metrics) SetDBConnections(open, inUse int) {
	m.dbConnectionsOpen.Set(float64(open))
	m.dbConnectionsInUse.Set(float64(inUse))
}

// RecordDBBusy records a retryable SQLite writer lock conflict.
func (m *Metrics) RecordDBBusy() {
	m.dbBusyTotal.Inc()
}

// ObserveDBWriteWait records the total elapsed time of one write transaction.
func (m *Metrics) ObserveDBWriteWait(duration time.Duration) {
	m.dbWriteWait.Observe(duration.Seconds())
}
