package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"net/http"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/apimgr/api/src/config"
	"github.com/apimgr/api/src/tor"
)

// Health status values (PART 13). These are the only permitted values of the
// top-level "status" field. "maintenance" is a STATUS value only; the "mode"
// field always reports the configured application mode.
const (
	StatusHealthy         = "healthy"
	StatusDegraded        = "degraded"
	StatusRestartRequired = "restart_required"
	StatusUnhealthy       = "unhealthy"
	StatusMaintenance     = "maintenance"
	StatusShuttingDown    = "shutting_down"
)

var (
	// startTime is when the server started
	startTime = time.Now()
	// Version information (set from main via ldflags)
	Version   = "dev"
	CommitID  = "unknown"
	BuildDate = "unknown"
	// dbPinger for health checks (set from main)
	dbPinger interface{ PingContext(context.Context) error }
	// schedulerProbe reports the internal scheduler's health (set from main).
	// The scheduler is mandatory and always running per AI.md PART 18, so this
	// is a health probe, not an enable/disable toggle.
	schedulerProbe func() error
	// torProbe reports the live Tor hidden-service state (set from main). When
	// unset the handler falls back to the process-wide Tor manager, so an
	// embedding that never registered a probe still reports real state.
	torProbe func() TorInfo
	// i2pProbe reports the live eepsite state (set from main). I2P is opt-in,
	// so an unset probe means the feature is off.
	i2pProbe func() I2PInfo
	// geoipProbe reports whether the GeoIP databases are usable (set from
	// main). It refines the configured toggle: the feature is only advertised
	// when it is both enabled in config and loaded at runtime.
	geoipProbe func() bool
	// statsProvider supplies the public-safe request aggregates (set from
	// main). When unset the handler's own in-process collector is used.
	statsProvider func() (total, last24h int64, active int)
)

var (
	// lifecycleMu guards the process lifecycle state below, which is written
	// by the server lifecycle (maintenance toggle, config watcher, shutdown
	// handler) and read concurrently by every health request.
	lifecycleMu sync.RWMutex
	// maintenanceMode reports whether maintenance mode is currently active
	maintenanceMode bool
	// shuttingDown reports whether a graceful shutdown is in progress
	shuttingDown bool
	// pendingRestart reports whether a config change requires a restart
	pendingRestart bool
	// restartReason lists the changed settings that require the restart
	restartReason []string
)

// HealthResponse is the canonical /server/healthz response structure (PART 13)
// Fields are ordered exactly as required by the spec.
type HealthResponse struct {
	Project ProjectInfo `json:"project"`

	Status         string   `json:"status"`
	PendingRestart bool     `json:"pending_restart,omitempty"`
	RestartReason  []string `json:"restart_reason,omitempty"`

	Version   string    `json:"version"`
	GoVersion string    `json:"go_version"`
	Build     BuildInfo `json:"build"`

	Uptime    string    `json:"uptime"`
	Mode      string    `json:"mode"`
	Timestamp time.Time `json:"timestamp"`

	Features FeaturesInfo `json:"features"`
	Checks   ChecksInfo   `json:"checks"`
	Stats    StatsInfo    `json:"stats"`
}

// ProjectInfo is sourced from branding config (PART 16)
type ProjectInfo struct {
	Name        string `json:"name"`
	Tagline     string `json:"tagline"`
	Description string `json:"description"`
}

// BuildInfo is sourced from build-time ldflags variables (PART 7)
type BuildInfo struct {
	Commit string `json:"commit"`
	Date   string `json:"date"`
}

// FeaturesInfo lists PUBLIC, non-negotiable features only
type FeaturesInfo struct {
	Tor   TorInfo `json:"tor"`
	I2P   I2PInfo `json:"i2p"`
	GeoIP bool    `json:"geoip"`
}

// TorInfo is sourced from the Tor manager (PART 31.1)
type TorInfo struct {
	Enabled  bool   `json:"enabled"`
	Running  bool   `json:"running"`
	Status   string `json:"status"`
	Hostname string `json:"hostname"`
}

// I2PInfo is sourced from the I2P manager (PART 31.2). I2P is opt-in, so
// every field stays at its zero value while the eepsite is disabled.
type I2PInfo struct {
	Enabled  bool   `json:"enabled"`
	Running  bool   `json:"running"`
	Status   string `json:"status"`
	Hostname string `json:"hostname"`
	Provider string `json:"provider"`
}

