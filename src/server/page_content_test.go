package server

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/apimgr/api/src/config"
	"github.com/stretchr/testify/assert"
)

func TestOperatorHTMLUsesConfiguredTermsAndSanitizes(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Server.Branding.Content = map[string]config.PageContentConfig{
		"terms": {
			Format: "md",
			Body:   "## Terms for {app_name}\n\n<a href=\"javascript:alert(1)\">safe text</a><script>alert(2)</script>",
		},
	}
	req := requestWithServerConfig(httptest.NewRequest("GET", "/server/terms", nil), cfg)

	content := string(operatorHTML(req, "terms"))
	assert.Contains(t, content, "Terms for "+cfg.Server.Branding.Title)
	assert.Contains(t, content, "safe text")
	assert.NotContains(t, content, "javascript:")
	assert.NotContains(t, content, "<script")
	assert.NotContains(t, content, "alert(2)")
}

func TestOperatorHTMLUsesStrictBrandingPolicyAndExtendedDocumentPolicy(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Server.Branding.Content = map[string]config.PageContentConfig{
		"header": {Format: "html", Body: `<table><tr><td>hidden structure</td></tr></table><u>underlined</u><s>struck</s><small>small</small><a href="/server/contact?type=abuse">contact</a><a href="https://example.com" target="_blank">external</a>`},
		"about":  {Format: "md", Body: `<table><tr><td>document table</td></tr></table>`},
	}
	request := httptest.NewRequest("GET", "/server/about", nil)
	request = requestWithServerConfig(request, cfg)

	header := string(operatorHTML(request, "header"))
	assert.NotContains(t, header, "<table>")
	assert.NotContains(t, header, "/server/contact")
	assert.Contains(t, header, "<u>underlined</u>")
	assert.Contains(t, header, "<s>struck</s>")
	assert.Contains(t, header, "<small>small</small>")
	assert.Contains(t, header, `rel="noopener noreferrer"`)
	assert.Contains(t, string(operatorHTML(request, "about")), "<table>")
}

func TestOperatorContentSourceEnforcesEntryFormatsAndFallback(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Server.Branding.Content = map[string]config.PageContentConfig{
		"header": {Format: "md", Body: "not allowed in a header"},
		"terms":  {Format: "md", Body: map[string]interface{}{"default": "Default terms", "fr": "Conditions"}},
	}
	request := httptest.NewRequest("GET", "/server/terms", nil)
	request.Header.Set("Accept-Language", "fr-CA,fr;q=0.8")
	request = requestWithServerConfig(request, cfg)

	_, _, ok := operatorContentSource(request, "header")
	assert.False(t, ok)
	body, format, ok := operatorContentSource(request, "terms")
	assert.True(t, ok)
	assert.Equal(t, "md", format)
	assert.Equal(t, "Conditions", body)
	_, _, ok = operatorContentSource(request, "unknown")
	assert.False(t, ok)
}

func TestNewPageDataAppliesOperatorExtensionsToEveryLayoutAndMatchingPage(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Server.Branding.Content = map[string]config.PageContentConfig{
		"header":  {Format: "html", Body: `<span>Header brand</span>`},
		"footer":  {Format: "html", Body: `<span>Footer brand</span>`},
		"about":   {Format: "md", Body: "About extension"},
		"privacy": {Format: "md", Body: "Privacy extension"},
	}
	request := httptest.NewRequest("GET", "/server/about", nil)
	about := newPageData(cfg, request, "about")
	assert.Contains(t, string(about.HeaderExtension), "Header brand")
	assert.Contains(t, string(about.FooterExtension), "Footer brand")
	assert.Contains(t, string(about.AboutExtension), "About extension")
	assert.Empty(t, about.PrivacyExtension)

	privacy := newPageData(cfg, request, "privacy")
	assert.Contains(t, string(privacy.PrivacyExtension), "Privacy extension")
}

func TestDefaultTermsNameUSLawAndSubstituteApplicationName(t *testing.T) {
	content := termsMarkdown("Example API")
	assert.Contains(t, content, "Using Example API means accepting these terms")
	assert.Contains(t, content, "laws of the United States")
	assert.NotContains(t, content, "{app_name}")
	assert.True(t, strings.Contains(content, "security policy at /server/security/policy"))
}
