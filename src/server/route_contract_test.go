package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/apimgr/api/src/config"
	"github.com/apimgr/api/src/mode"
	"github.com/apimgr/api/src/swagger"
	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

var projectRoutePattern = regexp.MustCompile(`^- (GET|HEAD|POST|PUT|PATCH|DELETE|OPTIONS) (\S+):`)
var markdownRoutePathPattern = regexp.MustCompile("`(/[^`]+)`")
var optionalRouteSegmentPattern = regexp.MustCompile(`\[([^\]]+)\]`)
var routeParameterPattern = regexp.MustCompile(`\{[^{}]+\}`)
var routeMethodPattern = regexp.MustCompile(`^(GET|HEAD|POST|PUT|PATCH|DELETE|OPTIONS)$`)
var aiPartHeadingPattern = regexp.MustCompile(`^# PART ([0-9]+):`)
var configRouteConditionPattern = regexp.MustCompile(`(?i)(server\.[a-z0-9_.]+)\s+is\s+(true|false)`)

func TestAPIRouteContractDrift(t *testing.T) {
	cfg := newTestConfig(t)
	cfg.Server.RateLimit.Read.Requests = 10000
	cfg.Server.Healthz.Root.Enabled = true
	srv := newTestServer(t, cfg)
	router, ok := srv.Handler.(*chi.Mux)
	require.True(t, ok, "server handler should be a chi router")

	want := readIDEAProjectRoutes(t, cfg)
	aiRoutes := readAITemplateRoutes(t, cfg)
	for key := range aiRoutes.routes {
		want[key] = struct{}{}
	}
	gotRouter := make(map[string]struct{})
	err := chi.Walk(router, func(method, path string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		if method == "*" && (path == "/mcp" || path == "/api/mcp" || strings.HasSuffix(path, "/server/mcp")) {
			method = http.MethodPost
		}
		normalized := normalizeContractPath(path, cfg)
		if method != "*" {
			key := routeKey(method, normalized)
			// Browser pages and form fallbacks have their own page-registry
			// conformance checks. Compare every API route so an undocumented
			// router addition cannot disappear from the contract check.
			_, documented := want[key]
			if (documented || isAPIContractPath(normalized)) && RoutePatternMatches(router, method, path) {
				gotRouter[key] = struct{}{}
			}
		}
		return nil
	})
	require.NoError(t, err)
	apiRootKey := routeKey(http.MethodGet, APIBasePath(cfg))
	apiRootRequest := httptest.NewRequest(http.MethodGet, APIBasePath(cfg), nil)
	apiRootRequest.Header.Set("Accept", "application/json")
	apiRootResponse := httptest.NewRecorder()
	router.ServeHTTP(apiRootResponse, apiRootRequest)
	require.Equal(t, http.StatusOK, apiRootResponse.Code)
	autodiscoverRequest := httptest.NewRequest(http.MethodGet, "/api/autodiscover", nil)
	autodiscoverRequest.Header.Set("Accept", "application/json")
	autodiscoverResponse := httptest.NewRecorder()
	router.ServeHTTP(autodiscoverResponse, autodiscoverRequest)
	require.Equal(t, http.StatusOK, autodiscoverResponse.Code)
	require.JSONEq(t, autodiscoverResponse.Body.String(), apiRootResponse.Body.String())
	trailingSlashResponse := httptest.NewRecorder()
	router.ServeHTTP(trailingSlashResponse, httptest.NewRequest(http.MethodGet, APIBasePath(cfg)+"/", nil))
	require.Equal(t, http.StatusMovedPermanently, trailingSlashResponse.Code)
	require.Equal(t, APIBasePath(cfg), trailingSlashResponse.Header().Get("Location"))
	// chi.Walk omits a mounted subrouter's exact prefix route. The direct HTTP
	// assertions above verify this documented root route independently.
	gotRouter[apiRootKey] = struct{}{}
	require.Equal(t, want, gotRouter, "AI.md template routes plus IDEA.md project routes must match the registered routes")

	superset, err := swagger.EmbeddedSuperset(Version, "", strings.TrimPrefix(APIBasePath(cfg), "/api/"))
	require.NoError(t, err)
	gotSuperset := swaggerSpecRouteKeys(superset, cfg)
	wantOpenAPI := make(map[string]struct{}, len(want))
	allRoutes := readIDEAProjectRoutes(t, cfg)
	for key := range aiRoutes.superset {
		allRoutes[key] = struct{}{}
	}
	for key := range allRoutes {
		parts := strings.SplitN(key, " ", 2)
		if len(parts) == 2 && isAPIContractPath(parts[1]) && !aiRoutes.internal[key] {
			wantOpenAPI[key] = struct{}{}
		}
	}
	require.Equal(t, wantOpenAPI, gotSuperset, "public AI.md and IDEA.md API routes must match the unfiltered OpenAPI superset; INTERNAL routes are deliberately excluded")

	req := httptest.NewRequest(http.MethodGet, "/api/swagger", nil)
	rec := httptest.NewRecorder()
	apiSwaggerSpecHandler(cfg)(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	var served swagger.Spec
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &served))
	gotServed := swaggerSpecRouteKeys(&served, cfg)
	wantServed := make(map[string]struct{}, len(want))
	for key := range want {
		parts := strings.SplitN(key, " ", 2)
		if len(parts) == 2 && isAPIContractPath(parts[1]) && !aiRoutes.internal[key] {
			wantServed[key] = struct{}{}
		}
	}
	require.Equal(t, wantServed, gotServed, "served OpenAPI operations must match the enabled route inventory")
}

