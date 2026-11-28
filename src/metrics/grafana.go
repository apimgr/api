package metrics

import (
	"fmt"
	"strings"

	"github.com/apimgr/api/src/common/sanitize"
)

type grafanaV2Dashboard struct {
	Kind       string            `json:"kind"`
	APIVersion string            `json:"apiVersion"`
	Metadata   grafanaV2Metadata `json:"metadata"`
	Spec       grafanaV2Spec     `json:"spec"`
}

type grafanaV2Metadata struct {
	Name string `json:"name"`
}

type grafanaV2Spec struct {
	Title        string                `json:"title"`
	Description  string                `json:"description"`
	Tags         []string              `json:"tags"`
	CursorSync   string                `json:"cursorSync"`
	Preload      bool                  `json:"preload"`
	Editable     bool                  `json:"editable"`
	Links        []string              `json:"links"`
	Annotations  []string              `json:"annotations"`
	Variables    []grafanaV2Variable   `json:"variables"`
	TimeSettings grafanaV2TimeSettings `json:"timeSettings"`
	Elements     sanitize.TaggedObject `json:"elements"`
	Layout       grafanaV2GridLayout   `json:"layout"`
}

type grafanaV2Variable struct {
	Kind string                      `json:"kind"`
	Spec grafanaV2DatasourceVariable `json:"spec"`
}

type grafanaV2DatasourceVariable struct {
	Name       string                    `json:"name"`
	PluginID   string                    `json:"pluginId"`
	Refresh    string                    `json:"refresh"`
	Regex      string                    `json:"regex"`
	Current    grafanaV2VariableOption   `json:"current"`
	Options    []grafanaV2VariableOption `json:"options"`
	Multi      bool                      `json:"multi"`
	IncludeAll bool                      `json:"includeAll"`
	Label      string                    `json:"label"`
	Hide       string                    `json:"hide"`
}

type grafanaV2VariableOption struct {
	Text  string `json:"text"`
	Value string `json:"value"`
}

type grafanaV2TimeSettings struct {
	From                 string   `json:"from"`
	To                   string   `json:"to"`
	Timezone             string   `json:"timezone"`
	AutoRefresh          string   `json:"autoRefresh"`
	AutoRefreshIntervals []string `json:"autoRefreshIntervals"`
	HideTimepicker       bool     `json:"hideTimepicker"`
	FiscalYearStartMonth int      `json:"fiscalYearStartMonth"`
}

type grafanaV2GridLayout struct {
	Kind string                  `json:"kind"`
	Spec grafanaV2GridLayoutSpec `json:"spec"`
}

type grafanaV2GridLayoutSpec struct {
	Items []grafanaV2GridItem `json:"items"`
}

type grafanaV2GridItem struct {
	Kind string                `json:"kind"`
	Spec grafanaV2GridItemSpec `json:"spec"`
}

type grafanaV2GridItemSpec struct {
	X       int                       `json:"x"`
	Y       int                       `json:"y"`
	Width   int                       `json:"width"`
	Height  int                       `json:"height"`
	Element grafanaV2ElementReference `json:"element"`
}

type grafanaV2ElementReference struct {
	Kind string `json:"kind"`
	Name string `json:"name"`
}

type grafanaV2Element struct {
	Kind string             `json:"kind"`
	Spec grafanaV2PanelSpec `json:"spec"`
}

type grafanaV2PanelSpec struct {
	ID          int                 `json:"id"`
	Title       string              `json:"title"`
	Description string              `json:"description"`
	Links       []string            `json:"links"`
	Data        grafanaV2QueryGroup `json:"data"`
	VizConfig   grafanaV2VizConfig  `json:"vizConfig"`
}

type grafanaV2QueryGroup struct {
	Kind string                  `json:"kind"`
	Spec grafanaV2QueryGroupSpec `json:"spec"`
}

type grafanaV2QueryGroupSpec struct {
	Queries         []grafanaV2PanelQuery `json:"queries"`
	Transformations []string              `json:"transformations"`
	QueryOptions    sanitize.TaggedObject `json:"queryOptions"`
}

type grafanaV2PanelQuery struct {
	Kind string                  `json:"kind"`
	Spec grafanaV2PanelQuerySpec `json:"spec"`
}

type grafanaV2PanelQuerySpec struct {
	RefID  string             `json:"refId"`
	Hidden bool               `json:"hidden"`
	Query  grafanaV2DataQuery `json:"query"`
}

