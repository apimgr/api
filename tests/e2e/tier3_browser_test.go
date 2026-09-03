//go:build e2e

package e2e

import (
	"context"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"testing"

	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/chromedp"
)

// visitAndRecord loads a path in a scripted tab and returns the tab context,
// its recorder and the rendered document.
func visitAndRecord(t *testing.T, path string) (context.Context, *pageRecorder, string) {
	t.Helper()
	ctx, rec := newTab(t, false)

	var html string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(suite.browserURL+path),
		chromedp.WaitReady("main", chromedp.ByQuery),
		chromedp.OuterHTML("html", &html),
	); err != nil {
		saveArtifacts(t, ctx, "tier3"+strings.ReplaceAll(path, "/", "-"))
		t.Fatalf("%s: load in the browser: %v", path, err)
	}
	return ctx, rec, html
}

// assertClean fails the test when the page logged a console error or any
// request failed or answered 4xx/5xx.
func assertClean(t *testing.T, ctx context.Context, path string, rec *pageRecorder) {
	t.Helper()
	failed := false
	for _, msg := range rec.consoleErrors() {
		failed = true
		t.Errorf("%s: JavaScript console error: %s", path, msg)
	}
	for _, msg := range rec.failedRequests() {
		failed = true
		t.Errorf("%s: request problem: %s", path, msg)
	}
	if failed {
		saveArtifacts(t, ctx, "tier3-dirty"+strings.ReplaceAll(path, "/", "-"))
	}
}

// TestTier3PagesLoadCleanly asserts every representative page loads with zero
// console errors and zero failed assets or XHRs.
func TestTier3PagesLoadCleanly(t *testing.T) {
	paths := []string{"/", "/categories", "/api"}
	for _, c := range categories {
		paths = append(paths, c.path)
	}
	for _, tool := range toolPages {
		paths = append(paths, tool.path)
	}
	for _, srv := range serverPages {
		paths = append(paths, srv.path)
	}
	paths = append(paths, "/server/healthz", "/server/docs/swagger", "/server/docs/graphql")

	seen := make(map[string]bool, len(paths))
	for _, path := range paths {
		if seen[path] {
			continue
		}
		seen[path] = true
		t.Run(path, func(t *testing.T) {
			ctx, rec, html := visitAndRecord(t, path)
			if !strings.Contains(html, "</main>") {
				t.Errorf("%s: page rendered no main element", path)
			}
			assertClean(t, ctx, path, rec)
		})
	}
}

// TestTier3InternalLinkCrawl starts at the home page, follows every internal
// link it can reach and fails on dead links and server errors.
func TestTier3InternalLinkCrawl(t *testing.T) {
	seeds := []string{"/", "/categories", "/api"}
	for _, c := range categories {
		seeds = append(seeds, c.path)
	}

	targets := map[string]string{}
	for _, seed := range seeds {
		ctx, _, _ := visitAndRecord(t, seed)

		var hrefs []string
		script := `Array.from(document.querySelectorAll('a[href]')).map(function (a) { return a.getAttribute('href'); })`
		if err := chromedp.Run(ctx, chromedp.Evaluate(script, &hrefs)); err != nil {
			t.Fatalf("%s: collect links: %v", seed, err)
		}
		for _, href := range hrefs {
			target, ok := internalPath(href)
			if !ok {
				continue
			}
			if _, exists := targets[target]; !exists {
				targets[target] = seed
			}
		}
	}

	if len(targets) == 0 {
		t.Fatal("crawl found no internal links to follow")
	}

	paths := make([]string, 0, len(targets))
	for target := range targets {
		paths = append(paths, target)
	}
	sort.Strings(paths)

	for _, path := range paths {
		resp, body := fetch(t, path, map[string]string{"Accept": "text/html"}, nil)
		switch {
		case resp.StatusCode == http.StatusNotFound:
			t.Errorf("%s: dead link from %s (HTTP 404)", path, targets[path])
		case resp.StatusCode >= 400:
			t.Errorf("%s: linked from %s and returned HTTP %d", path, targets[path], resp.StatusCode)
		case resp.StatusCode == http.StatusOK && strings.TrimSpace(body) == "":
			t.Errorf("%s: linked from %s and returned an empty body", path, targets[path])
		}
	}
}

// internalPath reduces an href to a same-origin request path, skipping
// fragments, external links and non-HTTP schemes.
func internalPath(href string) (string, bool) {
	href = strings.TrimSpace(href)
	if href == "" || strings.HasPrefix(href, "#") {
		return "", false
	}
	parsed, err := url.Parse(href)
	if err != nil {
		return "", false
	}
	if parsed.Scheme != "" && parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", false
	}
	if parsed.Host != "" {
		base, err := url.Parse(suite.browserURL)
		if err != nil || parsed.Host != base.Host {
			return "", false
		}
	}
	path := parsed.EscapedPath()
	if !strings.HasPrefix(path, "/") {
		return "", false
	}
	if parsed.RawQuery != "" {
		path += "?" + parsed.RawQuery
	}
	return path, true
}

