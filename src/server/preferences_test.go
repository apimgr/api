package server

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/apimgr/api/src/config"
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
		{"full url", "https://example.com/server/preferences/sync/import?theme=light&lang=es", "light", "es"},
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

	query := "theme=light&lang=fr"
	code := base64.RawURLEncoding.EncodeToString([]byte(query))
	assert.Equal(t, "fr", parsePreferenceInput(code+"==").Get("lang"))
	assert.Empty(t, parsePreferenceInput(strings.Repeat("a", 2049)))
}

func TestMergedPreferenceInputDirectValuesWin(t *testing.T) {
	code := base64.RawURLEncoding.EncodeToString([]byte("theme=dark&lang=fr"))
	values := mergedPreferenceInput(code, url.Values{"theme": {"light"}, "x": {"y"}})
	assert.Equal(t, "light", values.Get("theme"))
	assert.Equal(t, "fr", values.Get("lang"))
	assert.Equal(t, "y", values.Get("x"))
}

// applyPreferences must set cookies only for values that pass their own
// allow-list, and must drop unknown keys entirely.
func TestApplyPreferencesValidation(t *testing.T) {
	t.Run("valid values are applied", func(t *testing.T) {
		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodPost, "/server/preferences", nil)
		applied := applyPreferences(w, r, url.Values{"theme": {"light"}, "lang": {"fr"}})
		assert.ElementsMatch(t, []string{"theme", "lang"}, applied)
		assert.Equal(t, "light", cookieValue(w, "theme"))
		assert.Equal(t, "fr", cookieValue(w, "lang"))
	})

	t.Run("invalid values are dropped", func(t *testing.T) {
		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodPost, "/server/preferences", nil)
		applied := applyPreferences(w, r, url.Values{"theme": {"purple"}, "lang": {"zz"}})
		assert.Empty(t, applied)
		assert.Empty(t, w.Result().Cookies())
	})

	t.Run("automatic language clears the explicit cookie", func(t *testing.T) {
		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodPost, "/server/preferences", nil)
		r.AddCookie(&http.Cookie{Name: "lang", Value: "fr"})
		applied := applyPreferences(w, r, url.Values{"lang": {""}})
		assert.Equal(t, []string{"lang"}, applied)
		cookies := w.Result().Cookies()
		require.Len(t, cookies, 1)
		assert.Equal(t, "lang", cookies[0].Name)
		assert.Equal(t, -1, cookies[0].MaxAge)
	})

	t.Run("non-exportable keys are ignored", func(t *testing.T) {
		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodPost, "/server/preferences", nil)
		applied := applyPreferences(w, r, url.Values{
			"cookie_consent": {"all"},
			"ccpa_opt_out":   {"true"},
			"api_build":      {"1.0.0-abcdef1"},
		})
		assert.Empty(t, applied)
		assert.Empty(t, w.Result().Cookies())
	})
}

func TestApplyPreferenceValuesReportsIgnoredAndHonorsLimits(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/server/preferences/sync/import", nil)
	applied, ignored := applyPreferenceValues(w, r, url.Values{
		"theme":               {"bogus"},
		"lang":                {"not-a-language"},
		"cookie_consent":      {"all"},
		"consent_form":        {"1"},
		"consent_preferences": {"on"},
		"consent_analytics":   {"on"},
		"ccpa_opt_out":        {"true"},
		"code":                {"ignored"},
		"redirect":            {"/server/help"},
		"action":              {"reset"},
		"csrf_token":          {"token"},
		"api_build":           {"1.0.0-deadbee"},
		"unknown":             {strings.Repeat("x", 65)},
	}, false)
	assert.Empty(t, applied)
	assert.Equal(t, []string{"api_build", "cookie_consent", "lang", "theme", "unknown"}, ignored)
}

