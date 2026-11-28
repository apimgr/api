package metrics

// RecordAuthAttempt records one authentication attempt. method is a
// low-cardinality mechanism name (token, password, oauth); status is
// "success" or "failed" - never a specific failure reason, per PART 20
// label cardinality rules.
func (m *Metrics) RecordAuthAttempt(method, status string) {
	m.authAttemptsTotal.WithLabelValues(method, status).Inc()
}
