package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/apimgr/api/src/config"
	"github.com/apimgr/api/src/health"
)

// fakePinger is a minimal stand-in for the interface{ PingContext(...) error }
// dependency health.go expects from a *sql.DB, letting us exercise both the
// success and failure branches of checkDatabase without a real database.
type fakePinger struct {
	err error
}

func TestChecksInfoEmitsOrderedDatasetKeysAsSiblings(t *testing.T) {
	checks := ChecksInfo{Database: "ok", Cache: "ok", Disk: "ok", Scheduler: "ok", Datasets: []health.Check{{Key: "cve_nvd", Status: "degraded"}, {Key: "blocklist_ads", Status: "error"}}}
	data, err := json.Marshal(checks)
	require.NoError(t, err)
	encoded := string(data)
	require.Contains(t, encoded, `"cve_nvd":"degraded"`)
	require.Contains(t, encoded, `"blocklist_ads":"error"`)
	require.Less(t, strings.Index(encoded, `"scheduler"`), strings.Index(encoded, `"cve_nvd"`))
	require.Less(t, strings.Index(encoded, `"cve_nvd"`), strings.Index(encoded, `"blocklist_ads"`))
}

func (f fakePinger) PingContext(ctx context.Context) error {
	return f.err
}

// testConfig builds a minimal config.ServerConfig sufficient for
// BuildHealthResponse/ServerHealthz without touching disk or the paths
// package singleton.
func testConfig() *config.ServerConfig {
	cfg := &config.ServerConfig{}
	cfg.Server.Mode = "production"
	cfg.Server.Branding.Title = "Test API"
	cfg.Server.Branding.Tagline = "testing things"
	cfg.Server.Branding.Description = "a test description"
	return cfg
}

// resetHealthGlobals restores the package-level health state so tests don't
// leak the injected probes across each other.
func resetHealthGlobals(t *testing.T) {
	t.Helper()
	origPinger := dbPinger
	origSched := schedulerProbe
	origTor := torProbe
	origI2P := i2pProbe
	origGeoIP := geoipProbe
	origStats := statsProvider
	origMaintenance, origShutdown, origPending, origReason, origMaintenanceInfo := lifecycleState()
	t.Cleanup(func() {
		dbPinger = origPinger
		schedulerProbe = origSched
		torProbe = origTor
		i2pProbe = origI2P
		geoipProbe = origGeoIP
		statsProvider = origStats
		SetMaintenanceState(origMaintenanceInfo)
		if origMaintenance && origMaintenanceInfo == nil {
			SetMaintenanceMode(true)
		}
		SetShuttingDown(origShutdown)
		SetPendingRestart(origPending, origReason)
	})
	dbPinger = fakePinger{err: nil}
	SetSchedulerProbe(func() error { return nil })
	// Tor and I2P are probed rather than reaching for the process-wide
	// managers, so tests never depend on whether a Tor binary exists on the
	// machine running them.
	SetTorProbe(func() TorInfo { return TorInfo{Status: "disabled"} })
	SetI2PProbe(nil)
	SetGeoIPProbe(nil)
	SetStatsProvider(nil)
	SetMaintenanceMode(false)
	SetShuttingDown(false)
	SetPendingRestart(false, nil)
}

func TestBuildHealthResponseIncludesMaintenanceGuidance(t *testing.T) {
	resetHealthGlobals(t)
	since := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	last := since.Add(time.Minute)
	next := last.Add(30 * time.Second)
	SetMaintenanceState(&MaintenanceInfo{
		Reason: "database_connection", Message: "Database connection failed", Since: since,
		SelfHealing: SelfHealingInfo{Enabled: true, Attempts: 2, LastAttempt: &last, NextAttempt: &next},
	})
	response := BuildHealthResponse(testConfig())
	assert.Equal(t, StatusMaintenance, response.Status)
	assert.Equal(t, "error", response.Checks.Database)
	if assert.NotNil(t, response.Maintenance) {
		assert.Equal(t, "database_connection", response.Maintenance.Reason)
		assert.Equal(t, since, response.Maintenance.Since)
		assert.Equal(t, 2, response.Maintenance.SelfHealing.Attempts)
		assert.Equal(t, next, *response.Maintenance.SelfHealing.NextAttempt)
	}
}

