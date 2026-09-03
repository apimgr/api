package server

import (
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// cookieValue returns the value of the named cookie set on the recorder, or
// the empty string when the handler did not set it.
func cookieValue(w *httptest.ResponseRecorder, name string) string {
	for _, c := range w.Result().Cookies() {
		if c.Name == name {
			return c.Value
		}
	}
	return ""
}

// preferenceQuery must emit only the two exportable preferences, never the
// consent or build-stamp cookies.
func TestPreferenceQueryOnlyExportableKeys(t *testing.T) {
	q, err := url.ParseQuery(preferenceQuery("dark", "fr"))
	require.NoError(t, err)
	assert.Equal(t, "dark", q.Get("theme"))
	assert.Equal(t, "fr", q.Get("lang"))
	assert.Len(t, q, 2)
}

// preferenceCode must round-trip through parsePreferenceInput.
func TestPreferenceCodeRoundTrip(t *testing.T) {
	query := preferenceQuery("light", "de")
	values := parsePreferenceInput(preferenceCode(query))
	assert.Equal(t, "light", values.Get("theme"))
	assert.Equal(t, "de", values.Get("lang"))
}

// parsePreferenceInput must accept a bare query string, a full pasted export
// URL, and a base64url short code, and must reject garbage without panicking.
func TestParsePreferenceInputForms(t *testing.T) {
	tests := []struct {
		name  string
		input string
		theme string
		lang  string
	}{
		{"query string", "theme=dark&lang=ja", "dark", "ja"},
		{"full url", "https://example.com/server/preferences/import?theme=light&lang=es", "light", "es"},
		{"short code", base64.RawURLEncoding.EncodeToString([]byte("theme=auto&lang=ar")), "auto", "ar"},
		{"garbage", "!!!!", "", ""},
		{"empty", "", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			values := parsePreferenceInput(tt.input)
			assert.Equal(t, tt.theme, values.Get("theme"))
			assert.Equal(t, tt.lang, values.Get("lang"))
		})
	}
}

// applyPreferences must set cookies only for values that pass their own
// allow-list, and must drop unknown keys entirely.
func TestApplyPreferencesValidation(t *testing.T) {
	t.Run("valid values are applied", func(t *testing.T) {
		w := httptest.NewRecorder()
		applied := applyPreferences(w, url.Values{"theme": {"light"}, "lang": {"fr"}})
		assert.ElementsMatch(t, []string{"theme", "lang"}, applied)
		assert.Equal(t, "light", cookieValue(w, "theme"))
		assert.Equal(t, "fr", cookieValue(w, "lang"))
	})

	t.Run("invalid values are dropped", func(t *testing.T) {
		w := httptest.NewRecorder()
		applied := applyPreferences(w, url.Values{"theme": {"purple"}, "lang": {"zz"}})
		assert.Empty(t, applied)
		assert.Empty(t, w.Result().Cookies())
	})

	t.Run("non-exportable keys are ignored", func(t *testing.T) {
		w := httptest.NewRecorder()
		applied := applyPreferences(w, url.Values{
			"cookie_consent": {"all"},
			"ccpa_opt_out":   {"true"},
			"api_build":      {"1.0.0-abcdef1"},
		})
		assert.Empty(t, applied)
		assert.Empty(t, w.Result().Cookies())
	})
}

// preferenceRedirectTarget must never turn an attacker-supplied Referer into
// an open redirect.
func TestPreferenceRedirectTarget(t *testing.T) {
	tests := []struct {
		name    string
		referer string
		want    string
	}{
		{"no referer", "", "/"},
		{"same host path", "http://example.com/server/help", "/server/help"},
		{"foreign host", "https://evil.example.net/pwn", "/"},
		{"protocol relative", "//evil.example.net/pwn", "/"},
		{"import loop", "http://example.com/server/preferences/import?theme=dark", "/"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/server/preferences", nil)
			req.Host = "example.com"
			if tt.referer != "" {
				req.Header.Set("Referer", tt.referer)
			}
			assert.Equal(t, tt.want, preferenceRedirectTarget(req))
		})
	}
}

// The no-JS save form must answer 303 See Other so a refresh never resubmits.
func TestPreferencesSaveHandlerRedirects(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/server/preferences", strings.NewReader("theme=light&lang=de"))
	req.Host = "example.com"
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Referer", "http://example.com/server/help")
	w := httptest.NewRecorder()

	preferencesSaveHandler(w, req)

	assert.Equal(t, http.StatusSeeOther, w.Code)
	assert.Equal(t, "/server/help", w.Header().Get("Location"))
	assert.Equal(t, "light", cookieValue(w, "theme"))
	assert.Equal(t, "de", cookieValue(w, "lang"))
}

// Import must set cookies then redirect so the code never lingers in history.
func TestPreferencesImportHandler(t *testing.T) {
	t.Run("query parameters", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/server/preferences/import?theme=auto&lang=ja", nil)
		req.Host = "example.com"
		w := httptest.NewRecorder()

		preferencesImportHandler(w, req)

		assert.Equal(t, http.StatusSeeOther, w.Code)
		assert.Equal(t, "/", w.Header().Get("Location"))
		assert.Equal(t, "auto", cookieValue(w, "theme"))
		assert.Equal(t, "ja", cookieValue(w, "lang"))
	})

	t.Run("pasted short code", func(t *testing.T) {
		code := base64.RawURLEncoding.EncodeToString([]byte("theme=light&lang=es"))
		req := httptest.NewRequest(http.MethodGet, "/server/preferences/import?code="+url.QueryEscape(code), nil)
		req.Host = "example.com"
		w := httptest.NewRecorder()

		preferencesImportHandler(w, req)

		assert.Equal(t, http.StatusSeeOther, w.Code)
		assert.Equal(t, "light", cookieValue(w, "theme"))
		assert.Equal(t, "es", cookieValue(w, "lang"))
	})
}

// The API export mirror must return both transfer forms.
func TestAPIPreferencesExportHandler(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/server/preferences/export", nil)
	req.Host = "example.com"
	req.AddCookie(&http.Cookie{Name: "theme", Value: "light"})
	req.AddCookie(&http.Cookie{Name: "lang", Value: "fr"})
	w := httptest.NewRecorder()

	apiPreferencesExportHandler(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	body := w.Body.String()
	assert.Contains(t, body, "http://example.com/server/preferences/import?")
	assert.Contains(t, body, preferenceCode(preferenceQuery("light", "fr")))
}
