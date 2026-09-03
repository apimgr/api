package server

import (
	"encoding/base64"
	"net/http"
	"net/url"
	"strings"

	"github.com/apimgr/api/src/common/i18n"
	"github.com/apimgr/api/src/config"
)

// prefCookieMaxAge is the lifetime of every preference cookie: one year, per
// AI.md PART 16 "Client-Side Preferences (cookies)".
const prefCookieMaxAge = 365 * 24 * 60 * 60

// prefImportPath is the canonical import route. It is the only path an export
// URL ever points at, and the prefix the import form strips when a full URL is
// pasted into the short-code field.
const prefImportPath = "/server/preferences/import"

// setLangCookie persists a validated BCP 47 language tag. It mirrors
// SetThemeCookie: readable by JavaScript, SameSite=Lax so a preference link
// followed from another origin still applies.
func setLangCookie(w http.ResponseWriter, lang string) {
	http.SetCookie(w, &http.Cookie{
		Name:     i18n.CookieName,
		Value:    lang,
		Path:     "/",
		MaxAge:   prefCookieMaxAge,
		HttpOnly: false,
		SameSite: http.SameSiteLaxMode,
	})
}

// preferenceQuery renders the exportable preference state as a plain query
// string. Only theme and lang are exportable: cookie_consent and ccpa_opt_out
// are per-browser legal acknowledgments, and {project_name}_build is a
// device-local cache-purge stamp.
func preferenceQuery(theme, lang string) string {
	values := url.Values{}
	values.Set("theme", theme)
	values.Set("lang", lang)
	return values.Encode()
}

// preferenceCode encodes the query string as base64url for manual retyping on
// a device without copy/paste.
func preferenceCode(query string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(query))
}

// parsePreferenceInput turns whatever was pasted into the import field into
// query values. It accepts a bare query string, a full export URL (everything
// up to and including the first "?" is discarded), or a base64url short code.
func parsePreferenceInput(raw string) url.Values {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return url.Values{}
	}

	if idx := strings.Index(raw, "?"); idx >= 0 {
		raw = raw[idx+1:]
	}

	if !strings.Contains(raw, "=") {
		decoded, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(raw, "="))
		if err != nil {
			return url.Values{}
		}
		raw = string(decoded)
	}

	values, err := url.ParseQuery(raw)
	if err != nil {
		return url.Values{}
	}
	return values
}

// applyPreferences validates each supplied preference against its own
// allow-list and sets the matching cookie. Unknown keys and malformed values
// are dropped silently — an imported value is still untrusted input. It
// returns the names of the preferences that were actually applied.
func applyPreferences(w http.ResponseWriter, values url.Values) []string {
	applied := make([]string, 0, 2)

	switch values.Get("theme") {
	case string(ThemeDark):
		SetThemeCookie(w, ThemeDark)
		applied = append(applied, "theme")
	case string(ThemeLight):
		SetThemeCookie(w, ThemeLight)
		applied = append(applied, "theme")
	case string(ThemeAuto):
		SetThemeCookie(w, ThemeAuto)
		applied = append(applied, "theme")
	}

	if lang := values.Get("lang"); lang != "" && i18n.IsSupported(lang) {
		setLangCookie(w, lang)
		applied = append(applied, "lang")
	}

	return applied
}

// preferenceRedirectTarget picks where to send the visitor after a preference
// write. The referring page is preferred so the visitor lands back where they
// were, but only when it is a same-origin path — an attacker-supplied Referer
// must never turn this into an open redirect.
func preferenceRedirectTarget(r *http.Request) string {
	ref := r.Referer()
	if ref == "" {
		return "/"
	}
	parsed, err := url.Parse(ref)
	if err != nil {
		return "/"
	}
	if parsed.Host != "" && parsed.Host != r.Host {
		return "/"
	}
	if !strings.HasPrefix(parsed.Path, "/") || strings.HasPrefix(parsed.Path, "//") {
		return "/"
	}
	if strings.HasPrefix(parsed.Path, prefImportPath) {
		return "/"
	}
	target := parsed.Path
	if parsed.RawQuery != "" {
		target += "?" + parsed.RawQuery
	}
	return target
}