// TestCheckDatabase covers the healthy-ping and failing-ping branches.
func TestCheckDatabase(t *testing.T) {
	resetHealthGlobals(t)

	assert.Equal(t, "ok", checkDatabase(), "reset helper installs a healthy fake pinger")

	SetDatabase(fakePinger{err: nil})
	assert.Equal(t, "ok", checkDatabase())

	SetDatabase(fakePinger{err: errors.New("connection refused")})
	assert.Equal(t, "error", checkDatabase())
}

// TestCheckCache confirms the always-available in-memory cache reports ok.
func TestCheckCache(t *testing.T) {
	assert.Equal(t, "ok", checkCache())
}

// TestCheckScheduler covers the unwired, healthy and failing probe states.
func TestCheckScheduler(t *testing.T) {
	resetHealthGlobals(t)

	schedulerProbe = nil
	assert.Equal(t, "error", checkScheduler(), "the required scheduler cannot be unwired")

	SetSchedulerProbe(func() error { return nil })
	assert.Equal(t, "ok", checkScheduler())

	SetSchedulerProbe(func() error { return errors.New("loop stopped") })
	assert.Equal(t, "error", checkScheduler())
}

// TestOverallStatus is table-driven over the documented precedence:
// any "error" wins outright, otherwise any "warning" degrades, otherwise
// healthy.
func TestOverallStatus(t *testing.T) {
	tests := []struct {
		name   string
		checks ChecksInfo
		want   string
	}{
		{"all ok", ChecksInfo{Database: "ok", Cache: "ok", Disk: "ok", Scheduler: "ok"}, "healthy"},
		{"one degraded check", ChecksInfo{Database: "ok", Cache: "degraded", Disk: "ok", Scheduler: "ok"}, "degraded"},
		{"database error wins over degradation", ChecksInfo{Database: "error", Cache: "degraded", Disk: "ok", Scheduler: "ok"}, "unhealthy"},
		{"cache errors degrade", ChecksInfo{Database: "ok", Cache: "error", Disk: "ok", Scheduler: "ok"}, "degraded"},
		{"unknown cache check degrades", ChecksInfo{Database: "ok", Cache: "unknown", Disk: "ok", Scheduler: "ok"}, "degraded"},
		{"zero value", ChecksInfo{}, "healthy"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, overallStatus(tt.checks))
		})
	}
}

// TestResolveStatus is table-driven over all six PART 13 status values and
// the severity precedence between them: shutdown outranks maintenance, which
// outranks failing checks, which outrank a pending restart.
func TestResolveStatus(t *testing.T) {
	okChecks := ChecksInfo{Database: "ok", Cache: "ok", Disk: "ok", Scheduler: "ok"}
	warnChecks := ChecksInfo{Database: "ok", Cache: "degraded", Disk: "ok", Scheduler: "ok"}
	errChecks := ChecksInfo{Database: "error", Cache: "ok", Disk: "ok", Scheduler: "ok"}

	tests := []struct {
		name        string
		checks      ChecksInfo
		maintenance bool
		shutdown    bool
		pending     bool
		want        string
	}{
		{"all ok", okChecks, false, false, false, StatusHealthy},
		{"warning degrades", warnChecks, false, false, false, StatusDegraded},
		{"pending restart", okChecks, false, false, true, StatusRestartRequired},
		{"failing check", errChecks, false, false, false, StatusUnhealthy},
		{"maintenance", okChecks, true, false, false, StatusMaintenance},
		{"shutting down", okChecks, false, true, false, StatusShuttingDown},
		{"shutdown outranks maintenance", errChecks, true, true, true, StatusShuttingDown},
		{"maintenance outranks failing check", errChecks, true, false, true, StatusMaintenance},
		{"failing check outranks pending restart", errChecks, false, false, true, StatusUnhealthy},
		{"degraded outranks pending restart", warnChecks, false, false, true, StatusDegraded},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resolveStatus(tt.checks, tt.maintenance, tt.shutdown, tt.pending)
			assert.Equal(t, tt.want, got)
		})
	}
}

// TestHealthHTTPStatus covers the full PART 13 status-to-HTTP-code table.
func TestHealthHTTPStatus(t *testing.T) {
	tests := []struct {
		status string
		want   int
	}{
		{StatusHealthy, http.StatusOK},
		{StatusDegraded, http.StatusOK},
		{StatusRestartRequired, http.StatusOK},
		{StatusUnhealthy, http.StatusServiceUnavailable},
		{StatusMaintenance, http.StatusServiceUnavailable},
		{StatusShuttingDown, http.StatusServiceUnavailable},
	}

	for _, tt := range tests {
		t.Run(tt.status, func(t *testing.T) {
			assert.Equal(t, tt.want, healthHTTPStatus(tt.status))
		})
	}
}