type grafanaV2DataQuery struct {
	Kind       string                 `json:"kind"`
	Group      string                 `json:"group"`
	Version    string                 `json:"version"`
	Datasource grafanaV2DatasourceRef `json:"datasource"`
	Spec       grafanaV2DataQuerySpec `json:"spec"`
}

type grafanaV2DatasourceRef struct {
	Name string `json:"name"`
}

type grafanaV2DataQuerySpec struct {
	Expr         string `json:"expr"`
	LegendFormat string `json:"legendFormat"`
}

type grafanaV2VizConfig struct {
	Kind    string                 `json:"kind"`
	Group   string                 `json:"group"`
	Version string                 `json:"version"`
	Spec    grafanaV2VizConfigSpec `json:"spec"`
}

type grafanaV2VizConfigSpec struct {
	Options     sanitize.TaggedObject `json:"options"`
	FieldConfig grafanaV2FieldConfig  `json:"fieldConfig"`
}

type grafanaV2FieldConfig struct {
	Defaults  sanitize.TaggedObject `json:"defaults"`
	Overrides []string              `json:"overrides"`
}

type grafanaPanelDefinition struct {
	title   string
	visual  string
	queries []grafanaQueryDefinition
}

type grafanaQueryDefinition struct {
	expression string
	legend     string
}

