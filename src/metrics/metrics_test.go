package metrics

import (
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGet_ReturnsSingleton(t *testing.T) {
	m1 := Get()
	m2 := Get()
	assert.Same(t, m1, m2)
}

func TestNewMetrics_RegistersCollectors(t *testing.T) {
	m := newMetrics()
	require.NotNil(t, m)
	require.NotNil(t, m.registry)
	assert.False(t, m.startTime.IsZero())

	// A freshly-created registry with the collectors registered must
	// gather without error.
	families, err := m.registry.Gather()
	require.NoError(t, err)
	assert.NotEmpty(t, families)
}

func TestSetBuildInfo(t *testing.T) {
	m := newMetrics()
	m.SetBuildInfo("1.2.3", "abcdef", "2026-01-01")

	value := testutil.ToFloat64(m.appInfo.WithLabelValues("1.2.3", "abcdef", "2026-01-01", runtime.Version()))
	assert.Equal(t, float64(1), value)
}

func TestRecordRequest(t *testing.T) {
	m := newMetrics()

	m.RecordRequest("GET", "/health", "200", 15*time.Millisecond, 128, 256)

	count := testutil.ToFloat64(m.httpRequestsTotal.WithLabelValues("GET", "/health", "200"))
	assert.Equal(t, float64(1), count)

	// Recording again for the same labels increments the counter.
	m.RecordRequest("GET", "/health", "200", 20*time.Millisecond, 64, 128)
	count = testutil.ToFloat64(m.httpRequestsTotal.WithLabelValues("GET", "/health", "200"))
	assert.Equal(t, float64(2), count)

	// A different status label is tracked independently.
	m.RecordRequest("GET", "/health", "500", 5*time.Millisecond, 10, 20)
	errCount := testutil.ToFloat64(m.httpRequestsTotal.WithLabelValues("GET", "/health", "500"))
	assert.Equal(t, float64(1), errCount)
}

func TestActiveRequests(t *testing.T) {
	m := newMetrics()

	assert.Equal(t, float64(0), testutil.ToFloat64(m.httpActiveRequests))

	m.IncActiveRequests()
	m.IncActiveRequests()
	assert.Equal(t, float64(2), testutil.ToFloat64(m.httpActiveRequests))

	m.DecActiveRequests()
	assert.Equal(t, float64(1), testutil.ToFloat64(m.httpActiveRequests))
}

func TestServePrometheus(t *testing.T) {
	m := newMetrics()
	m.SetBuildInfo("9.9.9", "deadbeef", "2026-07-20")
	m.RecordRequest("POST", "/api/thing", "201", time.Millisecond, 1, 2)

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rec := httptest.NewRecorder()

	m.ServePrometheus(rec, req)

	resp := rec.Result()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	body := rec.Body.String()
	assert.True(t, strings.Contains(body, "api_http_requests_total"))
	assert.True(t, strings.Contains(body, "api_app_info"))
	assert.True(t, strings.Contains(body, "api_app_uptime_seconds"))
}
