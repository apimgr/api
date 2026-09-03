//go:build e2e

package e2e

import (
	"encoding/json"
	"net/http"
	"regexp"
	"strings"
	"testing"
)

// category couples a IDEA.md tool category with the frontend route that lists
// it and a marker that must appear in the server-rendered response.
type category struct {
	path    string
	heading string
}

// categories enumerates every user-facing tool category shipped by the
// project (IDEA.md "Business logic"), each of which has a landing page.
var categories = []category{
	{path: "/text", heading: "Text Utilities"},
	{path: "/crypto", heading: "Cryptography"},
	{path: "/network", heading: "Network Tools"},
	{path: "/datetime", heading: "Date &amp; Time"},
	{path: "/convert", heading: "Unit Conversion"},
	{path: "/dev", heading: "Developer Tools"},
	{path: "/docker", heading: "Docker Tools"},
	{path: "/fun", heading: "Fun &amp; Content"},
	{path: "/generate", heading: "Generators"},
	{path: "/geo", heading: "Geolocation"},
	{path: "/image", heading: "Images"},
	{path: "/language", heading: "Language Tools"},
	{path: "/lorem", heading: "Lorem &amp; Fake Data"},
	{path: "/math", heading: "Math &amp; Numbers"},
	{path: "/osint", heading: "OSINT Tools"},
	{path: "/parse", heading: "Parsers"},
	{path: "/research", heading: "Research Tools"},
	{path: "/system", heading: "Health &amp; System"},
	{path: "/testing", heading: "Testing Tools"},
	{path: "/validate", heading: "Validators"},
	{path: "/weather", heading: "Weather"},
}

// toolPage couples a representative tool page with domain content that must be
// present in the initial HTML response.
type toolPage struct {
	path    string
	title   string
	content string
}

// toolPages are the representative per-category tool pages the suite drives in
// every tier. They are chosen to avoid outbound network dependencies so the
// suite stays hermetic.
var toolPages = []toolPage{
	{path: "/text/uuid", title: "UUID Generator", content: "uuid-form"},
	{path: "/text/hash", title: "Hash", content: "tool-form"},
	{path: "/crypto/password", title: "Password", content: "password-form"},
	{path: "/crypto/hash", title: "Hash", content: "tool-card"},
	{path: "/datetime/now", title: "Current", content: "datetime-form"},
	{path: "/network/ip", title: "IP", content: "network-ip-form"},
	{path: "/network/subnet", title: "Subnet", content: "tool-card"},
	{path: "/geo/distance", title: "Distance", content: "tool-card"},
	{path: "/convert/length", title: "Length", content: "tool-card"},
}

// serverPage is a standard informational page required by PART 16.
type serverPage struct {
	path    string
	content string
}

var serverPages = []serverPage{
	{path: "/server/about", content: "About"},
	{path: "/server/help", content: "Help"},
	{path: "/server/privacy", content: "Privacy Policy"},
	{path: "/server/contact", content: "Contact"},
	{path: "/categories", content: "Browse All Categories"},
	{path: "/api", content: "tool-card"},
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
	requireContains(t, body, `href="/api"`, "/ api docs link")
	requireContains(t, body, "category-card", "/ category grid")

	if strings.Contains(body, `<div id="app"></div>`) {
		t.Error("/ served a client-side rendering shell, which violates PART 14")
	}
}

// TestTier1DocumentMetadata asserts the document-level metadata required by
// PART 28 SSR correctness on every representative page.
func TestTier1DocumentMetadata(t *testing.T) {
	paths := []string{"/", "/categories", "/text", "/text/uuid", "/server/about", "/api"}
	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			resp, body := getHTML(t, path)
			requireStatus(t, resp, path, http.StatusOK)
			requireContains(t, body, `<html lang="en"`, path+" lang attribute")
			requireContains(t, body, `<meta charset="UTF-8">`, path+" charset")
			requireContains(t, body, `name="viewport"`, path+" viewport")
			requireContains(t, body, "</title>", path+" title element")
			requireContains(t, body, `name="description"`, path+" description meta")
		})
	}
}

// TestTier1CategoryPagesSSR gives every IDEA.md tool category a Tier 1
// assertion: its landing page renders its own heading and links to its tools.
func TestTier1CategoryPagesSSR(t *testing.T) {
	for _, c := range categories {
		t.Run(c.path, func(t *testing.T) {
			resp, body := getHTML(t, c.path)
			requireStatus(t, resp, c.path, http.StatusOK)
			requireContains(t, body, c.heading, c.path+" heading")
			if !strings.Contains(body, `href="`+c.path+`/`) {
				t.Errorf("%s: category page lists no tool links", c.path)
			}
		})
	}
}

