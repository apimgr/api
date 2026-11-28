package server

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/apimgr/api/src/common/sanitize"
	"github.com/apimgr/api/src/common/i18n"
	"github.com/apimgr/api/src/config"
	"github.com/apimgr/api/src/database"
)

// prefCookieMaxAge is the lifetime of every preference cookie: one year, per
// AI.md PART 16 "Client-Side Preferences (cookies)".
const prefCookieMaxAge = 365 * 24 * 60 * 60

// prefImportPath is the canonical import route. It is the only path an export
// URL ever points at, and the prefix the import form strips when a full URL is
// pasted into the short-code field.
const prefImportPath = "/server/preferences/sync/import"

// setLangCookie persists a validated BCP 47 language tag. It mirrors
// SetThemeCookie: readable by JavaScript, SameSite=Lax so a preference link
// followed from another origin still applies.
func setPreferenceCookie(w http.ResponseWriter, r *http.Request, name, value string) {
	proto, _, _ := GetURLVars(r)
	http.SetCookie(w, &http.Cookie{
		Name:     name,
		Value:    value,
		Path:     "/",
		MaxAge:   prefCookieMaxAge,
		HttpOnly: false,
		Secure:   proto == "https",
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
	if raw == "" || len(raw) > 2048 {
		return url.Values{}
	}

	if idx := strings.Index(raw, "?"); idx >= 0 {
		raw = raw[idx+1:]
	}

	withoutPadding := strings.TrimRight(raw, "=")
	if !strings.Contains(withoutPadding, "=") {
		decoded, err := base64.RawURLEncoding.DecodeString(withoutPadding)
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

func mergedPreferenceInput(code string, direct url.Values) url.Values {
	merged := parsePreferenceInput(code)
	for key, values := range direct {
		if key == "code" || len(values) == 0 {
			continue
		}
		merged[key] = append([]string(nil), values...)
	}
	return merged
}

// applyPreferences validates each supplied preference against its own
// allow-list and sets the matching cookie. Unknown keys and malformed values
// are dropped silently — an imported value is still untrusted input. It
// returns the names of the preferences that were actually applied.
func applyPreferences(w http.ResponseWriter, r *http.Request, values url.Values) []string {
	applied, _ := applyPreferenceValues(w, r, values, true)
	return applied
}

func applyPreferenceValues(w http.ResponseWriter, r *http.Request, values url.Values, allowAutomatic bool) ([]string, []string) {
	applied := make([]string, 0, 2)
	values, overflow := limitPreferenceValues(values)
	ignored := append([]string(nil), overflow...)
	controlFields := map[string]bool{
		"code": true, "redirect": true, "action": true, "consent_form": true,
		"consent_preferences": true, "consent_analytics": true, "ccpa_opt_out": true,
		"csrf_token": true,
	}
	for key, entries := range values {
		if controlFields[key] {
			continue
		}
		if len(entries) == 0 || len(entries[0]) > 64 {
			ignored = append(ignored, key)
			delete(values, key)
			continue
		}
		values[key] = entries[:1]
	}
	if value, ok := values["theme"]; ok {
		switch value[0] {
		case string(ThemeDark):
			setPreferenceCookie(w, r, "theme", string(ThemeDark))
			applied = append(applied, "theme")
		case string(ThemeLight):
			setPreferenceCookie(w, r, "theme", string(ThemeLight))
			applied = append(applied, "theme")
		case string(ThemeAuto):
			setPreferenceCookie(w, r, "theme", string(ThemeAuto))
			applied = append(applied, "theme")
		default:
			ignored = append(ignored, "theme")
		}
	}

	if langValue, ok := values["lang"]; ok {
		lang := canonicalLanguage(r, langValue[0])
		if langValue[0] == "" && allowAutomatic {
			proto, _, _ := GetURLVars(r)
			http.SetCookie(w, &http.Cookie{Name: i18n.CookieName, Value: "", Path: "/", MaxAge: -1, HttpOnly: false, Secure: proto == "https", SameSite: http.SameSiteLaxMode})
			applied = append(applied, "lang")
		} else if lang != "" {
			setPreferenceCookie(w, r, i18n.CookieName, lang)
			applied = append(applied, "lang")
		} else {
			ignored = append(ignored, "lang")
		}
	}
	known := map[string]bool{"theme": true, "lang": true}
	for key := range values {
		if !known[key] && !controlFields[key] {
			ignored = append(ignored, key)
		}
	}
	sort.Strings(applied)
	sort.Strings(ignored)
	return applied, uniqueStrings(ignored)
}

func limitPreferenceValues(values url.Values) (url.Values, []string) {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	limited := make(url.Values)
	ignored := make([]string, 0)
	if len(keys) > 32 {
		ignored = append(ignored, keys[32:]...)
		keys = keys[:32]
	}
	for _, key := range keys {
		limited[key] = values[key]
	}
	return limited, ignored
}

func uniqueStrings(values []string) []string {
	if len(values) == 0 {
		return values
	}
	result := values[:1]
	for _, value := range values[1:] {
		if value != result[len(result)-1] {
			result = append(result, value)
		}
	}
	return result
}

type consentCookie struct {
	Essential   bool  `json:"essential"`
	Preferences bool  `json:"preferences"`
	Analytics   bool  `json:"analytics"`
	Timestamp   int64 `json:"timestamp"`
}

func requestCookieValue(r *http.Request, name string) string {
	cookie, err := r.Cookie(name)
	if err != nil {
		return ""
	}
	return cookie.Value
}

func readCookieConsent(r *http.Request) consentCookie {
	consent, _ := decodeCookieConsent(requestCookieValue(r, "cookie_consent"))
	return consent
}

func hasValidCookieConsent(r *http.Request) bool {
	_, valid := decodeCookieConsent(requestCookieValue(r, "cookie_consent"))
	return valid
}

func decodeCookieConsent(raw string) (consentCookie, bool) {
	var consent consentCookie
	if raw == "" {
		return consent, false
	}
	decoded, err := url.PathUnescape(raw)
	if err != nil {
		return consent, false
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal([]byte(decoded), &fields); err != nil || fields == nil {
		return consent, false
	}
	for _, key := range []string{"essential", "preferences", "analytics", "timestamp"} {
		if _, ok := fields[key]; !ok {
			return consent, false
		}
	}
	if err := json.Unmarshal([]byte(decoded), &consent); err != nil || !consent.Essential || consent.Timestamp <= 0 {
		return consentCookie{}, false
	}
	return consent, true
}

func preferenceCookieConsent(r *http.Request) bool {
	if cfg, ok := r.Context().Value(serverConfigKey).(*config.ServerConfig); ok && cfg != nil && !cfg.Server.Privacy.Cookies.Preferences.Enabled {
		return false
	}
	if _, ok := r.Context().Value(serverConfigKey).(*config.ServerConfig); !ok {
		return true
	}
	return hasValidCookieConsent(r) && readCookieConsent(r).Preferences
}

func applyPreferenceValuesWhenAllowed(w http.ResponseWriter, r *http.Request, values url.Values, allowAutomatic, allowed bool) ([]string, []string) {
	if allowed {
		return applyPreferenceValues(w, r, values, allowAutomatic)
	}
	controls := map[string]bool{"code": true, "redirect": true, "action": true, "csrf_token": true, "consent_form": true, "consent_preferences": true, "consent_analytics": true, "ccpa_opt_out": true}
	limited, overflow := limitPreferenceValues(values)
	ignored := append([]string(nil), overflow...)
	for key := range limited {
		if key == "theme" || key == "lang" {
			ignored = append(ignored, key)
		} else if !controls[key] {
			ignored = append(ignored, key)
		}
	}
	sort.Strings(ignored)
	return []string{}, uniqueStrings(ignored)
}

func applyCookieConsent(w http.ResponseWriter, r *http.Request, cfg *config.ServerConfig, values url.Values) {
	previous := readCookieConsent(r)
	consent := consentCookie{
		Essential:   true,
		Preferences: values.Get("consent_preferences") == "on" || values.Get("consent_preferences") == "true",
		Analytics:   values.Get("consent_analytics") == "on" || values.Get("consent_analytics") == "true",
		Timestamp:   time.Now().Unix(),
	}
	if !cfg.Server.Privacy.Cookies.Preferences.Enabled {
		consent.Preferences = false
	}
	if !cfg.Server.Privacy.Cookies.Analytics.Enabled {
		consent.Analytics = false
	}
	if cfg.Web.Headers.ClearSiteData.OnConsentWithdrawal &&
		((previous.Preferences && !consent.Preferences) || (previous.Analytics && !consent.Analytics)) {
		w.Header().Set("Clear-Site-Data", `"cache", "storage"`)
	}
	auditConsentChange(r, cfg, "preferences", consent.Preferences)
	auditConsentChange(r, cfg, "analytics", consent.Analytics)
	raw, err := json.Marshal(consent)
	if err == nil {
		setPreferenceCookie(w, r, "cookie_consent", url.PathEscape(string(raw)))
	}
	if !consent.Preferences {
		deletePreferenceCookie(w, r, "theme")
		deletePreferenceCookie(w, r, i18n.CookieName)
	}
	if cfg.Server.Privacy.Data.Sold {
		if values.Get("ccpa_opt_out") == "on" || values.Get("ccpa_opt_out") == "true" {
			setPreferenceCookie(w, r, "ccpa_opt_out", "true")
			auditConsentChange(r, cfg, "ccpa_opt_out", true)
		} else {
			deletePreferenceCookie(w, r, "ccpa_opt_out")
			auditConsentChange(r, cfg, "ccpa_opt_out", false)
		}
	}
}

func auditConsentChange(r *http.Request, cfg *config.ServerConfig, consentType string, value bool) {
	actorType := "anonymous"
	actor := "anonymous"
	details := map[string]any{
		"actor.type":     actorType,
		"consent_type":   consentType,
		"new_value":      value,
		"policy_version": cfg.Server.Privacy.Consent.Policy.Version,
		"result":         "success",
	}
	if credential, ok := ownerCookieCredential(r, cfg); ok {
		actorType = "owner"
		actor = credential.OwnerID
		details["actor.type"] = actorType
		details["actor.id"] = credential.OwnerID
	}
	ip := anonymizedConsentIP(r)
	details["actor.ip"] = ip
	if err := database.WriteAuditEvent(r.Context(), "compliance.consent_updated", actor, ip, details); err != nil {
		slog.Warn("privacy: failed to write consent audit event", "error", err)
	}
}

func anonymizedConsentIP(r *http.Request) string {
	if IsTorRequest(r) {
		if circuitID, ok := r.Context().Value(torCircuitContextKey).(string); ok && circuitID != "" {
			return circuitID
		}
		return "tor"
	}
	if isI2PBackendRequest(r) {
		return "i2p"
	}
	ip := net.ParseIP(getClientIP(r))
	if ip == nil {
		return ""
	}
	if ipv4 := ip.To4(); ipv4 != nil {
		return net.IPv4(ipv4[0], ipv4[1], ipv4[2], 0).String()
	}
	ipv6 := ip.To16()
	if ipv6 == nil {
		return ""
	}
	for i := 6; i < len(ipv6); i++ {
		ipv6[i] = 0
	}
	return ipv6.String()
}

func deletePreferenceCookie(w http.ResponseWriter, r *http.Request, name string) {
	proto, _, _ := GetURLVars(r)
	http.SetCookie(w, &http.Cookie{Name: name, Value: "", Path: "/", MaxAge: -1, Secure: proto == "https", SameSite: http.SameSiteLaxMode})
}

func resetGuestPreferences(w http.ResponseWriter, r *http.Request, cfg *config.ServerConfig) {
	deleteNames := map[string]bool{"theme": true, i18n.CookieName: true, "cookie_consent": true, "dismissed_announcements": true, "ccpa_opt_out": true, buildCookieName: true}
	for _, cookie := range r.Cookies() {
		name := cookie.Name
		lower := strings.ToLower(name)
		if strings.Contains(lower, "csrf") || strings.Contains(lower, "token") || strings.Contains(lower, "session") || strings.Contains(lower, "auth") {
			continue
		}
		deleteNames[name] = true
	}
	for name := range deleteNames {
		deletePreferenceCookie(w, r, name)
	}
	if cfg.Web.Headers.ClearSiteData.OnConsentWithdrawal {
		w.Header().Set("Clear-Site-Data", `"cache", "storage"`)
	}
	w.Header().Set("Cache-Control", "no-store")
}

func preferenceSnapshot(r *http.Request, values url.Values) taggedResponseObject {
	theme, lang := currentPreferences(r)
	if requested := values.Get("theme"); requested == string(ThemeDark) || requested == string(ThemeLight) || requested == string(ThemeAuto) {
		theme = requested
	}
	if requested := values.Get("lang"); requested != "" {
		if canonical := canonicalLanguage(r, requested); canonical != "" {
			lang = canonical
		}
	}
	return responseObject(
		responseField("theme", theme),
		responseField("lang", lang),
		responseField("prefs", responseObject()),
	)
}

func writePreferenceJSON(w http.ResponseWriter, r *http.Request, applied, ignored []string, shareURL, code string) {
	data := preferenceSnapshot(r, nil)
	if shareURL != "" {
		data = append(data, responseField("url", shareURL), responseField("code", sanitize.PublicResponseValue{Value: code}))
	}
	if applied != nil || ignored != nil {
		if applied == nil {
			applied = []string{}
		}
		if ignored == nil {
			ignored = []string{}
		}
		data = append(data, responseField("applied", applied), responseField("ignored", ignored))
	}
	w.Header().Set("Cache-Control", "no-store")
	writeEnvelopeOK(w, http.StatusOK, data)
}

func canonicalLanguage(r *http.Request, input string) string {
	input = strings.TrimSpace(input)
	if input == "" {
		return ""
	}
	for _, language := range i18n.AvailableLanguages() {
		if strings.EqualFold(input, language.Code) && i18n.IsEnabledForRequest(r, language.Code) {
			return language.Code
		}
	}
	base := strings.SplitN(strings.ReplaceAll(input, "_", "-"), "-", 2)[0]
	for _, language := range i18n.AvailableLanguages() {
		if strings.EqualFold(base, language.Code) && i18n.IsEnabledForRequest(r, language.Code) {
			return language.Code
		}
	}
	return ""
}

// preferenceRedirectTarget picks where to send the visitor after a preference
// write. The referring page is preferred so the visitor lands back where they
// were, but only when it is a same-origin path — an attacker-supplied Referer
// must never turn this into an open redirect.
func preferenceRedirectTarget(r *http.Request) string {
	target := r.URL.Query().Get("redirect")
	if target == "" {
		target = r.PostForm.Get("redirect")
	}
	if validPreferenceRedirect(r, target) {
		return target
	}
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
	target = parsed.Path
	if parsed.RawQuery != "" {
		target += "?" + parsed.RawQuery
	}
	if !validPreferenceRedirect(r, target) {
		return "/"
	}
	return target
}

func validPreferenceRedirect(r *http.Request, target string) bool {
	if target == "" || strings.ContainsAny(target, "\r\n") || !strings.HasPrefix(target, "/") ||
		strings.HasPrefix(target, "//") || strings.HasPrefix(target, "/\\") {
		return false
	}
	parsed, err := url.Parse(target)
	if err != nil || parsed.IsAbs() || parsed.Host != "" || parsed.Scheme != "" {
		return false
	}
	if parsed.Path == "/server/preferences" || parsed.Path == "/server/preferences/sync" ||
		parsed.Path == prefImportPath || strings.HasPrefix(parsed.Path, prefImportPath+"/") {
		return false
	}
	return true
}

func syncRedirectTarget(r *http.Request) string {
	target := r.URL.Query().Get("redirect")
	if target == "" {
		target = r.PostForm.Get("redirect")
	}
	if validPreferenceRedirect(r, target) {
		return target
	}
	if ref := r.Referer(); ref != "" {
		parsed, err := url.Parse(ref)
		if err == nil && (parsed.Host == "" || parsed.Host == r.Host) {
			target = parsed.Path
			if parsed.RawQuery != "" {
				target += "?" + parsed.RawQuery
			}
			if validPreferenceRedirect(r, target) {
				return target
			}
		}
	}
	return "/server/preferences/sync"
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
func preferencesPageHandler(cfg *config.ServerConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		r = requestWithServerConfig(r, cfg)
		theme, lang := currentPreferences(r)
		query := preferenceQuery(theme, lang)

		data := newPageData(cfg, r, "preferences")
		data.PageTitle = i18n.Translate(data.Lang, "nav.preferences")
		data.PageDescription = i18n.Translate(data.Lang, "page_text.text.theme.and.language.preferences.are.stored.in.ed090ff2da")
		data.PrefExportURL = requestBaseURL(r) + prefImportPath + "?" + query
		data.PrefExportCode = preferenceCode(query)
		if cookie, err := r.Cookie(i18n.CookieName); err == nil {
			data.PreferenceLanguage = canonicalLanguage(r, cookie.Value)
		}
		consent := readCookieConsent(r)
		data.ConsentPreferences = consent.Preferences
		data.ConsentAnalytics = consent.Analytics
		data.CCPAOptOut = requestCookieValue(r, "ccpa_opt_out") == "true"
		data.CancelURL = preferenceRedirectTarget(r)
		w.Header().Set("Cache-Control", "no-store")
		renderPage(w, "preferences", data)
	}
}

// preferencesSaveHandler applies a submitted preference form and redirects
// back to the page the visitor came from. It answers 303 See Other so the
// browser re-issues a GET and a refresh never resubmits the form.
func preferencesSaveHandler(cfg *config.ServerConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		r = requestWithServerConfig(r, cfg)
		_ = r.ParseForm()
		w.Header().Set("Cache-Control", "no-store")
		if r.PostForm.Get("action") == "reset" {
			resetGuestPreferences(w, r, cfg)
			redirectSeeOtherNoBody(w, "/")
			return
		}
		allowed := preferenceCookieConsent(r)
		if r.PostForm.Get("consent_form") == "1" {
			allowed = cfg.Server.Privacy.Cookies.Preferences.Enabled && (r.PostForm.Get("consent_preferences") == "on" || r.PostForm.Get("consent_preferences") == "true")
		}
		applyPreferenceValuesWhenAllowed(w, r, url.Values(r.PostForm), true, allowed)
		if r.PostForm.Get("consent_form") == "1" {
			applyCookieConsent(w, r, cfg, url.Values(r.PostForm))
		}
		redirectSeeOtherNoBody(w, preferenceRedirectTarget(r))
	}
}

func ccpaOptOutHandler(cfg *config.ServerConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		r = requestWithServerConfig(r, cfg)
		_ = r.ParseForm()
		if cfg.Server.Privacy.Data.Sold {
			if r.PostForm.Get("ccpa_opt_out") == "true" || r.PostForm.Get("ccpa_opt_out") == "on" {
				setPreferenceCookie(w, r, "ccpa_opt_out", "true")
				auditConsentChange(r, cfg, "ccpa_opt_out", true)
			} else {
				deletePreferenceCookie(w, r, "ccpa_opt_out")
				auditConsentChange(r, cfg, "ccpa_opt_out", false)
			}
		}
		w.Header().Set("Cache-Control", "no-store")
		redirectSeeOtherNoBody(w, preferenceRedirectTarget(r))
	}
}

// preferencesSyncPageHandler renders the existing export and import actions as
// one preferences sub-page, with native links and forms that work without JS.
func preferencesSyncPageHandler(cfg *config.ServerConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		r = requestWithServerConfig(r, cfg)
		theme, lang := currentPreferences(r)
		query := preferenceQuery(theme, lang)
		shareURL := requestBaseURL(r) + prefImportPath + "?" + query
		code := preferenceCode(query)
		accept := r.Header.Get("Accept")
		w.Header().Add("Vary", "Accept")
		if strings.Contains(accept, "text/plain") {
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			w.Header().Set("Cache-Control", "no-store")
			w.WriteHeader(http.StatusOK)
			if _, err := fmt.Fprintf(w, "URL: %s\nCode: %s\n", shareURL, code); err != nil {
				return
			}
			return
		}
		if strings.Contains(accept, "application/json") {
			writePreferenceJSON(w, r, []string(nil), []string(nil), shareURL, code)
			return
		}

		data := newPageData(cfg, r, "preferences")
		data.PageTitle = i18n.Translate(data.Lang, "page_text.text.sync.preferences.fc79211080")
		data.PageDescription = i18n.Translate(data.Lang, "page_text.text.transfer.your.theme.and.language.settings.be.b75518dbbc")
		data.PrefExportURL = shareURL
		data.PrefExportCode = code
		w.Header().Set("Cache-Control", "no-store")
		renderPage(w, "preferences_sync", data)
	}
}

func preferencesSyncPostHandler(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	values := mergedPreferenceInput(r.PostForm.Get("code"), r.PostForm)
	delete(values, "redirect")
	applyPreferenceValues(w, r, values, false)
	w.Header().Set("Cache-Control", "no-store")
	redirectSeeOtherNoBody(w, syncRedirectTarget(r))
}

// preferencesImportHandler validates the supplied preferences, sets the
// matching cookies and redirects, so the code never lingers in the visible URL
// or in browser history.
func preferencesImportHandler(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	values := mergedPreferenceInput(query.Get("code"), query)
	applyPreferenceValues(w, r, values, false)
	w.Header().Set("Cache-Control", "no-store")
	redirectSeeOtherNoBody(w, preferenceRedirectTarget(r))
}

// apiPreferencesHandler returns the visitor's current exportable preferences.
func apiPreferencesHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	writeEnvelopeOK(w, http.StatusOK, preferenceSnapshot(r, nil))
}

// apiPreferencesSaveHandler applies preferences supplied as form values or
// query parameters and echoes the resulting state.
func apiPreferencesSaveHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	values, err := requestPreferenceValues(r)
	if err != nil {
		writeLocalizedAPIError(w, r, http.StatusBadRequest, "BAD_REQUEST")
		return
	}
	applied, ignored := applyPreferenceValues(w, r, values, false)
	if len(applied) == 0 {
		writeLocalizedAPIError(w, r, http.StatusBadRequest, "VALIDATION_FAILED")
		return
	}
	data := preferenceSnapshot(r, values)
	data = append(data, responseField("applied", applied), responseField("ignored", ignored))
	w.Header().Set("Cache-Control", "no-store")
	writeEnvelopeOK(w, http.StatusOK, data)
}

