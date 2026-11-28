package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/apimgr/api/src/common/i18n"
	"github.com/apimgr/api/src/config"
	"github.com/apimgr/api/src/database"
	"github.com/apimgr/api/src/paths"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestActiveAnnouncementsFiltersScheduleTypeAndDismissal(t *testing.T) {
	now := time.Date(2026, time.November, 26, 12, 0, 0, 0, time.UTC)
	cfg := config.AnnouncementsConfig{
		Enabled: true,
		Messages: []config.AnnouncementMessage{
			{ID: "active", Type: "warning", Message: "Active", Start: now.Add(-time.Hour).Format(time.RFC3339), End: now.Add(time.Hour).Format(time.RFC3339)},
			{ID: "future", Type: "info", Message: "Future", Start: now.Add(time.Hour).Format(time.RFC3339)},
			{ID: "expired", Type: "success", Message: "Expired", End: now.Format(time.RFC3339)},
			{ID: "bad-type", Type: "critical", Message: "Invalid"},
			{ID: "bad,id", Type: "info", Message: "Invalid ID"},
		},
	}
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.AddCookie(&http.Cookie{Name: announcementCookieName, Value: "active"})

	assert.Empty(t, activeAnnouncements(cfg, request, now))
	request = httptest.NewRequest(http.MethodGet, "/", nil)
	active := activeAnnouncements(cfg, request, now)
	require.Len(t, active, 1)
	assert.Equal(t, "active", active[0].ID)
	assert.Equal(t, "alert", active[0].Role)
}

func TestCookieConsentValidationRequiresCompleteEncodedState(t *testing.T) {
	valid := url.PathEscape(`{"essential":true,"preferences":false,"analytics":false,"timestamp":1}`)
	for _, raw := range []string{"", "not-json", url.PathEscape(`{"essential":true}`), url.PathEscape(`{"essential":true,"preferences":false,"analytics":false,"timestamp":0}`)} {
		_, accepted := decodeCookieConsent(raw)
		assert.False(t, accepted, raw)
	}
	state, accepted := decodeCookieConsent(valid)
	assert.True(t, accepted)
	assert.True(t, state.Essential)
	assert.False(t, state.Preferences)
}

func TestAnonymizedConsentIPHandlesClearnetTorAndI2P(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "/server/consent", nil)
	request.RemoteAddr = "192.0.2.129:4321"
	assert.Equal(t, "192.0.2.0", anonymizedConsentIP(request))

	request = httptest.NewRequest(http.MethodPost, "/server/consent", nil)
	request.RemoteAddr = "2001:db8:abcd:1234::1:4321"
	assert.Equal(t, "2001:db8:abcd::", anonymizedConsentIP(request))

	request = request.WithContext(context.WithValue(request.Context(), torCircuitContextKey, "tor:0123456789abcdef"))
	assert.Equal(t, "tor:0123456789abcdef", anonymizedConsentIP(request))

	request = httptest.NewRequest(http.MethodPost, "/server/consent", nil)
	request = request.WithContext(context.WithValue(request.Context(), i2pBackendContextKey{}, true))
	assert.Equal(t, "i2p", anonymizedConsentIP(request))
}