// ChecksInfo reports component health as "ok"/"error" only, no details
type ChecksInfo struct {
	Database  string `json:"database"`
	Cache     string `json:"cache"`
	Disk      string `json:"disk"`
	Scheduler string `json:"scheduler"`
	Tor       string `json:"tor,omitempty"`
	I2P       string `json:"i2p,omitempty"`
}

// StatsInfo reports public-safe aggregate statistics
type StatsInfo struct {
	RequestsTotal int64 `json:"requests_total"`
	Requests24h   int64 `json:"requests_24h"`
	ActiveConns   int   `json:"active_connections"`
}

// VersionResponse represents the version endpoint response
type VersionResponse struct {
	Version   string `json:"version"`
	CommitID  string `json:"commit_id"`
	BuildDate string `json:"build_date"`
	GoVersion string `json:"go_version"`
	Platform  string `json:"platform"`
	Arch      string `json:"arch"`
}

// SetDatabase sets the database connection for health checks
func SetDatabase(db interface{ PingContext(context.Context) error }) {
	dbPinger = db
}

// SetSchedulerProbe registers the function that reports the internal
// scheduler's health. It returns nil when the scheduler loop is running and
// every required task is healthy.
func SetSchedulerProbe(probe func() error) {
	schedulerProbe = probe
}

// SetTorProbe registers the function that reports the Tor hidden-service
// state (PART 31.1). Passing nil restores the built-in lookup against the
// process-wide Tor manager.
func SetTorProbe(probe func() TorInfo) {
	torProbe = probe
}

// SetI2PProbe registers the function that reports the eepsite state
// (PART 31.2). Passing nil reports I2P as disabled, which is the correct
// answer for an opt-in feature nobody enabled.
func SetI2PProbe(probe func() I2PInfo) {
	i2pProbe = probe
}

// SetGeoIPProbe registers the function that reports whether the GeoIP
// databases are loaded and queryable (PART 19). The feature is advertised
// only when config enables it AND this probe agrees; passing nil trusts the
// config toggle alone.
func SetGeoIPProbe(probe func() bool) {
	geoipProbe = probe
}

// SetStatsProvider registers the source of the public-safe request
// aggregates. Passing nil restores the handler's in-process collector.
func SetStatsProvider(provider func() (total, last24h int64, active int)) {
	statsProvider = provider
}

// SetMaintenanceMode records whether maintenance mode is active. Maintenance
// is reported through the "status" field only; the "mode" field keeps
// reporting the configured application mode.
func SetMaintenanceMode(enabled bool) {
	lifecycleMu.Lock()
	defer lifecycleMu.Unlock()
	maintenanceMode = enabled
}

// SetShuttingDown records that a graceful shutdown has begun, so health
// probes stop advertising the instance as available.
func SetShuttingDown(down bool) {
	lifecycleMu.Lock()
	defer lifecycleMu.Unlock()
	shuttingDown = down
}

// SetPendingRestart records that a config change requires a restart, along
// with the names of the settings that changed. Passing false clears both the
// flag and the recorded reasons.
func SetPendingRestart(pending bool, reason []string) {
	lifecycleMu.Lock()
	defer lifecycleMu.Unlock()
	pendingRestart = pending
	if !pending {
		restartReason = nil
		return
	}
	restartReason = append([]string(nil), reason...)
}

// lifecycleState returns a consistent snapshot of the process lifecycle
// state. The returned slice is a copy, so callers can hand it to the JSON
// encoder without racing later writers.
func lifecycleState() (maintenance, shutdown, pending bool, reason []string) {
	lifecycleMu.RLock()
	defer lifecycleMu.RUnlock()
	if pendingRestart && len(restartReason) > 0 {
		reason = append([]string(nil), restartReason...)
	}
	return maintenanceMode, shuttingDown, pendingRestart, reason
}

