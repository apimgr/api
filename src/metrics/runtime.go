package metrics

import (
	"runtime"

	"github.com/prometheus/client_golang/prometheus"
)

// runtimeMetrics holds the optional Go runtime gauges enabled via
// server.metrics.include_runtime, per AI.md PART 20.
type runtimeMetrics struct {
	goroutines    prometheus.Gauge
	memAllocBytes prometheus.Gauge
	memSysBytes   prometheus.Gauge
	gcRunsTotal   prometheus.Counter
	gcPauseTotal  prometheus.Counter

	lastNumGC   uint32
	lastPauseNs uint64
}

// newRuntimeMetrics constructs the Go runtime metric family, stdlib
// "runtime" only, no third-party dependency required.
func newRuntimeMetrics() *runtimeMetrics {
	return &runtimeMetrics{
		goroutines: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: namespace,
			Subsystem: "go",
			Name:      "goroutines",
			Help:      "Current number of goroutines",
		}),

		memAllocBytes: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: namespace,
			Subsystem: "go",
			Name:      "mem_alloc_bytes",
			Help:      "Bytes of heap memory currently allocated",
		}),

		memSysBytes: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: namespace,
			Subsystem: "go",
			Name:      "mem_sys_bytes",
			Help:      "Bytes of memory obtained from the OS",
		}),

		gcRunsTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: "go",
			Name:      "gc_runs_total",
			Help:      "Total number of completed garbage collection cycles",
		}),

		gcPauseTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: "go",
			Name:      "gc_pause_total_seconds",
			Help:      "Cumulative garbage collection stop-the-world pause time",
		}),
	}
}

// collectors returns every gauge/counter for one-shot registry.MustRegister calls.
func (r *runtimeMetrics) collectors() []prometheus.Collector {
	return []prometheus.Collector{
		r.goroutines,
		r.memAllocBytes,
		r.memSysBytes,
		r.gcRunsTotal,
		r.gcPauseTotal,
	}
}

// collect samples current runtime values. GC counters are cumulative
// deltas against the last sample, since Prometheus counters may only
// increase.
func (r *runtimeMetrics) collect() {
	r.goroutines.Set(float64(runtime.NumGoroutine()))

	var stats runtime.MemStats
	runtime.ReadMemStats(&stats)
	r.memAllocBytes.Set(float64(stats.HeapAlloc))
	r.memSysBytes.Set(float64(stats.Sys))

	if stats.NumGC > r.lastNumGC {
		r.gcRunsTotal.Add(float64(stats.NumGC - r.lastNumGC))
		r.lastNumGC = stats.NumGC
	}
	if stats.PauseTotalNs > r.lastPauseNs {
		deltaNs := stats.PauseTotalNs - r.lastPauseNs
		r.gcPauseTotal.Add(float64(deltaNs) / 1e9)
		r.lastPauseNs = stats.PauseTotalNs
	}
}
