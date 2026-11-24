package metrics

// CacheDebugStats is an operator-only snapshot of cache metric families.
type CacheDebugStats struct {
	Hits      float64 `json:"hits"`
	Misses    float64 `json:"misses"`
	Evictions float64 `json:"evictions"`
	Size      float64 `json:"size"`
	Bytes     float64 `json:"bytes"`
}

// CacheDebugSnapshot returns per-cache counters for the authenticated debug
// endpoint without exposing cache keys or values.
func (m *Metrics) CacheDebugSnapshot() map[string]CacheDebugStats {
	result := make(map[string]CacheDebugStats)
	families, err := m.registry.Gather()
	if err != nil {
		return result
	}
	for _, family := range families {
		for _, metric := range family.GetMetric() {
			name := ""
			for _, label := range metric.GetLabel() {
				if label.GetName() == "cache" {
					name = label.GetValue()
					break
				}
			}
			if name == "" {
				continue
			}
			stats := result[name]
			switch family.GetName() {
			case "api_cache_hits_total":
				stats.Hits = metric.GetCounter().GetValue()
			case "api_cache_misses_total":
				stats.Misses = metric.GetCounter().GetValue()
			case "api_cache_evictions_total":
				stats.Evictions = metric.GetCounter().GetValue()
			case "api_cache_size":
				stats.Size = metric.GetGauge().GetValue()
			case "api_cache_bytes":
				stats.Bytes = metric.GetGauge().GetValue()
			default:
				continue
			}
			result[name] = stats
		}
	}
	return result
}

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
