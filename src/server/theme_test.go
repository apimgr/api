package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// GetTheme must default to dark when no cookie is present, honor the three
// valid cookie values, and fall back to dark for any unrecognized value.
func TestGetTheme(t *testing.T) {
	tests := []struct {
		name      string
		cookieVal string
		hasCookie bool
		wantTheme Theme
	}{
		{"no cookie defaults to dark", "", false, ThemeDark},
		{"dark cookie", "dark", true, ThemeDark},
		{"light cookie", "light", true, ThemeLight},
		{"auto cookie", "auto", true, ThemeAuto},
		{"invalid cookie falls back to dark", "purple", true, ThemeDark},
		{"empty cookie value falls back to dark", "", true, ThemeDark},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			if tt.hasCookie {
				req.AddCookie(&http.Cookie{Name: "theme", Value: tt.cookieVal})
			}
			assert.Equal(t, tt.wantTheme, GetTheme(req))
		})
	}
}

// SetThemeCookie must set the theme cookie with the documented attributes:
// path "/", 1-year MaxAge, not HttpOnly (JS needs to read it), Lax SameSite.
func TestSetThemeCookie(t *testing.T) {
	w := httptest.NewRecorder()
	SetThemeCookie(w, ThemeLight)

	resp := w.Result()
	cookies := resp.Cookies()
	require.Len(t, cookies, 1)

	c := cookies[0]
	assert.Equal(t, "theme", c.Name)
	assert.Equal(t, "light", c.Value)
	assert.Equal(t, "/", c.Path)
	assert.Equal(t, 365*24*60*60, c.MaxAge)
	assert.False(t, c.HttpOnly)
	assert.False(t, c.Secure)
	assert.Equal(t, http.SameSiteLaxMode, c.SameSite)
}

// ThemeClass must map each theme to its CSS class, and default to
// theme-dark for the zero value / any unrecognized Theme.
func TestThemeClass(t *testing.T) {
	tests := []struct {
		theme Theme
		want  string
	}{
		{ThemeDark, "theme-dark"},
		{ThemeLight, "theme-light"},
		{ThemeAuto, "theme-auto"},
		{Theme("bogus"), "theme-dark"},
		{Theme(""), "theme-dark"},
	}

	for _, tt := range tests {
		t.Run(string(tt.theme), func(t *testing.T) {
			assert.Equal(t, tt.want, ThemeClass(tt.theme))
		})
	}
}

// ThemeData must reflect the resolved theme (from cookie, defaulting to
// dark) into the IsDark/IsLight/IsAuto flags used by templates - note that
// per GetTheme/ThemeClass, "auto" counts as dark for IsDark purposes.
func TestThemeData(t *testing.T) {
	tests := []struct {
		name      string
		cookieVal string
		want      map[string]interface{}
	}{
		{
			name:      "dark",
			cookieVal: "dark",
			want: map[string]interface{}{
				"Theme":      "dark",
				"ThemeClass": "theme-dark",
				"IsDark":     true,
				"IsLight":    false,
				"IsAuto":     false,
			},
		},
		{
			name:      "light",
			cookieVal: "light",
			want: map[string]interface{}{
				"Theme":      "light",
				"ThemeClass": "theme-light",
				"IsDark":     false,
				"IsLight":    true,
				"IsAuto":     false,
			},
		},
		{
			name:      "auto",
			cookieVal: "auto",
			want: map[string]interface{}{
				"Theme":      "auto",
				"ThemeClass": "theme-auto",
				"IsDark":     true,
				"IsLight":    false,
				"IsAuto":     true,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.AddCookie(&http.Cookie{Name: "theme", Value: tt.cookieVal})
			assert.Equal(t, tt.want, ThemeData(req))
		})
	}
}

// HandleThemeSwitch must reject non-POST methods, reject invalid theme
// values, and on success set the cookie and echo the theme in a JSON body.
func TestHandleThemeSwitch(t *testing.T) {
	t.Run("rejects non-POST", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/theme", nil)
		w := httptest.NewRecorder()
		HandleThemeSwitch(w, req)
		assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
	})

	t.Run("rejects invalid theme", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/theme", strings.NewReader("theme=purple"))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()
		HandleThemeSwitch(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("rejects missing theme", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/theme", strings.NewReader(""))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()
		HandleThemeSwitch(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	for _, theme := range []string{"dark", "light", "auto"} {
		t.Run("accepts "+theme, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/api/v1/theme", strings.NewReader("theme="+theme))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			w := httptest.NewRecorder()
			HandleThemeSwitch(w, req)

			assert.Equal(t, http.StatusOK, w.Code)
			assert.Equal(t, "application/json", w.Header().Get("Content-Type"))
			assert.JSONEq(t, `{"success":true,"theme":"`+theme+`"}`, w.Body.String())

			cookies := w.Result().Cookies()
			require.Len(t, cookies, 1)
			assert.Equal(t, theme, cookies[0].Value)
		})
	}
}