// TestTier1ToolPagesSSR asserts each representative tool page ships its real
// domain content in the initial response.
func TestTier1ToolPagesSSR(t *testing.T) {
	for _, page := range toolPages {
		t.Run(page.path, func(t *testing.T) {
			resp, body := getHTML(t, page.path)
			requireStatus(t, resp, page.path, http.StatusOK)
			requireContains(t, body, page.title, page.path+" tool title")
			requireContains(t, body, page.content, page.path+" tool body")
			requireContains(t, body, "/api/v1/", page.path+" documented API endpoint")
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
	requireContains(t, body, "System Health", "/server/healthz page")

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

// TestTier1ContentNegotiation asserts the PART 14 negotiation rules: JSON by
// default on API routes, plain text via the .txt suffix and Accept header,
// HTML on frontend routes.
func TestTier1ContentNegotiation(t *testing.T) {
	t.Run("api json default", func(t *testing.T) {
		resp, body := fetch(t, "/api/v1/text/uuid", nil, nil)
		requireStatus(t, resp, "/api/v1/text/uuid", http.StatusOK)
		if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "application/json") {
			t.Errorf("/api/v1/text/uuid: Content-Type %q is not JSON", ct)
		}
		var payload struct {
			UUID string `json:"uuid"`
		}
		if err := json.Unmarshal([]byte(body), &payload); err != nil {
			t.Fatalf("/api/v1/text/uuid: invalid JSON: %v", err)
		}
		if !uuidPattern.MatchString(payload.UUID) {
			t.Errorf("/api/v1/text/uuid: %q is not a UUID", payload.UUID)
		}
	})

	t.Run("txt suffix", func(t *testing.T) {
		resp, body := fetch(t, "/api/v1/text/uuid.txt", nil, nil)
		requireStatus(t, resp, "/api/v1/text/uuid.txt", http.StatusOK)
		if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "text/plain") {
			t.Errorf("/api/v1/text/uuid.txt: Content-Type %q is not text/plain", ct)
		}
		if !uuidPattern.MatchString(strings.TrimSpace(body)) {
			t.Errorf("/api/v1/text/uuid.txt: %q is not a UUID", strings.TrimSpace(body))
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
		{path: "/api/v1/text/encode/base64/castools", field: "encoded", verify: func(t *testing.T, v string) {
			if v != "Y2FzdG9vbHM=" {
				t.Errorf("base64 of castools is %q, want Y2FzdG9vbHM=", v)
			}
		}},
		{path: "/api/v1/text/rot13/castools", field: "result", verify: func(t *testing.T, v string) {
			if v != "pnfgbbyf" {
				t.Errorf("rot13 of castools is %q, want pnfgbbyf", v)
			}
		}},
	}

	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			resp, body := fetch(t, tc.path, nil, nil)
			requireStatus(t, resp, tc.path, http.StatusOK)
			var payload map[string]any
			if err := json.Unmarshal([]byte(body), &payload); err != nil {
				t.Fatalf("%s: invalid JSON: %v", tc.path, err)
			}
			raw, ok := payload[tc.field]
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
		contentType string
		content     string
	}{
		{path: "/robots.txt", contentType: "text/plain", content: "User-agent"},
		{path: "/security.txt", contentType: "text/plain", content: "Contact:"},
		{path: "/.well-known/security.txt", contentType: "text/plain", content: "Contact:"},
		{path: "/manifest.json", contentType: "application/", content: "start_url"},
		{path: "/sw.js", contentType: "javascript", content: "addEventListener"},
		{path: "/offline.html", contentType: "text/html", content: "offline"},
	}
	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			resp, body := fetch(t, tc.path, nil, nil)
			requireStatus(t, resp, tc.path, http.StatusOK)
			if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, tc.contentType) {
				t.Errorf("%s: Content-Type %q does not contain %q", tc.path, ct, tc.contentType)
			}
			if !strings.Contains(strings.ToLower(body), strings.ToLower(tc.content)) {
				t.Errorf("%s: response is missing %q", tc.path, tc.content)
			}
		})
	}
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
		resp, body := fetch(t, "/api/swagger", nil, nil)
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