// TestTier3StaticAssetsLoad asserts every asset the layout references is
// fetched successfully by the browser itself.
func TestTier3StaticAssetsLoad(t *testing.T) {
	ctx, rec, _ := visitAndRecord(t, "/")

	stylesheet := false
	script := false
	for _, requested := range rec.requestURLs() {
		if strings.Contains(requested, "/static/css/") {
			stylesheet = true
		}
		if strings.Contains(requested, "/static/js/") {
			script = true
		}
	}
	if !stylesheet {
		saveArtifacts(t, ctx, "tier3-assets")
		t.Error("/: the browser requested no stylesheet, so the page is unstyled")
	}
	if !script {
		saveArtifacts(t, ctx, "tier3-assets")
		t.Error("/: the browser requested no script, so the enhancement layer never loaded")
	}
	assertClean(t, ctx, "/", rec)
}

// TestTier3ThemeComputedStyles asserts the dark and light themes actually
// change the rendered colours, not just the html class.
func TestTier3ThemeComputedStyles(t *testing.T) {
	background := func(theme string) string {
		ctx, _ := newTab(t, false)
		if err := setThemeCookie(ctx, theme); err != nil {
			t.Fatalf("set theme cookie %q: %v", theme, err)
		}
		var color string
		script := `getComputedStyle(document.body).backgroundColor`
		if err := chromedp.Run(ctx,
			chromedp.Navigate(suite.browserURL+"/"),
			chromedp.WaitReady("main", chromedp.ByQuery),
			chromedp.Evaluate(script, &color),
		); err != nil {
			saveArtifacts(t, ctx, "tier3-theme-"+theme)
			t.Fatalf("read the computed background for theme %q: %v", theme, err)
		}
		return color
	}

	dark := background("dark")
	light := background("light")
	if dark == "" || light == "" {
		t.Fatalf("computed background colours are empty (dark=%q light=%q)", dark, light)
	}
	if dark == light {
		t.Errorf("dark and light both compute body background %q, so the theme has no visual effect", dark)
	}
}

// TestTier3AutoThemeRenders asserts the auto theme renders a usable page in
// the browser, completing the dark/light/auto set.
func TestTier3AutoThemeRenders(t *testing.T) {
	ctx, rec := newTab(t, false)
	if err := setThemeCookie(ctx, "auto"); err != nil {
		t.Fatalf("set the auto theme cookie: %v", err)
	}

	var classes, color string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(suite.browserURL+"/"),
		chromedp.WaitReady("main", chromedp.ByQuery),
		chromedp.AttributeValue("html", "class", &classes, nil, chromedp.ByQuery),
		chromedp.Evaluate(`getComputedStyle(document.body).backgroundColor`, &color),
	); err != nil {
		saveArtifacts(t, ctx, "tier3-theme-auto")
		t.Fatalf("render the auto theme: %v", err)
	}

	if !strings.Contains(classes, "theme-auto") {
		t.Errorf("auto theme: html class is %q, want it to contain theme-auto", classes)
	}
	if color == "" || color == "rgba(0, 0, 0, 0)" {
		t.Errorf("auto theme: body has no resolved background colour (%q)", color)
	}
	assertClean(t, ctx, "/", rec)
}

// TestTier3MobileViewport asserts the 375x812 mobile viewport renders without
// horizontal overflow and keeps the navigation reachable.
func TestTier3MobileViewport(t *testing.T) {
	paths := []string{"/", "/categories", "/text", "/text/uuid"}
	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			ctx, rec := newTab(t, false)

			var overflow bool
			var navCount int
			overflowScript := `document.documentElement.scrollWidth > document.documentElement.clientWidth`
			navScript := `document.querySelectorAll('nav a').length`
			if err := chromedp.Run(ctx,
				emulation.SetDeviceMetricsOverride(375, 812, 2, true),
				chromedp.Navigate(suite.browserURL+path),
				chromedp.WaitReady("main", chromedp.ByQuery),
				chromedp.Evaluate(overflowScript, &overflow),
				chromedp.Evaluate(navScript, &navCount),
			); err != nil {
				saveArtifacts(t, ctx, "tier3-mobile"+strings.ReplaceAll(path, "/", "-"))
				t.Fatalf("%s: render at 375x812: %v", path, err)
			}

			if overflow {
				saveArtifacts(t, ctx, "tier3-mobile"+strings.ReplaceAll(path, "/", "-"))
				t.Errorf("%s: page scrolls horizontally at 375x812", path)
			}
			if navCount == 0 {
				t.Errorf("%s: no navigation links are reachable at 375x812", path)
			}
			assertClean(t, ctx, path, rec)
		})
	}
}

// TestTier3APIDocsPagesRender asserts the Swagger UI and GraphiQL pages boot
// in a real browser and describe the routes the server serves.
func TestTier3APIDocsPagesRender(t *testing.T) {
	t.Run("swagger", func(t *testing.T) {
		ctx, rec, html := visitAndRecord(t, "/server/docs/swagger")
		if !strings.Contains(strings.ToLower(html), "swagger") {
			saveArtifacts(t, ctx, "tier3-swagger")
			t.Error("/server/docs/swagger: page does not render the Swagger UI")
		}
		assertClean(t, ctx, "/server/docs/swagger", rec)
	})

	t.Run("graphql", func(t *testing.T) {
		ctx, rec, html := visitAndRecord(t, "/server/docs/graphql")
		if !strings.Contains(strings.ToLower(html), "graphql") {
			saveArtifacts(t, ctx, "tier3-graphql")
			t.Error("/server/docs/graphql: page does not render the GraphQL explorer")
		}
		assertClean(t, ctx, "/server/docs/graphql", rec)
	})
}

