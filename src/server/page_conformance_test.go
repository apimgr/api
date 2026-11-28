package server

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/apimgr/api/src/config"
	"github.com/apimgr/api/src/database"
	"github.com/apimgr/api/src/paths"
	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	nethtml "golang.org/x/net/html"
)

// TestServerPageConformance checks every public server page specified by the
// standard-page routes, the pages in the help registry, and each shared error
// response against the common server-rendered page contract in AI.md PART 16.
func TestServerPageConformance(t *testing.T) {
	cfg := newTestConfig(t)
	cfg.Server.RateLimit.Read.Requests = 10000
	cfg.Server.RateLimit.GlobalBurst = 10000
	require.NoError(t, database.InitDatabase(cfg.Server.Database, paths.DataDir()))
	t.Cleanup(func() { _ = database.Close() })
	require.NoError(t, database.EnsureAppSecrets(t.Context()))
	require.NoError(t, database.EnsureCaptchaSchema(t.Context()))
	require.NoError(t, database.EnsureRateLimitSchema(t.Context()))
	srv := newTestServer(t, cfg)

	pagePaths := []string{
		"/server/about",
		"/server/privacy",
		"/server/contact",
		"/server/help",
		"/server/terms",
		"/server/dpo",
		"/server/security",
		"/server/security/policy",
		"/server/security/thanks",
		"/server/healthz",
		"/server/docs/swagger",
		"/server/docs/graphql",
		"/server/preferences",
	}
	statusToken := "conformance-status-token"
	statusTokenHash := sha256.Sum256([]byte(statusToken))
	trackingDigest := sha256.Sum256([]byte(t.Name()))
	trackingID := "sec_" + hex.EncodeToString(trackingDigest[:8])
	report := database.SecurityReportInput{
		TrackingID: trackingID,
		IDWindow:   time.Now().UTC().Unix() / (48 * 60 * 60),
		Severity:   "low",
		Body:       []byte("encrypted-body-fixture"),
		BodyAlg:    "pgp",
		Contact:    []byte("encrypted-contact-fixture"),
		CreditPref: "anon",
		TokenHash:  hex.EncodeToString(statusTokenHash[:]),
	}
	require.NoError(t, database.StoreSecurityReport(t.Context(), report))
	statusURL := "/server/security/report/" + trackingID + "?token=" + url.QueryEscape(statusToken)
	pagePaths = append(pagePaths, statusURL)
	registry := registeredServerPages("en")
	assertHubRouteInventory(t, srv, registry)
	for _, page := range append(registry.Help, registry.Preferences...) {
		pagePaths = append(pagePaths, page.Path)
	}
	pagePaths = uniquePagePaths(pagePaths)

	var aboutHTML string
	for _, pagePath := range pagePaths {
		pagePath := pagePath
		t.Run(pagePath, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, pagePath, nil)
			req.Header.Set("Accept", "text/html")
			rec := httptest.NewRecorder()
			srv.Handler.ServeHTTP(rec, req)
			require.Equal(t, http.StatusOK, rec.Code)
			require.Contains(t, rec.Header().Get("Content-Type"), "text/html")
			body := rec.Body.String()
			if pagePath == statusURL {
				assert.Equal(t, "no-store", rec.Header().Get("Cache-Control"))
				assert.Equal(t, "no-referrer", rec.Header().Get("Referrer-Policy"))
				assert.NotContains(t, body, statusToken)
			}
			assert.Equal(t, 1, countLinkHrefInFooter(body, registry.HelpHubPath), "footer must link to the registered help hub: %s", pagePath)
			assert.Equal(t, 1, countLinkHrefInFooter(body, registry.PreferencesHubPath), "footer must link to the registered preferences hub: %s", pagePath)
			if pagePath == "/server/about" {
				aboutHTML = body
			}
			assertServerHTMLConformance(t, srv, pagePath, body, false)
			assertHubBreadcrumbConformance(t, pagePath, body, registry)
		})
	}

	helpResponse := requestServerPage(t, srv.Handler, "/server/help")
	for _, page := range registry.Help {
		assert.Equal(t, 1, countLinkHref(helpResponse, page.Path), "help registry entry must appear once on its hub: %s", page.Path)
	}
	helpAPIResponse := requestServerPage(t, srv.Handler, APIBasePath(cfg)+"/server/help")
	for _, page := range registry.Help {
		assert.Equal(t, 1, strings.Count(helpAPIResponse, page.Path), "help API must list its registered page once: %s", page.Path)
	}
	preferencesResponse := requestServerPage(t, srv.Handler, "/server/preferences")
	for _, page := range registry.Preferences {
		assert.Equal(t, 1, countLinkHref(preferencesResponse, page.Path), "preferences page link must appear once on its hub: %s", page.Path)
	}

	for _, tt := range []struct {
		name   string
		method string
		path   string
		status int
	}{
		{name: "not found", method: http.MethodGet, path: "/missing-page", status: http.StatusNotFound},
		{name: "method not allowed", method: http.MethodTrace, path: "/server/about", status: http.StatusMethodNotAllowed},
	} {
		t.Run("error/"+tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.path, nil)
			req.Header.Set("Accept", "text/html")
			rec := httptest.NewRecorder()
			srv.Handler.ServeHTTP(rec, req)
			require.Equal(t, tt.status, rec.Code)
			assertServerHTMLConformance(t, srv, tt.path, rec.Body.String(), false)
		})
	}

	t.Run("error/internal server error", func(t *testing.T) {
		rec := httptest.NewRecorder()
		data := newPageData(cfg, httptest.NewRequest(http.MethodGet, "/server/about", nil), "error")
		data.CSRFToken = "conformance-token"
		renderServerError(rec, data)
		require.Equal(t, http.StatusInternalServerError, rec.Code)
		assertServerHTMLConformance(t, srv, "/server/about", rec.Body.String(), false)
	})

	t.Run("header branding fragment", func(t *testing.T) {
		cfg.Server.Branding.Content = map[string]config.PageContentConfig{
			"header": {Format: "html", Body: "<span>Conformance header extension</span>"},
		}
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/server/about", nil)
		req.Header.Set("Accept", "text/html")
		req.AddCookie(&http.Cookie{Name: "csrf_token", Value: "conformance-token"})
		srv.Handler.ServeHTTP(rec, req)
		require.Equal(t, http.StatusOK, rec.Code)
		body := rec.Body.String()
		assertServerHTMLConformance(t, srv, "/server/about", body, true)
		assert.Equal(t, sharedHeaderContainer(t, aboutHTML), sharedHeaderContainer(t, body), "configured branding must not alter the shared header")
		assert.True(t, strings.Contains(headerBody(t, body), "Conformance header extension"), "branding fragment must be last in the header")
	})
}