func swaggerSpecRouteKeys(spec *swagger.Spec, cfg *config.ServerConfig) map[string]struct{} {
	routes := make(map[string]struct{})
	for path, item := range spec.Paths {
		if !isAPIContractPath(path) {
			continue
		}
		for method, present := range map[string]bool{
			http.MethodGet: item.Get != nil, http.MethodHead: item.Head != nil, http.MethodPost: item.Post != nil,
			http.MethodPut: item.Put != nil, http.MethodPatch: item.Patch != nil,
			http.MethodDelete: item.Delete != nil, http.MethodOptions: item.Options != nil,
		} {
			if present {
				routes[routeKey(method, normalizeContractPath(path, cfg))] = struct{}{}
			}
		}
	}
	return routes
}

type aiTemplateRouteInventory struct {
	routes   map[string]struct{}
	superset map[string]struct{}
	internal map[string]bool
}

func readAITemplateRoutes(t *testing.T, cfg *config.ServerConfig) aiTemplateRouteInventory {
	t.Helper()
	contents, err := os.ReadFile("../../AI.md")
	require.NoError(t, err)
	lines := strings.Split(string(contents), "\n")
	inventory := aiTemplateRouteInventory{routes: make(map[string]struct{}), superset: make(map[string]struct{}), internal: make(map[string]bool)}
	inFence := false
	optionalHeadings := make([]bool, 7)
	headingConditions := make([]string, 7)
	currentPart := 0
	for index := 0; index < len(lines); index++ {
		line := strings.TrimSpace(lines[index])
		if strings.HasPrefix(line, "```") {
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}
		if match := aiPartHeadingPattern.FindStringSubmatch(line); match != nil {
			currentPart, _ = strconv.Atoi(match[1])
			for heading := range optionalHeadings {
				optionalHeadings[heading] = false
				headingConditions[heading] = ""
			}
		}
		if level, title, ok := markdownHeading(line); ok {
			optionalHeadings[level] = optionalHeadings[level-1] || strings.Contains(strings.ToUpper(title), "OPTIONAL")
			headingConditions[level] = headingConditions[level-1]
			if strings.Contains(strings.ToLower(title), "--debug") || strings.Contains(strings.ToLower(title), "debug=true") {
				headingConditions[level] = "--debug"
			}
			for deeper := level + 1; deeper < len(optionalHeadings); deeper++ {
				optionalHeadings[deeper] = false
				headingConditions[deeper] = ""
			}
			continue
		}
		if !strings.HasPrefix(line, "|") || index+1 >= len(lines) || isIllustrativeTable(lines, index) {
			continue
		}
		optionalSection := false
		sectionCondition := ""
		for _, markedOptional := range optionalHeadings {
			optionalSection = optionalSection || markedOptional
		}
		for _, condition := range headingConditions {
			if condition != "" {
				sectionCondition = condition
			}
		}
		header := markdownTableCells(line)
		pathColumn, methodColumn, internalColumn, conditionColumn := -1, -1, -1, -1
		for column, name := range header {
			switch strings.TrimSpace(name) {
			case "Endpoint", "Path", "Route":
				pathColumn = column
			case "Method", "Methods":
				methodColumn = column
			case "Internal", "Visibility", "Access", "Classification", "Auth":
				internalColumn = column
			case "x-mounted-when", "Mounted When", "Condition":
				conditionColumn = column
			}
		}
		if pathColumn < 0 || methodColumn < 0 || !isMarkdownTableSeparator(lines[index+1]) {
			continue
		}
		for row := index + 2; row < len(lines); row++ {
			rowLine := strings.TrimSpace(lines[row])
			if !strings.HasPrefix(rowLine, "|") {
				break
			}
			cells := markdownTableCells(rowLine)
			if pathColumn >= len(cells) || methodColumn >= len(cells) {
				continue
			}
			methods := parseContractMethods(cells[methodColumn])
			if len(methods) == 0 {
				continue
			}
			rowCondition := sectionCondition
			if rowCondition == "" {
				rowCondition = inferRouteCondition(cells)
			}
			if optionalSection && conditionColumn < 0 && rowCondition == "" {
				require.FailNow(t, "optional AI.md route table is missing an x-mounted-when/condition column", "line %d: %s", row+1, rowLine)
			}
			conditionEnabled := true
			if optionalSection || rowCondition != "" || conditionColumn >= 0 {
				condition := rowCondition
				if conditionColumn >= 0 && conditionColumn < len(cells) {
					condition = cells[conditionColumn]
				} else if conditionColumn >= 0 {
					require.FailNow(t, "route inventory condition is missing", "line %d: %s", row+1, rowLine)
				}
				var conditionErr error
				conditionEnabled, conditionErr = contractConditionEnabled(condition, cfg)
				require.NoError(t, conditionErr, "unsupported x-mounted-when condition at AI.md line %d", row+1)
			}
			for _, match := range markdownRoutePathPattern.FindAllStringSubmatch(cells[pathColumn], -1) {
				for _, expanded := range expandOptionalRouteSegments(match[1]) {
					if strings.Contains(expanded, "{resource}") {
						continue
					}
					path := normalizeContractPath(expanded, cfg)
					for _, method := range methods {
						key := routeKey(method, path)
						inventory.superset[key] = struct{}{}
						if conditionEnabled {
							inventory.routes[key] = struct{}{}
						}
						if (internalColumn >= 0 && internalColumn < len(cells) && isInternalClassification(cells[internalColumn], strings.EqualFold(header[internalColumn], "Internal"))) || currentPart == 20 {
							inventory.internal[key] = true
						}
					}
				}
			}
		}
		index++
	}
	require.NotEmpty(t, inventory.routes, "AI.md must contain a method-and-path API route inventory")
	return inventory
}

