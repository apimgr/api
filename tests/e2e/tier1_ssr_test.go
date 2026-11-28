//go:build e2e

package e2e

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	stdhtml "html"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/apimgr/api/src/config"
	"github.com/apimgr/api/src/database"
)

// category couples a IDEA.md tool category with the frontend route that lists
// it and a marker that must appear in the server-rendered response.
type category struct {
	path    string
	heading string
}

// toolPage couples a registered frontend route with its rendered title.
type toolPage struct {
	path  string
	title string
}

var (
	inventoryOnce     sync.Once
	categoryInventory []category
	toolInventory     []toolPage
	inventoryError    error
	anchorPattern     = regexp.MustCompile(`(?is)<a\b([^>]*)>(.*?)</a>`)
	attributePattern  = regexp.MustCompile(`(?i)([a-z][a-z0-9_-]*)\s*=\s*"([^"]*)"`)
	h1Pattern         = regexp.MustCompile(`(?is)<h1\b[^>]*>(.*?)</h1>`)
	tagPattern        = regexp.MustCompile(`(?s)<[^>]*>`)
)

type inventoryLink struct{ href, class string }

// registeredFeatureInventory follows links rendered by the server's registered
// page catalog instead of maintaining a second list of category and tool routes.
func registeredFeatureInventory() ([]category, []toolPage, error) {
	inventoryOnce.Do(func() {
		categoryHTML, err := inventoryPage("/categories")
		if err != nil {
			inventoryError = err
			return
		}
		for _, link := range inventoryLinks(categoryHTML) {
			if !hasClass(link.class, "category-card") {
				continue
			}
			parsed, err := url.Parse(link.href)
			if err != nil || !strings.HasPrefix(parsed.Path, "/") || strings.Count(strings.Trim(parsed.Path, "/"), "/") != 0 {
				continue
			}
			page, err := inventoryPage(parsed.Path)
			if err != nil {
				inventoryError = err
				return
			}
			categoryInventory = append(categoryInventory, category{path: parsed.Path, heading: inventoryHeading(page)})
		}
		sort.Slice(categoryInventory, func(i, j int) bool { return categoryInventory[i].path < categoryInventory[j].path })
		for _, c := range categoryInventory {
			page, err := inventoryPage(c.path)
			if err != nil {
				inventoryError = err
				return
			}
			for _, link := range inventoryLinks(page) {
				if !hasClass(link.class, "category-card") && !hasClass(link.class, "tool-card-link") {
					continue
				}
				parsed, err := url.Parse(link.href)
				if err != nil {
					continue
				}
				path := strings.TrimSuffix(parsed.Path, "/")
				prefix := strings.TrimSuffix(c.path, "/") + "/"
				if !strings.HasPrefix(path, prefix) || strings.Contains(strings.TrimPrefix(path, prefix), "/") {
					continue
				}
				toolHTML, err := inventoryPage(path)
				if err != nil {
					inventoryError = err
					return
				}
				title := inventoryHeading(toolHTML)
				if title == "" {
					inventoryError = fmt.Errorf("registered tool page %s has no h1", path)
					return
				}
				toolInventory = append(toolInventory, toolPage{path: path, title: title})
			}
		}
		toolInventory = uniqueToolPages(toolInventory)
		if len(categoryInventory) == 0 || len(toolInventory) == 0 {
			inventoryError = fmt.Errorf("registered page inventory is empty: categories=%d tools=%d", len(categoryInventory), len(toolInventory))
		}
	})
	return append([]category(nil), categoryInventory...), append([]toolPage(nil), toolInventory...), inventoryError
}

func inventoryPage(path string) (string, error) {
	request, err := http.NewRequest(http.MethodGet, suite.localURL+path, nil)
	if err != nil {
		return "", fmt.Errorf("build GET %s: %w", path, err)
	}
	request.Header.Set("Accept", "text/html,application/xhtml+xml")
	request.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64) e2e-suite")
	resp, err := httpClient().Do(request)
	if err != nil {
		return "", fmt.Errorf("GET %s: %w", path, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", path, err)
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GET %s: HTTP %d", path, resp.StatusCode)
	}
	return string(body), nil
}

