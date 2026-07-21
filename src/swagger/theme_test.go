package swagger

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// generateSwaggerHTML must embed the spec URL and the dark theme colors by
// default.
func TestGenerateSwaggerHTMLDark(t *testing.T) {
	html := generateSwaggerHTML("/openapi.json", "dark")
	assert.Contains(t, html, "url: '/openapi.json'")
	assert.Contains(t, html, "#1e1e1e")
	assert.Contains(t, html, "<title>API Documentation - Swagger UI</title>")
}

// The light theme must swap in the light palette instead of the dark one.
func TestGenerateSwaggerHTMLLight(t *testing.T) {
	html := generateSwaggerHTML("/openapi.json", "light")
	assert.Contains(t, html, "#ffffff; color: #1e1e1e")
	assert.NotContains(t, html, "#1e1e1e; color: #d4d4d4")
}

// The auto theme must wrap both palettes in prefers-color-scheme media
// queries rather than picking one outright.
func TestGenerateSwaggerHTMLAuto(t *testing.T) {
	html := generateSwaggerHTML("/openapi.json", "auto")
	assert.Contains(t, html, "@media (prefers-color-scheme: dark)")
	assert.Contains(t, html, "@media (prefers-color-scheme: light)")
}

// An unrecognized theme value must fall back to the dark palette, since
// themeCSS is only ever reassigned for "light" and "auto".
func TestGenerateSwaggerHTMLUnknownFallsBackToDark(t *testing.T) {
	html := generateSwaggerHTML("/openapi.json", "not-a-theme")
	assert.Contains(t, html, "#1e1e1e; color: #d4d4d4")
}

// ServeUI must default to the dark theme when no cookie is present.
func TestServeUINoCookie(t *testing.T) {
	handler := ServeUI("/spec.json")
	req := httptest.NewRequest(http.MethodGet, "/docs", nil)
	rec := httptest.NewRecorder()
	handler(rec, req)

	res := rec.Result()
	assert.Equal(t, http.StatusOK, res.StatusCode)
	assert.Equal(t, "text/html; charset=utf-8", res.Header.Get("Content-Type"))

	body := rec.Body.String()
	assert.Contains(t, body, "#1e1e1e; color: #d4d4d4")
	assert.Contains(t, body, "url: '/spec.json'")
}

// ServeUI must honor an explicit "light" theme cookie.
func TestServeUILightCookie(t *testing.T) {
	handler := ServeUI("/spec.json")
	req := httptest.NewRequest(http.MethodGet, "/docs", nil)
	req.AddCookie(&http.Cookie{Name: "theme", Value: "light"})
	rec := httptest.NewRecorder()
	handler(rec, req)

	body := rec.Body.String()
	assert.Contains(t, body, "#ffffff; color: #1e1e1e")
}

// An invalid cookie value must not crash the handler and must keep the
// default dark theme (the switch statement has no matching case for it).
func TestServeUIInvalidCookieValue(t *testing.T) {
	handler := ServeUI("/spec.json")
	req := httptest.NewRequest(http.MethodGet, "/docs", nil)
	req.AddCookie(&http.Cookie{Name: "theme", Value: "sepia"})
	rec := httptest.NewRecorder()
	handler(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	assert.True(t, strings.Contains(rec.Body.String(), "#1e1e1e; color: #d4d4d4"))
}
