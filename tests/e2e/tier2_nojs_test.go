//go:build e2e

package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	stdhtml "html"
	"image"
	"image/color"
	"image/png"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	cryptosvc "github.com/apimgr/api/src/service/crypto"
	"github.com/chromedp/cdproto/network"
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
	categories, _ := requireFeatureInventory(t)
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

// TestTier2ToolPagesRenderWithoutJavaScript asserts every registered tool
// page ships its controls in the server-rendered markup.
func TestTier2ToolPagesRenderWithoutJavaScript(t *testing.T) {
	_, tools := requireFeatureInventory(t)
	for _, tool := range tools {
		t.Run(tool.path, func(t *testing.T) {
			ctx, _ := newTab(t, true)
			html := renderNoJS(t, ctx, tool.path)

			if !strings.Contains(html, stdhtml.EscapeString(tool.title)) {
				saveArtifacts(t, ctx, "tier2-tool"+strings.ReplaceAll(tool.path, "/", "-"))
				t.Errorf("%s: rendered document is missing %q without JavaScript", tool.path, tool.title)
			}
			if !strings.Contains(html, `<main id="main-content"`) {
				saveArtifacts(t, ctx, "tier2-tool"+strings.ReplaceAll(tool.path, "/", "-"))
				t.Errorf("%s: rendered document has no main content landmark without JavaScript", tool.path)
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

	_, tools := requireFeatureInventory(t)
	for _, tool := range tools {
		t.Run(tool.path, func(t *testing.T) {
			ctx, rec := newTab(t, true)
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
			if err := submitToolForm(ctx, rec, true, tool.path, html); err != nil {
				saveArtifacts(t, ctx, "tier2-submit"+strings.ReplaceAll(tool.path, "/", "-"))
				t.Fatalf("%s: submit tool form without JavaScript: %v", tool.path, err)
			}
			if !recordedPostTo(rec, tool.path) {
				t.Errorf("%s: no native form POST reached the server", tool.path)
			}
			var resultHTML string
			if err := chromedp.Run(ctx, chromedp.Evaluate(`document.querySelector("main")?.outerHTML ?? ""`, &resultHTML)); err != nil {
				t.Fatalf("%s: read submitted response: %v", tool.path, err)
			}
			if !strings.Contains(resultHTML, `class="tool-result`) && !strings.Contains(resultHTML, `class="tool-result-image`) && !strings.Contains(resultHTML, `class="tool-server-result-error"`) && !strings.Contains(resultHTML, `role="alert"`) {
				saveArtifacts(t, ctx, "tier2-result"+strings.ReplaceAll(tool.path, "/", "-"))
				var pageState string
				_ = chromedp.Run(ctx, chromedp.Evaluate(`JSON.stringify({url: location.href, title: document.title, mime: document.contentType, ready: document.readyState, htmlLength: document.documentElement?.outerHTML.length ?? 0, hasMain: document.querySelector("main") !== null, message: document.body?.innerText ?? ""})`, &pageState))
				t.Errorf("%s: native submission returned neither a result nor a rendered error (responses: %v; page: %s; failed requests: %v)", tool.path, rec.responseSummaries(tool.path), pageState, rec.failedRequests())
			}
		})
	}
}

func submitToolForm(ctx context.Context, rec *pageRecorder, scriptsDisabled bool, path, pageHTML string) error {
	// The consent POST redirects and replaces the document. Wait for that
	// navigation before finding controls in the tool form beneath the banner.
	var consentVisible bool
	if err := chromedp.Run(ctx, chromedp.Evaluate(`document.querySelector(".cookie-banner .btn-accept") !== null`, &consentVisible)); err != nil {
		return fmt.Errorf("inspect consent banner on %s: %w", path, err)
	}
	if consentVisible {
		previousLoad := rec.loadCount()
		if err := chromedp.Run(ctx, chromedp.Click(".cookie-banner .btn-accept", chromedp.ByQuery)); err != nil {
			return fmt.Errorf("accept consent before submitting %s: %w", path, err)
		}
		if scriptsDisabled {
			if err := rec.waitForLoadAfter(ctx, previousLoad); err != nil {
				return fmt.Errorf("wait for consent redirect on %s: %w", path, err)
			}
		} else if err := chromedp.Run(ctx, chromedp.WaitNotPresent(".cookie-banner", chromedp.ByQuery)); err != nil {
			return fmt.Errorf("wait for consent update on %s: %w", path, err)
		}
		if err := chromedp.Run(ctx, chromedp.Evaluate(`document.documentElement.outerHTML`, &pageHTML)); err != nil {
			return fmt.Errorf("refresh %s markup after consent: %w", path, err)
		}
	}
	for _, form := range formBlockPattern.FindAllStringSubmatch(pageHTML, -1) {
		tag, body := "<form"+form[1]+">", form[2]
		class := attrPattern("class").FindStringSubmatch(tag)
		if class == nil || !hasClass(class[1], "tool-form") {
			continue
		}
		id := attrPattern("id").FindStringSubmatch(tag)
		selector := "main form.tool-form"
		if id != nil {
			selector = "#" + id[1]
		}
		actions := make([]chromedp.Action, 0)
		radioGroups := make(map[string]bool)
		for _, control := range formControlPattern.FindAllStringSubmatch(body, -1) {
			controlTag, attrs := strings.ToLower(control[1]), control[2]
			controlID := attrPattern("id").FindStringSubmatch(attrs)
			name := attrPattern("name").FindStringSubmatch(attrs)
			typeValue := attrPattern("type").FindStringSubmatch(attrs)
			if controlID == nil || name == nil {
				continue
			}
			typ := "text"
			if typeValue != nil {
				typ = strings.ToLower(typeValue[1])
			}
			fieldSelector := "#" + controlID[1]
			switch typ {
			case "hidden", "submit", "button", "reset", "image":
				continue
			case "file":
				file, err := samplePNGFile()
				if err != nil {
					return err
				}
				actions = append(actions, chromedp.SetUploadFiles(fieldSelector, []string{file}, chromedp.ByQuery))
			case "checkbox", "radio":
				if typ == "checkbox" {
					if path == "/docker/network-helper" && name[1] == "internal" {
						continue
					}
					if !regexp.MustCompile(`(?i)\bchecked(?:\s|=|$)`).MatchString(attrs) {
						actions = append(actions, chromedp.Click(fieldSelector, chromedp.ByQuery))
					}
					continue
				}
				if !radioGroups[name[1]] {
					radioGroups[name[1]] = true
					actions = append(actions, chromedp.Click(fieldSelector, chromedp.ByQuery))
				}
			default:
				value := sampleValue(name[1], typ, attrs)
				if placeholder := attrPattern("placeholder").FindStringSubmatch(attrs); placeholder != nil && placeholder[1] != "" {
					value = samplePlaceholder(name[1], placeholder[1])
				}
				if strings.Contains(strings.ToLower(name[1]), "token") {
					value = "eyJhbGciOiJub25lIn0.eyJzdWIiOiJ0ZXN0In0."
				}
				if path == "/crypto/decrypt" && name[1] == "body" {
					ciphertext, err := cryptosvc.AESEncrypt("E2E sample plaintext", "correct horse battery staple")
					if err != nil {
						return fmt.Errorf("create a valid decrypt fixture: %w", err)
					}
					body, err := json.Marshal(map[string]string{"ciphertext": ciphertext, "key": "correct horse battery staple"})
					if err != nil {
						return fmt.Errorf("encode decrypt fixture: %w", err)
					}
					value = string(body)
				}
				if defaultValue := attrPattern("value").FindStringSubmatch(attrs); defaultValue != nil && defaultValue[1] != "" {
					value = stdhtml.UnescapeString(defaultValue[1])
				}
				switch {
				case path == "/network/ip" && name[1] == "ip":
					value = "8.8.8.8"
				case path == "/network/subnet" && name[1] == "cidr":
					value = "192.168.1.0/24"
				case path == "/docker/network-helper" && (name[1] == "subnet" || name[1] == "gateway"):
					value = ""
				case path == "/image/crop" && (name[1] == "width" || name[1] == "height"):
					value = "1"
				case path == "/image/crop" && (name[1] == "x" || name[1] == "y"):
					value = "0"
				case path == "/osint/tech-stack" && name[1] == "url":
					value = "https://example.com/"
				case path == "/research/arxiv" && name[1] == "id":
					value = "2101.00001"
				case path == "/testing/curl-generator" && name[1] == "body":
					value = `{"method":"POST","url":"https://example.com/api","headers":{"Content-Type":"application/json"},"body":"{\"key\":\"value\"}"}`
				}
				if controlTag == "select" {
					selectPattern := regexp.MustCompile(`(?is)<select\b([^>]*)>(.*?)</select>`)
					for _, selectMatch := range selectPattern.FindAllStringSubmatch(body, -1) {
						selectID := attrPattern("id").FindStringSubmatch(selectMatch[1])
						if selectID == nil || selectID[1] != controlID[1] {
							continue
						}
						option := regexp.MustCompile(`(?is)<option\b[^>]*\bvalue="([^"]+)"`).FindStringSubmatch(selectMatch[2])
						if option != nil {
							value = stdhtml.UnescapeString(option[1])
						}
						break
					}
				}
				if value != "" {
					actions = append(actions, chromedp.SetValue(fieldSelector, value, chromedp.ByQuery))
				}
			}
		}
		if len(actions) > 0 {
			if err := chromedp.Run(ctx, actions...); err != nil {
				return fmt.Errorf("populate form for %s: %w", path, err)
			}
		}
		var invalidControls []string
		if err := chromedp.Run(ctx, chromedp.Evaluate(`Array.from(document.querySelectorAll("main form.tool-form :invalid")).map((element) => element.name || element.id || element.tagName.toLowerCase())`, &invalidControls)); err != nil {
			return fmt.Errorf("check native form validity for %s: %w", path, err)
		}
		if len(invalidControls) > 0 {
			return fmt.Errorf("form for %s remains invalid after sample values: %s", path, strings.Join(invalidControls, ", "))
		}
		button := selector + ` button[type="submit"]`
		previousLoad := rec.loadCount()
		if err := chromedp.Run(ctx, chromedp.Click(button, chromedp.ByQuery)); err != nil {
			return fmt.Errorf("click submit on %s: %w", path, err)
		}
		if scriptsDisabled {
			if err := rec.waitForLoadAfter(ctx, previousLoad); err != nil {
				return fmt.Errorf("wait for native form response on %s: %w (POST paths: %v)", path, err, rec.postPaths())
			}
		} else if err := waitForToolResult(ctx); err != nil {
			return fmt.Errorf("wait for submitted %s result: %w (responses: %v; console errors: %v)", path, err, rec.responseSummaries(path), rec.consoleErrors())
		}
		return nil
	}
	return fmt.Errorf("%s has no tool form to submit", path)
}

func waitForToolResult(ctx context.Context) error {
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for {
		var ready bool
		err := chromedp.Run(ctx, chromedp.Evaluate(`Array.from(document.querySelectorAll("main .tool-result, main .tool-result-image, main .tool-server-result-error, main [role=alert]")).some((element) => !element.hidden && (element.matches("img") ? element.hasAttribute("src") : element.textContent.trim() !== ""))`, &ready))
		if err == nil && ready {
			return nil
		}
		select {
		case <-ctx.Done():
			if err != nil {
				return err
			}
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func sampleValue(name, typ, attrs string) string {
	name = strings.ToLower(name)
	switch typ {
	case "number", "range":
		switch {
		case strings.Contains(name, "lat"):
			return "37.7749"
		case strings.Contains(name, "lon"), strings.Contains(name, "lng"):
			return "-122.4194"
		default:
			value := 1.0
			if min := attrPattern("min").FindStringSubmatch(attrs); min != nil {
				if lower, err := strconv.ParseFloat(min[1], 64); err == nil && value < lower {
					value = lower
				}
			}
			if max := attrPattern("max").FindStringSubmatch(attrs); max != nil {
				if upper, err := strconv.ParseFloat(max[1], 64); err == nil && value > upper {
					value = upper
				}
			}
			return strconv.FormatFloat(value, 'f', -1, 64)
		}
	case "date":
		return "2024-01-01"
	case "email":
		return "test@example.invalid"
	case "url":
		return "https://example.invalid/"
	case "password":
		return "EXAMPLE-password"
	}
	switch {
	case strings.Contains(name, "email"):
		return "test@example.invalid"
	case strings.Contains(name, "url"):
		return "https://example.invalid/"
	case strings.Contains(name, "domain"), strings.Contains(name, "host"):
		return "example.invalid"
	case strings.Contains(name, "ip"):
		return "8.8.8.8"
	case strings.Contains(name, "lat"):
		return "37.7749"
	case strings.Contains(name, "lon"), strings.Contains(name, "lng"):
		return "-122.4194"
	case strings.Contains(name, "json"), strings.Contains(name, "body"):
		return `{"example":"value"}`
	case strings.Contains(name, "date"):
		return "2024-01-01"
	case strings.Contains(name, "token"):
		return "EXAMPLE-token"
	default:
		return "example"
	}
}

func samplePlaceholder(name, placeholder string) string {
	value := strings.TrimSpace(stdhtml.UnescapeString(placeholder))
	if strings.Contains(strings.ToLower(name), "token") {
		return "eyJhbGciOiJub25lIn0.eyJzdWIiOiJ0ZXN0In0."
	}
	if strings.EqualFold(name, "value") && regexp.MustCompile(`(?i)^#?[0-9a-f]{6}$`).MatchString(value) && !strings.HasPrefix(value, "#") {
		return "#" + value
	}
	return value
}

func samplePNGFile() (string, error) {
	dir := os.Getenv("E2E_UPLOAD_ROOT")
	if dir == "" {
		dir = filepath.Join(suite.artifactDir, "uploads")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(dir, "e2e-upload.png")
	canvas := image.NewRGBA(image.Rect(0, 0, 2, 2))
	canvas.Set(0, 0, color.RGBA{R: 0xff, A: 0xff})
	canvas.Set(1, 0, color.RGBA{G: 0xff, A: 0xff})
	canvas.Set(0, 1, color.RGBA{B: 0xff, A: 0xff})
	canvas.Set(1, 1, color.RGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff})
	var content bytes.Buffer
	if err := png.Encode(&content, canvas); err != nil {
		return "", fmt.Errorf("encode E2E upload image: %w", err)
	}
	if err := os.WriteFile(path, content.Bytes(), 0644); err != nil {
		return "", err
	}
	return path, nil
}

func recordedPostTo(rec *pageRecorder, path string) bool {
	for _, postedPath := range rec.postPaths() {
		if strings.HasSuffix(postedPath, path) {
			return true
		}
	}
	return false
}

func recordedAPIRequest(rec *pageRecorder) bool {
	for _, raw := range rec.requestURLs() {
		parsed, err := url.Parse(raw)
		if err == nil && strings.Contains(parsed.Path, "/api/v1/") {
			return true
		}
	}
	return false
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

func TestTier2ProtectedDebug500PageWithoutJavaScript(t *testing.T) {
	ctx, _ := newTab(t, true)
	var html string
	if err := chromedp.Run(ctx,
		network.SetExtraHTTPHeaders(network.Headers{"Authorization": "Bearer " + suite.operatorToken}),
		chromedp.Navigate(suite.browserURL+"/debug/error500"),
		chromedp.WaitReady("body", chromedp.ByQuery),
		chromedp.OuterHTML("html", &html),
	); err != nil {
		saveArtifacts(t, ctx, "tier2-500")
		t.Fatalf("load the protected debug 500 page without JavaScript: %v", err)
	}
	if !strings.Contains(html, "500") || !strings.Contains(html, `<link rel="stylesheet"`) {
		saveArtifacts(t, ctx, "tier2-500")
		t.Error("protected debug 500 response is not a themed 500 page")
	}
	if strings.Contains(html, "goroutine") || strings.Contains(html, "panic:") {
		t.Error("protected debug 500 page leaks a stack trace")
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