func inventoryLinks(body string) []inventoryLink {
	var links []inventoryLink
	for _, anchor := range anchorPattern.FindAllStringSubmatch(body, -1) {
		attrs := make(map[string]string)
		for _, attr := range attributePattern.FindAllStringSubmatch(anchor[1], -1) {
			attrs[strings.ToLower(attr[1])] = stdhtml.UnescapeString(attr[2])
		}
		if attrs["href"] != "" {
			links = append(links, inventoryLink{href: attrs["href"], class: attrs["class"]})
		}
	}
	return links
}

func hasClass(class, target string) bool {
	for _, value := range strings.Fields(class) {
		if value == target {
			return true
		}
	}
	return false
}

func inventoryHeading(body string) string {
	match := h1Pattern.FindStringSubmatch(body)
	if len(match) < 2 {
		return ""
	}
	return strings.TrimSpace(stdhtml.UnescapeString(tagPattern.ReplaceAllString(match[1], "")))
}

func uniqueToolPages(pages []toolPage) []toolPage {
	seen := make(map[string]struct{}, len(pages))
	unique := make([]toolPage, 0, len(pages))
	for _, page := range pages {
		if _, ok := seen[page.path]; ok {
			continue
		}
		seen[page.path] = struct{}{}
		unique = append(unique, page)
	}
	sort.Slice(unique, func(i, j int) bool { return unique[i].path < unique[j].path })
	return unique
}

func requireFeatureInventory(t *testing.T) ([]category, []toolPage) {
	t.Helper()
	categories, pages, err := registeredFeatureInventory()
	if err != nil {
		t.Fatalf("discover registered feature pages: %v", err)
	}
	return categories, pages
}

// serverPage is a standard informational page required by PART 16.
type serverPage struct {
	path    string
	content string
}

var serverPages = []serverPage{
	{path: "/server/about", content: "About"},
	{path: "/server/help", content: "Help"},
	{path: "/server/help/mcp", content: "Connect an AI assistant"},
	{path: "/server/privacy", content: "Privacy Policy"},
	{path: "/server/contact", content: "Contact"},
	{path: "/server/preferences/tokens", content: "Owner tokens"},
	{path: "/categories", content: "category-grid"},
	{path: "/server/healthz", content: `class="status-banner`},
	{path: "/server/docs/swagger", content: `id="swagger-ui"`},
	{path: "/server/docs/graphql", content: `id="graphiql"`},
}

var uuidPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// TestTier1HomePageSSR asserts the landing page is fully rendered server-side,
// not an empty shell hydrated by JavaScript.
func TestTier1HomePageSSR(t *testing.T) {
	resp, body := getHTML(t, "/")
	requireStatus(t, resp, "/", http.StatusOK)

	requireContains(t, body, "<title>", "/ title tag")
	requireContains(t, body, "CasTools", "/ brand")
	requireContains(t, body, "Browse Categories", "/ hero call to action")
	requireContains(t, body, `href="/categories"`, "/ categories link")
	requireContains(t, body, `href="/server/docs/swagger"`, "/ api docs link")
	requireContains(t, body, "category-card", "/ category grid")

	if strings.Contains(body, `<div id="app"></div>`) {
		t.Error("/ served a client-side rendering shell, which violates PART 14")
	}
}

// TestTier1DocumentMetadata asserts the document-level metadata required by
// PART 28 SSR correctness on every registered page and tool route.
func TestTier1DocumentMetadata(t *testing.T) {
	paths := []string{"/"}
	categories, tools := requireFeatureInventory(t)
	for _, category := range categories {
		paths = append(paths, category.path)
	}
	for _, tool := range tools {
		paths = append(paths, tool.path)
	}
	for _, page := range serverPages {
		paths = append(paths, page.path)
	}
	paths = append(paths,
		"/server/preferences",
		"/server/preferences/sync",
		"/server/terms",
		"/server/dpo",
		"/server/security",
		"/server/security/policy",
		"/server/security/thanks",
		securityReportMetadataPath(t),
	)
	paths = uniqueStrings(paths)
	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			resp, body := getHTML(t, path)
			requireStatus(t, resp, path, http.StatusOK)
			requireContains(t, body, `<html lang="en"`, path+" lang attribute")
			if !regexp.MustCompile(`(?i)<meta\s+charset=(?:"?utf-8"?)`).MatchString(body) {
				t.Errorf("%s: missing UTF-8 charset declaration in response: %q", path, body[:min(len(body), 240)])
			}
			requireContains(t, body, `name="viewport"`, path+" viewport")
			requireContains(t, body, "</title>", path+" title element")
			requireContains(t, body, `name="description"`, path+" description meta")
		})
	}
}

