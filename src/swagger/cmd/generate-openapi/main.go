package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/apimgr/api/src/config"
	"github.com/apimgr/api/src/mode"
	"github.com/apimgr/api/src/server"
	"github.com/apimgr/api/src/swagger"
	"github.com/go-chi/chi/v5"
)

func main() {
	root := flag.String("root", ".", "project root containing the server package")
	flag.Parse()
	if err := generate(*root); err != nil {
		fmt.Fprintln(os.Stderr, "generate-openapi:", err)
		os.Exit(1)
	}
}

func generate(root string) error {
	cfg := config.DefaultConfig()
	if cfg == nil {
		return fmt.Errorf("default server configuration is unavailable")
	}
	// Build the superset by enabling every route gate represented in the
	// server router. Conditions remain attached so serving can filter per run.
	cfg.Server.Healthz.Root.Enabled = true
	cfg.Server.Metrics.Enabled = true
	cfg.Server.Metrics.Root.Enabled = true
	cfg.Server.Debug.RuntimeEndpoints = true
	cfg.Server.Debug.Pprof = true
	previousDebug := mode.IsDebugEnabled()
	mode.SetDebugEnabled(true)
	defer mode.SetDebugEnabled(previousDebug)

	httpServer := server.NewHTTPServer(cfg)
	router, ok := httpServer.Handler.(chi.Router)
	if !ok {
		return fmt.Errorf("server handler %T does not expose chi routes", httpServer.Handler)
	}
	apiPrefix := server.APIBasePath(cfg)
	routes := make([]swagger.Route, 0)
	err := chi.Walk(router, func(method, path string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		if !strings.HasPrefix(path, "/api/") && path != "/healthz" && path != "/mcp" {
			return nil
		}
		if method == "*" && (path == "/mcp" || path == "/api/mcp" || strings.HasSuffix(path, "/server/mcp")) {
			method = http.MethodPost
		}
		if !server.RoutePatternMatches(router, method, path) {
			return nil
		}
		if strings.HasPrefix(path, apiPrefix+"/") {
			path = "/api/{api_version}" + strings.TrimPrefix(path, apiPrefix)
		}
		route := server.BuildAPIRouteMetadata(method, path)
		routes = append(routes, route)
		return nil
	})
	if err != nil {
		return fmt.Errorf("walk registered server routes: %w", err)
	}
	apiRoot := swagger.Route{Method: http.MethodGet, Path: "/api/{api_version}"}
	apiRootRegistered := false
	for _, route := range routes {
		if route.Method == apiRoot.Method && route.Path == apiRoot.Path {
			apiRootRegistered = true
			break
		}
	}
	if !apiRootRegistered {
		routes = append(routes, server.BuildAPIRouteMetadata(apiRoot.Method, apiRoot.Path))
	}
	if len(routes) == 0 {
		return fmt.Errorf("server router registered no API routes")
	}
	spec := swagger.GenerateSupersetSpec("generated", "", "{api_version}", routes)
	output, err := json.MarshalIndent(spec, "", "  ")
	if err != nil {
		return fmt.Errorf("encode generated OpenAPI superset: %w", err)
	}
	output = append(output, '\n')
	path := filepath.Join(root, "src", "swagger", "openapi_superset.json")
	if err := os.WriteFile(path, output, 0644); err != nil {
		return fmt.Errorf("write generated OpenAPI superset: %w", err)
	}
	return nil
}