// TestServerHealthz_StatusCodes drives the handler end-to-end through every
// status value, asserting the HTTP code, the bare (un-enveloped) body, and
// that "mode" always reports the configured mode — never "maintenance".
func TestServerHealthz_StatusCodes(t *testing.T) {
	tests := []struct {
		name     string
		setup    func()
		want     string
		wantCode int
	}{
		{
			name:     "healthy",
			setup:    func() { SetDatabase(fakePinger{err: nil}) },
			want:     StatusHealthy,
			wantCode: http.StatusOK,
		},
		{
			name: "restart_required",
			setup: func() {
				SetDatabase(fakePinger{err: nil})
				SetPendingRestart(true, []string{"server.port"})
			},
			want:     StatusRestartRequired,
			wantCode: http.StatusOK,
		},
		{
			name:     "unhealthy",
			setup:    func() { SetDatabase(fakePinger{err: errors.New("boom")}) },
			want:     StatusUnhealthy,
			wantCode: http.StatusServiceUnavailable,
		},
		{
			name: "maintenance",
			setup: func() {
				SetDatabase(fakePinger{err: nil})
				SetMaintenanceMode(true)
			},
			want:     StatusMaintenance,
			wantCode: http.StatusServiceUnavailable,
		},
		{
			name: "shutting_down",
			setup: func() {
				SetDatabase(fakePinger{err: nil})
				SetShuttingDown(true)
			},
			want:     StatusShuttingDown,
			wantCode: http.StatusServiceUnavailable,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resetHealthGlobals(t)
			// A host with under 10% free space makes checkDisk report
			// "warning", which degrades the two 200-level cases. Skip
			// those rather than assert a result the environment controls.
			if tt.wantCode == http.StatusOK && checkDisk() != "ok" {
				t.Skip("host disk check is not ok; cannot assert a 200-level status")
			}
			tt.setup()
			SetSchedulerProbe(func() error { return nil })

			req := httptest.NewRequest(http.MethodGet, "/api/v1/server/healthz", nil)
			req.Header.Set("Accept", "application/json")
			req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh)")
			rec := httptest.NewRecorder()

			ServerHealthz(testConfig(), false)(rec, req)

			assert.Equal(t, tt.wantCode, rec.Code)

			var body map[string]any
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
			assert.Equal(t, tt.want, body["status"])
			assert.Equal(t, "production", body["mode"], "mode must always report the configured mode")
			assert.NotContains(t, body, "ok", "health responses are bare, never enveloped")
			assert.NotContains(t, body, "data", "health responses are bare, never enveloped")
		})
	}
}

// TestServerHealthz_Degraded covers the degraded status using the canonical
// dataset/cache vocabulary.
func TestServerHealthz_Degraded(t *testing.T) {
	checks := ChecksInfo{Database: "ok", Cache: "degraded", Disk: "ok", Scheduler: "ok"}
	status := resolveStatus(checks, false, false, false)

	assert.Equal(t, StatusDegraded, status)
	assert.Equal(t, http.StatusOK, healthHTTPStatus(status))
}

// TestPendingRestartFields confirms pending_restart/restart_reason are
// omitted entirely when no restart is pending and populated when one is.
func TestPendingRestartFields(t *testing.T) {
	resetHealthGlobals(t)
	SetDatabase(fakePinger{err: nil})
	SetSchedulerProbe(func() error { return nil })
	cfg := testConfig()

	raw, err := json.Marshal(BuildHealthResponse(cfg))
	require.NoError(t, err)
	assert.NotContains(t, string(raw), "pending_restart")
	assert.NotContains(t, string(raw), "restart_reason")

	SetPendingRestart(true, []string{"server.port", "database.driver"})
	resp := BuildHealthResponse(cfg)

	assert.True(t, resp.PendingRestart)
	assert.Equal(t, []string{"server.port", "database.driver"}, resp.RestartReason)
	if checkDisk() == "ok" {
		assert.Equal(t, StatusRestartRequired, resp.Status)
	}

	raw, err = json.Marshal(resp)
	require.NoError(t, err)
	assert.Contains(t, string(raw), `"pending_restart":true`)
	assert.Contains(t, string(raw), `"restart_reason":["server.port","database.driver"]`)
}

