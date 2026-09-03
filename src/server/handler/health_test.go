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
)

// fakePinger is a minimal stand-in for the interface{ PingContext(...) error }
// dependency health.go expects from a *sql.DB, letting us exercise both the
// success and failure branches of checkDatabase without a real database.
type fakePinger struct {
	err error
}

func (f fakePinger) PingContext(ctx context.Context) error {
	return f.err
}

// testConfig builds a minimal config.Config sufficient for
// BuildHealthResponse/ServerHealthz without touching disk or the paths
// package singleton.
func testConfig() *config.Config {
	cfg := &config.Config{}
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
	origMaintenance, origShutdown, origPending, origReason := lifecycleState()
	t.Cleanup(func() {
		dbPinger = origPinger
		schedulerProbe = origSched
		torProbe = origTor
		i2pProbe = origI2P
		geoipProbe = origGeoIP
		statsProvider = origStats
		SetMaintenanceMode(origMaintenance)
		SetShuttingDown(origShutdown)
		SetPendingRestart(origPending, origReason)
	})
	dbPinger = nil
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

// TestCheckDatabase covers the nil-pinger, healthy-ping, and failing-ping
// branches.
func TestCheckDatabase(t *testing.T) {
	resetHealthGlobals(t)

	assert.Equal(t, "unknown", checkDatabase(), "nil pinger should report unknown")

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
	assert.Equal(t, "unknown", checkScheduler(), "unwired scheduler should report unknown")

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
		{"one warning", ChecksInfo{Database: "ok", Cache: "warning", Disk: "ok", Scheduler: "ok"}, "degraded"},
		{"one error wins over warning", ChecksInfo{Database: "error", Cache: "warning", Disk: "ok", Scheduler: "ok"}, "unhealthy"},
		{"all error", ChecksInfo{Database: "error", Cache: "error", Disk: "error", Scheduler: "error"}, "unhealthy"},
		{"unknown does not degrade", ChecksInfo{Database: "unknown", Cache: "ok", Disk: "ok", Scheduler: "ok"}, "healthy"},
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
	warnChecks := ChecksInfo{Database: "ok", Cache: "warning", Disk: "ok", Scheduler: "ok"}
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

// TestServerHealthz_Degraded covers the remaining status value by driving a
// warning-level component check straight through BuildHealthResponse, since
// no live check currently reports "warning".
func TestServerHealthz_Degraded(t *testing.T) {
	checks := ChecksInfo{Database: "ok", Cache: "warning", Disk: "ok", Scheduler: "ok"}
	status := resolveStatus(checks, false, false, false)

	assert.Equal(t, StatusDegraded, status)
	assert.Equal(t, http.StatusOK, healthHTTPStatus(status))
}

// TestPendingRestartFields confirms pending_restart/restart_reason are
// omitted entirely when no restart is pending and populated when one is.
func TestPendingRestartFields(t *testing.T) {
	resetHealthGlobals(t)
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

	_, _, pending, got := lifecycleState()
	assert.True(t, pending)
	assert.Equal(t, []string{"server.port"}, got)
}

// TestBuildHealthResponse confirms the assembled response carries the
// branding/mode values through from config and has a coherent status.
func TestBuildHealthResponse(t *testing.T) {
	resetHealthGlobals(t)
	cfg := testConfig()

	resp := BuildHealthResponse(cfg)

	assert.Equal(t, "Test API", resp.Project.Name)
	assert.Equal(t, "testing things", resp.Project.Tagline)
	assert.Equal(t, "a test description", resp.Project.Description)
	assert.Equal(t, "production", resp.Mode)
	assert.NotEmpty(t, resp.Uptime)
	assert.NotEmpty(t, resp.GoVersion)
	assert.WithinDuration(t, time.Now().UTC(), resp.Timestamp, 5*time.Second)
	assert.Contains(t, []string{"healthy", "degraded", "unhealthy"}, resp.Status)
	assert.Equal(t, "unknown", resp.Checks.Database, "resetHealthGlobals leaves dbPinger nil")
	assert.Equal(t, "healthy", resp.Status, "unknown database check should not degrade overall status")
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
// then Accept: text/plain, then non-interactive UA, then default JSON.
func TestNegotiateHealthFormat_API(t *testing.T) {
	tests := []struct {
		name   string
		path   string
		accept string
		ua     string
		want   string
	}{
		{"txt suffix wins", "/server/healthz.txt", "application/json", "Mozilla/5.0", "text"},
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

// TestNegotiateHealthFormat_Frontend covers the frontend priority order:
// Accept: text/html, then text/plain, then UA sniffing, defaulting to html.
func TestNegotiateHealthFormat_Frontend(t *testing.T) {
	tests := []struct {
		name   string
		accept string
		ua     string
		want   string
	}{
		{"accept html wins", "text/html", "curl/8.0", "html"},
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

	var resp HealthResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.Equal(t, "Test API", resp.Project.Name)
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
	assert.True(t, strings.Contains(body, "project.name: Test API"))
	assert.True(t, strings.Contains(body, "status: "))
	assert.True(t, strings.Contains(body, "checks.database: "))
}

// TestServerHealthz_HTML drives the frontend handler with Accept: text/html
// and validates the PART 13 page structure: title, project header, status
// banner, the four section cards, and the shared stylesheets.
func TestServerHealthz_HTML(t *testing.T) {
	resetHealthGlobals(t)
	handlerFunc := ServerHealthz(testConfig(), true)

	req := httptest.NewRequest(http.MethodGet, "/server/healthz", nil)
	req.Header.Set("Accept", "text/html")
	rec := httptest.NewRecorder()

	handlerFunc(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "text/html; charset=utf-8", rec.Header().Get("Content-Type"))
	body := rec.Body.String()
	assert.True(t, strings.Contains(body, "<title>Test API - Health Status</title>"))
	assert.True(t, strings.Contains(body, "<h1>📦 Test API</h1>"))
	assert.True(t, strings.Contains(body, `class="tagline">testing things`))
	assert.True(t, strings.Contains(body, `class="status-banner status-ok"`))
	assert.True(t, strings.Contains(body, "All Systems Operational"))
	assert.True(t, strings.Contains(body, "/static/css/common.css"))
	assert.True(t, strings.Contains(body, "/static/js/app.js"))
	assert.True(t, strings.Contains(body, "<h2>🎛️ Features</h2>"))
	assert.True(t, strings.Contains(body, `class="data-table"`))
	assert.True(t, strings.Contains(body, "<h2>📈 Server Statistics</h2>"))
	assert.True(t, strings.Contains(body, `class="theme-dark"`))
}

// TestHealthHTMLThemeCookie confirms the theme cookie drives the <html>
// class so the page renders in the visitor's theme without init JavaScript.
func TestHealthHTMLThemeCookie(t *testing.T) {
	resetHealthGlobals(t)
	handlerFunc := ServerHealthz(testConfig(), true)

	req := httptest.NewRequest(http.MethodGet, "/server/healthz", nil)
	req.Header.Set("Accept", "text/html")
	req.AddCookie(&http.Cookie{Name: "theme", Value: "light"})
	rec := httptest.NewRecorder()

	handlerFunc(rec, req)

	assert.True(t, strings.Contains(rec.Body.String(), `class="theme-light"`))
}

// TestHealthHTMLOnionCopyButton confirms a running Tor hidden service renders
// its onion address in a copy-enabled code block (PART 13 field display
// rules), with the <pre> the shared clipboard helper reads from.
func TestHealthHTMLOnionCopyButton(t *testing.T) {
	resetHealthGlobals(t)
	const onion = "abc123xyz456abcdef789xyz456abcdef789xyz456abcdef789xyzab.onion"
	SetTorProbe(func() TorInfo {
		return TorInfo{Enabled: true, Running: true, Status: "healthy", Hostname: onion}
	})
	handlerFunc := ServerHealthz(testConfig(), true)

	req := httptest.NewRequest(http.MethodGet, "/server/healthz", nil)
	req.Header.Set("Accept", "text/html")
	rec := httptest.NewRecorder()

	handlerFunc(rec, req)

	body := rec.Body.String()
	assert.True(t, strings.Contains(body, `class="code-block"`))
	assert.True(t, strings.Contains(body, "<pre><code class=\"code-content\">"+onion+"</code></pre>"))
	assert.True(t, strings.Contains(body, `class="copy-btn" data-copy="`+onion+`"`))
	assert.True(t, strings.Contains(body, "🧅 Tor:"))
}

// TestHealthHTMLEscapesBranding confirms operator-supplied branding is
// escaped rather than injected into the page.
func TestHealthHTMLEscapesBranding(t *testing.T) {
	resetHealthGlobals(t)
	cfg := testConfig()
	cfg.Server.Branding.Title = `<script>alert(1)</script>`
	handlerFunc := ServerHealthz(cfg, true)

	req := httptest.NewRequest(http.MethodGet, "/server/healthz", nil)
	req.Header.Set("Accept", "text/html")
	rec := httptest.NewRecorder()

	handlerFunc(rec, req)

	body := rec.Body.String()
	assert.False(t, strings.Contains(body, "<script>alert(1)</script>"))
	assert.True(t, strings.Contains(body, "&lt;script&gt;alert(1)&lt;/script&gt;"))
}

// TestHealthBanner covers every PART 13 status-to-banner mapping.
func TestHealthBanner(t *testing.T) {
	tests := []struct {
		status string
		class  string
		icon   string
		text   string
	}{
		{StatusHealthy, "status-ok", "✅", "All Systems Operational"},
		{StatusDegraded, "status-warning", "⚠️", "Degraded Performance"},
		{StatusRestartRequired, "status-warning", "🔄", "Restart Required"},
		{StatusUnhealthy, "status-error", "❌", "Systems Unhealthy"},
		{StatusMaintenance, "status-error", "🚧", "Maintenance in Progress"},
		{StatusShuttingDown, "status-error", "🛑", "Shutting Down"},
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

// TestFormatThousands covers the comma grouping PART 13 requires for stats.
func TestFormatThousands(t *testing.T) {
	tests := []struct {
		in   int64
		want string
	}{
		{0, "0"},
		{42, "42"},
		{999, "999"},
		{1000, "1,000"},
		{45678, "45,678"},
		{1234567, "1,234,567"},
		{-1234, "-1,234"},
	}

	for _, tt := range tests {
		assert.Equal(t, tt.want, formatThousands(tt.in))
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
	assert.True(t, strings.Contains(body, "features.tor.hostname: example.onion"))
	assert.True(t, strings.Contains(body, "features.i2p.enabled: false"))
	assert.True(t, strings.Contains(body, "features.i2p.running: false"))
	assert.True(t, strings.Contains(body, "features.i2p.status: disabled"))
	assert.True(t, strings.Contains(body, "features.i2p.hostname: "))
	assert.True(t, strings.Contains(body, "features.i2p.provider: none"))
	assert.True(t, strings.Contains(body, "checks.tor: ok"))
	assert.False(t, strings.Contains(body, "checks.i2p:"), "disabled overlay omits its check line")
}

// TestHandleVersion validates the /api/v1/version handler's JSON body
// against the package-level Version/CommitID/BuildDate vars, including
// restoring them afterward so other tests aren't affected.
func TestHandleVersion(t *testing.T) {
	origVersion, origCommit, origBuild := Version, CommitID, BuildDate
	t.Cleanup(func() {
		Version, CommitID, BuildDate = origVersion, origCommit, origBuild
	})
	Version = "1.2.3"
	CommitID = "abcdef0"
	BuildDate = "2026-01-01"

	req := httptest.NewRequest(http.MethodGet, "/api/v1/version", nil)
	rec := httptest.NewRecorder()

	HandleVersion(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))

	var resp VersionResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.Equal(t, "1.2.3", resp.Version)
	assert.Equal(t, "abcdef0", resp.CommitID)
	assert.Equal(t, "2026-01-01", resp.BuildDate)
	assert.NotEmpty(t, resp.GoVersion)
	assert.NotEmpty(t, resp.Platform)
	assert.NotEmpty(t, resp.Arch)
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

// TestSetDatabase_Nil confirms passing a nil interface value resets the
// pinger back to the "unknown" reporting state.
func TestSetDatabase_Nil(t *testing.T) {
	resetHealthGlobals(t)
	SetDatabase(fakePinger{err: nil})
	assert.Equal(t, "ok", checkDatabase())

	SetDatabase(nil)
	assert.Equal(t, "unknown", checkDatabase())
}
