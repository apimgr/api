package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/apimgr/api/src/config"
	"github.com/apimgr/api/src/paths"
	"github.com/apimgr/api/src/server/handler"
)

func TestMaintenanceMiddlewareAllowsReadsAndRejectsWrites(t *testing.T) {
	previous := handler.MaintenanceState()
	defer handler.SetMaintenanceState(previous)
	handler.SetMaintenanceState(&handler.MaintenanceInfo{
		Reason: "database_connection", Message: "Database connection failed", Since: time.Now().UTC(),
		SelfHealing: handler.SelfHealingInfo{Enabled: true},
	})
	cfg := config.DefaultConfig()
	passed := false
	middleware := MaintenanceMiddleware(cfg)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		passed = true
		w.WriteHeader(http.StatusNoContent)
	}))

	read := httptest.NewRecorder()
	middleware.ServeHTTP(read, httptest.NewRequest(http.MethodGet, "/server/healthz", nil))
	if read.Code != http.StatusNoContent || !passed {
		t.Fatalf("GET health route = %d, passed=%t; want 204 and passed", read.Code, passed)
	}

	passed = false
	write := httptest.NewRecorder()
	middleware.ServeHTTP(write, httptest.NewRequest(http.MethodPost, "/api/v1/resources", nil))
	if write.Code != http.StatusServiceUnavailable || passed {
		t.Fatalf("POST route = %d, passed=%t; want 503 and blocked", write.Code, passed)
	}
	if write.Header().Get("Retry-After") != "30" || write.Header().Get("X-Maintenance-Mode") != "true" || write.Header().Get("X-Maintenance-Reason") != "database_connection" {
		t.Fatalf("maintenance headers = %v", write.Header())
	}
	var payload struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
	}
	if err := json.Unmarshal(write.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode maintenance response: %v", err)
	}
	if payload.OK || payload.Error != "MAINTENANCE" {
		t.Fatalf("maintenance response = %#v", payload)
	}
}

func TestMaintenanceRetryRunsDatabaseRecoveryForWriteFailuresAndStopsOnCancellation(t *testing.T) {
	cfg := config.DefaultConfig()
	previous := handler.MaintenanceState()
	defer handler.SetMaintenanceState(previous)
	calls := 0
	ctx, cancel := context.WithCancel(context.Background())
	monitor := &MaintenanceMonitor{
		cfg: cfg,
		recover: func(context.Context) error {
			calls++
			cancel()
			return nil
		},
		info: &handler.MaintenanceInfo{
			Reason: "database_write", Message: "database write failed", Since: time.Now().UTC(),
			SelfHealing: handler.SelfHealingInfo{Enabled: true},
		},
	}

	monitor.retry(ctx)

	if calls != 1 {
		t.Fatalf("database recovery calls = %d, want one before cancellation", calls)
	}
	if monitor.info == nil {
		t.Fatal("maintenance state was cleared before recovery health checks")
	}
	if monitor.info.SelfHealing.Attempts != 1 {
		t.Fatalf("attempts = %d, want 1", monitor.info.SelfHealing.Attempts)
	}
}

func TestMaintenanceCleanupPreservesAuditLogRotationRetention(t *testing.T) {
	logDir := t.TempDir()
	previousLogDir := paths.LogDir()
	paths.SetApplicationPathOverrides("", "", logDir)
	t.Cleanup(func() { paths.SetApplicationPathOverrides("", "", previousLogDir) })

	cfg := config.DefaultConfig()
	cfg.Server.Logs.Audit.Filename = "custom-audit.jsonl"
	monitor := &MaintenanceMonitor{cfg: cfg}
	for _, name := range []string{"custom-audit.jsonl.2025-01-01", "custom-audit.jsonl.2025-01-01.gz", "old-server.log"} {
		if err := os.WriteFile(filepath.Join(logDir, name), []byte("old"), 0600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
		old := time.Now().Add(-30 * 24 * time.Hour)
		if err := os.Chtimes(filepath.Join(logDir, name), old, old); err != nil {
			t.Fatalf("age %s: %v", name, err)
		}
	}
	if err := monitor.cleanupIfNeeded(); err != nil {
		t.Fatalf("cleanup: %v", err)
	}
	for _, name := range []string{"custom-audit.jsonl.2025-01-01", "custom-audit.jsonl.2025-01-01.gz"} {
		if _, err := os.Stat(filepath.Join(logDir, name)); err != nil {
			t.Errorf("audit retention file %s was removed: %v", name, err)
		}
	}
	if _, err := os.Stat(filepath.Join(logDir, "old-server.log")); !os.IsNotExist(err) {
		t.Errorf("old non-audit log remains, stat error = %v", err)
	}
}