func inferRouteCondition(cells []string) string {
	for _, cell := range cells {
		lower := strings.ToLower(cell)
		if strings.Contains(lower, "--debug") {
			return "--debug"
		}
		if match := configRouteConditionPattern.FindStringSubmatch(lower); len(match) == 3 {
			return strings.ToLower(match[1]) + " is " + strings.ToLower(match[2])
		}
	}
	return ""
}

func markdownHeading(line string) (int, string, bool) {
	level := 0
	for level < len(line) && line[level] == '#' {
		level++
	}
	if level == 0 || level >= 7 || len(line) <= level || line[level] != ' ' {
		return 0, "", false
	}
	return level, strings.TrimSpace(line[level:]), true
}

func isIllustrativeTable(lines []string, tableLine int) bool {
	paragraph := make([]string, 0, 4)
	for index := tableLine - 1; index >= 0; index-- {
		line := strings.TrimSpace(lines[index])
		if line == "" {
			if len(paragraph) > 0 {
				break
			}
			continue
		}
		if strings.HasPrefix(line, "|") || strings.HasPrefix(line, "#") {
			return false
		}
		paragraph = append(paragraph, strings.ToLower(line))
	}
	text := strings.Join(paragraph, " ")
	return (strings.Contains(text, "illustrative") && strings.Contains(text, "example")) ||
		strings.Contains(text, "example table") || strings.Contains(text, "example routes") ||
		strings.Contains(text, "example endpoint table") || strings.Contains(text, "generic rest row")
}