// buildGrafanaDashboard returns an importable Grafana V2 Resource dashboard
// with one datasource variable and panels for all metrics this project exports.
func buildGrafanaDashboard(projectName string) grafanaV2Dashboard {
	const datasourceVariable = "$datasource"
	ns := namespace

	definitions := []grafanaPanelDefinition{
		{title: "Request Rate", visual: "timeseries", queries: []grafanaQueryDefinition{
			{expression: fmt.Sprintf("rate(%s_http_requests_total[5m])", ns), legend: "{{method}} {{path}}"},
		}},
		{title: "Error Rate", visual: "timeseries", queries: []grafanaQueryDefinition{
			{expression: fmt.Sprintf(`rate(%s_http_requests_total{status=~"5.."}[5m])`, ns), legend: "{{method}} {{path}}"},
		}},
		{title: "Latency p50/p95/p99", visual: "timeseries", queries: []grafanaQueryDefinition{
			{expression: fmt.Sprintf("histogram_quantile(0.50, rate(%s_http_request_duration_seconds_bucket[5m]))", ns), legend: "p50"},
			{expression: fmt.Sprintf("histogram_quantile(0.95, rate(%s_http_request_duration_seconds_bucket[5m]))", ns), legend: "p95"},
			{expression: fmt.Sprintf("histogram_quantile(0.99, rate(%s_http_request_duration_seconds_bucket[5m]))", ns), legend: "p99"},
		}},
		{title: "Active Requests", visual: "stat", queries: []grafanaQueryDefinition{
			{expression: fmt.Sprintf("%s_http_active_requests", ns), legend: "active"},
		}},
		{title: "Database Connections", visual: "timeseries", queries: []grafanaQueryDefinition{
			{expression: fmt.Sprintf("%s_db_connections_open", ns), legend: "open"},
			{expression: fmt.Sprintf("%s_db_connections_in_use", ns), legend: "in_use"},
		}},
		{title: "Cache Hit Rate", visual: "timeseries", queries: []grafanaQueryDefinition{
			{expression: fmt.Sprintf("sum(rate(%s_cache_hits_total[5m])) / (sum(rate(%s_cache_hits_total[5m])) + sum(rate(%s_cache_misses_total[5m])))", ns, ns, ns), legend: "hit rate"},
		}},
		{title: "Scheduler Tasks", visual: "timeseries", queries: []grafanaQueryDefinition{
			{expression: fmt.Sprintf("%s_scheduler_tasks_running", ns), legend: "{{task}}"},
			{expression: fmt.Sprintf("rate(%s_scheduler_tasks_total[5m])", ns), legend: "{{task}} {{status}}"},
		}},
		{title: "Memory Usage", visual: "timeseries", queries: []grafanaQueryDefinition{
			{expression: fmt.Sprintf("%s_system_memory_usage_percent", ns), legend: "memory usage percent"},
			{expression: fmt.Sprintf("%s_system_disk_usage_percent", ns), legend: "disk usage percent"},
		}},
		{title: "Goroutines", visual: "timeseries", queries: []grafanaQueryDefinition{
			{expression: fmt.Sprintf("%s_go_goroutines", ns), legend: "goroutines"},
		}},
		{title: "Uptime", visual: "stat", queries: []grafanaQueryDefinition{
			{expression: fmt.Sprintf("%s_app_uptime_seconds", ns), legend: "uptime"},
		}},
		{title: "Active Owner Tokens", visual: "timeseries", queries: []grafanaQueryDefinition{
			{expression: fmt.Sprintf("%s_owner_tokens_active", ns), legend: "{{kind}}"},
		}},
	}

	elements := make(sanitize.TaggedObject, 0, len(definitions))
	items := make([]grafanaV2GridItem, 0, len(definitions))
	for index, definition := range definitions {
		name := fmt.Sprintf("panel-%d", index+1)
		queries := make([]grafanaV2PanelQuery, 0, len(definition.queries))
		for queryIndex, query := range definition.queries {
			queries = append(queries, grafanaV2PanelQuery{
				Kind: "PanelQuery",
				Spec: grafanaV2PanelQuerySpec{
					RefID: string(rune('A' + queryIndex)),
					Query: grafanaV2DataQuery{
						Kind: "DataQuery", Group: "prometheus", Version: "v0",
						Datasource: grafanaV2DatasourceRef{Name: datasourceVariable},
						Spec:       grafanaV2DataQuerySpec{Expr: query.expression, LegendFormat: query.legend},
					},
				},
			})
		}
		panel := grafanaV2Element{
			Kind: "Panel",
			Spec: grafanaV2PanelSpec{
				ID: index + 1, Title: definition.title, Links: []string{},
				Data: grafanaV2QueryGroup{
					Kind: "QueryGroup",
					Spec: grafanaV2QueryGroupSpec{Queries: queries, Transformations: []string{}, QueryOptions: sanitize.TaggedObject{}},
				},
				VizConfig: grafanaV2VizConfig{
					Kind: "VizConfig", Group: definition.visual, Version: "13.0.0",
					Spec: grafanaV2VizConfigSpec{
						Options:     sanitize.TaggedObject{},
						FieldConfig: grafanaV2FieldConfig{Defaults: sanitize.TaggedObject{}, Overrides: []string{}},
					},
				},
			},
		}
		elements = append(elements, sanitize.TaggedField{Name: name, Value: panel})
		items = append(items, grafanaV2GridItem{
			Kind: "GridLayoutItem",
			Spec: grafanaV2GridItemSpec{
				X: 0, Y: index * 8, Width: 12, Height: 8,
				Element: grafanaV2ElementReference{Kind: "ElementReference", Name: name},
			},
		})
	}

	return grafanaV2Dashboard{
		Kind: "Dashboard", APIVersion: "dashboard.grafana.app/v2",
		Metadata: grafanaV2Metadata{Name: dashboardResourceName(projectName)},
		Spec: grafanaV2Spec{
			Title: fmt.Sprintf("%s Metrics", projectName), CursorSync: "Off", Editable: true,
			Tags: []string{}, Links: []string{}, Annotations: []string{},
			Variables: []grafanaV2Variable{{
				Kind: "DatasourceVariable",
				Spec: grafanaV2DatasourceVariable{
					Name: "datasource", PluginID: "prometheus", Refresh: "onDashboardLoad",
					Current: grafanaV2VariableOption{},
					Options: []grafanaV2VariableOption{}, Label: "Datasource", Hide: "dontHide",
				},
			}},
			TimeSettings: grafanaV2TimeSettings{
				From: "now-6h", To: "now", Timezone: "browser",
				AutoRefreshIntervals: []string{"5s", "10s", "30s", "1m", "5m", "15m", "30m", "1h", "2h", "1d"},
			},
			Elements: elements,
			Layout:   grafanaV2GridLayout{Kind: "GridLayout", Spec: grafanaV2GridLayoutSpec{Items: items}},
		},
	}
}

func dashboardResourceName(projectName string) string {
	name := strings.ToLower(projectName)
	var output strings.Builder
	separator := false
	for _, r := range name {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			if separator && output.Len() > 0 {
				output.WriteByte('-')
			}
			output.WriteRune(r)
			separator = false
		} else {
			separator = true
		}
	}
	base := strings.Trim(output.String(), "-")
	if base == "" {
		base = "api"
	}
	if len(base) > 48 {
		base = strings.Trim(base[:48], "-")
	}
	return base + "-metrics"
}
