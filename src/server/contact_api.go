package server

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/apimgr/api/src/common/i18n"
	"github.com/apimgr/api/src/config"
	"github.com/go-chi/chi/v5"
)

type contactAPIDescriptor struct {
	Name          string   `json:"name"`
	Type          string   `json:"type"`
	Required      bool     `json:"required"`
	MaxLength     *int     `json:"max_length"`
	RequiredWhen  any      `json:"required_when"`
	MaxLengthWhen any      `json:"max_length_when"`
	Options       []string `json:"options,omitempty"`
}

type contactRequiredWhen struct {
	Field  string   `json:"field"`
	Equals string   `json:"equals,omitempty"`
	In     []string `json:"in,omitempty"`
}

type contactMaxLengthWhen struct {
	Prefix    string `json:"prefix"`
	MaxLength int    `json:"max_length"`
}

// contactAPIHandler serves the machine-readable contact form contract and its
// two-step submission flow.
func contactAPIHandler(cfg *config.Config, router chi.Router) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !contactConfigEnabled(cfg) {
			contactAPIError(w, r, http.StatusNotFound, "NOT_FOUND", "contact.errors.unavailable", nil)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		if r.Method == http.MethodGet {
			contactAPIGet(cfg, w, r)
			return
		}
		if r.Method == http.MethodPost {
			r = r.WithContext(context.WithValue(r.Context(), contactRouteMatcherContextKey{}, router))
			contactAPIPost(cfg, w, r)
			return
		}
		contactAPIError(w, r, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "errors.method_not_allowed", nil)
	}
}

func contactAPIGet(cfg *config.Config, w http.ResponseWriter, r *http.Request) {
	lang := i18n.LangFromRequest(r)
	fields := contactAPIFields()
	securityURL := ""
	id, _, err := currentSecurityID(r.Context(), time.Now().UTC())
	if err != nil {
		contactAPIError(w, r, http.StatusServiceUnavailable, "FORM_UNAVAILABLE", "contact.errors.unavailable", nil)
		return
	}
	securityURL = BuildURL(r, "/server/contact?security_id="+id)
	securityID := strings.TrimSpace(r.URL.Query().Get("security_id"))
	if securityID != "" {
		if _, valid, err := validSecurityID(r.Context(), securityID, time.Now().UTC()); err != nil || !valid {
			contactAPIError(w, r, http.StatusBadRequest, "VALIDATION_FAILED", "contact.errors.invalid_security_link", nil)
			return
		}
		fields = securityAPIFields()
	}
	captcha, err := IssueSimpleCaptcha(r.Context(), lang)
	if err != nil {
		contactAPIError(w, r, http.StatusServiceUnavailable, "CAPTCHA_UNAVAILABLE", "contact.errors.unavailable", nil)
		return
	}
	formToken, err := issueContactFormToken(r.Context())
	if err != nil {
		contactAPIError(w, r, http.StatusServiceUnavailable, "FORM_UNAVAILABLE", "contact.errors.unavailable", nil)
		return
	}
	abuseEmail := strings.TrimSpace(cfg.Server.Contact.Abuse.Email)
	if abuseEmail == "" {
		abuseEmail = strings.TrimSpace(cfg.Server.Contact.General.Email)
	}
	writeEnvelopeOK(w, http.StatusOK, map[string]interface{}{"fields": fields, "captcha": captcha, "security_url": securityURL, "abuse_email": abuseEmail, "form_token": formToken})
}

