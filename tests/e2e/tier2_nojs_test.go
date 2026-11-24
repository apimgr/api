//go:build e2e

package e2e

import (
	"context"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
)

// formTagPattern matches an opening form tag so its attributes can be checked
// without executing any page script.
var formTagPattern = regexp.MustCompile(`(?is)<form\b[^>]*>`)
var formBlockPattern = regexp.MustCompile(`(?is)<form\b([^>]*)>(.*?)</form>`)
var formControlPattern = regexp.MustCompile(`(?is)<(input|select|textarea)\b([^>]*)>`)
var labelBlockPattern = regexp.MustCompile(`(?is)<label\b([^>]*)>(.*?)</label>`)

// attrPattern extracts a single attribute value from an HTML tag.
func attrPattern(name string) *regexp.Regexp {
	return regexp.MustCompile(`(?is)\b` + name + `\s*=\s*"([^"]*)"`)
}

// currentURL reads the active document URL from the navigation history, which
// needs no page script and therefore works with JavaScript disabled.
func currentURL(ctx context.Context) (string, error) {
	var location string
	err := chromedp.Run(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		index, entries, err := page.GetNavigationHistory().Do(ctx)
		if err != nil {
			return err
		}
		if index >= 0 && int(index) < len(entries) {
			location = entries[index].URL
		}
		return nil
	}))
	return location, err
}

// renderNoJS navigates the no-JavaScript tab to a path and returns the
// server-rendered document.
func renderNoJS(t *testing.T, ctx context.Context, path string) string {
	t.Helper()
	var html string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(suite.browserURL+path),
		chromedp.WaitReady("main", chromedp.ByQuery),
		chromedp.OuterHTML("html", &html),
	); err != nil {
		saveArtifacts(t, ctx, "tier2"+strings.ReplaceAll(path, "/", "-"))
		t.Fatalf("%s: render without JavaScript: %v", path, err)
	}
	return html
}

// TestTier2HomeRendersWithoutJavaScript asserts the landing page is complete
// with script execution disabled.
func TestTier2HomeRendersWithoutJavaScript(t *testing.T) {
	ctx, _ := newTab(t, true)
	html := renderNoJS(t, ctx, "/")

	for _, want := range []string{"<title>", "CasTools", "Browse Categories", "category-card"} {
		if !strings.Contains(html, want) {
			saveArtifacts(t, ctx, "tier2-home")
			t.Errorf("/: rendered document is missing %q without JavaScript", want)
		}
	}
}

// TestTier2NavigationWithoutJavaScript asserts the primary navigation is
// usable through plain links when JavaScript is unavailable.
func TestTier2NavigationWithoutJavaScript(t *testing.T) {
	ctx, _ := newTab(t, true)

	var html string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(suite.browserURL+"/"),
		chromedp.WaitReady(`nav a[href="/categories"]`, chromedp.ByQuery),
		chromedp.Click(`nav a[href="/categories"]`, chromedp.ByQuery, chromedp.NodeReady),
		chromedp.WaitReady("main", chromedp.ByQuery),
		chromedp.OuterHTML("html", &html),
	); err != nil {
		saveArtifacts(t, ctx, "tier2-nav")
		t.Fatalf("follow the categories link without JavaScript: %v", err)
	}

	location, err := currentURL(ctx)
	if err != nil {
		t.Fatalf("read current URL: %v", err)
	}
	if !strings.HasSuffix(location, "/categories") {
		t.Errorf("categories link landed on %q, want a URL ending in /categories", location)
	}
	if !strings.Contains(html, "category-grid") {
		saveArtifacts(t, ctx, "tier2-nav")
		t.Error("/categories did not render its category links without JavaScript")
	}
}

