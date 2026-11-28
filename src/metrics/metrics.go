// Package metrics implements PART 20's Prometheus-compatible metrics
// endpoint. All metric names are prefixed with "api_" (the project name)
// and follow Prometheus naming conventions (snake_case, base units,
// counters end in "_total").
package metrics

import (
	"log"
	"net/http"
	"runtime"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

const namespace = "api"

// defaultDurationBuckets is used for http_request_duration_seconds when
// MetricsOptions.DurationBuckets is empty, per AI.md PART 20 default config.
var defaultDurationBuckets = []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10}

// defaultSizeBuckets is used for http_request_size_bytes /
// http_response_size_bytes when MetricsOptions.SizeBuckets is empty.
var defaultSizeBuckets = []float64{100, 1000, 10000, 100000, 1000000, 10000000}

// dbDurationBuckets is the fixed bucket set for db_query_duration_seconds
// per AI.md PART 20 - not operator-configurable.
var dbDurationBuckets = []float64{0.0001, 0.0005, 0.001, 0.005, 0.01, 0.05, 0.1, 0.5, 1}

// schedulerDurationBuckets is the fixed bucket set for
// scheduler_task_duration_seconds per AI.md PART 20 - not operator-configurable.
var schedulerDurationBuckets = []float64{0.1, 0.5, 1, 5, 10, 30, 60, 300, 600}

// defaultCollectInterval is how often the periodic system/runtime collector
// samples values, when MetricsOptions.CollectInterval is zero.
const defaultCollectInterval = 15 * time.Second

// MetricsOptions configures optional metric families and operator-tunable
// histogram buckets, per AI.md PART 20 "Configuration".
type MetricsOptions struct {
	// DurationBuckets overrides http_request_duration_seconds buckets.
	DurationBuckets []float64
	// SizeBuckets overrides http_request_size_bytes / http_response_size_bytes buckets.
	SizeBuckets []float64
	// IncludeSystem enables CPU/memory/disk gauges (server.metrics.include_system).
	IncludeSystem bool
	// IncludeRuntime enables Go runtime gauges (server.metrics.include_runtime).
	IncludeRuntime bool
	// DataDir is the path reported by the system_disk_* gauges' "path" label.
	DataDir string
	// CollectInterval is how often system/runtime gauges are sampled.
	CollectInterval time.Duration
	// OwnerTokenCounts returns active primary and linked owner-token counts.
	OwnerTokenCounts func() (primary, linked float64, err error)
}

// Metrics holds all Prometheus collectors exposed at /server/metrics.
type Metrics struct {
	registry *prometheus.Registry
	opts     MetricsOptions

	appInfo           *prometheus.GaugeVec
	appStartTimestamp prometheus.Gauge

	httpRequestsTotal   *prometheus.CounterVec
	httpRequestDuration *prometheus.HistogramVec
	httpRequestSize     *prometheus.HistogramVec
	httpResponseSize    *prometheus.HistogramVec
	httpActiveRequests  prometheus.Gauge

	dbQueriesTotal     *prometheus.CounterVec
	dbQueryDuration    *prometheus.HistogramVec
	dbConnectionsOpen  prometheus.Gauge
	dbConnectionsInUse prometheus.Gauge
	dbErrorsTotal      *prometheus.CounterVec
	dbBusyTotal        prometheus.Counter
	dbWriteWait        prometheus.Histogram

	authAttemptsTotal *prometheus.CounterVec
	ownerTokensActive *prometheus.GaugeVec

	cacheHitsTotal      *prometheus.CounterVec
	cacheMissesTotal    *prometheus.CounterVec
	cacheEvictionsTotal *prometheus.CounterVec
	cacheSize           *prometheus.GaugeVec
	cacheBytes          *prometheus.GaugeVec

	schedulerTasksTotal   *prometheus.CounterVec
	schedulerTaskDuration *prometheus.HistogramVec
	schedulerTasksRunning *prometheus.GaugeVec
	schedulerLastRun      *prometheus.GaugeVec

	torEnabled            prometheus.Gauge
	torRunning            prometheus.Gauge
	torCircuitEstablished prometheus.Gauge
	torRequestsTotal      prometheus.Counter

	ratelimitRequestsTotal *prometheus.CounterVec
	ratelimitBlockedTotal  *prometheus.CounterVec

	system *systemMetrics
	rt     *runtimeMetrics

	startTime time.Time

	stopCollect chan struct{}
	stopOnce    sync.Once
}