func assertHubRouteInventory(t *testing.T, srv *http.Server, registry serverPageRegistry) {
	t.Helper()
	routes, ok := srv.Handler.(chi.Routes)
	require.True(t, ok, "server handler must expose its Chi route inventory")
	pagePaths := map[string]struct{}{
		registry.HelpHubPath:        {},
		registry.PreferencesHubPath: {},
	}
	for _, page := range append(registry.Help, registry.Preferences...) {
		pagePaths[page.Path] = struct{}{}
	}
	actionRoutes := map[string]struct{}{
		"POST /server/preferences":                    {},
		"POST /server/preferences/sync":               {},
		"GET /server/preferences/sync/import":         {},
		"POST /server/preferences/token/create":       {},
		"POST /server/preferences/token/restore":      {},
		"POST /server/preferences/token/reveal":       {},
		"POST /server/preferences/token/rotate":       {},
		"POST /server/preferences/token/revoke":       {},
		"POST /server/preferences/token/forget":       {},
		"POST /server/preferences/tokens":             {},
		"POST /server/preferences/tokens/{id}/reveal": {},
		"POST /server/preferences/tokens/{id}/rotate": {},
		"POST /server/preferences/tokens/{id}/update": {},
		"POST /server/preferences/tokens/{id}/revoke": {},
	}
	err := chi.Walk(routes, func(method, path string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		if !strings.HasPrefix(path, helpHubPath+"/") && !strings.HasPrefix(path, preferencesHubPath+"/") && path != helpHubPath && path != preferencesHubPath {
			return nil
		}
		if _, page := pagePaths[path]; page && method == http.MethodGet {
			return nil
		}
		if _, action := actionRoutes[method+" "+path]; action {
			return nil
		}
		assert.Fail(t, "hub route must be registered as a page or an action route", method+" "+path)
		return nil
	})
	require.NoError(t, err)
}