func contractConditionEnabled(condition string, cfg *config.ServerConfig) (bool, error) {
	condition = strings.Trim(strings.TrimSpace(condition), "`*_ ")
	if condition == "--debug" {
		return cfg.Runtime.Debug || mode.ShouldShowDebugEndpoints(), nil
	}
	if strings.EqualFold(condition, "always") || strings.EqualFold(condition, "always mounted") {
		return true, nil
	}
	if strings.EqualFold(condition, "never") || strings.EqualFold(condition, "disabled") {
		return false, nil
	}
	var values map[string]interface{}
	encoded, err := yaml.Marshal(cfg)
	if err != nil || yaml.Unmarshal(encoded, &values) != nil {
		return false, fmt.Errorf("cannot read server config for condition %q", condition)
	}
	for _, clause := range strings.Split(condition, " AND ") {
		clause = strings.Trim(strings.TrimSpace(clause), "`*_ ")
		expected := true
		for _, suffix := range []string{" is true", "=true", ": true"} {
			if strings.HasSuffix(strings.ToLower(clause), suffix) {
				clause = strings.TrimSpace(clause[:len(clause)-len(suffix)])
				break
			}
		}
		if strings.HasSuffix(strings.ToLower(clause), " is false") {
			expected = false
			clause = strings.TrimSpace(clause[:len(clause)-len(" is false")])
		}
		current, ok := nestedConfigValue(values, strings.Split(clause, "."))
		actual, boolValue := current.(bool)
		if !ok || !boolValue {
			return false, fmt.Errorf("condition %q is not a supported boolean config expression", clause)
		}
		if actual != expected {
			return false, nil
		}
	}
	return true, nil
}

func TestContractConditionEnabled(t *testing.T) {
	cfg := newTestConfig(t)
	cfg.Server.Healthz.Root.Enabled = true
	enabled, err := contractConditionEnabled("server.healthz.root.enabled is true", cfg)
	require.NoError(t, err)
	require.True(t, enabled)
	enabled, err = contractConditionEnabled("server.healthz.root.enabled is false", cfg)
	require.NoError(t, err)
	require.False(t, enabled)
	_, err = contractConditionEnabled("unknown.setting is true", cfg)
	require.Error(t, err)
}

func nestedConfigValue(values interface{}, path []string) (interface{}, bool) {
	if len(path) == 0 {
		return values, true
	}
	object, ok := values.(map[string]interface{})
	if !ok {
		return nil, false
	}
	value, exists := object[path[0]]
	if !exists {
		return nil, false
	}
	return nestedConfigValue(value, path[1:])
}

func isInternalClassification(value string, booleanInternalColumn bool) bool {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.Trim(value, "`*_ ")
	return value == "internal" || strings.HasPrefix(value, "internal ") || value == "machine-only" ||
		(booleanInternalColumn && (value == "yes" || value == "true"))
}

func markdownTableCells(line string) []string {
	line = strings.TrimSpace(line)
	line = strings.TrimPrefix(line, "|")
	line = strings.TrimSuffix(line, "|")
	cells := strings.Split(line, "|")
	for index := range cells {
		cells[index] = strings.TrimSpace(cells[index])
	}
	return cells
}

