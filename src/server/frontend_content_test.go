package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/apimgr/api/src/database"
	"github.com/apimgr/api/src/paths"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFrontendNegotiationAcrossRegisteredPages(t *testing.T) {
	cfg := newTestConfig(t)
	cfg.Server.RateLimit.Read.Requests = 100000
	cfg.Server.RateLimit.GlobalBurst = 100000
	cfg.Server.Healthz.Root.Enabled = true
	require.NoError(t, database.Init(cfg.Server.Database, paths.DataDir()))
	t.Cleanup(func() { _ = database.Close() })
	require.NoError(t, database.EnsureAppSecrets(t.Context()))
	require.NoError(t, database.EnsureCaptchaSchema(t.Context()))
	require.NoError(t, database.EnsureRateLimitSchema(t.Context()))
	srv := newTestServer(t, cfg)

	pagePaths := []string{
		"/", "/text", "/crypto", "/datetime", "/categories",
		"/server/about", "/server/privacy", "/server/dpo", "/server/contact", "/server/help",
		"/server/terms", "/server/security",
		"/server/security/policy", "/server/security/thanks",
		"/server/preferences",
		"/server/docs/swagger", "/server/docs/graphql", "/server/healthz", "/healthz",
	}
	registry := registeredServerPages("en")
	for _, page := range append(registry.Help, registry.Preferences...) {
		pagePaths = append(pagePaths, page.Path)
	}
	for _, category := range allCategories() {
		pagePaths = append(pagePaths, category.Path)
	}
	for _, page := range toolPages() {
		pagePaths = append(pagePaths, "/"+page.category+"/"+page.tool)
	}

	for _, path := range pagePaths {
		t.Run(path, func(t *testing.T) {
			browser := serveFrontendRequest(srv.Handler, path, "Mozilla/5.0", "text/html")
			require.Equal(t, http.StatusOK, browser.Code)
			assert.Contains(t, browser.Header().Get("Content-Type"), "text/html")
			assert.Contains(t, browser.Body.String(), "<html")

			jsonResponse := serveFrontendRequest(srv.Handler, path, "Mozilla/5.0", "application/json")
			assert.Contains(t, jsonResponse.Header().Get("Content-Type"), "application/json", "frontend JSON negotiation for %s", path)
			assert.NotContains(t, strings.ToLower(jsonResponse.Body.String()), "<!doctype html")

			plain := serveFrontendRequest(srv.Handler, path, "Mozilla/5.0", "text/plain")
			require.Equal(t, http.StatusOK, plain.Code)
			assert.Contains(t, plain.Header().Get("Content-Type"), "text/plain")
			assert.NotContains(t, strings.ToLower(plain.Body.String()), "<!doctype html")
			assert.True(t, strings.HasSuffix(plain.Body.String(), "\n"))

			curl := serveFrontendRequest(srv.Handler, path, "curl/8.0", "")
			require.Equal(t, http.StatusOK, curl.Code)
			assert.Contains(t, curl.Header().Get("Content-Type"), "text/plain")
			assert.NotContains(t, strings.ToLower(curl.Body.String()), "<!doctype html")

			textBrowser := serveFrontendRequest(srv.Handler, path, "Lynx/2.9", "text/html")
			require.Equal(t, http.StatusOK, textBrowser.Code)
			assert.Contains(t, textBrowser.Header().Get("Content-Type"), "text/html")
			assert.NotContains(t, strings.ToLower(textBrowser.Body.String()), "<script")
		})
	}
}

func serveFrontendRequest(handler http.Handler, path, userAgent, accept string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Header.Set("User-Agent", userAgent)
	if accept != "" {
		req.Header.Set("Accept", accept)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, req)
	return response
}

func TestFrontendTextNegotiationHonorsExplicitHTMLPrecedence(t *testing.T) {
	handler := frontendTextMiddleware("api")(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte("<html><body><h1>Title</h1></body></html>"))
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Accept", "text/html")
	req.Header.Set("User-Agent", "curl/8.0")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, req)
	assert.Contains(t, response.Header().Get("Content-Type"), "text/html")
	assert.Contains(t, response.Body.String(), "<html>")
}

func TestFrontendJSONNegotiationPreservesRootMetricsAliases(t *testing.T) {
	for _, path := range []string{"/metrics", "/metrics/prometheus"} {
		t.Run(path, func(t *testing.T) {
			cfg := newTestConfig(t)
			cfg.Server.Metrics.Root.Enabled = true
			var gotPath string
			handler := frontendJSONMiddleware(cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotPath = r.URL.Path
				w.WriteHeader(http.StatusNoContent)
			}))

			request := httptest.NewRequest(http.MethodGet, path, nil)
			request.Header.Set("Accept", "application/json")
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)

			assert.Equal(t, http.StatusNoContent, response.Code)
			assert.Equal(t, path, gotPath)
		})
	}
}

func TestFrontendJSONNegotiationPreservesRootHealthAlias(t *testing.T) {
	cfg := newTestConfig(t)
	cfg.Server.Healthz.Root.Enabled = true
	var gotPath string
	handler := frontendJSONMiddleware(cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusNoContent)
	}))

	request := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	request.Header.Set("Accept", "application/json")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	assert.Equal(t, http.StatusNoContent, response.Code)
	assert.Equal(t, "/healthz", gotPath)
}

func TestFrontendJSONNegotiationPreservesLocaleAssetPath(t *testing.T) {
	var gotPath string
	handler := frontendJSONMiddleware(newTestConfig(t))(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
	}))

	request := httptest.NewRequest(http.MethodGet, "/locales/en.json", nil)
	request.Header.Set("Accept", "application/json")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	assert.Equal(t, http.StatusOK, response.Code)
	assert.Equal(t, "/locales/en.json", gotPath)
	assert.Equal(t, "application/json", response.Header().Get("Content-Type"))
}
