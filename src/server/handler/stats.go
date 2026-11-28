package handler

import (
	"context"
	"log/slog"
	"sort"
	"sync"
	"time"

	"github.com/apimgr/api/src/database"
)

// statsWindowHours is the number of hourly buckets kept for the rolling
// 24-hour request count PART 13 reports as stats.requests_24h.
const statsWindowHours = 24

const requestStatsFlushInterval = 30 * time.Second

// requestStats holds the public-safe aggregate counters exposed through
// /server/healthz. Nothing per-client is recorded: only a lifetime total, a
// rolling 24-hour count and the number of in-flight requests. Per-IP and
// per-path detail belongs in the Prometheus metrics (PART 20), which is an
// internal-only endpoint.
type requestStats struct {
	mu sync.Mutex
	// total counts every counted request since the database was created.
	total int64
	// active counts requests currently being served.
	active int64
	// buckets is a ring of hourly counters indexed by absolute hour, so a
	// stale bucket is recognised by its recorded hour rather than by any
	// background sweeping goroutine.
	buckets [statsWindowHours]int64
	// bucketHours records which absolute hour each bucket last counted.
	bucketHours  [statsWindowHours]int64
	pendingTotal int64
	pending      map[int64]int64
	flushMu      sync.Mutex
	lifecycleMu  sync.Mutex
	warnOnce     sync.Once
	flushStop    chan struct{}
	flushDone    chan struct{}
	started      bool
	loaded       bool
}

// stats is the process-wide collector fed by the server's request middleware.
var stats requestStats

// RequestStarted records the beginning of a request. The returned function
// must be called when the request completes, which keeps the active-connection
// gauge balanced even when a handler panics and the recover middleware unwinds.
func RequestStarted() func() {
	return RequestStartedCounted(true)
}

// RequestStartedCounted tracks every in-flight request and optionally includes
// it in the persistent public request aggregates.
func RequestStartedCounted(count bool) func() {
	stats.begin(time.Now(), count)
	return func() {
		stats.finish()
	}
}

// StartRequestStats loads persisted totals and recent buckets, then starts the
// bounded periodic writer. A load failure is returned for startup logging; the
// in-memory collector remains active and will persist new deltas when possible.
func StartRequestStats(ctx context.Context) error {
	stats.lifecycleMu.Lock()
	defer stats.lifecycleMu.Unlock()
	stats.mu.Lock()
	if stats.started {
		stats.mu.Unlock()
		return nil
	}
	stats.mu.Unlock()

	now := time.Now().UTC()
	oldestHour := absoluteHour(now) - statsWindowHours + 1
	snapshot, loadErr := database.LoadRequestStats(ctx, oldestHour)

	stats.mu.Lock()
	stats.total = snapshot.Total
	stats.active = 0
	stats.buckets = [statsWindowHours]int64{}
	stats.bucketHours = [statsWindowHours]int64{}
	stats.pendingTotal = 0
	stats.pending = make(map[int64]int64)
	stats.loaded = loadErr == nil
	for _, bucket := range snapshot.Buckets {
		slot := int(bucket.Hour % statsWindowHours)
		stats.bucketHours[slot] = bucket.Hour
		stats.buckets[slot] = bucket.Count
	}
	stats.flushStop = make(chan struct{})
	stats.flushDone = make(chan struct{})
	stats.started = true
	go stats.flushLoop()
	stats.mu.Unlock()
	return loadErr
}

// StopRequestStats stops periodic persistence and synchronously flushes all
// pending request aggregates before the database closes.
func StopRequestStats(ctx context.Context) error {
	stats.lifecycleMu.Lock()
	defer stats.lifecycleMu.Unlock()
	stats.mu.Lock()
	started, stop, done := stats.started, stats.flushStop, stats.flushDone
	stats.mu.Unlock()
	if !started {
		return stats.flushPending(ctx)
	}
	close(stop)
	<-done
	stats.mu.Lock()
	stats.started = false
	stats.flushStop = nil
	stats.flushDone = nil
	stats.mu.Unlock()
	return stats.flushPending(ctx)
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
	stats.pendingTotal = 0
	stats.pending = make(map[int64]int64)
	stats.loaded = false
}