// TestSetPendingRestart_CopiesReason confirms the recorded reasons are
// snapshotted, so a caller mutating its slice afterward cannot alter what a
// later health response reports.
func TestSetPendingRestart_CopiesReason(t *testing.T) {
	resetHealthGlobals(t)

	reason := []string{"server.port"}
	SetPendingRestart(true, reason)
	reason[0] = "mutated"

	_, _, pending, got, _ := lifecycleState()
	assert.True(t, pending)
	assert.Equal(t, []string{"server.port"}, got)
}

// TestBuildHealthResponse confirms the assembled response carries the
// branding/mode values through from config and has a coherent status.
func TestBuildHealthResponse(t *testing.T) {
	resetHealthGlobals(t)
	SetDatabase(nil)
	cfg := testConfig()

	resp := BuildHealthResponse(cfg)

	assert.Equal(t, "Test API", resp.Project.Name)
	assert.Equal(t, "testing things", resp.Project.Tagline)
	assert.Equal(t, "a test description", resp.Project.Description)
	assert.Equal(t, "production", resp.Mode)
	assert.NotEmpty(t, resp.Uptime)
	assert.NotEmpty(t, resp.GoVersion)
	assert.WithinDuration(t, time.Now().UTC(), resp.Timestamp, 5*time.Second)
	assert.Zero(t, resp.Timestamp.Nanosecond(), "health timestamps use whole UTC seconds")
	assert.Contains(t, []string{"healthy", "degraded", "unhealthy"}, resp.Status)
	assert.Equal(t, "error", resp.Checks.Database, "missing database probe is unavailable")
	assert.Equal(t, "unhealthy", resp.Status, "missing required database probe must degrade service health")
}

// TestBuildHealthResponse_DatabaseError confirms an unhealthy DB check flips
// the overall status to unhealthy end-to-end.
func TestBuildHealthResponse_DatabaseError(t *testing.T) {
	resetHealthGlobals(t)
	SetDatabase(fakePinger{err: errors.New("boom")})
	cfg := testConfig()

	resp := BuildHealthResponse(cfg)

	assert.Equal(t, "error", resp.Checks.Database)
	assert.Equal(t, "unhealthy", resp.Status)
}

// TestNegotiateHealthFormat_API covers the API priority order: .txt suffix,
// explicit Accept headers, non-interactive UA, then default JSON.
func TestNegotiateHealthFormat_API(t *testing.T) {
	tests := []struct {
		name   string
		path   string
		accept string
		ua     string
		want   string
	}{
		{"txt suffix wins", "/server/healthz.txt", "application/json", "Mozilla/5.0", "text"},
		{"explicit json beats curl UA", "/server/healthz", "application/json", "curl/8.0", "json"},
		{"accept text/plain", "/server/healthz", "text/plain", "Mozilla/5.0", "text"},
		{"curl UA defaults to text", "/server/healthz", "", "curl/8.0", "text"},
		{"empty UA is non-interactive", "/server/healthz", "", "", "text"},
		{"browser UA defaults to json for API", "/server/healthz", "", "Mozilla/5.0 (Macintosh)", "json"},
		{"explicit json accept", "/server/healthz", "application/json", "Mozilla/5.0", "json"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tt.path, nil)
			if tt.accept != "" {
				req.Header.Set("Accept", tt.accept)
			}
			req.Header.Set("User-Agent", tt.ua)

			got := negotiateHealthFormat(req, false)
			assert.Equal(t, tt.want, got)
		})
	}
}

// TestNegotiateHealthFormat_Frontend covers explicit Accept headers before
// non-interactive-client detection, then defaults to HTML.
func TestNegotiateHealthFormat_Frontend(t *testing.T) {
	tests := []struct {
		name   string
		accept string
		ua     string
		want   string
	}{
		{"accept html wins", "text/html", "curl/8.0", "html"},
		{"explicit json beats curl UA", "application/json", "curl/8.0", "json"},
		{"accept text/plain", "text/plain", "Mozilla/5.0", "text"},
		{"curl UA defaults to text", "", "curl/8.0", "text"},
		{"browser UA defaults to html", "", "Mozilla/5.0 (Macintosh)", "html"},
		{"empty UA defaults to text", "", "", "text"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/server/healthz", nil)
			if tt.accept != "" {
				req.Header.Set("Accept", tt.accept)
			}
			req.Header.Set("User-Agent", tt.ua)

			got := negotiateHealthFormat(req, true)
			assert.Equal(t, tt.want, got)
		})
	}
}