func isMarkdownTableSeparator(line string) bool {
	if !strings.HasPrefix(strings.TrimSpace(line), "|") {
		return false
	}
	for _, cell := range markdownTableCells(line) {
		if strings.Trim(cell, ":- ") != "" {
			return false
		}
	}
	return true
}

func parseContractMethods(cell string) []string {
	methods := make([]string, 0, 2)
	for _, value := range strings.FieldsFunc(strings.ToUpper(cell), func(character rune) bool {
		return character == '/' || character == ',' || character == ' ' || character == '`'
	}) {
		if routeMethodPattern.MatchString(value) {
			methods = append(methods, value)
		}
	}
	return methods
}

func expandOptionalRouteSegments(path string) []string {
	match := optionalRouteSegmentPattern.FindStringSubmatchIndex(path)
	if match == nil {
		return []string{path}
	}
	without := path[:match[0]] + path[match[1]:]
	with := path[:match[0]] + path[match[2]:match[3]] + path[match[1]:]
	expanded := expandOptionalRouteSegments(with)
	return append([]string{without}, expanded...)
}

func normalizeContractPath(path string, cfg *config.ServerConfig) string {
	path = strings.ReplaceAll(path, "{api_version}", strings.TrimPrefix(APIBasePath(cfg), "/api/"))
	if len(path) > 1 {
		path = strings.TrimSuffix(path, "/")
	}
	if strings.HasSuffix(path, "/*") {
		path = strings.TrimSuffix(path, "/*") + "/{param}"
	}
	return routeParameterPattern.ReplaceAllString(path, "{param}")
}

func isAPIContractPath(path string) bool {
	return path == "/api/autodiscover" || strings.HasPrefix(path, "/api/")
}

func TestOwnerTokenWebRoutesAreMounted(t *testing.T) {
	srv := newTestServer(t, newTestConfig(t))
	router, ok := srv.Handler.(*chi.Mux)
	require.True(t, ok, "server handler should be a chi router")

	want := map[string]struct{}{
		routeKey(http.MethodGet, "/server/preferences/tokens"):              {},
		routeKey(http.MethodPost, "/server/preferences/token/reveal"):       {},
		routeKey(http.MethodPost, "/server/preferences/token/rotate"):       {},
		routeKey(http.MethodPost, "/server/preferences/token/revoke"):       {},
		routeKey(http.MethodPost, "/server/preferences/token/restore"):      {},
		routeKey(http.MethodPost, "/server/preferences/token/create"):       {},
		routeKey(http.MethodPost, "/server/preferences/token/forget"):       {},
		routeKey(http.MethodPost, "/server/preferences/tokens"):             {},
		routeKey(http.MethodPost, "/server/preferences/tokens/{id}/reveal"): {},
		routeKey(http.MethodPost, "/server/preferences/tokens/{id}/rotate"): {},
		routeKey(http.MethodPost, "/server/preferences/tokens/{id}/update"): {},
		routeKey(http.MethodPost, "/server/preferences/tokens/{id}/revoke"): {},
	}
	got := make(map[string]struct{})
	err := chi.Walk(router, func(method, path string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		key := routeKey(method, path)
		if _, ok := want[key]; ok {
			got[key] = struct{}{}
		}
		return nil
	})
	require.NoError(t, err)
	require.Equal(t, want, got)
}

func readIDEAProjectRoutes(t *testing.T, cfg *config.ServerConfig) map[string]struct{} {
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
			if !strings.HasPrefix(match[2], "/api/") {
				continue
			}
			routes[routeKey(match[1], normalizeContractPath(match[2], cfg))] = struct{}{}
		}
	}
	require.NotEmpty(t, routes, "IDEA.md must contain project API routes")
	return routes
}

func routeKey(method, path string) string {
	return strings.ToUpper(method) + " " + routeParameterPattern.ReplaceAllString(path, "{param}")
}
