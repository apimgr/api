// Package metrics implements PART 20's Prometheus-compatible metrics
// endpoint. All metric names are prefixed with "api_" (the project name)
// and follow Prometheus naming conventions (snake_case, base units,
// counters end in "_total").
package metrics

import (
	"net/http"
	"runtime"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

const namespace = "api"

// Metrics holds all Prometheus collectors exposed at /metrics.
type Metrics struct {
	registry *prometheus.Registry

	appInfo           *prometheus.GaugeVec
	appStartTimestamp prometheus.Gauge

	httpRequestsTotal   *prometheus.CounterVec
	httpRequestDuration *prometheus.HistogramVec
	httpRequestSize     *prometheus.HistogramVec
	httpResponseSize    *prometheus.HistogramVec
	httpActiveRequests  prometheus.Gauge

	startTime time.Time
}

var (
	globalMetrics *Metrics
	metricsOnce   sync.Once
)

// Get returns the singleton metrics instance.
func Get() *Metrics {
	metricsOnce.Do(func() {
		globalMetrics = newMetrics()
	})
	return globalMetrics
}

func newMetrics() *Metrics {
	registry := prometheus.NewRegistry()

	m := &Metrics{
		registry:  registry,
		startTime: time.Now(),

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
			Buckets:   []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10},
		}, []string{"method", "path"}),

		httpRequestSize: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: namespace,
			Subsystem: "http",
			Name:      "request_size_bytes",
			Help:      "HTTP request body size distribution",
			Buckets:   []float64{100, 1000, 10000, 100000, 1000000, 10000000},
		}, []string{"method", "path"}),

		httpResponseSize: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: namespace,
			Subsystem: "http",
			Name:      "response_size_bytes",
			Help:      "HTTP response body size distribution",
			Buckets:   []float64{100, 1000, 10000, 100000, 1000000, 10000000},
		}, []string{"method", "path"}),

		httpActiveRequests: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: namespace,
			Subsystem: "http",
			Name:      "active_requests",
			Help:      "Number of requests currently being processed",
		}),
	}

	registry.MustRegister(
		m.appInfo,
		m.appStartTimestamp,
		m.httpRequestsTotal,
		m.httpRequestDuration,
		m.httpRequestSize,
		m.httpResponseSize,
		m.httpActiveRequests,
	)
	registry.MustRegister(prometheus.NewGaugeFunc(prometheus.GaugeOpts{
		Namespace: namespace,
		Subsystem: "app",
		Name:      "uptime_seconds",
		Help:      "Seconds since application start",
	}, func() float64 {
		return time.Since(m.startTime).Seconds()
	}))

	m.appStartTimestamp.Set(float64(m.startTime.Unix()))

	return m
}

// SetBuildInfo records the application's version/build labels. Callers pass
// this once at startup after version info has been resolved.
func (m *Metrics) SetBuildInfo(version, commit, buildDate string) {
	m.appInfo.WithLabelValues(version, commit, buildDate, runtime.Version()).Set(1)
}

// RecordRequest records a completed HTTP request. path must already be a
// normalized route pattern (e.g. the chi route pattern), never a raw
// request path, to keep label cardinality bounded.
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
