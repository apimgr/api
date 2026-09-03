package metrics

// RecordRateLimitRequest records one request evaluated by the rate limiter.
//
// CRITICAL: limit must be one of the fixed enum values "global", "per_ip",
// "per_user", or "per_endpoint" - a category, never a raw client IP or
// other unbounded value. Per-address detail belongs in structured logs,
// never in a metric label, per AI.md PART 20's cardinality rules. status is
// "allowed" or "limited".
func (m *Metrics) RecordRateLimitRequest(limit, status string) {
	m.ratelimitRequestsTotal.WithLabelValues(limit, status).Inc()
}

// RecordRateLimitBlocked records one request blocked by the rate limiter.
// limit follows the same fixed-enum constraint as RecordRateLimitRequest.
func (m *Metrics) RecordRateLimitBlocked(limit string) {
	m.ratelimitBlockedTotal.WithLabelValues(limit).Inc()
}