// begin records a request arriving at the given time.
func (s *requestStats) begin(now time.Time, count bool) {
	hour := absoluteHour(now)
	slot := int(hour % statsWindowHours)

	s.mu.Lock()
	defer s.mu.Unlock()

	s.active++
	if !count {
		return
	}
	s.total++
	if s.pending == nil {
		s.pending = make(map[int64]int64)
	}
	if s.bucketHours[slot] != hour {
		s.bucketHours[slot] = hour
		s.buckets[slot] = 0
	}
	s.buckets[slot]++
	s.pendingTotal++
	s.pending[hour]++
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

func (s *requestStats) flushLoop() {
	ticker := time.NewTicker(requestStatsFlushInterval)
	defer ticker.Stop()
	defer close(s.flushDone)
	for {
		select {
		case <-ticker.C:
			if err := s.loadPersistedIfNeeded(context.Background()); err != nil {
				s.warnOnce.Do(func() { slog.Warn("health request statistics restore failed", "error", err) })
			}
			if err := s.flushPending(context.Background()); err != nil {
				s.warnOnce.Do(func() { slog.Warn("health request statistics batch write failed", "error", err) })
			}
		case <-s.flushStop:
			if err := s.loadPersistedIfNeeded(context.Background()); err != nil {
				s.warnOnce.Do(func() { slog.Warn("health request statistics restore failed", "error", err) })
			}
			if err := s.flushPending(context.Background()); err != nil {
				s.warnOnce.Do(func() { slog.Warn("health request statistics shutdown flush failed", "error", err) })
			}
			return
		}
	}
}

func (s *requestStats) loadPersistedIfNeeded(ctx context.Context) error {
	s.mu.Lock()
	if s.loaded {
		s.mu.Unlock()
		return nil
	}
	s.mu.Unlock()

	now := time.Now().UTC()
	snapshot, err := database.LoadRequestStats(ctx, absoluteHour(now)-statsWindowHours+1)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.loaded {
		return nil
	}
	s.total = snapshot.Total + s.pendingTotal
	s.buckets = [statsWindowHours]int64{}
	s.bucketHours = [statsWindowHours]int64{}
	for _, bucket := range snapshot.Buckets {
		slot := int(bucket.Hour % statsWindowHours)
		s.bucketHours[slot] = bucket.Hour
		s.buckets[slot] = bucket.Count
	}
	for hour, count := range s.pending {
		slot := int(hour % statsWindowHours)
		if s.bucketHours[slot] != hour {
			s.bucketHours[slot] = hour
			s.buckets[slot] = 0
		}
		s.buckets[slot] += count
	}
	s.loaded = true
	return nil
}

func (s *requestStats) flushPending(ctx context.Context) error {
	s.flushMu.Lock()
	defer s.flushMu.Unlock()

	s.mu.Lock()
	totalDelta := s.pendingTotal
	if totalDelta == 0 && len(s.pending) == 0 {
		s.mu.Unlock()
		return nil
	}
	byHour := s.pending
	s.pending = make(map[int64]int64)
	s.pendingTotal = 0
	s.mu.Unlock()

	hours := make([]int64, 0, len(byHour))
	for hour := range byHour {
		hours = append(hours, hour)
	}
	sort.Slice(hours, func(i, j int) bool { return hours[i] < hours[j] })
	buckets := make([]database.RequestStatsBucket, 0, len(hours))
	for _, hour := range hours {
		buckets = append(buckets, database.RequestStatsBucket{Hour: hour, Count: byHour[hour]})
	}
	oldestHour := absoluteHour(time.Now().UTC()) - statsWindowHours + 1
	if err := database.RecordRequestStats(ctx, totalDelta, buckets, oldestHour); err != nil {
		s.mu.Lock()
		s.pendingTotal += totalDelta
		for hour, count := range byHour {
			s.pending[hour] += count
		}
		s.mu.Unlock()
		return err
	}
	return nil
}

// absoluteHour converts a timestamp into a monotonically increasing hour
// number, so bucket staleness is a plain comparison rather than a wall-clock
// subtraction that a DST shift could distort.
func absoluteHour(t time.Time) int64 {
	return t.UTC().Unix() / 3600
}
