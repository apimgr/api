package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/apimgr/api/src/config"
	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLLMSHandlerDiscoversPublicAPIAndResolvesCurrentHost(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Server.Port = ""
	cfg.Server.Branding.Title = "Example API"
	cfg.Server.Branding.Description = "Utilities for developers"
	cfg.Server.Contact.General.Email = "api@example.test"
	cfg.Server.Contact.Security.Email = "security@example.test"
	routes := chi.NewRouter()
	routes.Get("/api/v1/weather", func(w http.ResponseWriter, _ *http.Request) {})
	routes.Get("/api/v1/server/metrics", func(w http.ResponseWriter, _ *http.Request) {})
	routes.Get("/api/v1/admin/secret", func(w http.ResponseWriter, _ *http.Request) {})

	req := httptest.NewRequest(http.MethodGet, "http://site.example/.well-known/llms.txt", nil)
	rec := httptest.NewRecorder()
	llmsHandler(cfg, routes)(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "text/plain; charset=utf-8", rec.Header().Get("Content-Type"))
	assert.Contains(t, rec.Body.String(), "# Example API")
	assert.Contains(t, rec.Body.String(), "Base URL: http://site.example/api/v1")
	assert.Contains(t, rec.Body.String(), "Authentication: The REST API has public endpoints and does not require credentials.")
	assert.Contains(t, rec.Body.String(), "Rate limit:")
	assert.Contains(t, rec.Body.String(), "MCP: http://site.example/mcp (Streamable HTTP; Bearer token required)")
	assert.Contains(t, rec.Body.String(), "- GET /api/v1/weather")
	assert.Contains(t, rec.Body.String(), "Text response: default message=Retrieved successfully")
	assert.Contains(t, rec.Body.String(), "- POST http://site.example/api/v1/server/mcp")
	assert.NotContains(t, rec.Body.String(), "/api/v1/server/metrics")
	assert.NotContains(t, rec.Body.String(), "/api/v1/admin/secret")
	assert.Contains(t, rec.Body.String(), "Security: security@example.test")
	assert.Contains(t, rec.Body.String(), "API issues: api@example.test")
	assert.True(t, strings.HasSuffix(rec.Body.String(), "\n"))
}

func TestLLMSHandlerHeadHasNoBody(t *testing.T) {
	cfg := config.DefaultConfig()
	req := httptest.NewRequest(http.MethodHead, "http://site.example/llms.txt", nil)
	rec := httptest.NewRecorder()
	llmsHandler(cfg, chi.NewRouter())(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Empty(t, rec.Body.String())
}

func TestLLMSHandlerIncludesOpenAPISchemasWhenConfigured(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Server.Port = ""
	cfg.Web.LLMS.IncludeSchemas = true
	routes := chi.NewRouter()
	routes.Get("/api/v1/text/uuid", func(w http.ResponseWriter, _ *http.Request) {})

	rec := httptest.NewRecorder()
	llmsHandler(cfg, routes)(rec, httptest.NewRequest(http.MethodGet, "http://site.example/llms.txt", nil))

	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "Schema: ")
	assert.Contains(t, rec.Body.String(), "Generate UUID")
	assert.Contains(t, rec.Body.String(), `"x-text-message":"Retrieved successfully"`)
}

func TestLLMSHandlerCanOmitEndpoints(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Server.Port = ""
	cfg.Web.LLMS.IncludeEndpoints = false
	routes := chi.NewRouter()
	routes.Get("/api/v1/text/uuid", func(w http.ResponseWriter, _ *http.Request) {})

	rec := httptest.NewRecorder()
	llmsHandler(cfg, routes)(rec, httptest.NewRequest(http.MethodGet, "http://site.example/llms.txt", nil))

	require.Equal(t, http.StatusOK, rec.Code)
	assert.NotContains(t, rec.Body.String(), "## Endpoints")
	assert.Contains(t, rec.Body.String(), "MCP: http://site.example/mcp")
}