// BuildHealthResponse assembles the canonical health response from config and
// live subsystem checks. Exported so it can be reused by both the frontend
// (/server/healthz) and API (/api/{api_version}/server/healthz) routes.
func BuildHealthResponse(cfg *config.Config) HealthResponse {
	torInfo := torFeature()
	i2pInfo := i2pFeature()

	checks := ChecksInfo{
		Database:  checkDatabase(),
		Cache:     checkCache(),
		Disk:      checkDisk(),
		Scheduler: checkScheduler(),
		Tor:       overlayCheck(torInfo.Enabled, torInfo.Status),
		I2P:       overlayCheck(i2pInfo.Enabled, i2pInfo.Status),
	}

	maintenance, shutdown, pending, reason := lifecycleState()

	requestsTotal, requests24h, activeConns := statsSnapshot()

	response := HealthResponse{
		Project: ProjectInfo{
			Name:        cfg.Server.Branding.Title,
			Tagline:     cfg.Server.Branding.Tagline,
			Description: cfg.Server.Branding.Description,
		},
		Status:         resolveStatus(checks, maintenance, shutdown, pending),
		PendingRestart: pending,
		RestartReason:  reason,
		Version:        Version,
		GoVersion:      runtime.Version(),
		Build: BuildInfo{
			Commit: CommitID,
			Date:   BuildDate,
		},
		Uptime:    getUptime(),
		Mode:      cfg.Server.Mode,
		Timestamp: time.Now().UTC(),
		Features: FeaturesInfo{
			Tor:   torInfo,
			I2P:   i2pInfo,
			GeoIP: geoipFeature(cfg),
		},
		Checks: checks,
		Stats: StatsInfo{
			RequestsTotal: requestsTotal,
			Requests24h:   requests24h,
			ActiveConns:   activeConns,
		},
	}

	return response
}

// overallStatus derives the component-check portion of the top-level status:
// any failing check is unhealthy, any warning degrades, otherwise healthy.
func overallStatus(checks ChecksInfo) string {
	values := []string{checks.Database, checks.Cache, checks.Disk, checks.Scheduler}
	degraded := false
	for _, v := range values {
		if v == "error" {
			return StatusUnhealthy
		}
		if v == "warning" {
			degraded = true
		}
	}
	if degraded {
		return StatusDegraded
	}
	return StatusHealthy
}

// resolveStatus derives the canonical PART 13 status value from the process
// lifecycle state and the component checks, in descending order of severity.
// A pending restart is only surfaced when nothing more serious is wrong.
func resolveStatus(checks ChecksInfo, maintenance, shutdown, pending bool) string {
	if shutdown {
		return StatusShuttingDown
	}
	if maintenance {
		return StatusMaintenance
	}
	if componentStatus := overallStatus(checks); componentStatus != StatusHealthy {
		return componentStatus
	}
	if pending {
		return StatusRestartRequired
	}
	return StatusHealthy
}

// healthHTTPStatus maps a PART 13 status value to its HTTP status code.
// healthy, degraded, and restart_required serve 200; unhealthy, maintenance,
// and shutting_down serve 503. The body renders normally in every state.
func healthHTTPStatus(status string) int {
	switch status {
	case StatusUnhealthy, StatusMaintenance, StatusShuttingDown:
		return http.StatusServiceUnavailable
	default:
		return http.StatusOK
	}
}

// ServerHealthz serves all four health routes: /server/healthz, the /healthz
// alias, /api/{api_version}/server/healthz, and the unversioned /api/healthz
// alias. Content negotiation: JSON by default, plain text dot-notation for
// Accept: text/plain or a .txt suffix, HTML for browser requests when
// htmlDefault is true (frontend mount only). The JSON body is BARE on every
// route in every state — no {ok, data} envelope (PART 13 envelope exception);
// the HTTP status code and the top-level status field carry the health state.
func ServerHealthz(cfg *config.Config, htmlDefault bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		response := BuildHealthResponse(cfg)
		code := healthHTTPStatus(response.Status)

		switch negotiateHealthFormat(r, htmlDefault) {
		case "text":
			writeHealthText(w, code, response)
		case "html":
			writeHealthHTML(w, code, r, response)
		default:
			writeHealthJSON(w, code, response)
		}
	}
}

// negotiateHealthFormat determines the response format following the PART 14
// content negotiation priority order. htmlDefault selects the frontend
// (/server/healthz) priority order; false selects the API
// (/api/{api_version}/server/healthz) order.
func negotiateHealthFormat(r *http.Request, htmlDefault bool) string {
	accept := r.Header.Get("Accept")

	if !htmlDefault {
		// API priority: .txt extension, then Accept: text/plain, then
		// non-interactive client, then default JSON.
		if strings.HasSuffix(r.URL.Path, ".txt") {
			return "text"
		}
		if strings.Contains(accept, "text/plain") {
			return "text"
		}
		if isNonInteractiveClient(r) {
			return "text"
		}
		return "json"
	}

	// Frontend priority: Accept: text/html, then Accept: text/plain, then
	// User-Agent browser detection, then CLI default text, then HTML.
	switch {
	case strings.Contains(accept, "text/html"):
		return "html"
	case strings.Contains(accept, "text/plain"):
		return "text"
	}
	if isNonInteractiveClient(r) {
		return "text"
	}
	return "html"
}