func securityReportMetadataPath(t *testing.T) string {
	t.Helper()

	dataDir := filepath.Join(suite.artifactDir, "data")
	databaseDir := filepath.Join(dataDir, "db", "sqlite")
	t.Setenv("DATABASE_DIR", databaseDir)
	if err := database.InitDatabase(config.DatabaseConfig{Driver: "sqlite"}, dataDir); err != nil {
		t.Fatalf("open E2E database for security-report metadata fixture: %v", err)
	}
	t.Cleanup(func() {
		if err := database.Close(); err != nil {
			t.Errorf("close E2E database metadata fixture: %v", err)
		}
	})

	const trackingID = "sec_0123456789abcdef"
	const token = "e2e-security-status-token"
	tokenHash := sha256.Sum256([]byte(token))
	if err := database.StoreSecurityReport(context.Background(), database.SecurityReportInput{
		TrackingID: trackingID,
		Severity:   "low",
		Component:  "E2E metadata fixture",
		Body:       []byte("fixture ciphertext"),
		BodyAlg:    "aes-256-gcm",
		Contact:    []byte("fixture ciphertext"),
		CreditPref: "no",
		TokenHash:  hex.EncodeToString(tokenHash[:]),
	}); err != nil {
		t.Fatalf("create E2E security-report metadata fixture: %v", err)
	}
	return "/server/security/report/" + trackingID + "?token=" + url.QueryEscape(token)
}

func uniqueStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	unique := make([]string, 0, len(values))
	for _, value := range values {
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		unique = append(unique, value)
	}
	sort.Strings(unique)
	return unique
}

// TestTier1CategoryPagesSSR gives every IDEA.md tool category a Tier 1
// assertion: its landing page renders its own heading and links to its tools.
func TestTier1CategoryPagesSSR(t *testing.T) {
	categories, _ := requireFeatureInventory(t)
	for _, c := range categories {
		t.Run(c.path, func(t *testing.T) {
			resp, body := getHTML(t, c.path)
			requireStatus(t, resp, c.path, http.StatusOK)
			requireContains(t, body, stdhtml.EscapeString(c.heading), c.path+" heading")
			if !strings.Contains(body, `class="category-card"`) && !strings.Contains(body, `class="tool-card-link"`) {
				t.Errorf("%s: category page lists no tool links", c.path)
			}
		})
	}
}

// TestTier1ToolPagesSSR asserts every registered tool page ships its title,
// labeled form, and processing endpoint in the initial response.
func TestTier1ToolPagesSSR(t *testing.T) {
	_, pages := requireFeatureInventory(t)
	for _, page := range pages {
		t.Run(page.path, func(t *testing.T) {
			resp, body := getHTML(t, page.path)
			requireStatus(t, resp, page.path, http.StatusOK)
			requireContains(t, body, stdhtml.EscapeString(page.title), page.path+" tool title")
			requireContains(t, body, `<main id="main-content"`, page.path+" main landmark")
			requireContains(t, body, `class="tool-form`, page.path+" server-rendered tool form")
			requireContains(t, body, "<label", page.path+" labeled domain input")
			if !strings.Contains(body, "data-endpoint=") && !strings.Contains(body, "data-api-endpoint=") && !strings.Contains(body, "action=") {
				t.Errorf("%s: server-rendered tool page declares no processing endpoint", page.path)
			}
		})
	}
}

// TestTier1ServerPagesSSR covers the standard informational pages.
func TestTier1ServerPagesSSR(t *testing.T) {
	for _, page := range serverPages {
		t.Run(page.path, func(t *testing.T) {
			resp, body := getHTML(t, page.path)
			requireStatus(t, resp, page.path, http.StatusOK)
			requireContains(t, body, page.content, page.path+" content")
		})
	}
}

