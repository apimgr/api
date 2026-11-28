package server

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/apimgr/api/src/common/i18n"
	"github.com/apimgr/api/src/config"
	"github.com/apimgr/api/src/paths"
	"github.com/apimgr/api/src/server/handler"
)

type healthPageData struct {
	PageData
	Health                 handler.HealthResponse
	StatusClass            string
	StatusIcon             string
	StatusLabel            string
	RestartReasons         string
	MaintenanceSince       string
	MaintenanceMessage     string
	MaintenanceNextAttempt string
	TimestampText          string
	TimestampISO           string
	BuildDateText          string
	BuildDateISO           string
	ModeLabel              string
	Features               []healthFeatureRow
	Checks                 []healthCheckRow
	RequestTotal           string
	Request24h             string
	ActiveConns            string
}

type healthFeatureRow struct {
	Icon       string
	Label      string
	Class      string
	Status     string
	Hostname   string
	HasAddress bool
}

type healthCheckRow struct {
	Label       string
	Status      string
	StatusClass string
	StatusIcon  string
}

func renderHealthPage(cfg *config.ServerConfig) func(http.ResponseWriter, *http.Request, int, handler.HealthResponse) {
	return func(w http.ResponseWriter, r *http.Request, code int, response handler.HealthResponse) {
		data := newHealthPageData(cfg, r, response)
		data.DocumentTitle = paths.ProjectName + " - " + i18n.Translate(data.Lang, "health.title")
		data.PageTitle = i18n.Translate(data.Lang, "health.title")
		data.PageDescription = data.PageTitle
		data.NoIndex = true
		data.Layout = "public"
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(code)
		renderPageWithFallback(w, "healthz", data, data.PageData)
	}
}

func newHealthPageData(cfg *config.ServerConfig, r *http.Request, response handler.HealthResponse) healthPageData {
	page := newPageData(cfg, r, "healthz")
	statusClass, statusIcon, statusKey := handler.HealthStatusPresentation(response.Status)
	data := healthPageData{
		PageData:       page,
		Health:         response,
		StatusClass:    statusClass,
		StatusIcon:     statusIcon,
		StatusLabel:    i18n.Translate(page.Lang, statusKey),
		RestartReasons: strings.Join(response.RestartReason, ", "),
		TimestampText:  response.Timestamp.UTC().Format("January 02, 2006 at 15:04:05 MST"),
		TimestampISO:   response.Timestamp.UTC().Format(time.RFC3339),
		ModeLabel:      healthModeLabel(page.Lang, response.Mode),
		RequestTotal:   formatHealthCount(response.Stats.RequestsTotal),
		Request24h:     formatHealthCount(response.Stats.Requests24h),
		ActiveConns:    formatHealthCount(int64(response.Stats.ActiveConns)),
	}
	if response.Maintenance != nil {
		data.MaintenanceSince = response.Maintenance.Since.UTC().Format(time.RFC3339)
		maintenanceKey := "health.maintenance_reason." + response.Maintenance.Reason
		data.MaintenanceMessage = i18n.Translate(page.Lang, maintenanceKey)
		if data.MaintenanceMessage == maintenanceKey {
			data.MaintenanceMessage = response.Maintenance.Message
		}
		if response.Maintenance.SelfHealing.NextAttempt != nil {
			data.MaintenanceNextAttempt = response.Maintenance.SelfHealing.NextAttempt.UTC().Format(time.RFC3339)
		}
	}
	if builtAt, err := time.Parse(time.RFC3339, response.Build.Date); err == nil {
		data.BuildDateISO = builtAt.UTC().Format(time.RFC3339)
		data.BuildDateText = builtAt.UTC().Format("January 02, 2006 at 15:04:05 MST")
	} else {
		data.BuildDateText = response.Build.Date
		data.BuildDateISO = response.Build.Date
	}
	data.Features = []healthFeatureRow{
		newHealthFeature(page.Lang, "🧅", "health.feature.tor", response.Features.Tor.Enabled, response.Features.Tor.Status, response.Features.Tor.Hostname),
		newHealthFeature(page.Lang, "🔗", "health.feature.i2p", response.Features.I2P.Enabled, response.Features.I2P.Status, response.Features.I2P.Hostname),
		{Icon: "🌍", Label: i18n.Translate(page.Lang, "health.feature.geoip"), Class: featureClass(response.Features.GeoIP), Status: i18n.Translate(page.Lang, enabledStatusKey(response.Features.GeoIP))},
	}
	core := []struct{ key, label string }{
		{"database", "health.check.database"}, {"cache", "health.check.cache"},
		{"disk", "health.check.disk"}, {"scheduler", "health.check.scheduler"},
		{"tor", "health.check.tor"}, {"i2p", "health.check.i2p"},
	}
	values := map[string]string{"database": response.Checks.Database, "cache": response.Checks.Cache, "disk": response.Checks.Disk, "scheduler": response.Checks.Scheduler, "tor": response.Checks.Tor, "i2p": response.Checks.I2P}
	for _, item := range core {
		if value := values[item.key]; value != "" {
			data.Checks = append(data.Checks, newHealthCheck(page.Lang, i18n.Translate(page.Lang, item.label), value))
		}
	}
	for _, item := range response.Checks.Datasets {
		var label string
		switch {
		case item.Key == "cve_nvd":
			label = i18n.Translate(page.Lang, "health.check.cve_nvd")
		case item.Key == "trivy_db":
			label = i18n.Translate(page.Lang, "health.check.trivy_db")
		case strings.HasPrefix(item.Key, "blocklist_"):
			label = i18n.TranslateFormat(page.Lang, "health.check.blocklist_source", map[string]string{
				"source_id": strings.TrimPrefix(item.Key, "blocklist_"),
			})
		default:
			label = i18n.TranslateFormat(page.Lang, "health.check.unknown", map[string]string{
				"check_id": item.Key,
			})
		}
		data.Checks = append(data.Checks, newHealthCheck(page.Lang, label, item.Status))
	}
	return data
}