// TestIsNonInteractiveClient is table-driven over known tool user agents,
// a browser UA, and the empty-UA edge case.
func TestIsNonInteractiveClient(t *testing.T) {
	tests := []struct {
		name string
		ua   string
		want bool
	}{
		{"empty is non-interactive", "", true},
		{"curl", "curl/8.4.0", true},
		{"wget", "Wget/1.21.3", true},
		{"httpie", "HTTPie/3.2.2", true},
		{"python-requests", "python-requests/2.31.0", true},
		{"go-http-client", "Go-http-client/1.1", true},
		{"case insensitive match", "CURL/8.0", true},
		{"chrome browser", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 Chrome/120.0", false},
		{"firefox browser", "Mozilla/5.0 (X11; Linux x86_64; rv:109.0) Gecko/20100101 Firefox/121.0", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.Header.Set("User-Agent", tt.ua)
			assert.Equal(t, tt.want, isNonInteractiveClient(req))
		})
	}
}

// TestServerHealthz_JSON drives the full handler end-to-end for the API
// (htmlDefault=false) JSON branch and validates status code, content type,
// and decodable body.
func TestServerHealthz_JSON(t *testing.T) {
	resetHealthGlobals(t)
	handlerFunc := ServerHealthz(testConfig(), false)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/server/healthz", nil)
	req.Header.Set("Accept", "application/json")
	// A browser User-Agent is required to avoid the isNonInteractiveClient
	// fallback to "text" that an empty/default User-Agent would trigger.
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh)")
	rec := httptest.NewRecorder()

	handlerFunc(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))

	raw := rec.Body.String()
	var resp HealthResponse
	require.NoError(t, json.NewDecoder(strings.NewReader(raw)).Decode(&resp))
	assert.Equal(t, "Test API", resp.Project.Name)
	assert.NotEmpty(t, resp.Features.Tor.State)
	assert.NotEmpty(t, resp.Features.I2P.State)
	orderedKeys := []string{`"project"`, `"status"`, `"version"`, `"go_version"`, `"build"`, `"uptime"`, `"mode"`, `"timestamp"`, `"features"`, `"checks"`, `"stats"`}
	previous := -1
	for _, key := range orderedKeys {
		index := strings.Index(raw, key)
		require.Greater(t, index, previous, "health JSON key %s should appear in canonical order", key)
		previous = index
	}
	assert.Zero(t, resp.Timestamp.Nanosecond())
}

// TestServerHealthz_Text drives the frontend (htmlDefault=true) handler with
// Accept: text/plain and validates the dot-notation body format.
func TestServerHealthz_Text(t *testing.T) {
	resetHealthGlobals(t)
	handlerFunc := ServerHealthz(testConfig(), true)

	req := httptest.NewRequest(http.MethodGet, "/server/healthz", nil)
	req.Header.Set("Accept", "text/plain")
	rec := httptest.NewRecorder()

	handlerFunc(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "text/plain; charset=utf-8", rec.Header().Get("Content-Type"))
	body := rec.Body.String()
	assert.Contains(t, body, "Test API - testing things")
	assert.Contains(t, body, "Status:")
	assert.Contains(t, body, "Database:")
	assert.NotContains(t, body, "project.name:")
	assert.NotContains(t, body, "checks.database:")
}

// TestServerHealthz_HTML delegates browser rendering to the server package so
// the handler keeps content negotiation separate from the shared page shell.
func TestServerHealthz_HTMLDelegatesRenderer(t *testing.T) {
	resetHealthGlobals(t)
	handlerFunc := ServerHealthz(testConfig(), true, func(w http.ResponseWriter, r *http.Request, code int, response HealthResponse) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(code)
		_, _ = w.Write([]byte("health page"))
	})
	req := httptest.NewRequest(http.MethodGet, "/server/healthz", nil)
	req.Header.Set("Accept", "text/html")
	rec := httptest.NewRecorder()
	handlerFunc(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "text/html; charset=utf-8", rec.Header().Get("Content-Type"))
	assert.Equal(t, "health page", rec.Body.String())
}

