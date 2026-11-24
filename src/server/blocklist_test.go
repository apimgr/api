package server

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/apimgr/api/src/config"
	"github.com/apimgr/api/src/securitydata"
)

func TestBlocklistMiddlewareRejectsListedClientIPBeforeHandler(t *testing.T) {
	cfg := config.DefaultConfig()
	dir := t.TempDir()
	cfg.Data["security_dir"] = dir
	cfg.Server.Security.Blocklists.Enabled = true
	cfg.Server.Security.Blocklists.ExtraSources = []config.BlocklistSource{{ID: "listed_domains", Type: "domain", URL: "https://example.test/list"}}
	blocklistDir := filepath.Join(dir, "blocklists")
	if err := os.MkdirAll(blocklistDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(blocklistDir, "firehol_level1.txt"), []byte("198.51.100.7\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(blocklistDir, "listed_domains.txt"), []byte("blocked.example\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(blocklistDir, ".last_updated"), []byte(time.Now().UTC().Format(time.RFC3339)+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	original := config.Get()
	config.Set(cfg)
	t.Cleanup(func() { config.Set(original); securitydata.DefaultBlocklists.Reload(original, time.Now().UTC()) })
	called := false
	handler := blocklistMiddleware(cfg)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true }))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "198.51.100.7:1234"
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, req)
	if response.Code != http.StatusForbidden {
		t.Fatalf("status=%d body=%q", response.Code, response.Body.String())
	}
	if called {
		t.Fatal("handler ran for blocked client")
	}
}

func TestAllowlistMiddlewareMarksConfiguredClient(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Server.Security.Allowlist = []config.AllowlistEntry{{CIDR: "198.51.100.7/32"}}
	original := config.Get()
	config.Set(cfg)
	t.Cleanup(func() { config.Set(original) })
	handler := allowlistMiddleware(cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !isAllowlistedRequest(r) {
			t.Error("allowlisted address missing request marker")
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "198.51.100.7:1234"
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, req)
	if response.Code != http.StatusNoContent {
		t.Fatalf("status=%d body=%q", response.Code, response.Body.String())
	}
}

func TestBlocklistMiddlewareRejectsOriginAndSubmittedEmailDomain(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Server.Security.Blocklists.Enabled = true
	originalConfig := config.Get()
	originalManager := securitydata.DefaultBlocklists
	config.Set(cfg)
	securitydata.DefaultBlocklists = &securitydata.BlocklistManager{}
	t.Cleanup(func() { config.Set(originalConfig); securitydata.DefaultBlocklists = originalManager })
	// Install parsed list state directly through a temporary source file.
	dir := t.TempDir()
	cfg.Data["security_dir"] = dir
	cfg.Server.Security.Blocklists.ExtraSources = []config.BlocklistSource{{ID: "domains", Type: "domain", URL: "https://example.test/domains"}}
	blocklistDir := filepath.Join(dir, "blocklists")
	if err := os.MkdirAll(blocklistDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(blocklistDir, "domains.txt"), []byte("blocked.example\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(blocklistDir, ".last_updated"), []byte(time.Now().UTC().Format(time.RFC3339)+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	securitydata.DefaultBlocklists.Reload(cfg, time.Now().UTC())
	for _, test := range []struct{ name, origin, body string }{
		{"origin", "https://blocked.example/page", ""},
		{"email", "", `{"email":"user@blocked.example"}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "/contact", strings.NewReader(test.body))
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set("Origin", test.origin)
			response := httptest.NewRecorder()
			blocklistMiddleware(cfg)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})).ServeHTTP(response, request)
			if response.Code != http.StatusForbidden {
				t.Fatalf("status=%d body=%q", response.Code, response.Body.String())
			}
		})
	}
}