// isNonInteractiveClient reports whether the request looks like it came
// from an HTTP tool (curl, wget, httpie) rather than a browser.
func isNonInteractiveClient(r *http.Request) bool {
	ua := strings.ToLower(r.Header.Get("User-Agent"))
	if ua == "" {
		return true
	}
	for _, tool := range []string{"curl", "wget", "httpie", "python-requests", "go-http-client"} {
		if strings.Contains(ua, tool) {
			return true
		}
	}
	return false
}

// writeHealthJSON writes the bare health payload — no {ok, data} envelope.
func writeHealthJSON(w http.ResponseWriter, code int, response HealthResponse) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(response)
}

func writeHealthText(w http.ResponseWriter, code int, r HealthResponse) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(code)

	fmt.Fprint(w, "# 1. Project (PART 16: branding)\n")
	fmt.Fprintf(w, "project.name: %s\n", r.Project.Name)
	fmt.Fprintf(w, "project.tagline: %s\n", r.Project.Tagline)
	fmt.Fprintf(w, "project.description: %s\n", r.Project.Description)

	fmt.Fprint(w, "# 2. Status\n")
	fmt.Fprintf(w, "status: %s\n", r.Status)
	if r.PendingRestart {
		fmt.Fprintf(w, "pending_restart: %t\n", r.PendingRestart)
		if len(r.RestartReason) > 0 {
			fmt.Fprintf(w, "restart_reason: %s\n", strings.Join(r.RestartReason, ", "))
		}
	}

	fmt.Fprint(w, "# 3. Version & build\n")
	fmt.Fprintf(w, "version: %s\n", r.Version)
	fmt.Fprintf(w, "go_version: %s\n", r.GoVersion)
	fmt.Fprintf(w, "build.commit: %s\n", r.Build.Commit)
	fmt.Fprintf(w, "build.date: %s\n", r.Build.Date)

	fmt.Fprint(w, "# 4. Runtime\n")
	fmt.Fprintf(w, "uptime: %s\n", r.Uptime)
	fmt.Fprintf(w, "mode: %s\n", r.Mode)
	fmt.Fprintf(w, "timestamp: %s\n", r.Timestamp.Format(time.RFC3339))

	fmt.Fprint(w, "# 5. Features\n")
	fmt.Fprintf(w, "features.tor.enabled: %t\n", r.Features.Tor.Enabled)
	fmt.Fprintf(w, "features.tor.running: %t\n", r.Features.Tor.Running)
	fmt.Fprintf(w, "features.tor.status: %s\n", r.Features.Tor.Status)
	fmt.Fprintf(w, "features.tor.hostname: %s\n", r.Features.Tor.Hostname)
	fmt.Fprintf(w, "features.i2p.enabled: %t\n", r.Features.I2P.Enabled)
	fmt.Fprintf(w, "features.i2p.running: %t\n", r.Features.I2P.Running)
	fmt.Fprintf(w, "features.i2p.status: %s\n", r.Features.I2P.Status)
	fmt.Fprintf(w, "features.i2p.hostname: %s\n", r.Features.I2P.Hostname)
	fmt.Fprintf(w, "features.i2p.provider: %s\n", r.Features.I2P.Provider)
	fmt.Fprintf(w, "features.geoip: %t\n", r.Features.GeoIP)

	fmt.Fprint(w, "# 6. Checks\n")
	fmt.Fprintf(w, "checks.database: %s\n", r.Checks.Database)
	fmt.Fprintf(w, "checks.cache: %s\n", r.Checks.Cache)
	fmt.Fprintf(w, "checks.disk: %s\n", r.Checks.Disk)
	fmt.Fprintf(w, "checks.scheduler: %s\n", r.Checks.Scheduler)
	// Overlay checks mirror the JSON body's omitempty: a disabled overlay has
	// no check to report, so the line is omitted rather than left blank.
	if r.Checks.Tor != "" {
		fmt.Fprintf(w, "checks.tor: %s\n", r.Checks.Tor)
	}
	if r.Checks.I2P != "" {
		fmt.Fprintf(w, "checks.i2p: %s\n", r.Checks.I2P)
	}

	fmt.Fprint(w, "# 7. Stats\n")
	fmt.Fprintf(w, "stats.requests_total: %d\n", r.Stats.RequestsTotal)
	fmt.Fprintf(w, "stats.requests_24h: %d\n", r.Stats.Requests24h)
	fmt.Fprintf(w, "stats.active_connections: %d\n", r.Stats.ActiveConns)
}