var (
	globalMetrics *Metrics
	globalOpts    MetricsOptions
	globalHasOpts bool
	metricsOnce   sync.Once
	optsMu        sync.Mutex
)

// InitMetrics records the MetricsOptions used the first time GetMetrics() constructs the
// singleton. Call it once at startup, before the first GetMetrics(), with values
// resolved from server.yml. Calling it after the singleton already exists
// has no effect - metric family registration happens once, at construction.
func InitMetrics(opts MetricsOptions) {
	optsMu.Lock()
	defer optsMu.Unlock()
	globalOpts = opts
	globalHasOpts = true
}

// GetMetrics returns the singleton metrics instance, constructing it on first call
// using the MetricsOptions passed to InitMetrics (or PART 20 defaults if InitMetrics was never
// called).
func GetMetrics() *Metrics {
	metricsOnce.Do(func() {
		optsMu.Lock()
		opts := globalOpts
		hasOpts := globalHasOpts
		optsMu.Unlock()
		if !hasOpts {
			opts = MetricsOptions{IncludeSystem: true, IncludeRuntime: true}
		}
		globalMetrics = newMetricsWithOptions(opts)
	})
	return globalMetrics
}

// newMetrics constructs a fresh, unregistered-to-any-global-state Metrics
// instance with PART 20 default options. Used directly by tests.
func newMetrics() *Metrics {
	return newMetricsWithOptions(MetricsOptions{})
}