func TestSiteBannerLayoutRendersConsentAndAnnouncementBeforeHeader(t *testing.T) {
	require.NoError(t, initTemplates())
	data := PageData{
		SiteTitle: "api", Lang: "en", LangDir: "ltr", ThemeClass: "theme-dark",
		BaseURL: "https://example.test", CSRFToken: "csrf", ConsentVisible: true,
		ConsentMessage: "Cookie message", ConsentPolicyURL: "/server/privacy",
		ConsentPolicyText: "Privacy", ConsentDeclineText: "Decline", ConsentAcceptText: "Accept",
		ConsentShowPreferences: true, ConsentPreferencesText: "Manage Preferences",
		CurrentPath: "/server/about", PreferencesHubPath: "/server/preferences",
		Announcements: []siteAnnouncement{{ID: "maintenance", Type: "warning", Role: "alert", Icon: "⚠", Message: "Maintenance", Dismissible: true}},
	}
	var output strings.Builder
	require.NoError(t, pageTemplates["about"].ExecuteTemplate(&output, "public.tmpl", data))
	html := output.String()
	assert.Less(t, strings.Index(html, `class="skip-link"`), strings.Index(html, `class="cookie-banner"`))
	assert.Less(t, strings.Index(html, `class="cookie-banner"`), strings.Index(html, `class="site-banner site-banner-warning"`))
	assert.Less(t, strings.Index(html, `class="site-banner site-banner-warning"`), strings.Index(html, `<header class="header"`))
	assert.Contains(t, html, `action="/server/consent"`)
	assert.Contains(t, html, `action="/announcements/dismiss"`)

	data.ActivePage = "preferences"
	data.CurrentPath = "/server/preferences"
	data.PreferencesHubPath = "/server/preferences"
	data.ConsentVisible = false
	output.Reset()
	require.NoError(t, pageTemplates["preferences"].ExecuteTemplate(&output, "public.tmpl", data))
	html = output.String()
	assert.NotContains(t, html, `class="cookie-banner`)
	assert.NotContains(t, html, `action="/server/consent"`)
	assert.Contains(t, html, `class="site-banner site-banner-warning"`)
	assert.NotContains(t, html, `class="site-banner-dismiss"`)
	assert.NotContains(t, html, `action="/announcements/dismiss"`)
	assert.NotContains(t, html, `class="theme-toggle-form"`)
	assert.Equal(t, 1, strings.Count(html, `method="POST"`))
	assert.Equal(t, 1, strings.Count(html, `id="preferences-form"`))
}

func TestPageDataLocalizesDefaultPrivacyCopy(t *testing.T) {
	cfg := newTestConfig(t)
	request := httptest.NewRequest(http.MethodGet, "/server/privacy?lang=es", nil)
	data := newPageData(cfg, request, "privacy")

	assert.Equal(t, i18n.Translate("es", "cookie_consent.message"), data.ConsentMessage)
	assert.Equal(t, i18n.Translate("es", "cookie_consent.accept"), data.ConsentAcceptText)
	assert.Equal(t, i18n.Translate("es", "cookie_consent.decline"), data.ConsentDeclineText)
	assert.Equal(t, i18n.Translate("es", "common.privacy_policy"), data.ConsentPolicyText)
	assert.Equal(t, i18n.Translate("es", "cookie_consent.manage_preferences"), data.ConsentPreferencesText)
	assert.Equal(t, i18n.Translate("es", "cookie_consent.essential_description"), data.Privacy.Cookies.Essential.Description)
	assert.Equal(t, i18n.Translate("es", "cookie_consent.preference_description"), data.Privacy.Cookies.Preferences.Description)
	assert.Equal(t, i18n.Translate("es", "cookie_consent.analytics_description")+" "+i18n.Translate("es", "cookie_consent.analytics_suffix_not_sold"), data.Privacy.AnalyticsCookieDescription())

	cfg.Server.Privacy.Consent.Message = "Custom consent message"
	cfg.Server.Privacy.Consent.Buttons.Accept = "Continue"
	cfg.Server.Privacy.Cookies.Essential.Description = "Custom essential description"
	custom := newPageData(cfg, request, "privacy")
	assert.Equal(t, "Custom consent message", custom.ConsentMessage)
	assert.Equal(t, "Continue", custom.ConsentAcceptText)
	assert.Equal(t, "Custom essential description", custom.Privacy.Cookies.Essential.Description)
}

func TestSiteBannerDataLocalizesSoldMessage(t *testing.T) {
	cfg := newTestConfig(t)
	cfg.Server.Privacy.Data.Sold = true
	request := httptest.NewRequest(http.MethodGet, "/?lang=fr", nil)
	_, message, _, _, _, _, _ := siteBannerData(cfg, request)
	assert.Equal(t, i18n.Translate("fr", "cookie_consent.message_if_sold"), message)
}

func TestSiteBannerDataOmitsConsentOnPreferencesPage(t *testing.T) {
	cfg := newTestConfig(t)
	cfg.Server.Privacy.Consent.ShowUntilAcknowledged = true

	for _, path := range []string{"/", "/server/privacy"} {
		request := httptest.NewRequest(http.MethodGet, path, nil)
		visible, _, _, _, _, _, _ := siteBannerData(cfg, request)
		assert.True(t, visible, path)
	}

	request := httptest.NewRequest(http.MethodGet, "/server/preferences?lang=fr", nil)
	visible, _, _, _, _, _, _ := siteBannerData(cfg, request)
	assert.False(t, visible)
}