// TestHealthBanner covers every PART 13 status-to-banner mapping.
func TestHealthBanner(t *testing.T) {
	tests := []struct {
		status string
		class  string
		icon   string
		text   string
	}{
		{StatusHealthy, "status-ok", "✅", "health.status.healthy"},
		{StatusDegraded, "status-warning", "⚠️", "health.status.degraded"},
		{StatusRestartRequired, "status-warning", "🔄", "health.status.restart_required"},
		{StatusUnhealthy, "status-error", "❌", "health.status.unhealthy"},
		{StatusMaintenance, "status-error", "🚧", "health.status.maintenance"},
		{StatusShuttingDown, "status-error", "🛑", "health.status.shutting_down"},
	}

	for _, tt := range tests {
		t.Run(tt.status, func(t *testing.T) {
			class, icon, text := healthBanner(tt.status)
			assert.Equal(t, tt.class, class)
			assert.Equal(t, tt.icon, icon)
			assert.Equal(t, tt.text, text)
		})
	}
}

// TestTorProbeOverride confirms the injected Tor probe supplies the feature
// block and the derived checks entry.
func TestTorProbeOverride(t *testing.T) {
	resetHealthGlobals(t)
	SetTorProbe(func() TorInfo {
		return TorInfo{Enabled: true, Running: true, Status: "healthy", Hostname: "example.onion"}
	})

	resp := BuildHealthResponse(testConfig())

	assert.True(t, resp.Features.Tor.Enabled)
	assert.True(t, resp.Features.Tor.Running)
	assert.Equal(t, "healthy", resp.Features.Tor.Status)
	assert.Equal(t, "example.onion", resp.Features.Tor.Hostname)
	assert.Equal(t, "ok", resp.Checks.Tor)
}

// TestI2PProbe covers the unset (opt-in disabled) default and an enabled
// eepsite, including the omitempty check entry.
func TestI2PProbe(t *testing.T) {
	resetHealthGlobals(t)

	resp := BuildHealthResponse(testConfig())
	assert.False(t, resp.Features.I2P.Enabled)
	assert.Equal(t, "disabled", resp.Features.I2P.Status)
	assert.Equal(t, "none", resp.Features.I2P.Provider)
	assert.Equal(t, "", resp.Checks.I2P)

	SetI2PProbe(func() I2PInfo {
		return I2PInfo{Enabled: true, Running: true, Status: "healthy", Hostname: "example.b32.i2p", Provider: "i2pd"}
	})

	resp = BuildHealthResponse(testConfig())
	assert.True(t, resp.Features.I2P.Enabled)
	assert.Equal(t, "i2pd", resp.Features.I2P.Provider)
	assert.Equal(t, "example.b32.i2p", resp.Features.I2P.Hostname)
	assert.Equal(t, "ok", resp.Checks.I2P)
}

// TestI2PProbeDefaultsFilled confirms a probe that omits status/provider
// still produces the spec's documented defaults rather than empty strings.
func TestI2PProbeDefaultsFilled(t *testing.T) {
	resetHealthGlobals(t)
	SetI2PProbe(func() I2PInfo { return I2PInfo{} })

	resp := BuildHealthResponse(testConfig())

	assert.Equal(t, "disabled", resp.Features.I2P.Status)
	assert.Equal(t, "none", resp.Features.I2P.Provider)
}

// TestGeoIPProbe confirms the config toggle gates the feature and the probe
// can further report the databases as not yet usable.
func TestGeoIPProbe(t *testing.T) {
	resetHealthGlobals(t)
	cfg := testConfig()

	assert.False(t, geoipFeature(cfg), "disabled in config reports false")

	cfg.Server.GeoIP.Enabled = true
	assert.True(t, geoipFeature(cfg), "no probe trusts the config toggle")

	SetGeoIPProbe(func() bool { return false })
	assert.False(t, geoipFeature(cfg), "probe reporting unloaded databases wins")

	SetGeoIPProbe(func() bool { return true })
	assert.True(t, geoipFeature(cfg))
}

// TestStatsProviderOverride confirms the injected provider supplies the
// public aggregates and that the in-process collector is the fallback.
func TestStatsProviderOverride(t *testing.T) {
	resetHealthGlobals(t)
	SetStatsProvider(func() (int64, int64, int) { return 1234567, 45678, 42 })

	resp := BuildHealthResponse(testConfig())

	assert.Equal(t, int64(1234567), resp.Stats.RequestsTotal)
	assert.Equal(t, int64(45678), resp.Stats.Requests24h)
	assert.Equal(t, 42, resp.Stats.ActiveConns)

	SetStatsProvider(nil)
	ResetRequestStats()
	done := RequestStarted()
	resp = BuildHealthResponse(testConfig())
	assert.Equal(t, int64(1), resp.Stats.RequestsTotal)
	assert.Equal(t, 1, resp.Stats.ActiveConns)
	done()
}

