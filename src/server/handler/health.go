package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/apimgr/api/src/cache"
	"github.com/apimgr/api/src/common/i18n"
	"github.com/apimgr/api/src/common/sanitize"
	"github.com/apimgr/api/src/config"
	"github.com/apimgr/api/src/health"
	"github.com/apimgr/api/src/i2p"
	"github.com/apimgr/api/src/mode"
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
	CommitID  = "N/A"
	BuildDate = "N/A"
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
	maintenanceInfo *MaintenanceInfo
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

	Status         string           `json:"status"`
	PendingRestart bool             `json:"pending_restart,omitempty"`
	RestartReason  []string         `json:"restart_reason,omitempty"`
	Maintenance    *MaintenanceInfo `json:"maintenance,omitempty"`

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

type MaintenanceInfo struct {
	Reason      string          `json:"reason"`
	Message     string          `json:"message"`
	Since       time.Time       `json:"since"`
	SelfHealing SelfHealingInfo `json:"self_healing"`
}

type SelfHealingInfo struct {
	Enabled     bool       `json:"enabled"`
	Exhausted   bool       `json:"exhausted"`
	Attempts    int        `json:"attempts"`
	LastAttempt *time.Time `json:"last_attempt"`
	NextAttempt *time.Time `json:"next_attempt"`
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
	State    string `json:"state"`
	Status   string `json:"status"`
	Hostname string `json:"hostname"`
}

// I2PInfo is sourced from the I2P manager (PART 31.2). I2P is opt-in, so
// every field stays at its zero value while the eepsite is disabled.
type I2PInfo struct {
	Enabled  bool   `json:"enabled"`
	Running  bool   `json:"running"`
	Status   string `json:"status"`
	State    string `json:"state"`
	Hostname string `json:"hostname"`
	Provider string `json:"provider"`
}

// ChecksInfo reports public-safe component statuses, without paths or error details.
type ChecksInfo struct {
	Database  string         `json:"database"`
	Cache     string         `json:"cache"`
	Disk      string         `json:"disk"`
	Scheduler string         `json:"scheduler"`
	Tor       string         `json:"tor,omitempty"`
	I2P       string         `json:"i2p,omitempty"`
	Datasets  []health.Check `json:"-"`
}

// TaggedResponseFields exposes the fixed health fields and the registry's
// explicitly declared dataset keys to the PART 11 response serializer.
func (c ChecksInfo) TaggedResponseFields() sanitize.TaggedObject {
	fields := sanitize.TaggedObject{
		{Name: "database", Value: c.Database},
		{Name: "cache", Value: c.Cache},
		{Name: "disk", Value: c.Disk},
		{Name: "scheduler", Value: c.Scheduler},
	}
	if c.Tor != "" {
		fields = append(fields, sanitize.TaggedField{Name: "tor", Value: c.Tor})
	}
	if c.I2P != "" {
		fields = append(fields, sanitize.TaggedField{Name: "i2p", Value: c.I2P})
	}
	for _, dataset := range c.Datasets {
		fields = append(fields, sanitize.TaggedField{Name: dataset.Key, Value: dataset.Status})
	}
	return fields
}

func (c ChecksInfo) MarshalJSON() ([]byte, error) {
	var out bytes.Buffer
	out.WriteByte('{')
	fields := []struct {
		key, value string
		present    bool
	}{
		{"database", c.Database, true}, {"cache", c.Cache, true}, {"disk", c.Disk, true}, {"scheduler", c.Scheduler, true},
		{"tor", c.Tor, c.Tor != ""}, {"i2p", c.I2P, c.I2P != ""},
	}
	first := true
	for _, field := range fields {
		if field.present {
			if !first {
				out.WriteByte(',')
			}
			first = false
			key, _ := json.Marshal(field.key)
			value, _ := json.Marshal(field.value)
			out.Write(key)
			out.WriteByte(':')
			out.Write(value)
		}
	}
	for _, check := range c.Datasets {
		if !first {
			out.WriteByte(',')
		}
		first = false
		key, _ := json.Marshal(check.Key)
		value, _ := json.Marshal(check.Status)
		out.Write(key)
		out.WriteByte(':')
		out.Write(value)
	}
	out.WriteByte('}')
	return out.Bytes(), nil
}