func TestApplyPreferenceValuesReportsKeysBeyondLimit(t *testing.T) {
	values := make(url.Values, 33)
	for i := 0; i < 33; i++ {
		values.Set(fmt.Sprintf("unknown_%02d", i), "value")
	}
	_, ignored := applyPreferenceValues(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/server/preferences/sync/import", nil), values, false)
	assert.Len(t, ignored, 33)
	assert.Contains(t, ignored, "unknown_32")
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
		{"import loop", "http://example.com/server/preferences/sync/import?theme=dark", "/"},
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

func TestValidPreferenceRedirectRejectsOpenRedirects(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "/server/preferences", nil)
	for _, target := range []string{"//evil.example", "/\\evil", "https://evil.example", "/\r\nLocation: https://evil.example", "/server/preferences", "/server/preferences/sync/import"} {
		if validPreferenceRedirect(r, target) {
			t.Errorf("validPreferenceRedirect(%q) = true, want false", target)
		}
	}
	for _, target := range []string{"/server/help", "/?from=prefs"} {
		if !validPreferenceRedirect(r, target) {
			t.Errorf("validPreferenceRedirect(%q) = false, want true", target)
		}
	}
}

func TestResetGuestPreferencesClearsNonCredentialCookies(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/server/preferences", nil)
	req.AddCookie(&http.Cookie{Name: "theme", Value: "light"})
	req.AddCookie(&http.Cookie{Name: "lang", Value: "fr"})
	req.AddCookie(&http.Cookie{Name: "cookie_consent", Value: "{}"})
	req.AddCookie(&http.Cookie{Name: "api_build", Value: "1.0.0-deadbee"})
	req.AddCookie(&http.Cookie{Name: "csrf_token", Value: "keep"})
	req.AddCookie(&http.Cookie{Name: "owner_token", Value: "keep"})
	w := httptest.NewRecorder()
	resetGuestPreferences(w, req)
	assert.Equal(t, `"storage"`, w.Header().Get("Clear-Site-Data"))
	deleted := map[string]bool{}
	for _, cookie := range w.Result().Cookies() {
		if cookie.MaxAge < 0 {
			deleted[cookie.Name] = true
		}
	}
	assert.True(t, deleted["theme"])
	assert.True(t, deleted["lang"])
	assert.True(t, deleted["cookie_consent"])
	assert.True(t, deleted["api_build"])
	assert.False(t, deleted["csrf_token"])
	assert.False(t, deleted["owner_token"])
}

func TestPreferencesSyncPostAlwaysRedirectsWithoutBody(t *testing.T) {
	for _, body := range []string{"", "code=invalid", "theme=invalid"} {
		req := httptest.NewRequest(http.MethodPost, "/server/preferences/sync", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()
		preferencesSyncPostHandler(w, req)
		assert.Equal(t, http.StatusSeeOther, w.Code)
		assert.Equal(t, "/server/preferences/sync", w.Header().Get("Location"))
		assert.Empty(t, w.Body.String())
		assert.Equal(t, "no-store", w.Header().Get("Cache-Control"))
	}
}

// The no-JS save form must answer 303 See Other so a refresh never resubmits.
func TestPreferencesSaveHandlerRedirects(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/server/preferences", strings.NewReader("theme=light&lang=de"))
	req.Host = "example.com"
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Referer", "http://example.com/server/help")
	req.AddCookie(&http.Cookie{Name: "cookie_consent", Value: url.PathEscape(`{"essential":true,"preferences":true,"analytics":false,"timestamp":1}`)})
	w := httptest.NewRecorder()

	preferencesSaveHandler(config.DefaultConfig())(w, req)

	assert.Equal(t, http.StatusSeeOther, w.Code)
	assert.Equal(t, "/server/help", w.Header().Get("Location"))
	assert.Equal(t, "light", cookieValue(w, "theme"))
	assert.Equal(t, "de", cookieValue(w, "lang"))
}

func TestPreferencesPageStartsNonEssentialCookiesOffWithoutConsent(t *testing.T) {
	require.NoError(t, initTemplates())
	cfg := newTestConfig(t)
	response := httptest.NewRecorder()
	preferencesPageHandler(cfg)(response, httptest.NewRequest(http.MethodGet, "/server/preferences", nil))

	assert.Equal(t, http.StatusOK, response.Code)
	assert.NotContains(t, response.Body.String(), `class="cookie-banner"`)
	assert.NotContains(t, response.Body.String(), `name="consent_preferences" checked`)
	assert.NotContains(t, response.Body.String(), `name="consent_analytics" checked`)
}

func TestPreferencesSaveRequiresPreferenceConsent(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/server/preferences", strings.NewReader("theme=light&lang=de"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()

	preferencesSaveHandler(config.DefaultConfig())(w, req)

	assert.Equal(t, http.StatusSeeOther, w.Code)
	assert.Empty(t, cookieValue(w, "theme"))
	assert.Empty(t, cookieValue(w, "lang"))
}

// Import must set cookies then redirect so the code never lingers in history.
func TestPreferencesImportHandler(t *testing.T) {
	t.Run("query parameters", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/server/preferences/sync/import?theme=auto&lang=ja", nil)
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
		req := httptest.NewRequest(http.MethodGet, "/server/preferences/sync/import?code="+url.QueryEscape(code), nil)
		req.Host = "example.com"
		w := httptest.NewRecorder()

		preferencesImportHandler(w, req)

		assert.Equal(t, http.StatusSeeOther, w.Code)
		assert.Equal(t, "light", cookieValue(w, "theme"))
		assert.Equal(t, "es", cookieValue(w, "lang"))
	})

	t.Run("import is additive and round trips exported values", func(t *testing.T) {
		query := preferenceQuery("light", "fr")
		req := httptest.NewRequest(http.MethodGet, "/server/preferences/sync/import?"+query, nil)
		req.Host = "example.com"
		req.AddCookie(&http.Cookie{Name: "api_pref_sort", Value: "name"})
		w := httptest.NewRecorder()

		preferencesImportHandler(w, req)

		assert.Equal(t, http.StatusSeeOther, w.Code)
		assert.Equal(t, "light", cookieValue(w, "theme"))
		assert.Equal(t, "fr", cookieValue(w, "lang"))
		assert.Empty(t, cookieValue(w, "api_pref_sort"))
	})
}

// The API export mirror must return both transfer forms.
func TestAPIPreferencesSyncHandler(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/server/preferences/sync", nil)
	req.Host = "example.com"
	req.AddCookie(&http.Cookie{Name: "theme", Value: "light"})
	req.AddCookie(&http.Cookie{Name: "lang", Value: "fr"})
	w := httptest.NewRecorder()

	preferencesSyncAPIHandler(config.DefaultConfig())(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	body := w.Body.String()
	assert.Contains(t, body, "/server/preferences/sync/import?")
	assert.Contains(t, body, preferenceCode(preferenceQuery("light", "fr")))
}

func TestPreferencesSyncPageContentNegotiation(t *testing.T) {
	tests := []struct {
		accept      string
		contentType string
		contains    string
	}{
		{"text/html", "text/html", "Sync preferences"},
		{"text/plain", "text/plain", "/server/preferences/sync/import?"},
		{"application/json", "application/json", `"code"`},
	}
	for _, tt := range tests {
		t.Run(tt.accept, func(t *testing.T) {
			require.NoError(t, initTemplates())
			req := httptest.NewRequest(http.MethodGet, "/server/preferences/sync", nil)
			req.Host = "example.com"
			req.Header.Set("Accept", tt.accept)
			w := httptest.NewRecorder()
			preferencesSyncPageHandler(config.DefaultConfig())(w, req)
			assert.Equal(t, http.StatusOK, w.Code)
			assert.Contains(t, w.Header().Get("Content-Type"), tt.contentType)
			assert.Contains(t, w.Header().Get("Vary"), "Accept")
			assert.Equal(t, "no-store", w.Header().Get("Cache-Control"))
			assert.Contains(t, w.Body.String(), tt.contains)
		})
	}
}