// TestTier1HealthEndpoints asserts both the frontend and API health routes,
// including the root alias enabled by the fixture config.
func TestTier1HealthEndpoints(t *testing.T) {
	resp, body := getHTML(t, "/server/healthz")
	requireStatus(t, resp, "/server/healthz", http.StatusOK)
	requireContains(t, body, `class="status-banner`, "/server/healthz page")

	for _, path := range []string{"/api/v1/server/healthz", "/api/healthz", "/healthz"} {
		t.Run(path, func(t *testing.T) {
			resp, body := fetch(t, path, map[string]string{"Accept": "application/json"}, nil)
			requireStatus(t, resp, path, http.StatusOK)
			var payload map[string]any
			if err := json.Unmarshal([]byte(body), &payload); err != nil {
				t.Fatalf("%s: response is not JSON: %v", path, err)
			}
			if _, ok := payload["status"]; !ok {
				t.Errorf("%s: health payload has no status field: %s", path, body)
			}
			for _, leak := range []string{"password", "token=", "/var/lib/", "database is"} {
				if strings.Contains(strings.ToLower(body), leak) {
					t.Errorf("%s: health payload leaks %q", path, leak)
				}
			}
		})
	}
}

func TestTier1ProtectedDebug500Page(t *testing.T) {
	resp, body := fetch(t, "/debug/error500", map[string]string{
		"Accept":        "text/html",
		"Authorization": "Bearer " + suite.operatorToken,
	}, nil)
	requireStatus(t, resp, "/debug/error500", http.StatusInternalServerError)
	requireContains(t, body, "500", "debug 500 error code")
	requireContains(t, body, `<link rel="stylesheet"`, "debug 500 theme stylesheet")
	if strings.Contains(body, "goroutine") || strings.Contains(body, "panic:") {
		t.Error("debug 500 page leaks a stack trace")
	}
}

// TestTier1ContentNegotiation asserts the PART 14 negotiation rules: JSON by
// default on API routes, plain text via the .txt suffix and Accept header,
// HTML on frontend routes.
func TestTier1ContentNegotiation(t *testing.T) {
	t.Run("api json when requested", func(t *testing.T) {
		resp, body := fetch(t, "/api/v1/text/uuid", map[string]string{"Accept": "application/json"}, nil)
		requireStatus(t, resp, "/api/v1/text/uuid", http.StatusOK)
		if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "application/json") {
			t.Errorf("/api/v1/text/uuid: Content-Type %q is not JSON", ct)
		}
		var payload struct {
			OK   bool `json:"ok"`
			Data struct {
				UUID string `json:"uuid"`
			} `json:"data"`
		}
		if err := json.Unmarshal([]byte(body), &payload); err != nil {
			t.Fatalf("/api/v1/text/uuid: invalid JSON: %v", err)
		}
		if !payload.OK {
			t.Errorf("/api/v1/text/uuid: response is not a success envelope")
		}
		if !uuidPattern.MatchString(payload.Data.UUID) {
			t.Errorf("/api/v1/text/uuid: %q is not a UUID", payload.Data.UUID)
		}
	})

	t.Run("txt suffix", func(t *testing.T) {
		resp, body := fetch(t, "/api/v1/text/uuid.txt", nil, nil)
		requireStatus(t, resp, "/api/v1/text/uuid.txt", http.StatusOK)
		if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "text/plain") {
			t.Errorf("/api/v1/text/uuid.txt: Content-Type %q is not text/plain", ct)
		}
		var uuid string
		for _, line := range strings.Split(strings.TrimSpace(body), "\n") {
			if value, ok := strings.CutPrefix(line, "uuid: "); ok {
				uuid = value
			}
		}
		if !uuidPattern.MatchString(uuid) {
			t.Errorf("/api/v1/text/uuid.txt: uuid field %q is malformed", uuid)
		}
		if !strings.HasSuffix(body, "\n") {
			t.Error("/api/v1/text/uuid.txt: text response must end with a single newline")
		}
	})

	t.Run("frontend html", func(t *testing.T) {
		resp, body := getHTML(t, "/text")
		requireStatus(t, resp, "/text", http.StatusOK)
		if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "text/html") {
			t.Errorf("/text: Content-Type %q is not HTML", ct)
		}
		requireContains(t, body, "<!DOCTYPE html>", "/text doctype")
	})
}

