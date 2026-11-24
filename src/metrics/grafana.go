package metrics

import "fmt"

// grafanaTarget is one Prometheus query bound to a dashboard panel.
type grafanaTarget struct {
	Expr         string `json:"expr"`
	LegendFormat string `json:"legendFormat"`
}

// grafanaPanel is one panel of the served dashboard.
type grafanaPanel struct {
	Title   string          `json:"title"`
	Type    string          `json:"type"`
	Targets []grafanaTarget `json:"targets"`
}

// grafanaDashboard is the top-level document served at the Grafana metrics
// endpoint. Datasource is intentionally omitted - Grafana resolves it via a
// template variable at import time, per AI.md PART 20.
type grafanaDashboard struct {
	Title  string         `json:"title"`
	Panels []grafanaPanel `json:"panels"`
}

// buildGrafanaDashboard returns the dashboard document for projectName,
// covering every metric category required by AI.md PART 20.
func buildGrafanaDashboard(projectName string) grafanaDashboard {
	ns := namespace

	return grafanaDashboard{
		Title: fmt.Sprintf("%s Metrics", projectName),
		Panels: []grafanaPanel{
			{
				Title: "Request Rate",
				Type:  "graph",
				Targets: []grafanaTarget{
					{Expr: fmt.Sprintf("rate(%s_http_requests_total[5m])", ns), LegendFormat: "{{method}} {{path}}"},
				},
			},
			{
				Title: "Error Rate",
				Type:  "graph",
				Targets: []grafanaTarget{
					{Expr: fmt.Sprintf(`rate(%s_http_requests_total{status=~"5.."}[5m])`, ns), LegendFormat: "{{method}} {{path}}"},
				},
			},
			{
				Title: "Latency p50/p95/p99",
				Type:  "graph",
				Targets: []grafanaTarget{
					{Expr: fmt.Sprintf("histogram_quantile(0.50, rate(%s_http_request_duration_seconds_bucket[5m]))", ns), LegendFormat: "p50"},
					{Expr: fmt.Sprintf("histogram_quantile(0.95, rate(%s_http_request_duration_seconds_bucket[5m]))", ns), LegendFormat: "p95"},
					{Expr: fmt.Sprintf("histogram_quantile(0.99, rate(%s_http_request_duration_seconds_bucket[5m]))", ns), LegendFormat: "p99"},
				},
			},
			{
				Title: "Active Requests",
				Type:  "stat",
				Targets: []grafanaTarget{
					{Expr: fmt.Sprintf("%s_http_active_requests", ns), LegendFormat: "active"},
				},
			},
			{
				Title: "Database Connections",
				Type:  "graph",
				Targets: []grafanaTarget{
					{Expr: fmt.Sprintf("%s_db_connections_open", ns), LegendFormat: "open"},
					{Expr: fmt.Sprintf("%s_db_connections_in_use", ns), LegendFormat: "in_use"},
				},
			},
			{
				Title: "Cache Hit Rate",
				Type:  "gauge",
				Targets: []grafanaTarget{
					{
						Expr: fmt.Sprintf(
							"sum(rate(%s_cache_hits_total[5m])) / (sum(rate(%s_cache_hits_total[5m])) + sum(rate(%s_cache_misses_total[5m])))",
							ns, ns, ns,
						),
						LegendFormat: "hit rate",
					},
				},
			},
			{
				Title: "Scheduler Tasks",
				Type:  "graph",
				Targets: []grafanaTarget{
					{Expr: fmt.Sprintf("%s_scheduler_tasks_running", ns), LegendFormat: "{{task}}"},
					{Expr: fmt.Sprintf("rate(%s_scheduler_tasks_total[5m])", ns), LegendFormat: "{{task}} {{status}}"},
				},
			},
			{
				Title: "Memory Usage",
				Type:  "graph",
				Targets: []grafanaTarget{
					{Expr: fmt.Sprintf("%s_system_memory_usage_percent", ns), LegendFormat: "memory usage percent"},
					{Expr: fmt.Sprintf("%s_system_disk_usage_percent", ns), LegendFormat: "disk usage percent"},
				},
			},
			{
				Title: "Goroutines",
				Type:  "graph",
				Targets: []grafanaTarget{
					{Expr: fmt.Sprintf("%s_go_goroutines", ns), LegendFormat: "goroutines"},
				},
			},
			{
				Title: "Uptime",
				Type:  "stat",
				Targets: []grafanaTarget{
					{Expr: fmt.Sprintf("%s_app_uptime_seconds", ns), LegendFormat: "uptime"},
				},
			},
			{
				Title: "Active Owner Tokens",
				Type:  "graph",
				Targets: []grafanaTarget{
					{Expr: fmt.Sprintf("%s_owner_tokens_active", ns), LegendFormat: "{{kind}}"},
				},
			},
		},
	}
}