func assertHubBreadcrumbConformance(t *testing.T, pagePath, body string, registry serverPageRegistry) {
	t.Helper()
	hubPath := ""
	for _, page := range registry.Help {
		if page.Path == pagePath {
			hubPath = registry.HelpHubPath
		}
	}
	for _, page := range registry.Preferences {
		if page.Path == pagePath {
			hubPath = registry.PreferencesHubPath
		}
	}
	if pagePath == registry.HelpHubPath || pagePath == registry.PreferencesHubPath {
		assert.Equal(t, 0, countClassElements(body, "nav", "breadcrumb"), "hub pages must not have a breadcrumb: %s", pagePath)
		return
	}
	if hubPath == "" {
		return
	}
	assert.Equal(t, 1, countClassElements(body, "nav", "breadcrumb"), "registered sub-pages must have exactly one breadcrumb: %s", pagePath)
	assert.Equal(t, 1, countLinksWithinClass(body, "breadcrumb", hubPath), "breadcrumb must link exactly once to its hub: %s", pagePath)
}

func countClassElements(body, elementName, className string) int {
	doc, err := nethtml.Parse(strings.NewReader(body))
	if err != nil {
		return 0
	}
	count := 0
	var walk func(*nethtml.Node)
	walk = func(node *nethtml.Node) {
		if node.Type == nethtml.ElementNode && node.Data == elementName && strings.Contains(" "+htmlAttributes(node)["class"]+" ", " "+className+" ") {
			count++
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(doc)
	return count
}

func countLinksWithinClass(body, className, href string) int {
	doc, err := nethtml.Parse(strings.NewReader(body))
	if err != nil {
		return 0
	}
	count := 0
	var walk func(*nethtml.Node, bool)
	walk = func(node *nethtml.Node, within bool) {
		if node.Type == nethtml.ElementNode {
			attrs := htmlAttributes(node)
			within = within || strings.Contains(" "+attrs["class"]+" ", " "+className+" ")
			if within && node.Data == "a" && attrs["href"] == href {
				count++
			}
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child, within)
		}
	}
	walk(doc, false)
	return count
}

func assertServerHTMLConformance(t *testing.T, srv *http.Server, pagePath, body string, headerExtension bool) {
	t.Helper()
	doc, err := nethtml.Parse(strings.NewReader(body))
	require.NoError(t, err)

	ids := make(map[string]bool)
	mainCount, h1Count, pageTitleCount := 0, 0, 0
	pageTitleText := ""
	var cards []*nethtml.Node
	var headings []*nethtml.Node
	var tables, links, forms, controls []*nethtml.Node
	var images []*nethtml.Node
	var focusables []*nethtml.Node
	var skipLinks []*nethtml.Node
	var walk func(*nethtml.Node)
	walk = func(node *nethtml.Node) {
		if node.Type == nethtml.ElementNode {
			attrs := htmlAttributes(node)
			if id := attrs["id"]; id != "" {
				ids[id] = true
			}
			switch node.Data {
			case "main":
				mainCount++
			case "h1":
				h1Count++
				if strings.Contains(" "+attrs["class"]+" ", " page-title ") {
					pageTitleCount++
					pageTitleText = strings.TrimSpace(nodeText(node))
				}
			case "h2":
				headings = append(headings, node)
			case "style":
				assert.Fail(t, "page contains a style element", pagePath)
			case "script":
				if attrs["src"] == "" && attrs["type"] != "application/ld+json" && strings.TrimSpace(nodeText(node)) != "" {
					assert.Fail(t, "page contains an executable inline script", pagePath)
				}
			case "img":
				images = append(images, node)
			case "table":
				tables = append(tables, node)
			case "a":
				links = append(links, node)
				if strings.Contains(" "+attrs["class"]+" ", " skip-link ") {
					skipLinks = append(skipLinks, node)
				}
			}
			if strings.Contains(" "+attrs["class"]+" ", " section-card ") {
				cards = append(cards, node)
			}
			for name := range attrs {
				if strings.HasPrefix(strings.ToLower(name), "on") {
					assert.Fail(t, "page contains an inline event handler", pagePath, name)
				}
			}
			if _, ok := attrs["style"]; ok {
				assert.Fail(t, "page contains an inline style attribute", pagePath)
			}
			if node.Data == "form" {
				forms = append(forms, node)
			}
			if node.Data == "input" || node.Data == "select" || node.Data == "textarea" {
				controls = append(controls, node)
			}
			if isFocusable(node, attrs) {
				focusables = append(focusables, node)
			}
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(doc)

	assert.Equal(t, 1, mainCount, "page must have exactly one main element: %s", pagePath)
	assert.Equal(t, 1, h1Count, "page must have exactly one h1: %s", pagePath)
	assert.Equal(t, 1, pageTitleCount, "the h1 must use .page-title: %s", pagePath)
	for _, heading := range headings {
		assert.True(t, hasClassAncestor(heading, "section-card"), "h2 must label a section card: %s", pagePath)
	}
	for _, card := range cards {
		var cardHeadings []*nethtml.Node
		for child := card.FirstChild; child != nil; child = child.NextSibling {
			if child.Type == nethtml.ElementNode && child.Data == "h2" {
				cardHeadings = append(cardHeadings, child)
			}
		}
		require.Len(t, cardHeadings, 1, "each section card must have one direct h2: %s", pagePath)
		headingAttrs := htmlAttributes(cardHeadings[0])
		assert.Equal(t, headingAttrs["id"], htmlAttributes(card)["aria-labelledby"], "section card must be labelled by its h2: %s", pagePath)
		assert.NotEqual(t, pageTitleText, strings.TrimSpace(nodeText(cardHeadings[0])), "page title must not repeat in a card: %s", pagePath)
	}
	require.GreaterOrEqual(t, len(focusables), 2, "skip links must be present first: %s", pagePath)
	require.GreaterOrEqual(t, len(skipLinks), 2, "both skip links must be present: %s", pagePath)
	assert.Same(t, focusables[0], skipLinks[0], "first focusable element must be the main skip link: %s", pagePath)
	assert.Same(t, focusables[1], skipLinks[1], "second focusable element must be the navigation skip link: %s", pagePath)
	assert.Equal(t, "#main-content", htmlAttributes(skipLinks[0])["href"], pagePath)
	assert.Equal(t, "#navigation", htmlAttributes(skipLinks[1])["href"], pagePath)
	assertHeaderConformance(t, doc, pagePath, headerExtension)

	for _, image := range images {
		_, hasAlt := htmlAttributes(image)["alt"]
		assert.True(t, hasAlt, "image must have alt attribute: %s", pagePath)
	}
	for _, control := range controls {
		attrs := htmlAttributes(control)
		if attrs["type"] == "hidden" || attrs["type"] == "submit" || attrs["type"] == "button" || attrs["type"] == "reset" {
			continue
		}
		assert.True(t, hasAssociatedLabel(doc, control), "form control must have a label: %s (%s)", pagePath, attrs["name"])
	}
	for _, form := range forms {
		if strings.EqualFold(htmlAttributes(form)["method"], http.MethodPost) {
			assert.True(t, hasNamedInput(form, "csrf_token"), "POST form must carry csrf_token: %s", pagePath)
		}
	}
	if pagePath == "/server/preferences" {
		var postForms []*nethtml.Node
		for _, form := range forms {
			if strings.EqualFold(htmlAttributes(form)["method"], http.MethodPost) {
				postForms = append(postForms, form)
			}
		}
		require.Len(t, postForms, 1, "preferences page must have exactly one POST form: %s", pagePath)
		assert.Equal(t, "/server/preferences", htmlAttributes(postForms[0])["action"], "preferences page POST form must save its own values")
	}
	for _, table := range tables {
		assert.True(t, hasElementDescendant(table, "thead"), "table must have a thead: %s", pagePath)
		assert.True(t, hasClassAncestor(table, "table-wrapper"), "table must be inside .table-wrapper: %s", pagePath)
	}

	for _, link := range links {
		href := htmlAttributes(link)["href"]
		if href == "" {
			continue
		}
		u, parseErr := url.Parse(href)
		if parseErr != nil {
			continue
		}
		if u.IsAbs() || strings.HasPrefix(href, "//") {
			baseRequest := httptest.NewRequest(http.MethodGet, pagePath, nil)
			base, _ := url.Parse(BuildURL(baseRequest, ""))
			if base == nil || !strings.EqualFold(u.Host, base.Host) {
				continue
			}
		}
		if u.Path == "" && u.Fragment != "" {
			assert.True(t, ids[u.Fragment], "fragment link must resolve: %s -> %s", pagePath, href)
			continue
		}
		path := u.Path
		if path == "" {
			path = pagePath
		}
		if !strings.HasPrefix(path, "/") {
			path = "/" + path
		}
		target := path
		if u.RawQuery != "" {
			target += "?" + u.RawQuery
		}
		req := httptest.NewRequest(http.MethodGet, target, nil)
		req.Header.Set("Accept", "text/html")
		rec := httptest.NewRecorder()
		srv.Handler.ServeHTTP(rec, req)
		assert.Less(t, rec.Code, 400, "internal link must resolve: %s -> %s", pagePath, href)
		if u.Fragment != "" && rec.Code < 400 {
			assert.True(t, responseHasID(rec.Body.String(), u.Fragment), "internal fragment must resolve: %s -> %s", pagePath, href)
		}
	}
}

func requestServerPage(t *testing.T, srv http.Handler, path string) string {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Header.Set("Accept", "text/html")
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	require.Less(t, rec.Code, 400, "page request must succeed: %s", path)
	return rec.Body.String()
}

func countLinkHref(body, href string) int {
	doc, err := nethtml.Parse(strings.NewReader(body))
	if err != nil {
		return 0
	}
	count := 0
	var walk func(*nethtml.Node)
	walk = func(node *nethtml.Node) {
		if node.Type == nethtml.ElementNode && node.Data == "a" && htmlAttributes(node)["href"] == href {
			count++
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(doc)
	return count
}

func countLinkHrefInFooter(body, href string) int {
	doc, err := nethtml.Parse(strings.NewReader(body))
	if err != nil {
		return 0
	}
	count := 0
	var walk func(*nethtml.Node, bool)
	walk = func(node *nethtml.Node, inFooter bool) {
		if node.Type == nethtml.ElementNode {
			attrs := htmlAttributes(node)
			inFooter = inFooter || strings.Contains(" "+attrs["class"]+" ", " footer ")
			if inFooter && node.Data == "a" && attrs["href"] == href {
				count++
			}
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child, inFooter)
		}
	}
	walk(doc, false)
	return count
}

func responseHasID(body, id string) bool {
	doc, err := nethtml.Parse(strings.NewReader(body))
	if err != nil {
		return false
	}
	var found bool
	var walk func(*nethtml.Node)
	walk = func(node *nethtml.Node) {
		if node.Type == nethtml.ElementNode && htmlAttributes(node)["id"] == id {
			found = true
			return
		}
		for child := node.FirstChild; child != nil && !found; child = child.NextSibling {
			walk(child)
		}
	}
	walk(doc)
	return found
}

func assertHeaderConformance(t *testing.T, doc *nethtml.Node, pagePath string, headerExtension bool) {
	t.Helper()
	var header *nethtml.Node
	var findHeader func(*nethtml.Node)
	findHeader = func(node *nethtml.Node) {
		if node.Type == nethtml.ElementNode && node.Data == "header" && strings.Contains(" "+htmlAttributes(node)["class"]+" ", " header ") {
			header = node
			return
		}
		for child := node.FirstChild; child != nil && header == nil; child = child.NextSibling {
			findHeader(child)
		}
	}
	findHeader(doc)
	require.NotNil(t, header, "shared header must exist: %s", pagePath)
	headerChildren := elementChildren(header)
	wantHeaderChildren := 1
	if headerExtension {
		wantHeaderChildren = 2
	}
	require.Len(t, headerChildren, wantHeaderChildren, "header must contain its shared shell and optional branding fragment only: %s", pagePath)
	if headerExtension {
		assert.Contains(t, nodeText(headerChildren[1]), "Conformance header extension", pagePath)
	}
	container := headerChildren[0]
	assert.True(t, strings.Contains(" "+htmlAttributes(container)["class"]+" ", " container "), "header shell must use the shared container: %s", pagePath)
	children := elementChildren(container)
	require.Len(t, children, 4, "header must contain only brand, navigation, hamburger, and actions in order: %s", pagePath)
	assert.Equal(t, "a", children[0].Data, pagePath)
	assert.True(t, strings.Contains(" "+htmlAttributes(children[0])["class"]+" ", " site-brand "), pagePath)
	assert.Equal(t, "nav", children[1].Data, pagePath)
	assert.True(t, strings.Contains(" "+htmlAttributes(children[1])["class"]+" ", " nav-links "), pagePath)
	assert.Equal(t, "label", children[2].Data, pagePath)
	assert.True(t, strings.Contains(" "+htmlAttributes(children[2])["class"]+" ", " nav-toggle "), pagePath)
	assert.Equal(t, "div", children[3].Data, pagePath)
	assert.True(t, strings.Contains(" "+htmlAttributes(children[3])["class"]+" ", " header-actions "), pagePath)
	actions := elementChildren(children[3])
	wantActions := 2
	if pagePath == "/server/preferences" {
		wantActions = 1
	}
	require.Len(t, actions, wantActions, "header actions must contain the documented preferences and theme controls: %s", pagePath)
	assert.Equal(t, "a", actions[0].Data, pagePath)
	if pagePath != "/server/preferences" {
		assert.Equal(t, "form", actions[1].Data, pagePath)
		assert.Equal(t, "POST", strings.ToUpper(htmlAttributes(actions[1])["method"]), pagePath)
	}
}

func sharedHeaderContainer(t *testing.T, body string) string {
	t.Helper()
	doc, err := nethtml.Parse(strings.NewReader(body))
	require.NoError(t, err)
	header := findSharedHeader(doc)
	require.NotNil(t, header)
	children := elementChildren(header)
	require.NotEmpty(t, children)
	var redactCSRF func(*nethtml.Node)
	redactCSRF = func(node *nethtml.Node) {
		if node.Type == nethtml.ElementNode && node.Data == "input" && htmlAttributes(node)["name"] == "csrf_token" {
			attrs := node.Attr[:0]
			for _, attr := range node.Attr {
				if attr.Key != "value" {
					attrs = append(attrs, attr)
				}
			}
			node.Attr = attrs
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			redactCSRF(child)
		}
	}
	redactCSRF(children[0])
	var rendered strings.Builder
	require.NoError(t, nethtml.Render(&rendered, children[0]))
	return rendered.String()
}

func headerBody(t *testing.T, body string) string {
	t.Helper()
	doc, err := nethtml.Parse(strings.NewReader(body))
	require.NoError(t, err)
	header := findSharedHeader(doc)
	require.NotNil(t, header)
	return nodeText(header)
}

func findSharedHeader(doc *nethtml.Node) *nethtml.Node {
	if doc.Type == nethtml.ElementNode && doc.Data == "header" && strings.Contains(" "+htmlAttributes(doc)["class"]+" ", " header ") {
		return doc
	}
	for child := doc.FirstChild; child != nil; child = child.NextSibling {
		if found := findSharedHeader(child); found != nil {
			return found
		}
	}
	return nil
}

func elementChildren(node *nethtml.Node) []*nethtml.Node {
	children := make([]*nethtml.Node, 0)
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		if child.Type == nethtml.ElementNode {
			children = append(children, child)
		}
	}
	return children
}

func htmlAttributes(node *nethtml.Node) map[string]string {
	attrs := make(map[string]string, len(node.Attr))
	for _, attr := range node.Attr {
		attrs[strings.ToLower(attr.Key)] = attr.Val
	}
	return attrs
}

func nodeText(node *nethtml.Node) string {
	var text strings.Builder
	var walk func(*nethtml.Node)
	walk = func(current *nethtml.Node) {
		if current.Type == nethtml.TextNode {
			text.WriteString(current.Data)
		}
		for child := current.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(node)
	return text.String()
}

func isFocusable(node *nethtml.Node, attrs map[string]string) bool {
	if _, ok := attrs["disabled"]; ok {
		return false
	}
	if _, ok := attrs["tabindex"]; ok {
		return true
	}
	switch node.Data {
	case "a", "button", "input", "select", "textarea", "summary":
		if node.Data == "a" {
			return attrs["href"] != ""
		}
		return attrs["type"] != "hidden"
	default:
		return false
	}
}

func hasAssociatedLabel(doc, control *nethtml.Node) bool {
	attrs := htmlAttributes(control)
	if id := attrs["id"]; id != "" && hasLabelFor(doc, id) {
		return true
	}
	for parent := control.Parent; parent != nil; parent = parent.Parent {
		if parent.Type == nethtml.ElementNode && parent.Data == "label" {
			return true
		}
	}
	return false
}

func hasLabelFor(node *nethtml.Node, id string) bool {
	if node.Type == nethtml.ElementNode && node.Data == "label" && htmlAttributes(node)["for"] == id {
		return true
	}
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		if hasLabelFor(child, id) {
			return true
		}
	}
	return false
}

func hasNamedInput(form *nethtml.Node, name string) bool {
	if form.Type == nethtml.ElementNode && form.Data == "input" {
		attrs := htmlAttributes(form)
		if attrs["name"] == name {
			return true
		}
	}
	for child := form.FirstChild; child != nil; child = child.NextSibling {
		if hasNamedInput(child, name) {
			return true
		}
	}
	return false
}

func hasElementDescendant(node *nethtml.Node, name string) bool {
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		if child.Type == nethtml.ElementNode && child.Data == name || hasElementDescendant(child, name) {
			return true
		}
	}
	return false
}

func hasClassAncestor(node *nethtml.Node, class string) bool {
	for parent := node.Parent; parent != nil; parent = parent.Parent {
		if strings.Contains(" "+htmlAttributes(parent)["class"]+" ", " "+class+" ") {
			return true
		}
	}
	return false
}

func uniquePagePaths(paths []string) []string {
	seen := make(map[string]bool, len(paths))
	unique := make([]string, 0, len(paths))
	for _, path := range paths {
		if !seen[path] {
			seen[path] = true
			unique = append(unique, path)
		}
	}
	return unique
}
