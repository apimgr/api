package server

import (
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/apimgr/api/src/common/i18n"
	"github.com/apimgr/api/src/config"
)

const announcementCookieName = "dismissed_announcements"

var announcementIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

type siteAnnouncement struct {
	ID          string
	Type        string
	Title       string
	Message     string
	Role        string
	Icon        string
	Dismissible bool
}

func siteBannerData(cfg *config.ServerConfig, r *http.Request) (bool, string, string, string, string, string, []siteAnnouncement) {
	privacy := cfg.Server.Privacy
	lang := i18n.LangFromRequest(r)
	consentVisible := privacy.Consent.ShowUntilAcknowledged && !hasValidCookieConsent(r) && r.URL.Path != "/server/preferences"
	policyURL := privacy.Consent.Policy.URL
	if policyURL == "" || !strings.HasPrefix(policyURL, "/") || strings.HasPrefix(policyURL, "//") {
		policyURL = "/server/privacy"
	}
	policyText := localizedConfigText(privacy.Consent.Policy.Text, config.DefaultPrivacyConsentPolicyText, lang, "common.privacy_policy")
	accept := localizedConfigText(privacy.Consent.Buttons.Accept, config.DefaultPrivacyConsentAcceptText, lang, "cookie_consent.accept")
	decline := localizedConfigText(privacy.Consent.Buttons.Decline, config.DefaultPrivacyConsentDeclineText, lang, "cookie_consent.decline")
	messageKey, defaultMessage := "cookie_consent.message", config.DefaultPrivacyConsentMessage
	if privacy.Data.Sold {
		messageKey, defaultMessage = "cookie_consent.message_if_sold", config.DefaultPrivacyConsentMessageIfSold
	}
	message := localizedConfigText(privacy.ConsentMessage(), defaultMessage, lang, messageKey)
	announcements := activeAnnouncements(cfg.Web.Announcements, r, time.Now())
	return consentVisible, message, policyURL, policyText, decline, accept, announcements
}

func localizedConfigText(value, defaultValue, lang, key string) string {
	if value == "" || value == defaultValue {
		return i18n.Translate(lang, key)
	}
	return value
}

func activeAnnouncements(cfg config.AnnouncementsConfig, r *http.Request, now time.Time) []siteAnnouncement {
	if !cfg.Enabled {
		return nil
	}
	dismissed := readDismissedAnnouncements(r)
	active := make([]siteAnnouncement, 0, len(cfg.Messages))
	for _, message := range cfg.Messages {
		if !announcementIDPattern.MatchString(message.ID) || message.Message == "" || dismissed[message.ID] {
			continue
		}
		kind := strings.ToLower(message.Type)
		role, icon := "status", "ℹ"
		switch kind {
		case "info":
		case "success":
			icon = "✓"
		case "warning":
			role, icon = "alert", "⚠"
		case "error":
			role, icon = "alert", "✕"
		default:
			continue
		}
		if message.Start != "" {
			start, err := time.Parse(time.RFC3339, message.Start)
			if err != nil || now.Before(start) {
				continue
			}
		}
		if message.End != "" {
			end, err := time.Parse(time.RFC3339, message.End)
			if err != nil || !now.Before(end) {
				continue
			}
		}
		active = append(active, siteAnnouncement{
			ID: message.ID, Type: kind, Title: message.Title, Message: message.Message,
			Role: role, Icon: icon, Dismissible: message.Dismissible,
		})
	}
	return active
}

func readDismissedAnnouncements(r *http.Request) map[string]bool {
	dismissed := map[string]bool{}
	for _, id := range dismissedAnnouncementIDs(r) {
		dismissed[id] = true
	}
	return dismissed
}

func dismissedAnnouncementIDs(r *http.Request) []string {
	value := requestCookieValue(r, announcementCookieName)
	if decoded, err := url.QueryUnescape(value); err == nil {
		value = decoded
	}
	seen := map[string]bool{}
	ids := make([]string, 0)
	for _, id := range strings.Split(value, ",") {
		if announcementIDPattern.MatchString(id) && !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	return ids
}

func announcementDismissHandler(cfg *config.ServerConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		r = requestWithServerConfig(r, cfg)
		_ = r.ParseForm()
		id := r.PostForm.Get("id")
		if !announcementIDPattern.MatchString(id) {
			writeEnvelopeError(w, http.StatusBadRequest, "INVALID_ANNOUNCEMENT", i18n.Translate(i18n.LangFromRequest(r), "errors.bad_request"), nil)
			return
		}
		valid := false
		for _, announcement := range activeAnnouncements(cfg.Web.Announcements, r, time.Now()) {
			if announcement.ID == id {
				for _, configured := range cfg.Web.Announcements.Messages {
					if configured.ID == id && configured.Dismissible {
						valid = true
						break
					}
				}
				break
			}
		}
		if !valid {
			writeEnvelopeError(w, http.StatusNotFound, "ANNOUNCEMENT_NOT_FOUND", i18n.Translate(i18n.LangFromRequest(r), "errors.not_found"), nil)
			return
		}
		ids := dismissedAnnouncementIDs(r)
		alreadyDismissed := false
		for _, dismissed := range ids {
			if dismissed == id {
				alreadyDismissed = true
				break
			}
		}
		if !alreadyDismissed {
			ids = append(ids, id)
		}
		encoded := url.QueryEscape(strings.Join(ids, ","))
		for len(ids) > 1 && len(encoded) > 3500 {
			ids = ids[1:]
			encoded = url.QueryEscape(strings.Join(ids, ","))
		}
		setPreferenceCookie(w, r, announcementCookieName, encoded)
		target := r.PostForm.Get("return_to")
		if !validPreferenceRedirect(r, target) {
			target = preferenceRedirectTarget(r)
		}
		w.Header().Set("Cache-Control", "no-store")
		redirectSeeOtherNoBody(w, target)
	}
}

func consentHandler(cfg *config.ServerConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		r = requestWithServerConfig(r, cfg)
		_ = r.ParseForm()
		values := url.Values{}
		switch r.PostForm.Get("choice") {
		case "accept":
			if cfg.Server.Privacy.Cookies.Preferences.Enabled {
				values.Set("consent_preferences", "on")
			}
			if cfg.Server.Privacy.Cookies.Analytics.Enabled {
				values.Set("consent_analytics", "on")
			}
		case "decline":
			if cfg.Web.Headers.ClearSiteData.OnConsentWithdrawal {
				w.Header().Set("Clear-Site-Data", `"cache", "storage"`)
			}
		default:
			writeEnvelopeError(w, http.StatusBadRequest, "INVALID_CONSENT_CHOICE", i18n.Translate(i18n.LangFromRequest(r), "errors.bad_request"), nil)
			return
		}
		applyCookieConsent(w, r, cfg, values)
		w.Header().Set("Cache-Control", "no-store")
		target := r.PostForm.Get("return_to")
		if !validPreferenceRedirect(r, target) {
			target = preferenceRedirectTarget(r)
		}
		redirectSeeOtherNoBody(w, target)
	}
}