func TestConsentHandlerStoresAcceptAndDeclineChoices(t *testing.T) {
	for _, test := range []struct {
		name        string
		choice      string
		preferences bool
		analytics   bool
	}{
		{name: "accept", choice: "accept", preferences: true, analytics: true},
		{name: "decline", choice: "decline"},
	} {
		t.Run(test.name, func(t *testing.T) {
			cfg := newTestConfig(t)
			form := url.Values{"choice": {test.choice}, "return_to": {"/server/about"}}
			request := httptest.NewRequest(http.MethodPost, "/server/consent", strings.NewReader(form.Encode()))
			request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			response := httptest.NewRecorder()
			consentHandler(cfg)(response, request)

			assert.Equal(t, http.StatusSeeOther, response.Code)
			assert.Equal(t, "/server/about", response.Header().Get("Location"))
			if test.choice == "decline" {
				assert.Equal(t, `"cache", "storage"`, response.Header().Get("Clear-Site-Data"))
				assert.NotContains(t, response.Header().Get("Clear-Site-Data"), "cookies")
			}
			cookie := response.Result().Cookies()[0]
			request = httptest.NewRequest(http.MethodGet, "/", nil)
			request.AddCookie(cookie)
			consent := readCookieConsent(request)
			assert.True(t, consent.Essential)
			assert.Equal(t, test.preferences, consent.Preferences)
			assert.Equal(t, test.analytics, consent.Analytics)
		})
	}
}

func TestConsentHandlerAuditsAnonymousChoiceWithPolicyVersionAndAnonymizedIP(t *testing.T) {
	cfg := newTestConfig(t)
	cfg.Server.Privacy.Consent.Policy.Version = "v2"
	require.NoError(t, database.InitDatabase(cfg.Server.Database, paths.DataDir()))
	t.Cleanup(func() { _ = database.Close() })

	form := url.Values{"choice": {"decline"}, "return_to": {"/server/about"}}
	request := httptest.NewRequest(http.MethodPost, "/server/consent", strings.NewReader(form.Encode()))
	request.RemoteAddr = "192.0.2.129:4321"
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response := httptest.NewRecorder()
	consentHandler(cfg)(response, request)
	require.Equal(t, http.StatusSeeOther, response.Code)

	rows, err := database.GetServerDB().Query(`SELECT details FROM audit_log WHERE event = 'compliance.consent_updated' ORDER BY id`)
	require.NoError(t, err)
	var events []map[string]any
	for rows.Next() {
		var raw string
		require.NoError(t, rows.Scan(&raw))
		var details map[string]any
		require.NoError(t, json.Unmarshal([]byte(raw), &details))
		events = append(events, details)
	}
	require.NoError(t, rows.Err())
	require.NoError(t, rows.Close())
	require.Len(t, events, 2)
	for _, details := range events {
		assert.Equal(t, "anonymous", details["actor.type"])
		assert.NotContains(t, details, "actor.id")
		assert.Equal(t, "192.0.2.0", details["actor.ip"])
		assert.Equal(t, "v2", details["policy_version"])
		assert.Equal(t, "success", details["result"])
	}
}

func TestAnnouncementDismissHandlerStoresOnlyConfiguredDismissibleIDs(t *testing.T) {
	cfg := newTestConfig(t)
	cfg.Web.Announcements = config.AnnouncementsConfig{Enabled: true, Messages: []config.AnnouncementMessage{
		{ID: "maintenance", Type: "warning", Message: "Maintenance", Dismissible: true},
	}}
	form := url.Values{"id": {"maintenance"}, "return_to": {"/server/about"}}
	request := httptest.NewRequest(http.MethodPost, "/announcements/dismiss", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response := httptest.NewRecorder()
	announcementDismissHandler(cfg)(response, request)

	assert.Equal(t, http.StatusSeeOther, response.Code)
	assert.Equal(t, "/server/about", response.Header().Get("Location"))
	cookies := response.Result().Cookies()
	require.Len(t, cookies, 1)
	request = httptest.NewRequest(http.MethodGet, "/", nil)
	request.AddCookie(cookies[0])
	assert.True(t, readDismissedAnnouncements(request)["maintenance"])
}
