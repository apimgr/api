package handler

import (
	"sync"
	"time"
)

// statsWindowHours is the number of hourly buckets kept for the rolling
// 24-hour request count PART 13 reports as stats.requests_24h.
const statsWindowHours = 24

// requestStats holds the public-safe aggregate counters exposed through
// /server/healthz. Nothing per-client is recorded: only a lifetime total, a
// rolling 24-hour count and the number of in-flight requests. Per-IP and
// per-path detail belongs in the Prometheus metrics (PART 20), which is an
// internal-only endpoint.
type requestStats struct {
	mu sync.Mutex
	// total counts every request served since process start.
	total int64
	// active counts requests currently being served.
	active int64
	// buckets is a ring of hourly counters indexed by absolute hour, so a
	// stale bucket is recognised by its recorded hour rather than by any
	// background sweeping goroutine.
	buckets [statsWindowHours]int64
	// bucketHours records which absolute hour each bucket last counted.
	bucketHours [statsWindowHours]int64
}

// stats is the process-wide collector fed by the server's request middleware.
var stats requestStats

// RequestStarted records the beginning of a request. The returned function
// must be called when the request completes, which keeps the active-connection
// gauge balanced even when a handler panics and the recover middleware unwinds.
func RequestStarted() func() {
	stats.begin(time.Now())
	return func() {
		stats.finish()
	}
}

// RequestStatsSnapshot returns the lifetime total, the rolling 24-hour count
// and the number of in-flight requests.
func RequestStatsSnapshot() (total, last24h int64, active int) {
	return stats.snapshot(time.Now())
}

// ResetRequestStats clears every counter. It exists so tests can assert on a
// known baseline.
func ResetRequestStats() {
	stats.mu.Lock()
	defer stats.mu.Unlock()
	stats.total = 0
	stats.active = 0
	stats.buckets = [statsWindowHours]int64{}
	stats.bucketHours = [statsWindowHours]int64{}
}

// begin records a request arriving at the given time.
func (s *requestStats) begin(now time.Time) {
	hour := absoluteHour(now)
	slot := int(hour % statsWindowHours)

	s.mu.Lock()
	defer s.mu.Unlock()

	s.total++
	s.active++
	if s.bucketHours[slot] != hour {
		s.bucketHours[slot] = hour
		s.buckets[slot] = 0
	}
	s.buckets[slot]++
}

// finish records a request completing.
func (s *requestStats) finish() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.active > 0 {
		s.active--
	}
}

// snapshot sums the buckets that still fall inside the 24-hour window ending
// at now.
func (s *requestStats) snapshot(now time.Time) (total, last24h int64, active int) {
	oldest := absoluteHour(now) - statsWindowHours + 1

	s.mu.Lock()
	defer s.mu.Unlock()

	for i := range s.buckets {
		if s.bucketHours[i] >= oldest {
			last24h += s.buckets[i]
		}
	}
	return s.total, last24h, int(s.active)
}

// absoluteHour converts a timestamp into a monotonically increasing hour
// number, so bucket staleness is a plain comparison rather than a wall-clock
// subtraction that a DST shift could distort.
func absoluteHour(t time.Time) int64 {
	return t.UTC().Unix() / 3600
}