// TestTier3UUIDToolInteraction drives the UUID generator exactly as a user
// does and asserts the enhanced flow produces a real UUID.
func TestTier3UUIDToolInteraction(t *testing.T) {
	ctx, rec := newTab(t, false)

	var result string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(suite.browserURL+"/text/uuid"),
		chromedp.WaitReady("#uuid-form", chromedp.ByID),
		chromedp.Click("#uuid-form button[type=submit]", chromedp.ByQuery, chromedp.NodeReady),
		chromedp.WaitVisible("#uuid-result", chromedp.ByID),
		chromedp.Text("#uuid-result", &result, chromedp.ByID),
	); err != nil {
		saveArtifacts(t, ctx, "tier3-uuid")
		t.Fatalf("/text/uuid: generate a UUID in the browser: %v", err)
	}

	fields := strings.Fields(result)
	if len(fields) == 0 {
		saveArtifacts(t, ctx, "tier3-uuid")
		t.Fatal("/text/uuid: the result panel is empty after submitting the form")
	}
	if !uuidPattern.MatchString(fields[0]) {
		saveArtifacts(t, ctx, "tier3-uuid")
		t.Errorf("/text/uuid: result %q does not start with a UUID", result)
	}
	assertClean(t, ctx, "/text/uuid", rec)
}

// TestTier3ToolFormsAreWired asserts every representative tool form is wired
// to something that can produce a result in the browser.
func TestTier3ToolFormsAreWired(t *testing.T) {
	for _, tool := range toolPages {
		t.Run(tool.path, func(t *testing.T) {
			ctx, rec, html := visitAndRecord(t, tool.path)
			if !strings.Contains(html, "<form") {
				t.Skipf("%s: page has no form", tool.path)
			}
			wiring := []string{
				"data-endpoint",
				"data-template",
				"data-body-endpoint",
				"data-query-post-endpoint",
				"data-image-template",
				"action=",
			}
			wired := false
			for _, attr := range wiring {
				if strings.Contains(html, attr) {
					wired = true
					break
				}
			}
			for _, legacy := range []string{"datetime-form", "network-ip-form", "password-form", "uuid-form"} {
				if strings.Contains(html, `id="`+legacy+`"`) {
					wired = true
					break
				}
			}
			if !wired {
				saveArtifacts(t, ctx, "tier3-form"+strings.ReplaceAll(tool.path, "/", "-"))
				t.Errorf("%s: the form is wired to neither an API endpoint nor a native action", tool.path)
			}
			assertClean(t, ctx, tool.path, rec)
		})
	}
}

// TestTier3HermeticOrigins asserts the browser never reaches outside the
// server under test while rendering the site.
func TestTier3HermeticOrigins(t *testing.T) {
	base, err := url.Parse(suite.browserURL)
	if err != nil {
		t.Fatalf("parse the server URL: %v", err)
	}

	for _, path := range []string{"/", "/text/uuid", "/server/docs/swagger", "/server/docs/graphql"} {
		t.Run(path, func(t *testing.T) {
			ctx, rec, _ := visitAndRecord(t, path)
			for _, requested := range rec.requestURLs() {
				parsed, err := url.Parse(requested)
				if err != nil {
					continue
				}
				switch parsed.Scheme {
				case "data", "blob", "about", "chrome", "chrome-extension":
					continue
				}
				if parsed.Host != base.Host {
					saveArtifacts(t, ctx, "tier3-egress"+strings.ReplaceAll(path, "/", "-"))
					t.Errorf("%s: page requested the external origin %s", path, requested)
				}
			}
		})
	}
}

// TestTier3ErrorPageThemed asserts a 404 renders the themed error page in a
// real browser, with no console noise and no stack trace.
func TestTier3ErrorPageThemed(t *testing.T) {
	ctx, rec := newTab(t, false)

	var html string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(suite.browserURL+"/this-route-does-not-exist-e2e"),
		chromedp.WaitReady("body", chromedp.ByQuery),
		chromedp.OuterHTML("html", &html),
	); err != nil {
		saveArtifacts(t, ctx, "tier3-404")
		t.Fatalf("load an unknown route: %v", err)
	}

	if !strings.Contains(html, `<link rel="stylesheet"`) {
		saveArtifacts(t, ctx, "tier3-404")
		t.Error("404 page is not themed: it references no site stylesheet (PART 16)")
	}
	if strings.Contains(html, "goroutine") || strings.Contains(html, "panic:") {
		t.Error("404 page leaks a stack trace")
	}
	for _, msg := range rec.consoleErrors() {
		t.Errorf("404 page: JavaScript console error: %s", msg)
	}
}