// healthBanner maps a PART 13 status value onto its status-banner class, icon
// and headline text.
func healthBanner(status string) (class, icon, text string) {
	switch status {
	case StatusDegraded:
		return "status-warning", "⚠️", "Degraded Performance"
	case StatusRestartRequired:
		return "status-warning", "🔄", "Restart Required"
	case StatusUnhealthy:
		return "status-error", "❌", "Systems Unhealthy"
	case StatusMaintenance:
		return "status-error", "🚧", "Maintenance in Progress"
	case StatusShuttingDown:
		return "status-error", "🛑", "Shutting Down"
	default:
		return "status-ok", "✅", "All Systems Operational"
	}
}

// checkBadge renders a component check value as the PART 13 status badge.
func checkBadge(value string) string {
	switch value {
	case "ok":
		return `<span class="status status-ok">✅ OK</span>`
	case "error":
		return `<span class="status status-error">❌ Error</span>`
	default:
		return fmt.Sprintf(`<span class="status status-warning">⚠️ %s</span>`, html.EscapeString(value))
	}
}

// healthThemeClass resolves the <html> theme class from the theme cookie so
// the page renders in the visitor's theme with no init JavaScript and no
// flash of the wrong theme. Dark is the default (PART 16).
func healthThemeClass(r *http.Request) string {
	cookie, err := r.Cookie("theme")
	if err != nil {
		return "theme-dark"
	}
	switch cookie.Value {
	case "light":
		return "theme-light"
	case "auto":
		return "theme-auto"
	default:
		return "theme-dark"
	}
}

// healthLang resolves the page language from the lang cookie, defaulting to
// English (PART 16 i18n fallback chain).
func healthLang(r *http.Request) (lang, dir string) {
	lang = "en"
	if cookie, err := r.Cookie("lang"); err == nil && cookie.Value != "" {
		lang = cookie.Value
	}
	dir = "ltr"
	if strings.HasPrefix(strings.ToLower(lang), "ar") {
		dir = "rtl"
	}
	return lang, dir
}

// formatThousands renders a count with comma separators, as PART 13 requires
// for the statistics section.
func formatThousands(n int64) string {
	digits := strconv.FormatInt(n, 10)
	sign := ""
	if strings.HasPrefix(digits, "-") {
		sign = "-"
		digits = digits[1:]
	}

	var out strings.Builder
	for i, r := range digits {
		if i > 0 && (len(digits)-i)%3 == 0 {
			out.WriteByte(',')
		}
		out.WriteRune(r)
	}
	return sign + out.String()
}

// overlayFeatureItem renders one overlay-network feature row: the status
// badge plus, when an address exists, a copy-enabled code block matching the
// PART 16 clipboard pattern (.code-block wrapping a <pre> the copy button
// reads from).
func overlayFeatureItem(icon, label, status, hostname string, enabled bool) string {
	class := "feature-disabled"
	badge := `<span class="status status-warning">disabled</span>`
	if enabled {
		class = "feature-enabled"
		badge = checkOverlayBadge(status)
	}

	item := fmt.Sprintf(`<li class="%s">%s %s: %s`, class, icon, html.EscapeString(label), badge)
	if hostname != "" {
		escaped := html.EscapeString(hostname)
		item += fmt.Sprintf(`<div class="code-block"><pre><code class="code-content">%s</code></pre>`+
			`<button type="button" class="copy-btn" data-copy="%s">`+
			`<span class="copy-icon">📋</span><span class="copy-text" aria-live="polite">Copy</span>`+
			`</button></div>`, escaped, escaped)
	}
	return item + "</li>"
}

// checkOverlayBadge renders an overlay feature's status string as a badge.
func checkOverlayBadge(status string) string {
	switch status {
	case "healthy":
		return `<span class="status status-ok">✅ healthy</span>`
	case "error":
		return `<span class="status status-error">❌ error</span>`
	default:
		return fmt.Sprintf(`<span class="status status-warning">⏳ %s</span>`, html.EscapeString(status))
	}
}