// StatsInfo reports public-safe aggregate statistics
type StatsInfo struct {
	RequestsTotal int64 `json:"requests_total"`
	Requests24h   int64 `json:"requests_24h"`
	ActiveConns   int   `json:"active_connections"`
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
	if !enabled {
		maintenanceInfo = nil
	} else if maintenanceInfo == nil {
		maintenanceInfo = &MaintenanceInfo{Reason: "maintenance", Message: "Server is in maintenance mode", Since: time.Now().UTC(), SelfHealing: SelfHealingInfo{Enabled: true}}
	}
}

// SetMaintenanceState publishes detailed maintenance guidance in health
// responses. Passing nil clears maintenance mode.
func SetMaintenanceState(info *MaintenanceInfo) {
	lifecycleMu.Lock()
	defer lifecycleMu.Unlock()
	if info == nil {
		maintenanceInfo = nil
		maintenanceMode = false
		return
	}
	copyOfInfo := *info
	copyOfInfo.SelfHealing = cloneSelfHealingInfo(info.SelfHealing)
	maintenanceInfo = &copyOfInfo
	maintenanceMode = true
}

// MaintenanceState returns a copy of the current health maintenance details.
func MaintenanceState() *MaintenanceInfo {
	lifecycleMu.RLock()
	defer lifecycleMu.RUnlock()
	if maintenanceInfo == nil {
		return nil
	}
	copyOfInfo := *maintenanceInfo
	copyOfInfo.SelfHealing = cloneSelfHealingInfo(maintenanceInfo.SelfHealing)
	return &copyOfInfo
}