func newMetricsWithOptions(opts MetricsOptions) *Metrics {
	if len(opts.DurationBuckets) == 0 {
		opts.DurationBuckets = defaultDurationBuckets
	}
	if len(opts.SizeBuckets) == 0 {
		opts.SizeBuckets = defaultSizeBuckets
	}
	if opts.CollectInterval <= 0 {
		opts.CollectInterval = defaultCollectInterval
	}

	registry := prometheus.NewRegistry()

	m := &Metrics{
		registry:    registry,
		opts:        opts,
		startTime:   time.Now(),
		stopCollect: make(chan struct{}),

		appInfo: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: namespace,
			Subsystem: "app",
			Name:      "info",
			Help:      "Application information (always 1, labels carry build info)",
		}, []string{"version", "commit", "build_date", "go_version"}),

		appStartTimestamp: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: namespace,
			Subsystem: "app",
			Name:      "start_timestamp",
			Help:      "Unix timestamp when the application started",
		}),

		httpRequestsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: "http",
			Name:      "requests_total",
			Help:      "Total number of HTTP requests processed",
		}, []string{"method", "path", "status"}),

		httpRequestDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: namespace,
			Subsystem: "http",
			Name:      "request_duration_seconds",
			Help:      "HTTP request latency distribution",
			Buckets:   opts.DurationBuckets,
		}, []string{"method", "path"}),

		httpRequestSize: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: namespace,
			Subsystem: "http",
			Name:      "request_size_bytes",
			Help:      "HTTP request body size distribution",
			Buckets:   opts.SizeBuckets,
		}, []string{"method", "path"}),

		httpResponseSize: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: namespace,
			Subsystem: "http",
			Name:      "response_size_bytes",
			Help:      "HTTP response body size distribution",
			Buckets:   opts.SizeBuckets,
		}, []string{"method", "path"}),

		httpActiveRequests: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: namespace,
			Subsystem: "http",
			Name:      "active_requests",
			Help:      "Number of requests currently being processed",
		}),

		dbQueriesTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: "db",
			Name:      "queries_total",
			Help:      "Total number of database queries",
		}, []string{"operation", "table"}),

		dbQueryDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: namespace,
			Subsystem: "db",
			Name:      "query_duration_seconds",
			Help:      "Database query latency distribution",
			Buckets:   dbDurationBuckets,
		}, []string{"operation", "table"}),

		dbConnectionsOpen: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: namespace,
			Subsystem: "db",
			Name:      "connections_open",
			Help:      "Number of open database connections in the pool",
		}),

		dbConnectionsInUse: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: namespace,
			Subsystem: "db",
			Name:      "connections_in_use",
			Help:      "Number of database connections actively in use",
		}),

		dbErrorsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: "db",
			Name:      "errors_total",
			Help:      "Total number of database errors",
		}, []string{"operation", "error_type"}),

		dbBusyTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: namespace, Subsystem: "db", Name: "busy_total",
			Help: "Number of SQLite busy or locked write attempts",
		}),

		dbWriteWait: prometheus.NewHistogram(prometheus.HistogramOpts{
			Namespace: namespace, Subsystem: "db", Name: "write_wait_seconds",
			Help:    "Database write transaction latency",
			Buckets: dbDurationBuckets,
		}),

		authAttemptsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: "auth",
			Name:      "attempts_total",
			Help:      "Total authentication attempts",
		}, []string{"method", "status"}),

		ownerTokensActive: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: namespace,
			Subsystem: "owner_tokens",
			Name:      "active",
			Help:      "Number of active owner tokens",
		}, []string{"kind"}),

		cacheHitsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: "cache",
			Name:      "hits_total",
			Help:      "Total number of cache hits",
		}, []string{"cache"}),

		cacheMissesTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: "cache",
			Name:      "misses_total",
			Help:      "Total number of cache misses",
		}, []string{"cache"}),

		cacheEvictionsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: "cache",
			Name:      "evictions_total",
			Help:      "Total number of cache evictions",
		}, []string{"cache"}),

		cacheSize: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: namespace,
			Subsystem: "cache",
			Name:      "size",
			Help:      "Current cache size (items)",
		}, []string{"cache"}),

		cacheBytes: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: namespace,
			Subsystem: "cache",
			Name:      "bytes",
			Help:      "Current cache size (bytes)",
		}, []string{"cache"}),

		schedulerTasksTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: "scheduler",
			Name:      "tasks_total",
			Help:      "Total number of scheduled task executions",
		}, []string{"task", "status"}),

		schedulerTaskDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: namespace,
			Subsystem: "scheduler",
			Name:      "task_duration_seconds",
			Help:      "Scheduled task execution duration",
			Buckets:   schedulerDurationBuckets,
		}, []string{"task"}),

		schedulerTasksRunning: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: namespace,
			Subsystem: "scheduler",
			Name:      "tasks_running",
			Help:      "Currently running scheduled task instances",
		}, []string{"task"}),

		schedulerLastRun: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: namespace,
			Subsystem: "scheduler",
			Name:      "last_run_timestamp",
			Help:      "Unix timestamp of last task execution",
		}, []string{"task"}),

		torEnabled: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: namespace,
			Subsystem: "tor",
			Name:      "enabled",
			Help:      "1 if Tor is enabled, 0 otherwise",
		}),

		torRunning: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: namespace,
			Subsystem: "tor",
			Name:      "running",
			Help:      "1 if the Tor process is running, 0 otherwise",
		}),

		torCircuitEstablished: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: namespace,
			Subsystem: "tor",
			Name:      "circuit_established",
			Help:      "1 if a circuit is established, 0 otherwise",
		}),

		torRequestsTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: "tor",
			Name:      "requests_total",
			Help:      "Total requests served via the Tor hidden service",
		}),

		ratelimitRequestsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: "ratelimit",
			Name:      "requests_total",
			Help:      "Total rate-limited requests evaluated",
		}, []string{"limit", "status"}),

		ratelimitBlockedTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: "ratelimit",
			Name:      "blocked_total",
			Help:      "Requests blocked by the rate limiter",
		}, []string{"limit"}),
	}

	registry.MustRegister(
		m.appInfo,
		m.appStartTimestamp,
		m.httpRequestsTotal,
		m.httpRequestDuration,
		m.httpRequestSize,
		m.httpResponseSize,
		m.httpActiveRequests,
		m.dbQueriesTotal,
		m.dbQueryDuration,
		m.dbConnectionsOpen,
		m.dbConnectionsInUse,
		m.dbErrorsTotal,
		m.dbBusyTotal,
		m.dbWriteWait,
		m.authAttemptsTotal,
		m.ownerTokensActive,
		m.cacheHitsTotal,
		m.cacheMissesTotal,
		m.cacheEvictionsTotal,
		m.cacheSize,
		m.cacheBytes,
		m.schedulerTasksTotal,
		m.schedulerTaskDuration,
		m.schedulerTasksRunning,
		m.schedulerLastRun,
		m.torEnabled,
		m.torRunning,
		m.torCircuitEstablished,
		m.torRequestsTotal,
		m.ratelimitRequestsTotal,
		m.ratelimitBlockedTotal,
	)
	registry.MustRegister(prometheus.NewGaugeFunc(prometheus.GaugeOpts{
		Namespace: namespace,
		Subsystem: "app",
		Name:      "uptime_seconds",
		Help:      "Seconds since application start",
	}, func() float64 {
		return time.Since(m.startTime).Seconds()
	}))

	if opts.IncludeSystem {
		m.system = newSystemMetrics(opts.DataDir)
		registry.MustRegister(m.system.collectors()...)
	}
	if opts.IncludeRuntime {
		m.rt = newRuntimeMetrics()
		registry.MustRegister(m.rt.collectors()...)
	}

	m.appStartTimestamp.Set(float64(m.startTime.Unix()))
	m.ownerTokensActive.WithLabelValues("primary").Set(0)
	m.ownerTokensActive.WithLabelValues("linked").Set(0)

	return m
}