// TestTier2CategoryToToolNavigation gives every IDEA.md category a no-JS
// scenario: its landing page is reachable and its first tool link navigates to
// a fully rendered tool page.
func TestTier2CategoryToToolNavigation(t *testing.T) {
	for _, c := range categories {
		t.Run(c.path, func(t *testing.T) {
			ctx, _ := newTab(t, true)
			selector := `main a.category-card, main a.tool-card-link`

			var html string
			if err := chromedp.Run(ctx,
				chromedp.Navigate(suite.browserURL+c.path),
				chromedp.WaitReady(selector, chromedp.ByQuery),
				chromedp.Click(selector, chromedp.ByQuery, chromedp.NodeReady),
				chromedp.WaitReady("main", chromedp.ByQuery),
				chromedp.OuterHTML("html", &html),
			); err != nil {
				saveArtifacts(t, ctx, "tier2-category"+strings.ReplaceAll(c.path, "/", "-"))
				t.Fatalf("%s: navigate to a tool without JavaScript: %v", c.path, err)
			}

			location, err := currentURL(ctx)
			if err != nil {
				t.Fatalf("%s: read current URL: %v", c.path, err)
			}
			if strings.TrimSuffix(location, "/") == strings.TrimSuffix(suite.browserURL+c.path, "/") {
				t.Errorf("%s: tool link did not navigate away from its category", c.path)
			}
			if !strings.Contains(html, "<h1") {
				saveArtifacts(t, ctx, "tier2-category"+strings.ReplaceAll(c.path, "/", "-"))
				t.Errorf("%s: tool page rendered no heading without JavaScript", c.path)
			}
		})
	}
}

// TestTier2ToolPagesRenderWithoutJavaScript asserts the representative tool
// pages ship their controls in the server-rendered markup.
func TestTier2ToolPagesRenderWithoutJavaScript(t *testing.T) {
	for _, tool := range toolPages {
		t.Run(tool.path, func(t *testing.T) {
			ctx, _ := newTab(t, true)
			html := renderNoJS(t, ctx, tool.path)

			if !strings.Contains(html, tool.title) {
				saveArtifacts(t, ctx, "tier2-tool"+strings.ReplaceAll(tool.path, "/", "-"))
				t.Errorf("%s: rendered document is missing %q without JavaScript", tool.path, tool.title)
			}
			if !strings.Contains(html, tool.content) {
				saveArtifacts(t, ctx, "tier2-tool"+strings.ReplaceAll(tool.path, "/", "-"))
				t.Errorf("%s: rendered document is missing %q without JavaScript", tool.path, tool.content)
			}
		})
	}
}

// unlabeledFormControls returns the visible form control names without an
// explicit or wrapping label.
func unlabeledFormControls(body string) []string {
	var missing []string
	labels := labelBlockPattern.FindAllStringSubmatch(body, -1)
	for _, control := range formControlPattern.FindAllStringSubmatch(body, -1) {
		attrs := control[2]
		if fieldType := attrPattern("type").FindStringSubmatch(attrs); fieldType != nil {
			switch strings.ToLower(fieldType[1]) {
			case "hidden", "submit", "button", "reset", "image":
				continue
			}
		}
		fieldID := attrPattern("id").FindStringSubmatch(attrs)
		associated := false
		for _, label := range labels {
			if fieldID != nil {
				if labelFor := attrPattern("for").FindStringSubmatch(label[1]); labelFor != nil && labelFor[1] == fieldID[1] {
					associated = true
					break
				}
			}
			if strings.Contains(label[2], control[0]) {
				associated = true
				break
			}
		}
		if !associated {
			name := attrPattern("name").FindStringSubmatch(attrs)
			if name != nil {
				missing = append(missing, name[1])
			} else {
				missing = append(missing, control[1])
			}
		}
	}
	return missing
}