func cloneSelfHealingInfo(info SelfHealingInfo) SelfHealingInfo {
	if info.LastAttempt != nil {
		last := *info.LastAttempt
		info.LastAttempt = &last
	}
	if info.NextAttempt != nil {
		next := *info.NextAttempt
		info.NextAttempt = &next
	}
	return info
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
func lifecycleState() (maintenance, shutdown, pending bool, reason []string, maintenanceDetails *MaintenanceInfo) {
	lifecycleMu.RLock()
	defer lifecycleMu.RUnlock()
	if pendingRestart && len(restartReason) > 0 {
		reason = append([]string(nil), restartReason...)
	}
	if maintenanceInfo != nil {
		copyOfInfo := *maintenanceInfo
		copyOfInfo.SelfHealing = cloneSelfHealingInfo(maintenanceInfo.SelfHealing)
		maintenanceDetails = &copyOfInfo
	}
	return maintenanceMode, shuttingDown, pendingRestart, reason, maintenanceDetails
}

// BuildHealthResponse assembles the canonical health response from config and
// live subsystem checks. Exported so it can be reused by both the frontend
// (/server/healthz) and API (/api/{api_version}/server/healthz) routes.
func BuildHealthResponse(cfg *config.ServerConfig) HealthResponse {
	torInfo := torFeature()
	i2pInfo := i2pFeature()

	checks := ChecksInfo{
		Database:  checkDatabase(),
		Cache:     checkCache(),
		Disk:      checkDisk(),
		Scheduler: checkScheduler(),
		Tor:       overlayCheck(torInfo.Enabled, torInfo.Status),
		I2P:       overlayCheck(i2pInfo.Enabled, i2pInfo.Status),
		Datasets:  health.Snapshot(),
	}

	maintenance, shutdown, pending, reason, maintenanceDetails := lifecycleState()
	if maintenanceDetails != nil && strings.HasPrefix(maintenanceDetails.Reason, "database_") {
		checks.Database = "error"
	}

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
		Maintenance:    maintenanceDetails,
		Version:        Version,
		GoVersion:      runtime.Version(),
		Build: BuildInfo{
			Commit: CommitID,
			Date:   BuildDate,
		},
		Uptime:    getUptime(),
		Mode:      cfg.Server.Mode,
		Timestamp: time.Now().UTC().Truncate(time.Second),
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
	if checks.Database == "error" || checks.Disk == "error" {
		return StatusUnhealthy
	}
	degraded := checks.Cache == "error" || checks.Cache == "degraded" || checks.Scheduler == "error" || checks.Scheduler == "degraded" || checks.Tor == "degraded" || checks.I2P == "degraded"
	if checks.Cache != "ok" && checks.Cache != "" {
		degraded = true
	}
	if checks.Scheduler != "ok" && checks.Scheduler != "" {
		degraded = true
	}
	for _, check := range checks.Datasets {
		if check.Status == "error" && check.Critical {
			return StatusUnhealthy
		}
		if check.Status == "error" || check.Status == "degraded" {
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
func ServerHealthz(cfg *config.ServerConfig, htmlDefault bool, htmlRenderer ...func(http.ResponseWriter, *http.Request, int, HealthResponse)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		response := BuildHealthResponse(cfg)
		code := healthHTTPStatus(response.Status)

		switch negotiateHealthFormat(r, htmlDefault) {
		case "text":
			if htmlDefault {
				writeHealthFrontendText(w, r, code, response)
			} else {
				writeHealthText(w, code, response)
			}
		case "html":
			if len(htmlRenderer) == 0 || htmlRenderer[0] == nil {
				http.Error(w, i18n.Translate(i18n.LangFromRequest(r), "errors.server_error"), http.StatusInternalServerError)
				return
			}
			htmlRenderer[0](w, r, code, response)
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
	accept := strings.ToLower(r.Header.Get("Accept"))

	if !htmlDefault {
		// API priority: .txt extension, explicit Accept headers, then
		// non-interactive clients, and finally default JSON.
		if strings.HasSuffix(r.URL.Path, ".txt") {
			return "text"
		}
		if strings.Contains(accept, "application/json") {
			return "json"
		}
		if strings.Contains(accept, "text/plain") {
			return "text"
		}
		if isNonInteractiveClient(r) {
			return "text"
		}
		return "json"
	}

	// Frontend priority: explicit Accept headers, non-interactive clients,
	// then browser HTML.
	switch {
	case strings.Contains(accept, "application/json"):
		return "json"
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
	if err := sanitize.WriteTaggedJSON(w, response, mode.IsAppModeProd()); err != nil {
		slog.Error("failed to write health JSON response", "error", err)
	}
}

func writeHealthText(w http.ResponseWriter, code int, r HealthResponse) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(code)

	_, _ = fmt.Fprint(w, "# 1. Project (PART 16: branding)\n")
	_, _ = fmt.Fprintf(w, "project.name: %s\n", r.Project.Name)
	_, _ = fmt.Fprintf(w, "project.tagline: %s\n", r.Project.Tagline)
	_, _ = fmt.Fprintf(w, "project.description: %s\n", r.Project.Description)

	_, _ = fmt.Fprint(w, "# 2. Status\n")
	_, _ = fmt.Fprintf(w, "status: %s\n", r.Status)
	if r.PendingRestart {
		_, _ = fmt.Fprintf(w, "pending_restart: %t\n", r.PendingRestart)
		if len(r.RestartReason) > 0 {
			_, _ = fmt.Fprintf(w, "restart_reason: %s\n", strings.Join(r.RestartReason, ", "))
		}
	}
	if r.Maintenance != nil {
		_, _ = fmt.Fprintf(w, "maintenance.reason: %s\n", r.Maintenance.Reason)
		_, _ = fmt.Fprintf(w, "maintenance.message: %s\n", r.Maintenance.Message)
		_, _ = fmt.Fprintf(w, "maintenance.since: %s\n", r.Maintenance.Since.UTC().Format(time.RFC3339))
		_, _ = fmt.Fprintf(w, "maintenance.self_healing.enabled: %t\n", r.Maintenance.SelfHealing.Enabled)
		_, _ = fmt.Fprintf(w, "maintenance.self_healing.exhausted: %t\n", r.Maintenance.SelfHealing.Exhausted)
		_, _ = fmt.Fprintf(w, "maintenance.self_healing.attempts: %d\n", r.Maintenance.SelfHealing.Attempts)
		if r.Maintenance.SelfHealing.LastAttempt == nil {
			_, _ = fmt.Fprintln(w, "maintenance.self_healing.last_attempt: null")
		} else {
			_, _ = fmt.Fprintf(w, "maintenance.self_healing.last_attempt: %s\n", r.Maintenance.SelfHealing.LastAttempt.UTC().Format(time.RFC3339))
		}
		if r.Maintenance.SelfHealing.NextAttempt == nil {
			_, _ = fmt.Fprintln(w, "maintenance.self_healing.next_attempt: null")
		} else {
			_, _ = fmt.Fprintf(w, "maintenance.self_healing.next_attempt: %s\n", r.Maintenance.SelfHealing.NextAttempt.UTC().Format(time.RFC3339))
		}
	}

	_, _ = fmt.Fprint(w, "# 3. Version & build\n")
	_, _ = fmt.Fprintf(w, "version: %s\n", r.Version)
	_, _ = fmt.Fprintf(w, "go_version: %s\n", r.GoVersion)
	_, _ = fmt.Fprintf(w, "build.commit: %s\n", r.Build.Commit)
	_, _ = fmt.Fprintf(w, "build.date: %s\n", r.Build.Date)

	_, _ = fmt.Fprint(w, "# 4. Runtime\n")
	_, _ = fmt.Fprintf(w, "uptime: %s\n", r.Uptime)
	_, _ = fmt.Fprintf(w, "mode: %s\n", r.Mode)
	_, _ = fmt.Fprintf(w, "timestamp: %s\n", r.Timestamp.Format(time.RFC3339))

	_, _ = fmt.Fprint(w, "# 5. Features\n")
	writeAPIHealthFeature(w, "tor", r.Features.Tor.Status, r.Features.Tor.Hostname, "", r.Features.Tor.State)
	writeAPIHealthFeature(w, "i2p", r.Features.I2P.Status, r.Features.I2P.Hostname, r.Features.I2P.Provider, r.Features.I2P.State)
	geoIPStatus := "disabled"
	if r.Features.GeoIP {
		geoIPStatus = "enabled"
	}
	_, _ = fmt.Fprintf(w, "features.geoip: %s\n", geoIPStatus)

	_, _ = fmt.Fprint(w, "# 6. Checks\n")
	_, _ = fmt.Fprintf(w, "checks.database: %s\n", r.Checks.Database)
	_, _ = fmt.Fprintf(w, "checks.cache: %s\n", r.Checks.Cache)
	_, _ = fmt.Fprintf(w, "checks.disk: %s\n", r.Checks.Disk)
	_, _ = fmt.Fprintf(w, "checks.scheduler: %s\n", r.Checks.Scheduler)
	// Overlay checks mirror the JSON body's omitempty: a disabled overlay has
	// no check to report, so the line is omitted rather than left blank.
	if r.Checks.Tor != "" {
		_, _ = fmt.Fprintf(w, "checks.tor: %s\n", r.Checks.Tor)
	}
	if r.Checks.I2P != "" {
		_, _ = fmt.Fprintf(w, "checks.i2p: %s\n", r.Checks.I2P)
	}
	for _, check := range r.Checks.Datasets {
		_, _ = fmt.Fprintf(w, "checks.%s: %s\n", check.Key, check.Status)
	}

	_, _ = fmt.Fprint(w, "# 7. Stats\n")
	_, _ = fmt.Fprintf(w, "stats.requests_total: %d\n", r.Stats.RequestsTotal)
	_, _ = fmt.Fprintf(w, "stats.requests_24h: %d\n", r.Stats.Requests24h)
	_, _ = fmt.Fprintf(w, "stats.active_connections: %d\n", r.Stats.ActiveConns)
}

func writeAPIHealthFeature(w http.ResponseWriter, name, status, hostname, provider, state string) {
	_, _ = fmt.Fprintf(w, "features.%s: %s", name, status)
	if hostname != "" {
		_, _ = fmt.Fprintf(w, " %s", hostname)
	}
	if provider != "" && provider != "none" {
		_, _ = fmt.Fprintf(w, " provider=%s", provider)
	}
	if state != "" {
		_, _ = fmt.Fprintf(w, " state=%s", state)
	}
	_, _ = fmt.Fprintln(w)
}

func writeHealthFrontendText(w http.ResponseWriter, r *http.Request, code int, response HealthResponse) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(code)
	lang := i18n.LangFromRequest(r)
	if response.Project.Tagline == "" {
		_, _ = fmt.Fprintln(w, response.Project.Name)
	} else {
		_, _ = fmt.Fprintf(w, "%s - %s\n", response.Project.Name, response.Project.Tagline)
	}
	_, _ = fmt.Fprintln(w)
	_, _, statusKey := HealthStatusPresentation(response.Status)
	writeFrontendHealthField(w, i18n.Translate(lang, "health.field.status"), i18n.Translate(lang, statusKey))
	writeFrontendHealthField(w, i18n.Translate(lang, "health.field.version"), response.Version)
	buildDate := response.Build.Date
	if builtAt, err := time.Parse(time.RFC3339, buildDate); err == nil {
		buildDate = humanHealthTimestamp(builtAt)
	}
	writeFrontendHealthField(w, i18n.Translate(lang, "health.field.build_commit"), response.Build.Commit+" ("+buildDate+")")
	writeFrontendHealthField(w, i18n.Translate(lang, "health.field.go_version"), response.GoVersion)
	writeFrontendHealthField(w, i18n.Translate(lang, "health.field.uptime"), humanHealthUptime(time.Since(startTime)))
	writeFrontendHealthField(w, i18n.Translate(lang, "health.field.mode"), i18n.Translate(lang, "health.mode."+response.Mode))
	writeFrontendHealthField(w, i18n.Translate(lang, "health.field.timestamp"), humanHealthTimestamp(response.Timestamp))

	_, _ = fmt.Fprintln(w)
	_, _ = fmt.Fprintln(w, i18n.Translate(lang, "health.section.features"))
	writeFrontendHealthFeature(w, lang, "tor", response.Features.Tor.Enabled, response.Features.Tor.Status, response.Features.Tor.Hostname)
	writeFrontendHealthFeature(w, lang, "i2p", response.Features.I2P.Enabled, response.Features.I2P.Status, response.Features.I2P.Hostname)
	geoIPStatus := i18n.Translate(lang, "health.feature_status.disabled")
	if response.Features.GeoIP {
		geoIPStatus = i18n.Translate(lang, "health.feature_status.enabled")
	}
	writeFrontendHealthField(w, i18n.Translate(lang, "health.feature.geoip"), geoIPStatus)

	_, _ = fmt.Fprintln(w)
	_, _ = fmt.Fprintln(w, i18n.Translate(lang, "health.section.checks"))
	coreChecks := []struct{ key, value string }{
		{"database", response.Checks.Database}, {"cache", response.Checks.Cache},
		{"disk", response.Checks.Disk}, {"scheduler", response.Checks.Scheduler},
		{"tor", response.Checks.Tor}, {"i2p", response.Checks.I2P},
	}
	for _, check := range coreChecks {
		if check.value != "" {
			writeFrontendHealthField(w, i18n.Translate(lang, "health.check."+check.key), i18n.Translate(lang, "health.check_status."+check.value))
		}
	}
	for _, check := range response.Checks.Datasets {
		writeFrontendHealthField(w, healthDatasetLabel(lang, check.Key), i18n.Translate(lang, "health.check_status."+check.Status))
	}

	_, _ = fmt.Fprintln(w)
	_, _ = fmt.Fprintln(w, i18n.Translate(lang, "health.section.stats"))
	writeFrontendHealthField(w, i18n.Translate(lang, "health.stat.requests_total"), groupedHealthCount(response.Stats.RequestsTotal))
	writeFrontendHealthField(w, i18n.Translate(lang, "health.stat.requests_24h"), groupedHealthCount(response.Stats.Requests24h))
	writeFrontendHealthField(w, i18n.Translate(lang, "health.stat.active_connections"), groupedHealthCount(int64(response.Stats.ActiveConns)))
}

func writeFrontendHealthField(w http.ResponseWriter, label, value string) {
	_, _ = fmt.Fprintf(w, "%-24s %s\n", label+":", value)
}

func writeFrontendHealthFeature(w http.ResponseWriter, lang, key string, enabled bool, status, hostname string) {
	statusKey := "health.feature_status." + status
	if !enabled {
		statusKey = "health.feature_status.disabled"
	}
	value := i18n.Translate(lang, statusKey)
	if hostname != "" {
		value += " - " + hostname
	}
	writeFrontendHealthField(w, i18n.Translate(lang, "health.feature."+key), value)
}

func healthDatasetLabel(lang, key string) string {
	switch {
	case key == "cve_nvd", key == "trivy_db":
		return i18n.Translate(lang, "health.check."+key)
	case strings.HasPrefix(key, "blocklist_"):
		return i18n.TranslateFormat(lang, "health.check.blocklist_source", map[string]string{"source_id": strings.TrimPrefix(key, "blocklist_")})
	default:
		return i18n.TranslateFormat(lang, "health.check.unknown", map[string]string{"check_id": key})
	}
}

func humanHealthTimestamp(value time.Time) string {
	return value.UTC().Format("January 02, 2006 at 15:04:05 MST")
}

func humanHealthUptime(duration time.Duration) string {
	if duration < 0 {
		duration = 0
	}
	days := int(duration.Hours()) / 24
	hours := int(duration.Hours()) % 24
	minutes := int(duration.Minutes()) % 60
	seconds := int(duration.Seconds())
	switch {
	case days > 0:
		return healthUnitCount(days, "day") + optionalHealthUnitCount(hours, "hour")
	case hours > 0:
		return healthUnitCount(hours, "hour") + optionalHealthUnitCount(minutes, "minute")
	case minutes > 0:
		return healthUnitCount(minutes, "minute")
	default:
		return healthUnitCount(seconds, "second")
	}
}

func healthUnitCount(value int, unit string) string {
	if value != 1 {
		unit += "s"
	}
	return strconv.Itoa(value) + " " + unit
}

func optionalHealthUnitCount(value int, unit string) string {
	if value == 0 {
		return ""
	}
	return " " + healthUnitCount(value, unit)
}

func groupedHealthCount(value int64) string {
	if value < 0 {
		return strconv.FormatInt(value, 10)
	}
	text := strconv.FormatInt(value, 10)
	for index := len(text) - 3; index > 0; index -= 3 {
		text = text[:index] + "," + text[index:]
	}
	return text
}

// healthBanner maps a PART 13 status value onto its banner class, icon, and translation key.
func healthBanner(status string) (class, icon, translationKey string) {
	switch status {
	case StatusDegraded:
		return "status-warning", "⚠️", "health.status.degraded"
	case StatusRestartRequired:
		return "status-warning", "🔄", "health.status.restart_required"
	case StatusUnhealthy:
		return "status-error", "❌", "health.status.unhealthy"
	case StatusMaintenance:
		return "status-error", "🚧", "health.status.maintenance"
	case StatusShuttingDown:
		return "status-error", "🛑", "health.status.shutting_down"
	default:
		return "status-ok", "✅", "health.status.healthy"
	}
}

// HealthStatusPresentation returns the CSS class, icon, and translation key for a health status.
func HealthStatusPresentation(status string) (class, icon, translationKey string) {
	return healthBanner(status)
}

// checkBadge renders a component check value as the PART 13 status badge.
func humanizeCheckKey(key string) string {
	if key == "" {
		return "Health Check"
	}
	parts := strings.SplitN(key, "_", 2)
	if len(parts) == 1 {
		return strings.ToUpper(parts[0][:1]) + parts[0][1:]
	}
	return strings.ToUpper(parts[0][:1]) + parts[0][1:] + ": " + strings.ReplaceAll(parts[1], "_", " ")
}

// HumanizeHealthCheckKey converts a stable check key into its fallback label.
func HumanizeHealthCheckKey(key string) string {
	return humanizeCheckKey(key)
}

// UptimeSeconds returns server uptime in whole seconds, for callers (such as
// the GraphQL resolvers) that need a numeric value rather than the
// human-readable string used by the REST/HTML health endpoint.
func UptimeSeconds() int64 {
	return int64(time.Since(startTime).Seconds())
}

// Status reports the overall health status using the same component checks
// and lifecycle state as the REST health endpoint, for callers that don't
// have a *config.ServerConfig (such as the GraphQL resolvers).
func Status() string {
	checks := ChecksInfo{
		Database:  checkDatabase(),
		Cache:     checkCache(),
		Disk:      checkDisk(),
		Scheduler: checkScheduler(),
		Datasets:  health.Snapshot(),
	}
	maintenance, shutdown, pending, _, _ := lifecycleState()
	return resolveStatus(checks, maintenance, shutdown, pending)
}

// checkDatabase checks database connectivity
func checkDatabase() string {
	if dbPinger == nil {
		return "error"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := dbPinger.PingContext(ctx); err != nil {
		return "error"
	}

	return "ok"
}

// checkCache checks cache connectivity
func checkCache() string {
	return cache.HealthStatus()
}

// checkScheduler reports the required internal scheduler's health. A missing
// probe is an error because this service always runs its built-in scheduler.
func checkScheduler() string {
	if schedulerProbe == nil {
		return "error"
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
		return normalizeTorInfo(torProbe())
	}

	manager := tor.Get()
	if manager == nil {
		return TorInfo{State: "no_provider", Status: "disabled"}
	}

	enabled, running, status, hostname := manager.StatusSnapshot()
	if !enabled {
		return TorInfo{State: "no_provider", Status: "disabled"}
	}
	return normalizeTorInfo(TorInfo{Enabled: enabled, Running: running, Status: status, Hostname: hostname})
}

func normalizeTorInfo(info TorInfo) TorInfo {
	if !info.Enabled {
		info.Running = false
		info.Status = "disabled"
		info.State = "no_provider"
		info.Hostname = ""
		return info
	}
	if info.Running {
		info.State = "running"
		info.Status = "healthy"
		return info
	}
	switch info.State {
	case "starting", "building":
		info.Status = "starting"
	case "error", "unavailable":
		info.State = "error"
		info.Status = "unavailable"
	default:
		if info.Status == "starting" {
			info.State = "starting"
		} else {
			info.State = "error"
			info.Status = "unavailable"
		}
	}
	info.Hostname = ""
	return info
}

// i2pFeature reports the eepsite state (PART 31.2). I2P is opt-in, so without
// a registered provider the feature reports itself as disabled rather than
// claiming an eepsite that does not exist.
func i2pFeature() I2PInfo {
	var info I2PInfo
	if i2pProbe != nil {
		info = i2pProbe()
	} else if manager := i2p.Get(); manager != nil {
		snapshot := manager.Snapshot()
		info = I2PInfo{Enabled: snapshot.Enabled, Running: snapshot.Running, Status: normalizeI2PHealthStatus(i2pLifecycleState(snapshot.I2PState)), State: string(snapshot.I2PState), Hostname: snapshot.Address, Provider: string(snapshot.Provider)}
	} else {
		return I2PInfo{Status: "disabled", State: "disabled", Provider: "none"}
	}
	if info.Provider == "" {
		info.Provider = "none"
	}
	if info.Status == "" {
		info.Status = normalizeI2PHealthStatus(StateToI2P(info.State))
	}
	if info.State == "" {
		info.State = string(StateToI2P(info.Status))
	}
	return info
}

type i2pLifecycleState string

const (
	i2pDisabled    i2pLifecycleState = "disabled"
	i2pStarting    i2pLifecycleState = "starting"
	i2pBuilding    i2pLifecycleState = "building"
	i2pRunning     i2pLifecycleState = "running"
	i2pUnavailable i2pLifecycleState = "error"
	i2pNoProvider  i2pLifecycleState = "no_provider"
)

func StateToI2P(state string) i2pLifecycleState {
	switch state {
	case "healthy":
		return i2pRunning
	case "unavailable":
		return i2pUnavailable
	case "starting":
		return i2pStarting
	case "building":
		return i2pBuilding
	case "running":
		return i2pRunning
	case "error":
		return i2pUnavailable
	case "no_provider":
		return i2pNoProvider
	default:
		return i2pDisabled
	}
}

func normalizeI2PHealthStatus(state i2pLifecycleState) string {
	switch state {
	case i2pStarting, i2pBuilding:
		return "starting"
	case i2pRunning:
		return "healthy"
	case i2pUnavailable:
		return "unavailable"
	default:
		return "disabled"
	}
}

// geoipFeature reports whether GeoIP lookups are actually available
// (PART 19). The configured toggle is authoritative for "off"; a registered
// probe can additionally report "on in config but not loaded yet".
func geoipFeature(cfg *config.ServerConfig) bool {
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
	if status == "unavailable" || status == "error" {
		return "degraded"
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
