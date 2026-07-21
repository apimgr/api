package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"runtime"
	"strings"
	"time"

	"github.com/apimgr/api/src/config"
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
	// schedulerEnabled reports whether the internal scheduler is active (set from main)
	schedulerEnabled bool
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
	GeoIP bool    `json:"geoip"`
}

// TorInfo is sourced from the Tor manager (PART 31); not yet implemented
type TorInfo struct {
	Enabled  bool   `json:"enabled"`
	Running  bool   `json:"running"`
	Status   string `json:"status"`
	Hostname string `json:"hostname"`
}

// ChecksInfo reports component health as "ok"/"error" only, no details
type ChecksInfo struct {
	Database  string `json:"database"`
	Cache     string `json:"cache"`
	Disk      string `json:"disk"`
	Scheduler string `json:"scheduler"`
	Tor       string `json:"tor,omitempty"`
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

// SetSchedulerEnabled records whether the internal scheduler is active
func SetSchedulerEnabled(enabled bool) {
	schedulerEnabled = enabled
}

// BuildHealthResponse assembles the canonical health response from config and
// live subsystem checks. Exported so it can be reused by both the frontend
// (/server/healthz) and API (/api/{api_version}/server/healthz) routes.
func BuildHealthResponse(cfg *config.Config) HealthResponse {
	checks := ChecksInfo{
		Database:  checkDatabase(),
		Cache:     checkCache(),
		Disk:      checkDisk(),
		Scheduler: checkScheduler(),
	}

	response := HealthResponse{
		Project: ProjectInfo{
			Name:        cfg.Server.Branding.Title,
			Tagline:     cfg.Server.Branding.Tagline,
			Description: cfg.Server.Branding.Description,
		},
		Status:    overallStatus(checks),
		Version:   Version,
		GoVersion: runtime.Version(),
		Build: BuildInfo{
			Commit: CommitID,
			Date:   BuildDate,
		},
		Uptime:    getUptime(),
		Mode:      cfg.Server.Mode,
		Timestamp: time.Now().UTC(),
		Features: FeaturesInfo{
			Tor:   TorInfo{Enabled: false, Running: false, Status: "disabled"},
			GeoIP: false,
		},
		Checks: checks,
		Stats: StatsInfo{
			RequestsTotal: 0,
			Requests24h:   0,
			ActiveConns:   0,
		},
	}

	return response
}

// overallStatus derives the top-level status from component checks
func overallStatus(checks ChecksInfo) string {
	values := []string{checks.Database, checks.Cache, checks.Disk, checks.Scheduler}
	degraded := false
	for _, v := range values {
		if v == "error" {
			return "unhealthy"
		}
		if v == "warning" {
			degraded = true
		}
	}
	if degraded {
		return "degraded"
	}
	return "healthy"
}

// ServerHealthz serves /server/healthz and /api/{api_version}/server/healthz.
// Content negotiation: JSON by default, plain text dot-notation for
// Accept: text/plain or a .txt suffix, HTML for browser requests when
// htmlDefault is true (frontend mount only).
func ServerHealthz(cfg *config.Config, htmlDefault bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		response := BuildHealthResponse(cfg)

		switch negotiateHealthFormat(r, htmlDefault) {
		case "text":
			writeHealthText(w, response)
		case "html":
			writeHealthHTML(w, response)
		default:
			writeHealthJSON(w, response)
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

func writeHealthJSON(w http.ResponseWriter, response HealthResponse) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}

func writeHealthText(w http.ResponseWriter, r HealthResponse) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)

	fmt.Fprint(w, "# 1. Project (PART 16: branding)\n")
	fmt.Fprintf(w, "project.name: %s\n", r.Project.Name)
	fmt.Fprintf(w, "project.tagline: %s\n", r.Project.Tagline)
	fmt.Fprintf(w, "project.description: %s\n", r.Project.Description)

	fmt.Fprint(w, "# 2. Status\n")
	fmt.Fprintf(w, "status: %s\n", r.Status)

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
	fmt.Fprintf(w, "features.geoip: %t\n", r.Features.GeoIP)

	fmt.Fprint(w, "# 6. Checks\n")
	fmt.Fprintf(w, "checks.database: %s\n", r.Checks.Database)
	fmt.Fprintf(w, "checks.cache: %s\n", r.Checks.Cache)
	fmt.Fprintf(w, "checks.disk: %s\n", r.Checks.Disk)
	fmt.Fprintf(w, "checks.scheduler: %s\n", r.Checks.Scheduler)

	fmt.Fprint(w, "# 7. Stats\n")
	fmt.Fprintf(w, "stats.requests_total: %d\n", r.Stats.RequestsTotal)
	fmt.Fprintf(w, "stats.requests_24h: %d\n", r.Stats.Requests24h)
	fmt.Fprintf(w, "stats.active_connections: %d\n", r.Stats.ActiveConns)
}

// writeHealthHTML renders a minimal, dependency-free HTML page so the
// endpoint is browser-usable even before the page-template pipeline renders
// it via the caller-provided renderer. Callers that have a full page
// renderer (see server.HealthzPageHandler) should use that instead; this is
// the fallback used when none is registered.
func writeHealthHTML(w http.ResponseWriter, r HealthResponse) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)

	fmt.Fprintf(w, "<!DOCTYPE html><html><head><title>%s - Health Status</title></head><body>", r.Project.Name)
	fmt.Fprintf(w, "<h1>%s</h1><p>%s</p><p>%s</p>", r.Project.Name, r.Project.Tagline, r.Project.Description)
	fmt.Fprintf(w, "<p>Status: %s</p>", r.Status)
	fmt.Fprintf(w, "<p>Version: %s | Go: %s | Build: %s (%s)</p>", r.Version, r.GoVersion, r.Build.Commit, r.Build.Date)
	fmt.Fprintf(w, "<p>Uptime: %s | Mode: %s | Timestamp: %s</p>", r.Uptime, r.Mode, r.Timestamp.Format(time.RFC3339))
	fmt.Fprintf(w, "<p>Database: %s | Cache: %s | Disk: %s | Scheduler: %s</p>",
		r.Checks.Database, r.Checks.Cache, r.Checks.Disk, r.Checks.Scheduler)
	fmt.Fprintf(w, "<p>Requests total: %d | 24h: %d | Active connections: %d</p>",
		r.Stats.RequestsTotal, r.Stats.Requests24h, r.Stats.ActiveConns)
	fmt.Fprint(w, "</body></html>")
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

// checkScheduler reports the internal scheduler's health
func checkScheduler() string {
	if !schedulerEnabled {
		return "ok"
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