func contactAPIFields() []contactAPIDescriptor {
	fields := []contactAPIDescriptor{
		{Name: "name", Type: "text", Required: true, MaxLength: intPtr(100)},
		{Name: "email", Type: "email", Required: true, MaxLength: intPtr(254)},
		{Name: "subject", Type: "text", Required: true, MaxLength: intPtr(200)},
		{Name: "message", Type: "textarea", Required: true, MaxLength: intPtr(5000)},
		{Name: "type", Type: "enum", Required: true, Options: []string{"abuse", "dmca", "privacy"}},
		{Name: "reported_url", Type: "text", RequiredWhen: contactRequiredWhen{Field: "type", In: []string{"abuse", "dmca"}}, MaxLength: intPtr(2048)},
		{Name: "reason", Type: "select", RequiredWhen: contactRequiredWhen{Field: "type", Equals: "abuse"}, Options: []string{"illegal_content", "terms_violation", "harassment", "malware_phishing", "other"}},
		{Name: "copyrighted_work", Type: "textarea", RequiredWhen: contactRequiredWhen{Field: "type", Equals: "dmca"}, MaxLength: intPtr(2000)},
		{Name: "good_faith", Type: "checkbox", RequiredWhen: contactRequiredWhen{Field: "type", Equals: "dmca"}},
		{Name: "accuracy", Type: "checkbox", RequiredWhen: contactRequiredWhen{Field: "type", Equals: "dmca"}},
		{Name: "signature", Type: "text", RequiredWhen: contactRequiredWhen{Field: "type", Equals: "dmca"}, MaxLength: intPtr(100)},
		{Name: "request_kind", Type: "select", RequiredWhen: contactRequiredWhen{Field: "type", Equals: "privacy"}, Options: []string{"access", "export", "deletion", "correction", "opt_out", "other"}},
		{Name: "step", Type: "hidden", MaxLength: intPtr(10), Options: []string{"submit", "confirm", "edit"}},
		{Name: "review_token", Type: "hidden", RequiredWhen: contactRequiredWhen{Field: "step", Equals: "confirm"}, MaxLength: intPtr(300)},
		{Name: "confirmed", Type: "checkbox", RequiredWhen: contactRequiredWhen{Field: "step", Equals: "confirm"}},
		{Name: "captcha_id", Type: "hidden", Required: true},
		{Name: "captcha_answer", Type: "text", Required: true},
		{Name: "form_token", Type: "hidden", Required: true, MaxLength: intPtr(128)},
	}
	return fields
}

func intPtr(value int) *int { return &value }

func securityAPIFields() []contactAPIDescriptor {
	return []contactAPIDescriptor{
		{Name: "name", Type: "text", Required: true, MaxLength: intPtr(100)},
		{Name: "email", Type: "email", Required: true, MaxLength: intPtr(254)},
		{Name: "researcher_gpg", Type: "textarea", MaxLength: intPtr(16384), MaxLengthWhen: []contactMaxLengthWhen{{Prefix: "https://", MaxLength: 2048}}},
		{Name: "component", Type: "text", Required: true, MaxLength: intPtr(255)},
		{Name: "endpoint", Type: "text", MaxLength: intPtr(2048)},
		{Name: "severity", Type: "select", Required: true, Options: []string{"critical", "high", "medium", "low", "informational"}},
		{Name: "summary", Type: "text", Required: true, MaxLength: intPtr(200)},
		{Name: "steps", Type: "textarea", Required: true, MaxLength: intPtr(10000)},
		{Name: "impact", Type: "textarea", Required: true, MaxLength: intPtr(5000)},
		{Name: "suggested_fix", Type: "textarea", MaxLength: intPtr(5000)},
		{Name: "cve_requested", Type: "checkbox"},
		{Name: "timeline_days", Type: "number", MaxLength: intPtr(3)},
		{Name: "credit_preference", Type: "select", Required: true, Options: []string{"name", "handle", "no", "anonymous"}},
		{Name: "credit_handle", Type: "text", RequiredWhen: contactRequiredWhen{Field: "credit_preference", Equals: "handle"}, MaxLength: intPtr(100)},
		{Name: "disclosure_agreement", Type: "checkbox", Required: true},
		{Name: "step", Type: "hidden", MaxLength: intPtr(10), Options: []string{"submit", "confirm", "edit"}},
		{Name: "review_token", Type: "hidden", RequiredWhen: contactRequiredWhen{Field: "step", Equals: "confirm"}, MaxLength: intPtr(300)},
		{Name: "confirmed", Type: "checkbox", RequiredWhen: contactRequiredWhen{Field: "step", Equals: "confirm"}},
		{Name: "captcha_id", Type: "hidden", Required: true},
		{Name: "captcha_answer", Type: "text", Required: true},
		{Name: "form_token", Type: "hidden", Required: true, MaxLength: intPtr(128)},
	}
}