// TestTier2ToolFormsSubmitWithoutJavaScript asserts every tool form has a
// native POST action, a CSRF field, API endpoint metadata and accessible labels.
func TestTier2ToolFormsSubmitWithoutJavaScript(t *testing.T) {
	methodAttr := attrPattern("method")
	actionAttr := attrPattern("action")
	idAttr := attrPattern("id")
	endpointAttrs := []*regexp.Regexp{
		attrPattern("data-endpoint"),
		attrPattern("data-template"),
		attrPattern("data-body-endpoint"),
		attrPattern("data-query-post-endpoint"),
		attrPattern("data-image-template"),
		attrPattern("data-api-endpoint"),
	}

	for _, tool := range toolPages {
		t.Run(tool.path, func(t *testing.T) {
			ctx, _ := newTab(t, true)
			html := renderNoJS(t, ctx, tool.path)

			mainStart := strings.Index(html, `<main id="main-content"`)
			mainEnd := strings.Index(html, `</main>`)
			if mainStart < 0 || mainEnd < mainStart {
				t.Fatalf("%s: rendered document has no main content landmark", tool.path)
			}
			forms := formBlockPattern.FindAllStringSubmatch(html[mainStart:mainEnd], -1)
			if len(forms) == 0 {
				t.Skipf("%s: page has no form", tool.path)
			}
			for _, form := range forms {
				tag, body := "<form"+form[1]+">", form[2]
				id := ""
				if match := idAttr.FindStringSubmatch(tag); match != nil {
					id = match[1]
				}
				method := methodAttr.FindStringSubmatch(tag)
				action := actionAttr.FindStringSubmatch(tag)
				if method == nil || !strings.EqualFold(method[1], "post") || action == nil || strings.TrimSpace(action[1]) == "" {
					t.Errorf("%s: form %q must submit with POST to a same-origin frontend path without JavaScript: %s",
						tool.path, id, tag)
					continue
				}
				parsedAction, err := url.Parse(action[1])
				if err != nil || parsedAction.IsAbs() || parsedAction.Host != "" || !strings.HasSuffix(parsedAction.Path, tool.path) {
					t.Errorf("%s: form %q action %q does not target its same-origin tool route", tool.path, id, action[1])
				}
				if !strings.Contains(body, `name="csrf_token"`) {
					t.Errorf("%s: form %q is missing its CSRF field", tool.path, id)
				}
				wired := false
				for _, attr := range endpointAttrs {
					if match := attr.FindStringSubmatch(tag); match != nil && strings.TrimSpace(match[1]) != "" {
						wired = true
						break
					}
				}
				if !wired {
					t.Errorf("%s: form %q does not declare API endpoint metadata", tool.path, id)
				}
				if missing := unlabeledFormControls(body); len(missing) > 0 {
					t.Errorf("%s: form %q has controls without associated labels: %s", tool.path, id, strings.Join(missing, ", "))
				}
			}
		})
	}
}

// TestTier2ThemesRenderWithoutJavaScript asserts the theme is applied by the
// server from the cookie, with no client-side initialisation.
func TestTier2ThemesRenderWithoutJavaScript(t *testing.T) {
	for _, theme := range []string{"dark", "light", "auto"} {
		t.Run(theme, func(t *testing.T) {
			ctx, _ := newTab(t, true)
			if err := setThemeCookie(ctx, theme); err != nil {
				t.Fatalf("set theme cookie %q: %v", theme, err)
			}

			var classes string
			if err := chromedp.Run(ctx,
				chromedp.Navigate(suite.browserURL+"/"),
				chromedp.WaitReady("main", chromedp.ByQuery),
				chromedp.AttributeValue("html", "class", &classes, nil, chromedp.ByQuery),
			); err != nil {
				saveArtifacts(t, ctx, "tier2-theme-"+theme)
				t.Fatalf("render theme %q without JavaScript: %v", theme, err)
			}

			if !strings.Contains(classes, "theme-"+theme) {
				t.Errorf("theme %q: html class is %q, want it to contain theme-%s", theme, classes, theme)
			}
		})
	}
}

// TestTier2ErrorPageRendersWithoutJavaScript asserts an unknown route serves
// the PART 16 themed error page rather than a blank body or plain text.
func TestTier2ErrorPageRendersWithoutJavaScript(t *testing.T) {
	ctx, _ := newTab(t, true)

	var html string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(suite.browserURL+"/this-route-does-not-exist-e2e"),
		chromedp.WaitReady("body", chromedp.ByQuery),
		chromedp.OuterHTML("html", &html),
	); err != nil {
		saveArtifacts(t, ctx, "tier2-404")
		t.Fatalf("navigate to an unknown route without JavaScript: %v", err)
	}

	if !strings.Contains(html, `<link rel="stylesheet"`) {
		saveArtifacts(t, ctx, "tier2-404")
		t.Error("unknown route did not render the themed error page (PART 16): no site stylesheet in the response")
	}
	if !strings.Contains(html, "404") {
		saveArtifacts(t, ctx, "tier2-404")
		t.Error("error page does not state the 404 status")
	}
	if strings.Contains(html, "goroutine") || strings.Contains(html, "panic:") {
		t.Error("error page leaks a stack trace")
	}
}

// TestTier2ServerPagesWithoutJavaScript asserts the standard informational
// pages are readable with scripting disabled.
func TestTier2ServerPagesWithoutJavaScript(t *testing.T) {
	for _, srv := range serverPages {
		t.Run(srv.path, func(t *testing.T) {
			ctx, _ := newTab(t, true)
			html := renderNoJS(t, ctx, srv.path)
			if !strings.Contains(html, srv.content) {
				saveArtifacts(t, ctx, "tier2-server"+strings.ReplaceAll(srv.path, "/", "-"))
				t.Errorf("%s: rendered document is missing %q without JavaScript", srv.path, srv.content)
			}
		})
	}
}
