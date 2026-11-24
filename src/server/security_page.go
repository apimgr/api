package server

import (
	"net/http"
	"strings"
	"time"

	"github.com/apimgr/api/src/common/i18n"
	"github.com/apimgr/api/src/config"
)

func securityContacts(cfg *config.Config, r *http.Request) []string {
	contacts := make([]string, 0, 3)
	if reportURL := strings.TrimSpace(cfg.Web.Security.ReportURL); reportURL != "" {
		contacts = append(contacts, reportURL)
	}
	if contactPageEnabled(cfg) {
		if id, _, err := currentSecurityID(r.Context(), time.Now().UTC()); err == nil {
			contacts = append(contacts, BuildURL(r, "/server/contact?security_id="+id))
		}
	}
	if email := strings.TrimSpace(cfg.Server.Contact.ResolveSecurity().Email); email != "" {
		contacts = append(contacts, "mailto:"+securityMailto(email))
	}
	return contacts
}

func contactPageEnabled(cfg *config.Config) bool {
	if cfg == nil || cfg.Server.Pages == nil {
		return true
	}
	contact, ok := cfg.Server.Pages["contact"].(map[string]interface{})
	if !ok {
		return true
	}
	enabled, ok := contact["enabled"].(bool)
	return !ok || enabled
}

func securityMailto(email string) string {
	email = strings.TrimSpace(email)
	if len(email) >= len("mailto:") && strings.EqualFold(email[:len("mailto:")], "mailto:") {
		email = email[len("mailto:"):]
	}
	return email
}

func securityPageHandler(cfg *config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		r = requestWithServerConfig(r, cfg)
		data := newPageData(cfg, r, "security")
		data.PageTitle = i18n.Translate(data.Lang, "security.title")
		data.PageDescription = i18n.TranslateFormat(data.Lang, "security.lead", map[string]string{"app_name": data.SiteTitle})
		data.SecurityReportURL = cfg.Web.Security.ReportURL
		data.SecurityExpires = cfg.Web.Security.Expires.UTC().Format(time.RFC3339)
		data.SecurityEmail = securityMailto(cfg.Server.Contact.ResolveSecurity().Email)
		if cfg.Web.Security.PublishPGPKey {
			if _, _, err := readSecurityPublicKey(); err == nil {
				data.SecurityPGPURL = BuildURL(r, "/.well-known/pgp-key.asc")
			}
		}
		if id, _, err := currentSecurityID(r.Context(), time.Now().UTC()); err == nil {
			data.SecurityContactURL = BuildURL(r, "/server/contact?security_id="+id)
		}
		w.Header().Set("Cache-Control", "no-store")
		renderPage(w, "security", data)
	}
}

func securityAPIHandler(cfg *config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		r = requestWithServerConfig(r, cfg)
		pgpURL, fingerprint := any(nil), any(nil)
		if cfg.Web.Security.PublishPGPKey {
			if _, keyFingerprint, err := readSecurityPublicKey(); err == nil {
				pgpURL, fingerprint = BuildURL(r, "/.well-known/pgp-key.asc"), keyFingerprint
			}
		}
		data := map[string]interface{}{
			"contacts":   securityContactDescriptors(cfg, r),
			"expires":    cfg.Web.Security.Expires.UTC().Format(time.RFC3339),
			"pgp":        map[string]interface{}{"available": pgpURL != nil, "url": pgpURL, "fingerprint": fingerprint},
			"policy_url": BuildURL(r, "/server/security/policy"),
			"thanks_url": BuildURL(r, "/server/security/thanks"),
		}
		if contactPageEnabled(cfg) {
			data["report_url"] = BuildURL(r, "/server/contact")
		}
		writeEnvelopeOK(w, http.StatusOK, data)
	}
}

func securityContactDescriptors(cfg *config.Config, r *http.Request) []map[string]string {
	contacts := make([]map[string]string, 0, 3)
	if reportURL := strings.TrimSpace(cfg.Web.Security.ReportURL); reportURL != "" {
		contacts = append(contacts, map[string]string{"type": "github", "value": reportURL})
	}
	if contactPageEnabled(cfg) {
		if id, _, err := currentSecurityID(r.Context(), time.Now().UTC()); err == nil {
			contacts = append(contacts, map[string]string{"type": "contact_form", "value": BuildURL(r, "/server/contact?security_id="+id)})
		}
	}
	if email := strings.TrimSpace(cfg.Server.Contact.ResolveSecurity().Email); email != "" {
		contacts = append(contacts, map[string]string{"type": "email", "value": "mailto:" + securityMailto(email)})
	}
	return contacts
}