// currentPreferences reads the visitor's effective preference state from
// cookies, falling back to the documented defaults (theme dark, language from
// the Accept-Language header).
func currentPreferences(r *http.Request) (theme string, lang string) {
	return string(GetTheme(r)), i18n.LangFromRequest(r)
}

// preferencesPageHandler renders the guest preferences page: a no-JS form for
// theme and language plus the export URL and short code for carrying the same
// state to another browser or device.
func preferencesPageHandler(cfg *config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		theme, lang := currentPreferences(r)
		query := preferenceQuery(theme, lang)

		data := newPageData(cfg, r, "preferences")
		data.PageTitle = "Preferences"
		data.PageDescription = "Theme and language preferences for " + cfg.Server.Branding.Title
		data.PrefExportURL = requestBaseURL(r) + prefImportPath + "?" + query
		data.PrefExportCode = preferenceCode(query)
		renderPage(w, "preferences", data)
	}
}

// preferencesSaveHandler applies a submitted preference form and redirects
// back to the page the visitor came from. It answers 303 See Other so the
// browser re-issues a GET and a refresh never resubmits the form.
func preferencesSaveHandler(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Invalid form submission", http.StatusBadRequest)
		return
	}
	applyPreferences(w, url.Values(r.PostForm))
	http.Redirect(w, r, preferenceRedirectTarget(r), http.StatusSeeOther)
}

// preferencesExportPageHandler renders the export URL and short code as HTML.
func preferencesExportPageHandler(cfg *config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		theme, lang := currentPreferences(r)
		query := preferenceQuery(theme, lang)

		data := newPageData(cfg, r, "preferences")
		data.PageTitle = "Export Preferences"
		data.PageDescription = "Carry your theme and language settings to another device"
		data.PrefExportURL = requestBaseURL(r) + prefImportPath + "?" + query
		data.PrefExportCode = preferenceCode(query)
		renderPage(w, "preferences_export", data)
	}
}

// preferencesImportHandler validates the supplied preferences, sets the
// matching cookies and redirects, so the code never lingers in the visible URL
// or in browser history.
func preferencesImportHandler(w http.ResponseWriter, r *http.Request) {
	values := r.URL.Query()
	if code := values.Get("code"); code != "" {
		values = parsePreferenceInput(code)
	}
	applyPreferences(w, values)
	http.Redirect(w, r, preferenceRedirectTarget(r), http.StatusSeeOther)
}

// apiPreferencesHandler returns the visitor's current exportable preferences.
func apiPreferencesHandler(w http.ResponseWriter, r *http.Request) {
	theme, lang := currentPreferences(r)
	writeEnvelopeOK(w, http.StatusOK, map[string]string{
		"theme": theme,
		"lang":  lang,
	})
}

// apiPreferencesSaveHandler applies preferences supplied as form values or
// query parameters and echoes the resulting state.
func apiPreferencesSaveHandler(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		writeEnvelopeError(w, http.StatusBadRequest, "BAD_REQUEST", "Invalid form submission", nil)
		return
	}
	applied := applyPreferences(w, url.Values(r.Form))
	if len(applied) == 0 {
		writeEnvelopeError(w, http.StatusBadRequest, "VALIDATION_FAILED", "No valid preference supplied", nil)
		return
	}
	theme, lang := currentPreferences(r)
	for _, name := range applied {
		switch name {
		case "theme":
			theme = r.Form.Get("theme")
		case "lang":
			lang = r.Form.Get("lang")
		}
	}
	writeEnvelopeOK(w, http.StatusOK, map[string]interface{}{
		"theme":   theme,
		"lang":    lang,
		"applied": applied,
	})
}

// apiPreferencesExportHandler returns both export forms as JSON.
func apiPreferencesExportHandler(w http.ResponseWriter, r *http.Request) {
	theme, lang := currentPreferences(r)
	query := preferenceQuery(theme, lang)
	writeEnvelopeOK(w, http.StatusOK, map[string]string{
		"theme": theme,
		"lang":  lang,
		"url":   requestBaseURL(r) + prefImportPath + "?" + query,
		"code":  preferenceCode(query),
	})
}