func requestPreferenceValues(r *http.Request) (url.Values, error) {
	if strings.Contains(strings.ToLower(r.Header.Get("Content-Type")), "application/json") {
		var raw map[string]json.RawMessage
		decoder := json.NewDecoder(io.LimitReader(r.Body, 64*1024+1))
		if err := decoder.Decode(&raw); err != nil {
			return nil, err
		}
		if decoder.Decode(new(interface{})) != io.EOF {
			return nil, fmt.Errorf("invalid or oversized JSON request")
		}
		values := make(url.Values)
		for key, item := range raw {
			var value string
			if err := json.Unmarshal(item, &value); err == nil {
				values.Set(key, value)
			} else {
				values.Set(key, string(item))
			}
		}
		return values, nil
	}
	if err := r.ParseForm(); err != nil {
		return nil, err
	}
	return url.Values(r.Form), nil
}

func redirectSeeOtherNoBody(w http.ResponseWriter, target string) {
	w.Header().Set("Location", target)
	w.WriteHeader(http.StatusSeeOther)
}

func preferencesSyncAPIHandler(cfg *config.ServerConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		r = requestWithServerConfig(r, cfg)
		theme, lang := currentPreferences(r)
		query := preferenceQuery(theme, lang)
		writePreferenceJSON(w, r, nil, nil, requestBaseURL(r)+prefImportPath+"?"+query, preferenceCode(query))
	}
}

func preferencesImportAPIHandler(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	values := mergedPreferenceInput(query.Get("code"), query)
	applied, ignored := applyPreferenceValues(w, r, values, false)
	data := preferenceSnapshot(r, values)
	data = append(data, responseField("applied", applied), responseField("ignored", ignored))
	w.Header().Set("Cache-Control", "no-store")
	writeEnvelopeOK(w, http.StatusOK, data)
}