func newHealthFeature(lang, icon, key string, enabled bool, status, hostname string) healthFeatureRow {
	statusKey := "health.feature_status." + status
	if !enabled {
		statusKey = "health.feature_status.disabled"
	}
	return healthFeatureRow{Icon: icon, Label: i18n.Translate(lang, key), Class: featureClass(enabled), Status: i18n.Translate(lang, statusKey), Hostname: hostname, HasAddress: hostname != ""}
}

func newHealthCheck(lang, label, value string) healthCheckRow {
	class, icon, key := "status-warning", "⚠️", "health.check_status.degraded"
	switch value {
	case "ok":
		class, icon, key = "status-ok", "✅", "health.check_status.ok"
	case "error":
		class, icon, key = "status-error", "❌", "health.check_status.error"
	}
	return healthCheckRow{Label: label, Status: i18n.Translate(lang, key), StatusClass: class, StatusIcon: icon}
}

func featureClass(enabled bool) string {
	if enabled {
		return "feature-enabled"
	}
	return "feature-disabled"
}

func enabledStatusKey(enabled bool) string {
	if enabled {
		return "health.feature_status.enabled"
	}
	return "health.feature_status.disabled"
}

func healthModeLabel(lang, mode string) string {
	key := "health.mode." + mode
	translated := i18n.Translate(lang, key)
	if translated != key {
		return translated
	}
	return mode
}

func formatHealthCount(value int64) string {
	digits := fmt.Sprintf("%d", value)
	if value < 0 {
		return digits
	}
	var out strings.Builder
	for index, digit := range digits {
		if index > 0 && (len(digits)-index)%3 == 0 {
			out.WriteByte(',')
		}
		out.WriteByte(byte(digit))
	}
	return out.String()
}