// TestTier1APIFeatureCoverage asserts the API surface behind the tool pages
// returns real computed data, one case per exercised feature area.
func TestTier1APIFeatureCoverage(t *testing.T) {
	cases := []struct {
		path   string
		field  string
		verify func(t *testing.T, value string)
	}{
		{path: "/api/v1/text/uuid/4", field: "uuid", verify: func(t *testing.T, v string) {
			if !uuidPattern.MatchString(v) {
				t.Errorf("uuid %q is malformed", v)
			}
		}},
		{path: "/api/v1/text/hash/sha256/castools", field: "hash", verify: func(t *testing.T, v string) {
			if len(v) != 64 {
				t.Errorf("sha256 hash %q is not 64 hex characters", v)
			}
		}},
		{path: "/api/v1/text/encode/base64/castools", field: "output", verify: func(t *testing.T, v string) {
			if v != "Y2FzdG9vbHM=" {
				t.Errorf("base64 of castools is %q, want Y2FzdG9vbHM=", v)
			}
		}},
		{path: "/api/v1/text/rot13/castools", field: "output", verify: func(t *testing.T, v string) {
			if v != "pnfgbbyf" {
				t.Errorf("rot13 of castools is %q, want pnfgbbyf", v)
			}
		}},
	}

	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			resp, body := fetch(t, tc.path, map[string]string{"Accept": "application/json"}, nil)
			requireStatus(t, resp, tc.path, http.StatusOK)
			var payload map[string]any
			if err := json.Unmarshal([]byte(body), &payload); err != nil {
				t.Fatalf("%s: invalid JSON: %v", tc.path, err)
			}
			if payload["ok"] != true {
				t.Fatalf("%s: response is not a success envelope", tc.path)
			}
			data, ok := payload["data"].(map[string]any)
			if !ok {
				t.Fatalf("%s: envelope data is not an object", tc.path)
			}
			raw, ok := data[tc.field]
			if !ok {
				t.Fatalf("%s: response has no %q field: %s", tc.path, tc.field, body)
			}
			value, ok := raw.(string)
			if !ok {
				t.Fatalf("%s: field %q is not a string: %v", tc.path, tc.field, raw)
			}
			tc.verify(t, value)
		})
	}
}

// TestTier1ThemeCookieRendering asserts the theme is resolved server-side from
// the cookie and applied to the html element, with no JavaScript involved.
func TestTier1ThemeCookieRendering(t *testing.T) {
	cases := map[string]string{
		"dark":  "theme-dark",
		"light": "theme-light",
		"auto":  "theme-auto",
	}
	for value, class := range cases {
		t.Run(value, func(t *testing.T) {
			resp, body := fetch(t, "/", map[string]string{"Accept": "text/html"}, map[string]string{"theme": value})
			requireStatus(t, resp, "/", http.StatusOK)
			requireContains(t, body, class, "theme cookie "+value)
			if strings.Contains(body, `<body class="`+class) {
				t.Errorf("theme class %q must be on <html>, never on <body>", class)
			}
		})
	}
}

// TestTier1SpecialFiles asserts the always-on public files render with the
// right content type and content.
func TestTier1SpecialFiles(t *testing.T) {
	cases := []struct {
		path        string
		status      int
		contentType string
		content     string
	}{
		{path: "/robots.txt", status: http.StatusOK, contentType: "text/plain", content: "User-agent"},
		{path: "/security.txt", status: http.StatusNotFound},
		{path: "/.well-known/security.txt", status: http.StatusOK, contentType: "text/plain", content: "Contact:"},
		{path: "/manifest.json", status: http.StatusOK, contentType: "application/", content: "start_url"},
		{path: "/sw.js", status: http.StatusOK, contentType: "javascript", content: "addEventListener"},
	}
	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			resp, body := fetch(t, tc.path, nil, nil)
			requireStatus(t, resp, tc.path, tc.status)
			if tc.status != http.StatusOK {
				return
			}
			if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, tc.contentType) {
				t.Errorf("%s: Content-Type %q does not contain %q", tc.path, ct, tc.contentType)
			}
			if !strings.Contains(strings.ToLower(body), strings.ToLower(tc.content)) {
				t.Errorf("%s: response is missing %q", tc.path, tc.content)
			}
		})
	}
	resp, _ := fetch(t, "/offline.html", nil, nil)
	requireStatus(t, resp, "/offline.html", http.StatusNotFound)
}