func contactAPIPost(cfg *config.Config, w http.ResponseWriter, r *http.Request) {
	settings := contactAntispamConfig(cfg)
	settings.RouteExists = func(path string) bool {
		router, ok := r.Context().Value(contactRouteMatcherContextKey{}).(chi.Router)
		return ok && contactRouteExists(router, path)
	}
	r.Body = http.MaxBytesReader(w, r.Body, 256<<10)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		contactAPIValidationError(w, r)
		return
	}
	var raw map[string]json.RawMessage
	decoder := json.NewDecoder(bytes.NewReader(body))
	if err := decoder.Decode(&raw); err != nil || raw == nil {
		contactAPIValidationError(w, r)
		return
	}
	if decoder.Decode(new(interface{})) != io.EOF {
		contactAPIValidationError(w, r)
		return
	}
	fields := make(map[string]string, len(raw))
	for key, value := range raw {
		var text string
		if err := json.Unmarshal(value, &text); err == nil {
			fields[key] = strings.TrimSpace(text)
			continue
		}
		var boolean bool
		if err := json.Unmarshal(value, &boolean); err == nil {
			if boolean {
				if key == "confirmed" {
					fields[key] = "true"
				} else {
					fields[key] = "on"
				}
			} else {
				fields[key] = ""
			}
			continue
		}
		var number json.Number
		if err := json.Unmarshal(value, &number); err == nil {
			fields[key] = number.String()
			continue
		}
		contactAPIValidationError(w, r)
		return
	}
	if _, exists := fields["website"]; exists {
		// The JSON API has no honeypot field; reject its presence as an unknown
		// request shape rather than allowing bots to probe it.
		contactAPIValidationError(w, r)
		return
	}
	securityID := strings.TrimSpace(r.URL.Query().Get("security_id"))
	if securityID != "" {
		if fields["type"] != "" && fields["type"] != "security" {
			contactAPIValidationError(w, r)
			return
		}
		fields["type"] = "security"
		fields["security_id"] = securityID
	}
	typeName := fields["type"]
	securityMode := typeName == "security"
	if securityMode {
		if securityID == "" {
			contactAPIValidationError(w, r)
			return
		}
		if _, valid, err := validSecurityID(r.Context(), securityID, time.Now().UTC()); err != nil || !valid {
			contactAPIValidationError(w, r)
			return
		}
		if err := validateSecurityReportFields(fields); err != nil {
			contactAPIValidationError(w, r)
			return
		}
	} else if err := validateOrdinaryContactFields(typeName, fields); err != nil {
		contactAPIValidationError(w, r)
		return
	}
	step := fields["step"]
	if step == "" && (fields["review_token"] != "" || fields["confirmed"] != "") {
		step = "confirm"
	}
	if step == "" || step == "submit" {
		if !validContactFormToken(r.Context(), fields["form_token"], settings) || !consumeContactToken("form", fields["form_token"], time.Now().UTC().Add(settings.MaxAge)) {
			recordContactStrike(r, settings)
			contactAPIError(w, r, http.StatusBadRequest, "VALIDATION_FAILED", "contact.errors.wait_and_submit", map[string]interface{}{"field": "form_token"})
			return
		}
		if !contactSecurityRateAllowed(r, fields["email"], false, settings) {
			contactRateLimitResponse(w, r)
			return
		}
		valid, captchaErr := VerifySimpleCaptcha(r.Context(), fields["captcha_id"], fields["captcha_answer"])
		if captchaErr != nil || !valid {
			recordContactStrike(r, settings)
			contactAPIError(w, r, http.StatusBadRequest, "VALIDATION_FAILED", "contact.errors.captcha_incorrect", map[string]interface{}{"field": "captcha"})
			return
		}
		if !securityMode && !ordinaryContactContentAllowed(typeName, fields, r, settings) {
			recordContactStrike(r, settings)
			contactAPIError(w, r, http.StatusBadRequest, "VALIDATION_FAILED", "contact.errors.message_rejected", nil)
			return
		}
		var idWindow int64
		if securityMode {
			var valid bool
			idWindow, valid, err = validSecurityID(r.Context(), fields["security_id"], time.Now().UTC())
			if err != nil || !valid {
				contactAPIValidationError(w, r)
				return
			}
		}
		token, err := issueContactReviewToken(r.Context(), fields, idWindow, false, settings)
		if err != nil {
			contactAPIError(w, r, http.StatusInternalServerError, "INTERNAL", "contact.errors.review_unavailable", nil)
			return
		}
		subject := fields["subject"]
		if securityMode {
			subject = fields["summary"]
		}
		review := map[string]string{"type": typeName, "email": fields["email"], "subject": subject}
		writeEnvelopeOK(w, http.StatusOK, map[string]interface{}{"review": review, "review_token": token, "confirm_after_seconds": int(settings.ReviewMin.Seconds())})
		return
	}
	var idWindow int64
	if securityMode {
		var valid bool
		idWindow, valid, err = validSecurityID(r.Context(), fields["security_id"], time.Now().UTC())
		if err != nil || !valid {
			contactAPIValidationError(w, r)
			return
		}
	}
	if step != "confirm" || fields["confirmed"] != "true" || !validContactFormToken(r.Context(), fields["form_token"], settings) || !consumeContactReviewToken(r.Context(), fields["review_token"], fields, idWindow, settings) {
		recordContactStrike(r, settings)
		contactAPIError(w, r, http.StatusBadRequest, "VALIDATION_FAILED", "contact.errors.confirm_required", map[string]interface{}{"field": "confirmed"})
		return
	}
	if !contactSecurityRateAllowed(r, fields["email"], true, settings) {
		contactRateLimitResponse(w, r)
		return
	}
	if securityMode {
		fields["researcher_gpg"], err = normalizeResearcherKey(r.Context(), fields["researcher_gpg"])
		if err != nil {
			contactAPIError(w, r, http.StatusBadRequest, "VALIDATION_FAILED", "contact.errors.invalid_researcher_key", map[string]interface{}{"field": "researcher_gpg"})
			return
		}
		trackingID, _, err := acceptSecurityReport(cfg, r.Context(), r, fields, idWindow)
		if err != nil {
			contactAPIError(w, r, http.StatusServiceUnavailable, "DELIVERY_UNAVAILABLE", "contact.errors.delivery_unavailable", nil)
			return
		}
		writeEnvelopeOK(w, http.StatusOK, map[string]interface{}{"tracking_id": trackingID})
		return
	}
	if contactDuplicate(fields, settings) {
		recordContactStrike(r, settings)
		writeEnvelopeOK(w, http.StatusOK, map[string]interface{}{"message": i18n.Translate(i18n.LangFromRequest(r), "contact.errors.report_received")})
		return
	}
	if err := sendOrdinaryContact(cfg, typeName, fields); err != nil {
		contactAPIError(w, r, http.StatusServiceUnavailable, "DELIVERY_UNAVAILABLE", "contact.errors.delivery_unavailable", nil)
		return
	}
	writeEnvelopeOK(w, http.StatusOK, map[string]interface{}{"message": i18n.Translate(i18n.LangFromRequest(r), "contact.errors.report_received")})
}

func contactAPIError(w http.ResponseWriter, r *http.Request, status int, code, key string, details map[string]interface{}) {
	writeEnvelopeError(w, status, code, i18n.Translate(i18n.LangFromRequest(r), key), details)
}

func contactAPIValidationError(w http.ResponseWriter, r *http.Request) {
	contactAPIError(w, r, http.StatusBadRequest, "VALIDATION_FAILED", "contact.errors.invalid_submission", nil)
}

func contactConfigEnabled(cfg *config.Config) bool {
	return contactPageEnabled(cfg)
}