// SetBuildInfo records the application's version/build labels. Callers pass
// this once at startup after version info has been resolved.
func (m *Metrics) SetBuildInfo(version, commit, buildDate string) {
	m.appInfo.WithLabelValues(version, commit, buildDate, runtime.Version()).Set(1)
}

// RecordRequest records a completed HTTP request. path must already be a
// normalized route pattern (e.g. via NormalizePath), never a raw request
// path, to keep label cardinality bounded.
func (m *Metrics) RecordRequest(method, path, status string, duration time.Duration, requestSize, responseSize int) {
	m.httpRequestsTotal.WithLabelValues(method, path, status).Inc()
	m.httpRequestDuration.WithLabelValues(method, path).Observe(duration.Seconds())
	m.httpRequestSize.WithLabelValues(method, path).Observe(float64(requestSize))
	m.httpResponseSize.WithLabelValues(method, path).Observe(float64(responseSize))
}

// IncActiveRequests increments the in-flight request gauge.
func (m *Metrics) IncActiveRequests() {
	m.httpActiveRequests.Inc()
}

// DecActiveRequests decrements the in-flight request gauge.
func (m *Metrics) DecActiveRequests() {
	m.httpActiveRequests.Dec()
}

// ServePrometheus serves metrics in Prometheus text exposition format.
func (m *Metrics) ServePrometheus(w http.ResponseWriter, r *http.Request) {
	promhttp.HandlerFor(m.registry, promhttp.HandlerOpts{}).ServeHTTP(w, r)
}

// StartCollectors starts the periodic system/runtime and owner-token metric
// samplers included at construction. Safe to call once at server startup.
func (m *Metrics) StartCollectors() {
	if m.system == nil && m.rt == nil && m.opts.OwnerTokenCounts == nil {
		return
	}
	go m.collectLoop()
}

// StopCollectors stops the periodic sampler started by StartCollectors.
// Safe to call multiple times and safe to call even if StartCollectors was
// never called.
func (m *Metrics) StopCollectors() {
	m.stopOnce.Do(func() {
		close(m.stopCollect)
	})
}

func (m *Metrics) collectLoop() {
	ticker := time.NewTicker(m.opts.CollectInterval)
	defer ticker.Stop()

	m.sample()
	for {
		select {
		case <-ticker.C:
			m.sample()
		case <-m.stopCollect:
			return
		}
	}
}

func (m *Metrics) sample() {
	if m.opts.OwnerTokenCounts != nil {
		primary, linked, err := m.opts.OwnerTokenCounts()
		if err != nil {
			log.Printf("Metrics: unable to count active owner tokens: %v", err)
		} else {
			m.ownerTokensActive.WithLabelValues("primary").Set(primary)
			m.ownerTokensActive.WithLabelValues("linked").Set(linked)
		}
	}
	if m.system != nil {
		m.system.collect()
	}
	if m.rt != nil {
		m.rt.collect()
	}
}
