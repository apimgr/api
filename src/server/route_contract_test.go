package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/apimgr/api/src/config"
	"github.com/apimgr/api/src/swagger"
	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"
)

var projectRoutePattern = regexp.MustCompile(`^- (GET|POST|PUT|PATCH|DELETE|OPTIONS) (\S+):`)

func TestProjectAPIRouteContract(t *testing.T) {
	cfg := newTestConfig(t)
	cfg.Server.RateLimit.Read.Requests = 10000
	srv := newTestServer(t, cfg)
	router, ok := srv.Handler.(*chi.Mux)
	require.True(t, ok, "server handler should be a chi router")

	want := readIDEAProjectRoutes(t)
	gotRouter := make(map[string]struct{})
	err := chi.Walk(router, func(method, path string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		if isProjectRoute(path, APIBasePath(cfg)) {
			gotRouter[routeKey(method, ideaRoutePath(path, cfg))] = struct{}{}
		}
		return nil
	})
	require.NoError(t, err)
	require.Equal(t, want, gotRouter, "IDEA.md API routes must match the registered project routes")

	req := httptest.NewRequest(http.MethodGet, "/api/swagger", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	var spec swagger.Spec
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &spec))
	gotSpec := make(map[string]struct{})
	for path, item := range spec.Paths {
		if !isProjectRoute(path, APIBasePath(cfg)) {
			continue
		}
		for method, present := range map[string]bool{
			http.MethodGet: item.Get != nil, http.MethodPost: item.Post != nil,
			http.MethodPut: item.Put != nil, http.MethodPatch: item.Patch != nil,
			http.MethodDelete: item.Delete != nil, http.MethodOptions: item.Options != nil,
		} {
			if present {
				gotSpec[routeKey(method, ideaRoutePath(path, cfg))] = struct{}{}
			}
		}
	}
	require.Equal(t, want, gotSpec, "OpenAPI operations must match IDEA.md project routes")
}

func readIDEAProjectRoutes(t *testing.T) map[string]struct{} {
	t.Helper()
	contents, err := os.ReadFile("../../IDEA.md")
	require.NoError(t, err)
	lines := strings.Split(string(contents), "\n")
	inRoutes := false
	routes := make(map[string]struct{})
	for _, line := range lines {
		if strings.TrimSpace(line) == "### API routes" {
			inRoutes = true
			continue
		}
		if inRoutes && strings.HasPrefix(line, "### ") {
			break
		}
		if !inRoutes {
			continue
		}
		match := projectRoutePattern.FindStringSubmatch(line)
		if match != nil {
			routes[routeKey(match[1], match[2])] = struct{}{}
		}
	}
	require.NotEmpty(t, routes, "IDEA.md must contain project API routes")
	return routes
}

func routeKey(method, path string) string {
	return strings.ToUpper(method) + " " + path
}

func isProjectRoute(path, apiBase string) bool {
	return strings.HasPrefix(path, apiBase+"/") &&
		!strings.HasPrefix(path, apiBase+"/server/") && path != apiBase+"/version"
}

func ideaRoutePath(path string, cfg *config.Config) string {
	path = strings.Replace(path, APIBasePath(cfg)+"/", "/api/{api_version}/", 1)
	return strings.ReplaceAll(path, "/*", "/{path}")
}
