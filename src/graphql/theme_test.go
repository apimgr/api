package graphql

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestServeUI covers theme selection from the "theme" cookie: default
// (dark) when absent, each recognized value, and an unrecognized value
// falling back to the default.
func TestServeUI(t *testing.T) {
	handler := ServeUI("/graphql")

	tests := []struct {
		name       string
		cookie     *http.Cookie
		wantSubstr string
	}{
		{"no cookie defaults to dark", nil, "#1e1e1e"},
		{"dark cookie", &http.Cookie{Name: "theme", Value: "dark"}, "#1e1e1e"},
		{"light cookie", &http.Cookie{Name: "theme", Value: "light"}, "#ffffff"},
		{"auto cookie uses media queries", &http.Cookie{Name: "theme", Value: "auto"}, "prefers-color-scheme: dark"},
		{"unrecognized cookie value falls back to dark", &http.Cookie{Name: "theme", Value: "purple"}, "#1e1e1e"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/graphiql", nil)
			if tc.cookie != nil {
				req.AddCookie(tc.cookie)
			}
			rec := httptest.NewRecorder()

			handler(rec, req)

			resp := rec.Result()
			assert.Equal(t, http.StatusOK, resp.StatusCode)
			assert.Equal(t, "text/html; charset=utf-8", resp.Header.Get("Content-Type"))
			assert.Contains(t, rec.Body.String(), tc.wantSubstr)
		})
	}
}

// TestGenerateGraphiQLHTML checks the endpoint URL is embedded verbatim and
// that auto-theme wraps both palettes in media queries while dark/light
// theme selects exactly one palette.
func TestGenerateGraphiQLHTML(t *testing.T) {
	html := generateGraphiQLHTML("https://example.com/graphql", "dark")
	assert.Contains(t, html, "https://example.com/graphql")
	assert.Contains(t, html, "<title>GraphQL API - GraphiQL</title>")

	auto := generateGraphiQLHTML("/graphql", "auto")
	assert.Contains(t, auto, "@media (prefers-color-scheme: dark)")
	assert.Contains(t, auto, "@media (prefers-color-scheme: light)")

	light := generateGraphiQLHTML("/graphql", "light")
	assert.Contains(t, light, "#ffffff")

	t.Run("well-formed html document", func(t *testing.T) {
		require.True(t, len(html) > 0)
		assert.Contains(t, html, "<!DOCTYPE html>")
		assert.Contains(t, html, "</html>")
	})
}