// writeHealthHTML renders the health page directly. The health handler lives
// in the handler package, which the server package imports, so it cannot
// reach the page-template set without an import cycle; the markup below uses
// the same shared stylesheets and PART 16 class names the templates do, in
// the PART 13 display order (project, status banner, version & build,
// runtime, features, checks, stats). Everything works with JavaScript
// disabled — app.js only enhances the copy buttons.
func writeHealthHTML(w http.ResponseWriter, code int, req *http.Request, r HealthResponse) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(code)

	lang, dir := healthLang(req)
	bannerClass, bannerIcon, bannerText := healthBanner(r.Status)
	name := html.EscapeString(r.Project.Name)

	fmt.Fprintf(w, "<!DOCTYPE html>\n<html lang=\"%s\" dir=\"%s\" class=\"%s\">\n",
		html.EscapeString(lang), dir, healthThemeClass(req))
	fmt.Fprint(w, "<head>\n<meta charset=\"utf-8\">\n")
	fmt.Fprint(w, "<meta name=\"viewport\" content=\"width=device-width, initial-scale=1\">\n")
	fmt.Fprint(w, "<meta name=\"robots\" content=\"noindex\">\n")
	fmt.Fprintf(w, "<title>%s - Health Status</title>\n", name)
	fmt.Fprint(w, "<link rel=\"stylesheet\" href=\"/static/css/common.css\">\n")
	fmt.Fprint(w, "<link rel=\"stylesheet\" href=\"/static/css/components.css\">\n")
	fmt.Fprint(w, "<link rel=\"stylesheet\" href=\"/static/css/public.css\">\n")
	fmt.Fprint(w, "</head>\n<body>\n<main class=\"container\">\n")

	// 1. Project identification.
	fmt.Fprint(w, "<header class=\"health-header\">\n")
	fmt.Fprintf(w, "<h1>📦 %s</h1>\n", name)
	if r.Project.Tagline != "" {
		fmt.Fprintf(w, "<p class=\"tagline\">%s</p>\n", html.EscapeString(r.Project.Tagline))
	}
	if r.Project.Description != "" {
		fmt.Fprintf(w, "<p>%s</p>\n", html.EscapeString(r.Project.Description))
	}
	fmt.Fprint(w, "</header>\n")

	// 2. Status banner.
	fmt.Fprintf(w, "<div class=\"status-banner %s\" role=\"status\">"+
		"<span class=\"status-icon\" aria-hidden=\"true\">%s</span> "+
		"<span class=\"status-text\">%s</span></div>\n", bannerClass, bannerIcon, bannerText)
	if r.PendingRestart && len(r.RestartReason) > 0 {
		fmt.Fprintf(w, "<p class=\"restart-reason\">Restart required for: %s</p>\n",
			html.EscapeString(strings.Join(r.RestartReason, ", ")))
	}

	// 3. Version and build.
	fmt.Fprint(w, "<section class=\"section-card\">\n<h2>ℹ️ Version</h2>\n<dl class=\"info-list\">\n")
	fmt.Fprintf(w, "<dt>🏷️ Version</dt>\n<dd><code>%s</code></dd>\n", html.EscapeString(r.Version))
	fmt.Fprintf(w, "<dt>🐹 Go Version</dt>\n<dd><code>%s</code></dd>\n", html.EscapeString(r.GoVersion))
	fmt.Fprintf(w, "<dt>🔨 Build</dt>\n<dd><code>%s</code> (%s)</dd>\n",
		html.EscapeString(r.Build.Commit), html.EscapeString(r.Build.Date))
	fmt.Fprint(w, "</dl>\n</section>\n")

	// 4. Runtime.
	fmt.Fprint(w, "<section class=\"section-card\">\n<h2>⏱️ Runtime</h2>\n<dl class=\"info-list\">\n")
	fmt.Fprintf(w, "<dt>⏱️ Uptime</dt>\n<dd>%s</dd>\n", html.EscapeString(r.Uptime))
	fmt.Fprintf(w, "<dt>🚀 Mode</dt>\n<dd><span class=\"badge badge-%s\">%s</span></dd>\n",
		html.EscapeString(r.Mode), html.EscapeString(r.Mode))
	fmt.Fprintf(w, "<dt>🕒 Timestamp</dt>\n<dd><time datetime=\"%s\">%s</time></dd>\n",
		r.Timestamp.Format(time.RFC3339), r.Timestamp.Format("Jan 2, 2006 15:04 MST"))
	fmt.Fprint(w, "</dl>\n</section>\n")

	// 5. Features.
	fmt.Fprint(w, "<section class=\"section-card\">\n<h2>🎛️ Features</h2>\n<ul class=\"feature-list\">\n")
	fmt.Fprintf(w, "%s\n", overlayFeatureItem("🧅", "Tor", r.Features.Tor.Status, r.Features.Tor.Hostname, r.Features.Tor.Enabled))
	fmt.Fprintf(w, "%s\n", overlayFeatureItem("🔗", "I2P", r.Features.I2P.Status, r.Features.I2P.Hostname, r.Features.I2P.Enabled))
	if r.Features.GeoIP {
		fmt.Fprint(w, "<li class=\"feature-enabled\">🌍 GeoIP</li>\n")
	} else {
		fmt.Fprint(w, "<li class=\"feature-disabled\">🌍 GeoIP</li>\n")
	}
	fmt.Fprint(w, "</ul>\n</section>\n")

	// 6. Component checks.
	fmt.Fprint(w, "<section class=\"section-card\">\n<h2>🔧 Component Status</h2>\n")
	fmt.Fprint(w, "<div class=\"table-wrapper\">\n<table class=\"data-table\">\n")
	fmt.Fprint(w, "<thead><tr><th>Component</th><th>Status</th></tr></thead>\n<tbody>\n")
	rows := []struct {
		label string
		value string
	}{
		{"🗄️ Database", r.Checks.Database},
		{"💾 Cache", r.Checks.Cache},
		{"💿 Disk", r.Checks.Disk},
		{"⏰ Scheduler", r.Checks.Scheduler},
		{"🧅 Tor", r.Checks.Tor},
		{"🔗 I2P", r.Checks.I2P},
	}
	for _, row := range rows {
		if row.value == "" {
			continue
		}
		fmt.Fprintf(w, "<tr><td>%s</td><td>%s</td></tr>\n", row.label, checkBadge(row.value))
	}
	fmt.Fprint(w, "</tbody>\n</table>\n</div>\n</section>\n")

	// 7. Statistics.
	fmt.Fprint(w, "<section class=\"section-card\">\n<h2>📈 Server Statistics</h2>\n")
	fmt.Fprint(w, "<dl class=\"info-list stats-grid\">\n")
	fmt.Fprintf(w, "<dt>📥 Total Requests</dt>\n<dd>%s</dd>\n", formatThousands(r.Stats.RequestsTotal))
	fmt.Fprintf(w, "<dt>📅 Requests (24 hours)</dt>\n<dd>%s</dd>\n", formatThousands(r.Stats.Requests24h))
	fmt.Fprintf(w, "<dt>🔌 Active Connections</dt>\n<dd>%s</dd>\n", formatThousands(int64(r.Stats.ActiveConns)))
	fmt.Fprint(w, "</dl>\n</section>\n")

	fmt.Fprintf(w, "<footer class=\"health-footer\">\n<p>Last checked: <time datetime=\"%s\">%s</time></p>\n</footer>\n",
		r.Timestamp.Format(time.RFC3339), r.Timestamp.Format("Jan 2, 2006 15:04 MST"))
	fmt.Fprint(w, "</main>\n<script src=\"/static/js/app.js\" defer></script>\n</body>\n</html>\n")
}

