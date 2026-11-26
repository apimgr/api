package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/apimgr/api/src/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSitemapHandlerUsesRequestOriginAndPublicRoutes(t *testing.T) {
	t.Setenv("DOMAIN", "")
	cfg := config.DefaultConfig()
	cfg.Server.Port = ""
	cfg.Web.Robots.Deny = append(cfg.Web.Robots.Deny, "/crypto")
	req := httptest.NewRequest(http.MethodGet, "/sitemap.xml", nil)
	req.Host = "site.example"
	rec := httptest.NewRecorder()

	sitemapHandler(cfg).ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Header().Get("Content-Type"), "application/xml")
	assert.Contains(t, rec.Body.String(), "http://site.example/")
	assert.Contains(t, rec.Body.String(), "http://site.example/server/docs/swagger")
	assert.Contains(t, rec.Body.String(), "http://site.example/server/terms")
	assert.Contains(t, rec.Body.String(), "http://site.example/server/dpo")
	assert.Contains(t, rec.Body.String(), "http://site.example/server/security/policy")
	assert.Contains(t, rec.Body.String(), "http://site.example/server/help/mcp")
	assert.NotContains(t, rec.Body.String(), "http://site.example/crypto")
	assert.NotContains(t, rec.Body.String(), "http://site.example/server/contact")
	assert.NotContains(t, rec.Body.String(), "http://site.example/server/preferences/sync")
	assert.NotContains(t, rec.Body.String(), "http://site.example/server/preferences/tokens")
	assert.NotContains(t, rec.Body.String(), "http://site.example/api/")
}

func TestFeedHandlerReturnsCachedValidEmptyFeed(t *testing.T) {
	t.Setenv("DOMAIN", "")
	cfg := config.DefaultConfig()
	cfg.Server.Port = ""
	cfg.Server.Branding.Title = "API & tools"
	cfg.Server.Branding.Description = "Utilities & tools"
	handler := feedHandler(cfg, false)
	firstRequest := httptest.NewRequest(http.MethodGet, "/feed.xml", nil)
	firstRequest.Host = "site.example"
	firstResponse := httptest.NewRecorder()
	handler.ServeHTTP(firstResponse, firstRequest)

	require.Equal(t, http.StatusOK, firstResponse.Code)
	assert.Contains(t, firstResponse.Header().Get("Content-Type"), "application/atom+xml")
	assert.Equal(t, "public, max-age=300", firstResponse.Header().Get("Cache-Control"))
	assert.NotEmpty(t, firstResponse.Header().Get("ETag"))
	assert.NotEmpty(t, firstResponse.Header().Get("Last-Modified"))
	assert.Contains(t, firstResponse.Body.String(), "<title>API &amp; tools</title>")
	assert.Contains(t, firstResponse.Body.String(), "http://site.example/feed.xml")
	assert.NotContains(t, firstResponse.Body.String(), "<entry>")

	conditional := httptest.NewRequest(http.MethodGet, "/feed.xml", nil)
	conditional.Host = "site.example"
	conditional.Header.Set("If-None-Match", firstResponse.Header().Get("ETag"))
	conditionalResponse := httptest.NewRecorder()
	handler.ServeHTTP(conditionalResponse, conditional)
	assert.Equal(t, http.StatusNotModified, conditionalResponse.Code)
	assert.Empty(t, conditionalResponse.Body.String())

	headRequest := httptest.NewRequest(http.MethodHead, "/feed.xml", nil)
	headRequest.Host = "site.example"
	headResponse := httptest.NewRecorder()
	handler.ServeHTTP(headResponse, headRequest)
	assert.Equal(t, http.StatusOK, headResponse.Code)
	assert.Empty(t, headResponse.Body.String())
	assert.True(t, strings.Contains(headResponse.Header().Get("Content-Type"), "application/atom+xml"))
}

func TestRSSHandlerUsesRSSContentType(t *testing.T) {
	t.Setenv("DOMAIN", "")
	req := httptest.NewRequest(http.MethodGet, "/rss.xml", nil)
	req.Host = "site.example"
	rec := httptest.NewRecorder()
	cfg := config.DefaultConfig()
	cfg.Server.Port = ""
	feedHandler(cfg, true).ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Header().Get("Content-Type"), "application/rss+xml")
	assert.Contains(t, rec.Body.String(), "<rss version=\"2.0\">")
}

func TestCanonicalPageURLAndSEOValidation(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/server/about?preview=1", nil)
	req.Host = "site.example"
	assert.Equal(t, "https://canonical.example/base/server/about", canonicalPageURL(req, "https://canonical.example/base"))
	assert.Equal(t, "http://site.example/server/about", canonicalPageURL(req, "http://insecure.example"))

	meta := validSEOMetaTags(map[string]string{
		"theme-color": "#123456",
		"bad name":    "ignored",
		"robots":      "index,follow",
	})
	assert.Equal(t, map[string]string{"theme-color": "#123456"}, meta)

	tags := validSEOVerification(config.VerificationConfig{
		Google: "valid_123-code",
		Bing:   "ABCDEF0123456789",
		Yandex: "ABCDEF",
		Custom: []config.CustomVerificationTag{
			{Name: "service-check", Content: "ok"},
			{Name: "bad name", Content: "ignored"},
			{Name: "name", Property: "property", Content: "ignored"},
		},
	})
	require.Len(t, tags, 3)
	assert.Equal(t, "google-site-verification", tags[0].Name)
	assert.Equal(t, "msvalidate.01", tags[1].Name)
	assert.Equal(t, "service-check", tags[2].Name)
}
