package metrics

import (
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
	"time"
)

// Metrics tracks application metrics
type Metrics struct {
	// Request counters
	totalRequests   uint64
	successRequests uint64
	errorRequests   uint64

	// Status code counters
	status2xx uint64
	status3xx uint64
	status4xx uint64
	status5xx uint64

	// Latency tracking
	mu             sync.RWMutex
	latencies      []time.Duration
	maxLatency     time.Duration
	minLatency     time.Duration
	totalLatencyMs uint64

	// Endpoint counters
	endpointCounts map[string]uint64
	endpointMu     sync.RWMutex

	// Start time
	startTime time.Time
}

var (
	globalMetrics *Metrics
	metricsOnce   sync.Once
)

// Get returns the singleton metrics instance
func Get() *Metrics {
	metricsOnce.Do(func() {
		globalMetrics = &Metrics{
			latencies:      make([]time.Duration, 0, 1000),
			endpointCounts: make(map[string]uint64),
			startTime:      time.Now(),
			minLatency:     time.Hour, // Set high initial value
		}
	})
	return globalMetrics
}

// RecordRequest records a completed HTTP request
func (m *Metrics) RecordRequest(status int, latency time.Duration, endpoint string) {
	// Increment total requests
	atomic.AddUint64(&m.totalRequests, 1)

	// Track by status code
	if status >= 200 && status < 300 {
		atomic.AddUint64(&m.successRequests, 1)
		atomic.AddUint64(&m.status2xx, 1)
	} else if status >= 300 && status < 400 {
		atomic.AddUint64(&m.status3xx, 1)
	} else if status >= 400 && status < 500 {
		atomic.AddUint64(&m.errorRequests, 1)
		atomic.AddUint64(&m.status4xx, 1)
	} else if status >= 500 {
		atomic.AddUint64(&m.errorRequests, 1)
		atomic.AddUint64(&m.status5xx, 1)
	}

	// Track latency
	m.mu.Lock()
	if latency > m.maxLatency {
		m.maxLatency = latency
	}
	if latency < m.minLatency {
		m.minLatency = latency
	}
	atomic.AddUint64(&m.totalLatencyMs, uint64(latency.Milliseconds()))

	// Keep last 1000 latencies for percentile calculations
	if len(m.latencies) >= 1000 {
		m.latencies = m.latencies[1:]
	}
	m.latencies = append(m.latencies, latency)
	m.mu.Unlock()

	// Track endpoint usage
	m.endpointMu.Lock()
	m.endpointCounts[endpoint]++
	m.endpointMu.Unlock()
}

// GetStats returns current metrics statistics
func (m *Metrics) GetStats() map[string]interface{} {
	m.mu.RLock()
	defer m.mu.RUnlock()

	total := atomic.LoadUint64(&m.totalRequests)
	success := atomic.LoadUint64(&m.successRequests)
	errors := atomic.LoadUint64(&m.errorRequests)
	totalLatencyMs := atomic.LoadUint64(&m.totalLatencyMs)

	avgLatencyMs := float64(0)
	if total > 0 {
		avgLatencyMs = float64(totalLatencyMs) / float64(total)
	}

	uptime := time.Since(m.startTime)

	return map[string]interface{}{
		"uptime_seconds":    uptime.Seconds(),
		"total_requests":    total,
		"success_requests":  success,
		"error_requests":    errors,
		"status_2xx":        atomic.LoadUint64(&m.status2xx),
		"status_3xx":        atomic.LoadUint64(&m.status3xx),
		"status_4xx":        atomic.LoadUint64(&m.status4xx),
		"status_5xx":        atomic.LoadUint64(&m.status5xx),
		"avg_latency_ms":    avgLatencyMs,
		"min_latency_ms":    m.minLatency.Milliseconds(),
		"max_latency_ms":    m.maxLatency.Milliseconds(),
		"requests_per_sec":  float64(total) / uptime.Seconds(),
	}
}

