package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/disk"
	"github.com/shirou/gopsutil/v3/mem"
)

// systemMetrics holds the optional CPU/memory/disk gauges enabled via
// server.metrics.include_system, per AI.md PART 20.
type systemMetrics struct {
	dataDir string

	cpuUsagePercent  prometheus.Gauge
	memUsagePercent  prometheus.Gauge
	memUsedBytes     prometheus.Gauge
	memTotalBytes    prometheus.Gauge
	diskUsagePercent *prometheus.GaugeVec
	diskUsedBytes    *prometheus.GaugeVec
	diskTotalBytes   *prometheus.GaugeVec
}

// newSystemMetrics constructs the system metric family. dataDir is reported
// as the "path" label on the disk gauges (the volume the app actually
// writes to), per PART 20's illustrative SystemCollector.
func newSystemMetrics(dataDir string) *systemMetrics {
	if dataDir == "" {
		dataDir = "/"
	}
	return &systemMetrics{
		dataDir: dataDir,

		cpuUsagePercent: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: namespace,
			Subsystem: "system",
			Name:      "cpu_usage_percent",
			Help:      "Current CPU usage percentage",
		}),

		memUsagePercent: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: namespace,
			Subsystem: "system",
			Name:      "memory_usage_percent",
			Help:      "Current memory usage percentage",
		}),

		memUsedBytes: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: namespace,
			Subsystem: "system",
			Name:      "memory_used_bytes",
			Help:      "Current memory used, in bytes",
		}),

		memTotalBytes: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: namespace,
			Subsystem: "system",
			Name:      "memory_total_bytes",
			Help:      "Total physical memory, in bytes",
		}),

		diskUsagePercent: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: namespace,
			Subsystem: "system",
			Name:      "disk_usage_percent",
			Help:      "Current disk usage percentage for the data directory volume",
		}, []string{"path"}),

		diskUsedBytes: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: namespace,
			Subsystem: "system",
			Name:      "disk_used_bytes",
			Help:      "Disk space used on the data directory volume, in bytes",
		}, []string{"path"}),

		diskTotalBytes: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: namespace,
			Subsystem: "system",
			Name:      "disk_total_bytes",
			Help:      "Total disk space on the data directory volume, in bytes",
		}, []string{"path"}),
	}
}

// collectors returns every gauge for one-shot registry.MustRegister calls.
func (s *systemMetrics) collectors() []prometheus.Collector {
	return []prometheus.Collector{
		s.cpuUsagePercent,
		s.memUsagePercent,
		s.memUsedBytes,
		s.memTotalBytes,
		s.diskUsagePercent,
		s.diskUsedBytes,
		s.diskTotalBytes,
	}
}

// collect samples current CPU/memory/disk values. Failures are ignored -
// System metrics are best-effort and must never block or crash the server,
// consistent with PART 20's fail-open posture for optional metric families.
func (s *systemMetrics) collect() {
	s.collectCPU()
	s.collectMemory()
	s.collectDisk()
}

func (s *systemMetrics) collectCPU() {
	percents, err := cpu.Percent(0, false)
	if err != nil || len(percents) == 0 {
		return
	}
	s.cpuUsagePercent.Set(percents[0])
}

func (s *systemMetrics) collectMemory() {
	v, err := mem.VirtualMemory()
	if err != nil {
		return
	}
	s.memUsagePercent.Set(v.UsedPercent)
	s.memUsedBytes.Set(float64(v.Used))
	s.memTotalBytes.Set(float64(v.Total))
}

func (s *systemMetrics) collectDisk() {
	u, err := disk.Usage(s.dataDir)
	if err != nil {
		return
	}
	s.diskUsagePercent.WithLabelValues(s.dataDir).Set(u.UsedPercent)
	s.diskUsedBytes.WithLabelValues(s.dataDir).Set(float64(u.Used))
	s.diskTotalBytes.WithLabelValues(s.dataDir).Set(float64(u.Total))
}
