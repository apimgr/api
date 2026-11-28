package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/apimgr/api/src/server/handler"
)

func TestHealthPageRendersMaintenanceGuidance(t *testing.T) {
	cfg := newTestConfig(t)
	since := time.Date(2026, time.January, 2, 3, 4, 5, 0, time.UTC)
	next := since.Add(30 * time.Second)
	previous := handler.MaintenanceState()
	defer handler.SetMaintenanceState(previous)
	handler.SetMaintenanceState(&handler.MaintenanceInfo{
		Reason: "database_connection", Message: "Database connection failed. Verify database availability.", Since: since,
		SelfHealing: handler.SelfHealingInfo{Enabled: true, Attempts: 3, NextAttempt: &next},
	})

	server := NewHTTPServer(cfg)
	req := httptest.NewRequest(http.MethodGet, "/server/healthz", nil)
	req.Header.Set("Accept", "text/html")
	recorder := httptest.NewRecorder()
	server.Handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusServiceUnavailable)
	}
	body := recorder.Body.String()
	for _, expected := range []string{
		"Maintenance recovery",
		"The database is unavailable. Verify database availability, network access, and credentials.",
		"2026-01-02T03:04:05Z",
		"2026-01-02T03:04:35Z",
		">3</dd>",
		"Automatic recovery is retrying.",
	} {
		if !strings.Contains(body, expected) {
			t.Errorf("health page does not contain %q", expected)
		}
	}
}