// TestHealthTextOverlayFields confirms the plain-text body carries every
// PART 13 feature line, including the Tor hostname and the full I2P block,
// and that overlay checks follow the JSON body's omitempty behaviour.
func TestHealthTextOverlayFields(t *testing.T) {
	resetHealthGlobals(t)
	SetTorProbe(func() TorInfo {
		return TorInfo{Enabled: true, Running: true, Status: "healthy", Hostname: "example.onion"}
	})
	handlerFunc := ServerHealthz(testConfig(), true)

	req := httptest.NewRequest(http.MethodGet, "/server/healthz", nil)
	req.Header.Set("Accept", "text/plain")
	rec := httptest.NewRecorder()

	handlerFunc(rec, req)

	body := rec.Body.String()
	assert.Contains(t, body, "Tor:")
	assert.Contains(t, body, "example.onion")
	assert.Contains(t, body, "I2P:")
	assert.Contains(t, body, "Component Status")
	assert.NotContains(t, body, "features.tor.hostname:")
	assert.NotContains(t, body, "features.i2p.enabled:")
	assert.NotContains(t, body, "checks.i2p:", "disabled overlay omits its check line")
}

func TestAPIHealthTextUsesOneMachineLinePerFeature(t *testing.T) {
	resetHealthGlobals(t)
	SetTorProbe(func() TorInfo {
		return TorInfo{Enabled: true, Running: false, State: "starting", Status: "starting"}
	})
	handlerFunc := ServerHealthz(testConfig(), false)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/server/healthz", nil)
	request.Header.Set("Accept", "text/plain")
	request.Header.Set("User-Agent", "Mozilla/5.0")
	recorder := httptest.NewRecorder()
	handlerFunc(recorder, request)

	body := recorder.Body.String()
	assert.Contains(t, body, "features.tor: starting state=starting")
	assert.Contains(t, body, "features.i2p: disabled state=disabled")
	assert.Contains(t, body, "features.geoip: disabled")
	assert.NotContains(t, body, "features.tor.status:")
}

// TestGetUptime_Boundaries exercises the days/hours/minutes formatting tiers
// by manipulating the package-level startTime, restoring it afterward.
func TestGetUptime_Boundaries(t *testing.T) {
	origStart := startTime
	t.Cleanup(func() { startTime = origStart })

	tests := []struct {
		name string
		ago  time.Duration
		want string
	}{
		{"just started, minutes only", 30 * time.Second, "0m"},
		{"minutes only", 5 * time.Minute, "5m"},
		{"hours and minutes", 2*time.Hour + 3*time.Minute, "2h 3m"},
		{"exact hour boundary", 1 * time.Hour, "1h"},
		{"days hours minutes", 25*time.Hour + 4*time.Minute, "1d 1h 4m"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			startTime = time.Now().Add(-tt.ago)
			assert.Equal(t, tt.want, getUptime())
		})
	}
}

// TestFormatUptime_DirectComponents covers the helper directly for all three
// tiers plus the zero-value case, independent of real elapsed time.
func TestFormatUptime_DirectComponents(t *testing.T) {
	tests := []struct {
		name                string
		a, b, c             int
		aUnit, bUnit, cUnit string
		want                string
	}{
		{"three parts", 1, 2, 3, "d", "h", "m", "1d 2h 3m"},
		{"two parts, c zero", 4, 5, 0, "h", "m", "", "4h 5m"},
		{"one part, b zero", 6, 0, 0, "m", "", "", "6m"},
		{"c positive but no cUnit falls back to two parts", 1, 2, 3, "d", "h", "", "1d 2h"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatUptime(tt.a, tt.b, tt.c, tt.aUnit, tt.bUnit, tt.cUnit)
			assert.Equal(t, tt.want, got)
		})
	}
}

// TestSetDatabase_Nil confirms a nil database pinger reports an error because
// the database is a required service dependency.
func TestSetDatabase_Nil(t *testing.T) {
	resetHealthGlobals(t)
	SetDatabase(fakePinger{err: nil})
	assert.Equal(t, "ok", checkDatabase())

	SetDatabase(nil)
	assert.Equal(t, "error", checkDatabase())
}
