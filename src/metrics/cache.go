package metrics

// RecordCacheHit records one cache lookup that found a value. cache is the
// low-cardinality cache/namespace name (e.g. "sessions", "geoip").
func (m *Metrics) RecordCacheHit(cache string) {
	m.cacheHitsTotal.WithLabelValues(cache).Inc()
}

// RecordCacheMiss records one cache lookup that found no value.
func (m *Metrics) RecordCacheMiss(cache string) {
	m.cacheMissesTotal.WithLabelValues(cache).Inc()
}

// RecordCacheEviction records one cache entry evicted (TTL expiry, LRU, or
// explicit invalidation).
func (m *Metrics) RecordCacheEviction(cache string) {
	m.cacheEvictionsTotal.WithLabelValues(cache).Inc()
}

// SetCacheSize records the current number of items held by a cache.
func (m *Metrics) SetCacheSize(cache string, n int) {
	m.cacheSize.WithLabelValues(cache).Set(float64(n))
}

// SetCacheBytes records the current byte size of a cache.
func (m *Metrics) SetCacheBytes(cache string, n int64) {
	m.cacheBytes.WithLabelValues(cache).Set(float64(n))
}