// TestTier1StaticAssets asserts every asset referenced by the layout loads.
func TestTier1StaticAssets(t *testing.T) {
	assets := []string{
		"/static/css/common.css",
		"/static/css/components.css",
		"/static/css/public.css",
		"/static/js/app.js",
	}
	for _, asset := range assets {
		t.Run(asset, func(t *testing.T) {
			resp, body := fetch(t, asset, nil, nil)
			requireStatus(t, resp, asset, http.StatusOK)
			if len(body) == 0 {
				t.Errorf("%s: served an empty body", asset)
			}
		})
	}
}

// TestTier1APIDocsPages asserts the Swagger UI and GraphiQL pages render and
// reference the specs the server actually serves.
func TestTier1APIDocsPages(t *testing.T) {
	t.Run("/server/docs/swagger", func(t *testing.T) {
		resp, body := getHTML(t, "/server/docs/swagger")
		requireStatus(t, resp, "/server/docs/swagger", http.StatusOK)
		if !strings.Contains(strings.ToLower(body), "swagger") {
			t.Error("/server/docs/swagger: page does not reference Swagger")
		}
	})
	t.Run("/server/docs/graphql", func(t *testing.T) {
		resp, body := getHTML(t, "/server/docs/graphql")
		requireStatus(t, resp, "/server/docs/graphql", http.StatusOK)
		if !strings.Contains(strings.ToLower(body), "graphql") {
			t.Error("/server/docs/graphql: page does not reference GraphQL")
		}
	})
	t.Run("/api/swagger", func(t *testing.T) {
		resp, body := fetch(t, "/api/swagger", map[string]string{"Accept": "application/json"}, nil)
		requireStatus(t, resp, "/api/swagger", http.StatusOK)
		var spec map[string]any
		if err := json.Unmarshal([]byte(body), &spec); err != nil {
			t.Fatalf("/api/swagger: spec is not valid JSON: %v", err)
		}
		if _, ok := spec["paths"]; !ok {
			t.Error("/api/swagger: spec has no paths object")
		}
	})
}

// TestTier1UnknownPathReturns404 asserts unknown routes answer 404 rather than
// a redirect, a 200 shell or a stack trace.
func TestTier1UnknownPathReturns404(t *testing.T) {
	path := "/this-route-does-not-exist-e2e"
	resp, body := getHTML(t, path)
	requireStatus(t, resp, path, http.StatusNotFound)
	for _, leak := range []string{"goroutine", ".go:", "panic:"} {
		if strings.Contains(body, leak) {
			t.Errorf("%s: 404 body leaks internal detail %q", path, leak)
		}
	}
}

// TestTier1TrailingSlashRedirect asserts the PART 16 canonical URL rule:
// trailing slashes redirect to the slashless form.
func TestTier1TrailingSlashRedirect(t *testing.T) {
	path := "/text/"
	resp, _ := getHTML(t, path)
	if resp.StatusCode != http.StatusMovedPermanently {
		t.Fatalf("%s: got HTTP %d, want 301", path, resp.StatusCode)
	}
	if loc := resp.Header.Get("Location"); loc != "/text" {
		t.Errorf("%s: Location is %q, want /text", path, loc)
	}
}

// TestTier1SecurityHeaders asserts the PART 11 header set is present on a
// normal page response.
func TestTier1SecurityHeaders(t *testing.T) {
	resp, _ := getHTML(t, "/")
	required := map[string]string{
		"X-Content-Type-Options":  "nosniff",
		"X-Frame-Options":         "",
		"Referrer-Policy":         "",
		"Content-Security-Policy": "",
		"X-Request-ID":            "",
	}
	for header, want := range required {
		value := resp.Header.Get(header)
		if value == "" {
			t.Errorf("/: response is missing the %s header", header)
			continue
		}
		if want != "" && !strings.Contains(value, want) {
			t.Errorf("/: %s is %q, want it to contain %q", header, value, want)
		}
	}
}
