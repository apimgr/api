package server

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/apimgr/api/src/server/handler"
	"github.com/stretchr/testify/require"
)

func TestHealthStatsMiddlewareExcludesHealthAndMetricsRoutes(t *testing.T) {
	paths := []string{
		"/server/healthz",
		"/healthz",
		"/api/v1/server/healthz",
		"/api/healthz",
		"/server/metrics",
		"/api/v1/server/metrics/prometheus",
		"/metrics",
		"//server//healthz",
		"/server/metrics//prometheus",
	}
	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			handler.ResetRequestStats()
			wrapped := healthStatsMiddleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
			wrapped.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, path, nil))
			total, _, active := handler.RequestStatsSnapshot()
			require.Zero(t, total)
			require.Zero(t, active)
		})
	}
}

func TestHealthStatsMiddlewareCountsOrdinaryRequests(t *testing.T) {
	handler.ResetRequestStats()
	wrapped := healthStatsMiddleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	wrapped.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/server/about", nil))
	total, recent, active := handler.RequestStatsSnapshot()
	require.EqualValues(t, 1, total)
	require.EqualValues(t, 1, recent)
	require.Zero(t, active)
}
