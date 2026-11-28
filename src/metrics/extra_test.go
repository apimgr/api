package metrics

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestNormalizePath(t *testing.T) {
	cases := map[string]string{
		"/api/v1/items/123": "/api/v1/items/:id",
		"/api/v1/items/550e8400-e29b-41d4-a716-446655440000": "/api/v1/items/:id",
		"/server/healthz": "/server/healthz",
	}
	for in, want := range cases {
		if got := NormalizePath(in); got != want {
			t.Errorf("NormalizePath(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestRecordDBQueryAndError(t *testing.T) {
	m := newMetrics()
	m.RecordDBQuery("select", "users", 5*time.Millisecond)
	if got := testutil.ToFloat64(m.dbQueriesTotal.WithLabelValues("select", "users")); got != 1 {
		t.Fatalf("dbQueriesTotal = %v, want 1", got)
	}

	m.RecordDBError("select", "timeout")
	if got := testutil.ToFloat64(m.dbErrorsTotal.WithLabelValues("select", "timeout")); got != 1 {
		t.Fatalf("dbErrorsTotal = %v, want 1", got)
	}

	m.SetDBConnections(10, 3)
	if got := testutil.ToFloat64(m.dbConnectionsOpen); got != 10 {
		t.Fatalf("dbConnectionsOpen = %v, want 10", got)
	}
	if got := testutil.ToFloat64(m.dbConnectionsInUse); got != 3 {
		t.Fatalf("dbConnectionsInUse = %v, want 3", got)
	}
}

func TestCacheSchedulerTorRateLimitRecorders(t *testing.T) {
	m := newMetrics()

	m.RecordCacheHit("sessions")
	m.RecordCacheMiss("sessions")
	m.RecordCacheEviction("sessions")
	m.SetCacheSize("sessions", 5)
	m.SetCacheBytes("sessions", 1024)
	if got := testutil.ToFloat64(m.cacheHitsTotal.WithLabelValues("sessions")); got != 1 {
		t.Fatalf("cacheHitsTotal = %v, want 1", got)
	}

	m.RecordSchedulerTaskStart("backup_daily")
	m.RecordSchedulerTaskEnd("backup_daily", 2*time.Second, nil)
	if got := testutil.ToFloat64(m.schedulerTasksTotal.WithLabelValues("backup_daily", "success")); got != 1 {
		t.Fatalf("schedulerTasksTotal success = %v, want 1", got)
	}

	m.SetTorEnabled(true)
	m.SetTorRunning(true)
	m.SetTorCircuitEstablished(false)
	m.IncTorRequests()
	if got := testutil.ToFloat64(m.torEnabled); got != 1 {
		t.Fatalf("torEnabled = %v, want 1", got)
	}
	if got := testutil.ToFloat64(m.torCircuitEstablished); got != 0 {
		t.Fatalf("torCircuitEstablished = %v, want 0", got)
	}

	m.RecordRateLimitRequest("per_ip", "allowed")
	m.RecordRateLimitBlocked("per_ip")
	if got := testutil.ToFloat64(m.ratelimitBlockedTotal.WithLabelValues("per_ip")); got != 1 {
		t.Fatalf("ratelimitBlockedTotal = %v, want 1", got)
	}
}

func TestAuthMiddleware(t *testing.T) {
	ok := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	empty := AuthMiddleware("", false, ok)
	rec := httptest.NewRecorder()
	empty.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("empty token: status = %d, want 403", rec.Code)
	}

	unauth := AuthMiddleware("secret", true, ok)
	rec = httptest.NewRecorder()
	unauth.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("allow_unauthenticated: status = %d, want 200", rec.Code)
	}

	guarded := AuthMiddleware("secret", false, ok)

	rec = httptest.NewRecorder()
	guarded.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("no header: status = %d, want 401", rec.Code)
	}

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer wrong")
	rec = httptest.NewRecorder()
	guarded.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("wrong token: status = %d, want 401", rec.Code)
	}

	req = httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer secret")
	rec = httptest.NewRecorder()
	guarded.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("correct token: status = %d, want 200", rec.Code)
	}
}

func TestMiddlewareRecordsRequest(t *testing.T) {
	m := newMetrics()
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte("hello"))
	})

	handler := m.Middleware(next)
	req := httptest.NewRequest(http.MethodPost, "/items/42", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if got := testutil.ToFloat64(m.httpRequestsTotal.WithLabelValues("POST", "__unmatched__", "201")); got != 1 {
		t.Fatalf("httpRequestsTotal = %v, want 1", got)
	}
}

func TestMiddlewareUsesMatchedRouteAndCountsChunkedRequestBytes(t *testing.T) {
	m := newMetrics()
	router := chi.NewRouter()
	router.Use(m.Middleware)
	router.Post("/reports/{tracking_id}", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		_, _ = w.Write([]byte("ok"))
	}))
	handler := router
	req := httptest.NewRequest(http.MethodPost, "/reports/01J8M4Q6G7H2D9P3K5T1V8W0XA", strings.NewReader("chunked request payload"))
	req.ContentLength = -1
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if got := testutil.ToFloat64(m.httpRequestsTotal.WithLabelValues("POST", "/reports/{tracking_id}", "200")); got != 1 {
		t.Fatalf("route-template request count = %v, want 1", got)
	}
	families, err := m.registry.Gather()
	if err != nil {
		t.Fatal(err)
	}
	for _, family := range families {
		if family.GetName() != "api_http_request_size_bytes" || len(family.Metric) != 1 {
			continue
		}
		histogram := family.Metric[0].GetHistogram()
		if histogram.GetSampleCount() != 1 || histogram.GetSampleSum() != float64(len("chunked request payload")) {
			t.Fatalf("request size histogram = count %d sum %v", histogram.GetSampleCount(), histogram.GetSampleSum())
		}
		return
	}
	t.Fatal("request size histogram was not gathered")
}

func TestGrafanaHandlerServesDashboard(t *testing.T) {
	m := newMetrics()
	handler := m.GrafanaHandler("api")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json; charset=utf-8" {
		t.Fatalf("Content-Type = %q", ct)
	}
	for _, category := range []string{"scheduler_tasks_running", "system_memory_usage_percent", "owner_tokens_active"} {
		if !strings.Contains(rec.Body.String(), category) {
			t.Errorf("dashboard is missing %s category", category)
		}
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, rec.Body.Bytes()); err != nil {
		t.Fatalf("dashboard JSON is invalid: %v", err)
	}
	for _, contract := range []string{`"apiVersion":"dashboard.grafana.app/v2"`, `"elements"`, `"kind":"DatasourceVariable"`, `"pluginId":"prometheus"`, `"name":"$datasource"`, `"kind":"GridLayout"`, `"group":"timeseries"`} {
		if !strings.Contains(compact.String(), contract) {
			t.Errorf("dashboard is missing import contract %s", contract)
		}
	}
}

type fakeLogSource struct {
	entries []LogEntry
}

func (f fakeLogSource) RecentEntries(maxAge time.Duration, maxEntries int) []LogEntry {
	return f.entries
}

func TestLokiHandlerGroupsStreams(t *testing.T) {
	m := newMetrics()
	src := fakeLogSource{entries: []LogEntry{
		{Time: time.Now(), Line: "hello", Labels: map[string]string{"level": "info"}},
		{Time: time.Now(), Line: "world", Labels: map[string]string{"level": "info"}},
		{Time: time.Now(), Line: "boom", Labels: map[string]string{"level": "error"}},
	}}

	handler := m.LokiHandler(src, 100, time.Hour)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
}
