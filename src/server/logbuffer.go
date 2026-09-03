package server

import (
	"sync"
	"time"

	"github.com/apimgr/api/src/metrics"
)

// recentLogCapacity is the hard ceiling on how many log entries are held in
// memory for the Loki metrics service (PART 20). It is deliberately larger
// than the documented server.metrics.loki.max_entries default (1000) so an
// operator can raise that setting without a rebuild, and small enough that
// the buffer stays a bounded, fixed-cost structure.
const recentLogCapacity = 5000

// recentLogBuffer is a fixed-size ring of the most recent structured log
// entries. It is the in-process backing store for /server/metrics/loki:
// the log FILES are the durable record, and this buffer only exists so the
// Loki service can serve a recent window without re-reading and re-parsing
// them on every scrape.
//
// Entries arrive already sanitized: every writer path feeding this buffer
// passes through the same redaction the log files use, so no credential
// ever reaches it.
type recentLogBuffer struct {
	mu      sync.Mutex
	entries []metrics.LogEntry
	next    int
	filled  bool
}

// newRecentLogBuffer allocates a ring of recentLogCapacity entries.
func newRecentLogBuffer() *recentLogBuffer {
	return &recentLogBuffer{entries: make([]metrics.LogEntry, recentLogCapacity)}
}

// add records one entry, overwriting the oldest once the ring is full.
func (b *recentLogBuffer) add(level, message string, labels map[string]string) {
	if b == nil {
		return
	}
	stream := map[string]string{"level": level}
	for k, v := range labels {
		stream[k] = v
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.entries[b.next] = metrics.LogEntry{Time: time.Now(), Line: message, Labels: stream}
	b.next++
	if b.next == len(b.entries) {
		b.next = 0
		b.filled = true
	}
}

// RecentEntries implements metrics.LogSource. It returns entries no older
// than maxAge, oldest first, capped at maxEntries.
func (b *recentLogBuffer) RecentEntries(maxAge time.Duration, maxEntries int) []metrics.LogEntry {
	if b == nil || maxEntries <= 0 {
		return nil
	}
	cutoff := time.Now().Add(-maxAge)

	b.mu.Lock()
	defer b.mu.Unlock()

	// Walk the ring newest-first so the maxEntries cap keeps the most
	// recent window, then reverse into chronological order for Loki.
	count := len(b.entries)
	if !b.filled {
		count = b.next
	}
	out := make([]metrics.LogEntry, 0, maxEntries)
	for i := 0; i < count && len(out) < maxEntries; i++ {
		idx := b.next - 1 - i
		for idx < 0 {
			idx += len(b.entries)
		}
		e := b.entries[idx]
		if e.Time.Before(cutoff) {
			break
		}
		out = append(out, e)
	}
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out
}

// globalRecentLog is the process-wide buffer the Loki metrics service
// reads from. It is allocated eagerly so log writers never have to check
// whether logging has been initialized yet.
var globalRecentLog = newRecentLogBuffer()

// metricsLogSource returns the metrics.LogSource backing
// /server/metrics/loki.
func metricsLogSource() metrics.LogSource {
	return globalRecentLog
}
