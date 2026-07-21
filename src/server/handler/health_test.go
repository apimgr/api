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
// leak dbPinger/schedulerEnabled across each other.
func resetHealthGlobals(t *testing.T) {
	t.Helper()
	origPinger := dbPinger
	origSched := schedulerEnabled
	t.Cleanup(func() {
		dbPinger = origPinger
		schedulerEnabled = origSched
	})
	dbPinger = nil
	schedulerEnabled = false
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

// TestCheckScheduler covers both the enabled and disabled states; per the
// current implementation both report "ok" (scheduler absence is not itself
// an error condition).
func TestCheckScheduler(t *testing.T) {
	resetHealthGlobals(t)

	SetSchedulerEnabled(false)
	assert.Equal(t, "ok", checkScheduler())

	SetSchedulerEnabled(true)
	assert.Equal(t, "ok", checkScheduler())
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
// and validates the minimal HTML fallback output.
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
	assert.True(t, strings.Contains(body, "<h1>Test API</h1>"))
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
