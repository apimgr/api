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
	assert.Contains(t, rec.Body.String(), "http://site.example/server/security/policy")
	assert.NotContains(t, rec.Body.String(), "http://site.example/crypto")
	assert.NotContains(t, rec.Body.String(), "http://site.example/server/contact")
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