// HandleVersion handles /api/v1/version endpoint
func HandleVersion(w http.ResponseWriter, r *http.Request) {
	response := VersionResponse{
		Version:   Version,
		CommitID:  CommitID,
		BuildDate: BuildDate,
		GoVersion: runtime.Version(),
		Platform:  runtime.GOOS,
		Arch:      runtime.GOARCH,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// UptimeSeconds returns server uptime in whole seconds, for callers (such as
// the GraphQL resolvers) that need a numeric value rather than the
// human-readable string used by the REST/HTML health endpoint.
func UptimeSeconds() int64 {
	return int64(time.Since(startTime).Seconds())
}

// Status reports the overall health status using the same component checks
// and lifecycle state as the REST health endpoint, for callers that don't
// have a *config.Config (such as the GraphQL resolvers).
func Status() string {
	checks := ChecksInfo{
		Database:  checkDatabase(),
		Cache:     checkCache(),
		Disk:      checkDisk(),
		Scheduler: checkScheduler(),
	}
	maintenance, shutdown, pending, _ := lifecycleState()
	return resolveStatus(checks, maintenance, shutdown, pending)
}

// checkDatabase checks database connectivity
func checkDatabase() string {
	if dbPinger == nil {
		return "unknown"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := dbPinger.PingContext(ctx); err != nil {
		return "error"
	}

	return "ok"
}

// checkCache checks cache connectivity
func checkCache() string {
	// In-memory cache is always available
	// Valkey/Redis check would go here if configured
	return "ok"
}

// checkScheduler reports the internal scheduler's health. An unwired probe
// reports "unknown", matching checkDatabase: the health endpoint can be served
// by an embedding that never started a scheduler, and an unknown component is
// not treated as a failure of the whole server.
func checkScheduler() string {
	if schedulerProbe == nil {
		return "unknown"
	}
	if schedulerProbe() != nil {
		return "error"
	}
	return "ok"
}

// torFeature reports the live Tor hidden-service state (PART 31.1). Tor is
// auto-enabled whenever a Tor binary is present, so a nil manager means the
// binary was never found and the feature is simply off.
func torFeature() TorInfo {
	if torProbe != nil {
		return torProbe()
	}

	manager := tor.Get()
	if manager == nil {
		return TorInfo{Status: "disabled"}
	}

	info := TorInfo{Enabled: true}
	if !manager.Running() {
		info.Status = "starting"
		return info
	}

	info.Running = true
	info.Hostname = manager.OnionAddress()
	switch {
	case manager.Ping() != nil:
		info.Status = "error"
	case info.Hostname == "":
		info.Status = "starting"
	default:
		info.Status = "healthy"
	}
	return info
}

// i2pFeature reports the eepsite state (PART 31.2). I2P is opt-in, so without
// a registered provider the feature reports itself as disabled rather than
// claiming an eepsite that does not exist.
func i2pFeature() I2PInfo {
	if i2pProbe == nil {
		return I2PInfo{Status: "disabled", Provider: "none"}
	}

	info := i2pProbe()
	if info.Provider == "" {
		info.Provider = "none"
	}
	if info.Status == "" {
		info.Status = "disabled"
	}
	return info
}

// geoipFeature reports whether GeoIP lookups are actually available
// (PART 19). The configured toggle is authoritative for "off"; a registered
// probe can additionally report "on in config but not loaded yet".
func geoipFeature(cfg *config.Config) bool {
	if !cfg.Server.GeoIP.Enabled {
		return false
	}
	if geoipProbe == nil {
		return true
	}
	return geoipProbe()
}

// statsSnapshot returns the public-safe request aggregates, preferring a
// registered provider over the handler's in-process collector.
func statsSnapshot() (total, last24h int64, active int) {
	if statsProvider != nil {
		return statsProvider()
	}
	return RequestStatsSnapshot()
}

// overlayCheck maps an overlay-network feature status onto the "ok"/"error"
// vocabulary the checks section allows. Disabled overlays report "", which
// omitempty drops from the response entirely.
func overlayCheck(enabled bool, status string) string {
	if !enabled {
		return ""
	}
	if status == "error" {
		return "error"
	}
	return "ok"
}

// checkDisk is implemented in health_unix.go and health_windows.go

// getUptime returns server uptime as human-readable string
func getUptime() string {
	duration := time.Since(startTime)

	days := int(duration.Hours()) / 24
	hours := int(duration.Hours()) % 24
	minutes := int(duration.Minutes()) % 60

	if days > 0 {
		return formatUptime(days, hours, minutes, "d", "h", "m")
	}
	if hours > 0 {
		return formatUptime(hours, minutes, 0, "h", "m", "")
	}
	return formatUptime(minutes, 0, 0, "m", "", "")
}

// formatUptime formats uptime components
func formatUptime(a, b, c int, aUnit, bUnit, cUnit string) string {
	if c > 0 && cUnit != "" {
		return formatDuration(a, aUnit, b, bUnit, c, cUnit)
	}
	if b > 0 && bUnit != "" {
		return formatDuration2(a, aUnit, b, bUnit)
	}
	return formatDuration1(a, aUnit)
}

// formatDuration formats 3-part duration
func formatDuration(a int, aUnit string, b int, bUnit string, c int, cUnit string) string {
	return fmt.Sprintf("%d%s %d%s %d%s", a, aUnit, b, bUnit, c, cUnit)
}

// formatDuration2 formats 2-part duration
func formatDuration2(a int, aUnit string, b int, bUnit string) string {
	return fmt.Sprintf("%d%s %d%s", a, aUnit, b, bUnit)
}

// formatDuration1 formats 1-part duration
func formatDuration1(a int, aUnit string) string {
	return fmt.Sprintf("%d%s", a, aUnit)
}