// ServePrometheus serves metrics in Prometheus format
func (m *Metrics) ServePrometheus(w http.ResponseWriter, r *http.Request) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	total := atomic.LoadUint64(&m.totalRequests)
	success := atomic.LoadUint64(&m.successRequests)
	errors := atomic.LoadUint64(&m.errorRequests)
	totalLatencyMs := atomic.LoadUint64(&m.totalLatencyMs)

	w.Header().Set("Content-Type", "text/plain; version=0.0.4")

	fmt.Fprintf(w, "# HELP api_requests_total Total number of HTTP requests\n")
	fmt.Fprintf(w, "# TYPE api_requests_total counter\n")
	fmt.Fprintf(w, "api_requests_total %d\n", total)
	fmt.Fprintf(w, "\n")

	fmt.Fprintf(w, "# HELP api_requests_success Total number of successful HTTP requests\n")
	fmt.Fprintf(w, "# TYPE api_requests_success counter\n")
	fmt.Fprintf(w, "api_requests_success %d\n", success)
	fmt.Fprintf(w, "\n")

	fmt.Fprintf(w, "# HELP api_requests_errors Total number of failed HTTP requests\n")
	fmt.Fprintf(w, "# TYPE api_requests_errors counter\n")
	fmt.Fprintf(w, "api_requests_errors %d\n", errors)
	fmt.Fprintf(w, "\n")

	fmt.Fprintf(w, "# HELP api_status_2xx Total 2xx status codes\n")
	fmt.Fprintf(w, "# TYPE api_status_2xx counter\n")
	fmt.Fprintf(w, "api_status_2xx %d\n", atomic.LoadUint64(&m.status2xx))
	fmt.Fprintf(w, "\n")

	fmt.Fprintf(w, "# HELP api_status_4xx Total 4xx status codes\n")
	fmt.Fprintf(w, "# TYPE api_status_4xx counter\n")
	fmt.Fprintf(w, "api_status_4xx %d\n", atomic.LoadUint64(&m.status4xx))
	fmt.Fprintf(w, "\n")

	fmt.Fprintf(w, "# HELP api_status_5xx Total 5xx status codes\n")
	fmt.Fprintf(w, "# TYPE api_status_5xx counter\n")
	fmt.Fprintf(w, "api_status_5xx %d\n", atomic.LoadUint64(&m.status5xx))
	fmt.Fprintf(w, "\n")

	avgLatency := float64(0)
	if total > 0 {
		avgLatency = float64(totalLatencyMs) / float64(total)
	}

	fmt.Fprintf(w, "# HELP api_latency_avg_ms Average request latency in milliseconds\n")
	fmt.Fprintf(w, "# TYPE api_latency_avg_ms gauge\n")
	fmt.Fprintf(w, "api_latency_avg_ms %.2f\n", avgLatency)
	fmt.Fprintf(w, "\n")

	fmt.Fprintf(w, "# HELP api_latency_max_ms Maximum request latency in milliseconds\n")
	fmt.Fprintf(w, "# TYPE api_latency_max_ms gauge\n")
	fmt.Fprintf(w, "api_latency_max_ms %d\n", m.maxLatency.Milliseconds())
	fmt.Fprintf(w, "\n")

	// Endpoint-specific metrics
	m.endpointMu.RLock()
	for endpoint, count := range m.endpointCounts {
		fmt.Fprintf(w, "api_endpoint_requests{endpoint=\"%s\"} %d\n", endpoint, count)
	}
	m.endpointMu.RUnlock()
}

// ServeJSON serves metrics in JSON format
func (m *Metrics) ServeJSON(w http.ResponseWriter, r *http.Request) {
	stats := m.GetStats()

	w.Header().Set("Content-Type", "application/json")
	fmt.Fprintf(w, `{
  "uptime_seconds": %.2f,
  "total_requests": %d,
  "success_requests": %d,
  "error_requests": %d,
  "status_codes": {
    "2xx": %d,
    "3xx": %d,
    "4xx": %d,
    "5xx": %d
  },
  "latency": {
    "avg_ms": %.2f,
    "min_ms": %d,
    "max_ms": %d
  },
  "requests_per_second": %.2f
}`,
		stats["uptime_seconds"],
		stats["total_requests"],
		stats["success_requests"],
		stats["error_requests"],
		stats["status_2xx"],
		stats["status_3xx"],
		stats["status_4xx"],
		stats["status_5xx"],
		stats["avg_latency_ms"],
		stats["min_latency_ms"],
		stats["max_latency_ms"],
		stats["requests_per_sec"],
	)
}

