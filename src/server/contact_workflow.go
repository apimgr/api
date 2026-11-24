package server

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/apimgr/api/src/common/i18n"
	"github.com/apimgr/api/src/config"
	"github.com/apimgr/api/src/database"
	"github.com/apimgr/api/src/email"
	servicecrypto "github.com/apimgr/api/src/service/crypto"
	"github.com/go-chi/chi/v5"
)

const (
	contactFormMaxAge    = 12 * time.Hour
	contactFillMinimum   = 3 * time.Second
	contactReviewMinimum = 2 * time.Second
	contactReviewMaximum = 15 * time.Minute
)

type contactWorkflowField struct {
	Name     string
	Label    string
	Type     string
	Value    string
	Required bool
	Max      int
	Options  []string
}

type contactWorkflowPageData struct {
	PageData
	Type        string
	TitleKey    string
	Fields      []contactWorkflowField
	FormToken   string
	ReviewToken string
	SecurityID  string
	IsReview    bool
	CaptchaID   string
	CaptchaText string
	ErrorKey    string
	TrackingID  string
	EmailSent   bool
	Completed   bool
	AbuseEmail  string
	Honeypot    bool
}

type contactWorkflowReview struct {
	Issued int64             `json:"issued"`
	Nonce  string            `json:"nonce"`
	Fields map[string]string `json:"fields"`
	IDWin  int64             `json:"id_window"`
}

type contactWorkflowReplay struct {
	sync.Mutex
	forms      map[string]time.Time
	reviews    map[string]time.Time
	duplicates map[string]contactDuplicateEntry
}

type contactDuplicateEntry struct {
	expires  time.Time
	accessed time.Time
}

var contactWorkflowTokens = contactWorkflowReplay{forms: map[string]time.Time{}, reviews: map[string]time.Time{}, duplicates: map[string]contactDuplicateEntry{}}
var contactEmailPattern = regexp.MustCompile(`^[^\s@,;<>]+@[^\s@,;<>]+\.[^\s@,;<>]+$`)

type contactRouteMatcherContextKey struct{}

// contactWorkflowHandler serves the server-rendered report form and its
// submit/review/confirm flow. The route registration is owned by server.go.
func contactWorkflowHandler(cfg *config.Config, router chi.Router) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodPost {
			http.Error(w, i18n.Translate(i18n.LangFromRequest(r), "errors.method_not_allowed"), http.StatusMethodNotAllowed)
			return
		}
		if !contactPageEnabled(cfg) {
			http.NotFound(w, r)
			return
		}
		if r.Method == http.MethodGet {
			contactWorkflowGet(cfg, w, r)
			return
		}
		r = r.WithContext(context.WithValue(r.Context(), contactRouteMatcherContextKey{}, router))
		contactWorkflowPost(cfg, w, r)
	}
}

func contactWorkflowGet(cfg *config.Config, w http.ResponseWriter, r *http.Request) {
	securityID := strings.TrimSpace(r.URL.Query().Get("security_id"))
	window, valid, err := validSecurityID(r.Context(), securityID, time.Now().UTC())
	if err != nil {
		http.Error(w, i18n.Translate(i18n.LangFromRequest(r), "errors.server_error"), http.StatusInternalServerError)
		return
	}
	if !valid {
		if securityID != "" {
			logInvalidSecurityID(r, securityID)
		}
		securityID = ""
	}
	page := contactWorkflowPageData{PageData: newPageData(cfg, r, "contact"), SecurityID: securityID}
	page.Honeypot = contactAntispamConfig(cfg).Honeypot
	page.NoIndex = true
	page.AbuseEmail = strings.TrimSpace(cfg.Server.Contact.Abuse.Email)
	if page.AbuseEmail == "" {
		page.AbuseEmail = strings.TrimSpace(cfg.Server.Contact.General.Email)
	}
	page.PageTitle = i18n.Translate(page.Lang, "contact.title")
	page.PageDescription = i18n.Translate(page.Lang, "contact.purpose")
	if securityID != "" {
		page.Type = "security"
		page.TitleKey = "contact.security_title"
		page.Fields = securityReportFields(nil)
		page.FormToken, err = issueContactFormToken(r.Context())
		if err == nil {
			var captcha CaptchaDescriptor
			captcha, err = IssueSimpleCaptcha(r.Context(), page.Lang)
			if err == nil {
				page.CaptchaID, page.CaptchaText = captcha.CaptchaID, captcha.Question
			}
		}
		if err != nil {
			http.Error(w, i18n.Translate(i18n.LangFromRequest(r), "errors.server_error"), http.StatusInternalServerError)
			return
		}
	} else {
		typeName := r.URL.Query().Get("type")
		if typeName == "abuse" || typeName == "dmca" || typeName == "privacy" {
			page.Type = typeName
			page.TitleKey = "contact.type." + typeName
			page.Fields = ordinaryContactFields(typeName, nil)
			page.FormToken, err = issueContactFormToken(r.Context())
			if err == nil {
				var captcha CaptchaDescriptor
				captcha, err = IssueSimpleCaptcha(r.Context(), page.Lang)
				if err == nil {
					page.CaptchaID, page.CaptchaText = captcha.CaptchaID, captcha.Question
				}
			}
			if err != nil {
				http.Error(w, i18n.Translate(i18n.LangFromRequest(r), "errors.server_error"), http.StatusInternalServerError)
				return
			}
		}
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Robots-Tag", "noindex, nofollow")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	contactWorkflowRender(w, page)
	_ = window
}

func contactWorkflowPost(cfg *config.Config, w http.ResponseWriter, r *http.Request) {
	var err error
	settings := contactAntispamConfig(cfg)
	settings.RouteExists = func(path string) bool {
		router, ok := r.Context().Value(contactRouteMatcherContextKey{}).(chi.Router)
		return ok && contactRouteExists(router, path)
	}
	r.Body = http.MaxBytesReader(w, r.Body, 256<<10)
	if err := r.ParseForm(); err != nil {
		writeEnvelopeError(w, http.StatusBadRequest, "VALIDATION_FAILED", i18n.Translate(i18n.LangFromRequest(r), "contact.errors.invalid_submission"), nil)
		return
	}
	fields := make(map[string]string, len(r.PostForm))
	for key, values := range r.PostForm {
		if len(values) != 1 || len(key) > 64 || len(values[0]) > 40000 {
			contactWorkflowError(w, r, "contact.error.invalid")
			return
		}
		fields[key] = strings.TrimSpace(values[0])
	}
	typeName := fields["type"]
	if typeName != "security" && typeName != "abuse" && typeName != "dmca" && typeName != "privacy" {
		contactWorkflowError(w, r, "contact.error.type")
		return
	}
	securityID := fields["security_id"]
	var idWindow int64
	if typeName == "security" {
		var valid bool
		idWindow, valid, err = validSecurityID(r.Context(), securityID, time.Now().UTC())
		if err != nil || !valid {
			logInvalidSecurityID(r, securityID)
			contactWorkflowError(w, r, "contact.error.security_id")
			return
		}
	} else if securityID != "" {
		contactWorkflowError(w, r, "contact.error.type")
		return
	}
	if !contactOriginValid(r) {
		recordContactStrike(r, settings)
		contactWorkflowError(w, r, "contact.error.origin")
		return
	}
	if !validContactFormToken(r.Context(), fields["form_token"], settings) {
		recordContactStrike(r, settings)
		contactWorkflowError(w, r, "contact.error.form_token")
		return
	}
	fields["name"] = strings.TrimSpace(fields["name"])
	fields["email"] = strings.TrimSpace(fields["email"])
	var validationErr error
	if typeName == "security" {
		validationErr = validateSecurityReportFields(fields)
	} else {
		validationErr = validateOrdinaryContactFields(typeName, fields)
	}
	if validationErr != nil {
		contactWorkflowError(w, r, "contact.error.fields")
		return
	}
	step := fields["step"]
	if step == "" {
		step = "submit"
	}
	if step == "edit" {
		if !consumeContactReviewToken(r.Context(), fields["review_token"], fields, idWindow, settings) {
			recordContactStrike(r, settings)
			contactWorkflowGetWithValues(cfg, w, r, fields, "contact.error.review")
			return
		}
		contactWorkflowGetWithValues(cfg, w, r, fields, "contact.error.review")
		return
	}
	if step == "submit" {
		if !consumeContactToken("form", fields["form_token"], time.Now().UTC().Add(settings.MaxAge)) {
			recordContactStrike(r, settings)
			contactWorkflowGetWithValues(cfg, w, r, fields, "contact.error.form_token")
			return
		}
		ok, captchaErr := VerifySimpleCaptcha(r.Context(), fields["captcha_id"], fields["captcha_answer"])
		if captchaErr != nil || !ok {
			recordContactStrike(r, settings)
			contactWorkflowGetWithValues(cfg, w, r, fields, "contact.error.captcha")
			return
		}
		if !contactSecurityRateAllowed(r, fields["email"], false, settings) {
			contactRateLimitResponse(w, r)
			return
		}
		if typeName == "security" {
			fields["researcher_gpg"], err = normalizeResearcherKey(r.Context(), fields["researcher_gpg"])
			if err != nil {
				contactWorkflowGetWithValues(cfg, w, r, fields, "contact.error.fields")
				return
			}
		}
		if fields["website"] != "" && settings.Honeypot {
			recordContactStrike(r, settings)
		}
		token, tokenErr := issueContactReviewToken(r.Context(), fields, idWindow, fields["website"] != "" && settings.Honeypot, settings)
		if tokenErr != nil {
			http.Error(w, i18n.Translate(i18n.LangFromRequest(r), "errors.server_error"), http.StatusInternalServerError)
			return
		}
		fieldList, titleKey := contactFieldsForType(typeName, fields)
		page := contactWorkflowPageData{PageData: newPageData(cfg, r, "contact"), Type: typeName, TitleKey: titleKey, Fields: fieldList, FormToken: fields["form_token"], ReviewToken: token, SecurityID: securityID, IsReview: true}
		page.NoIndex = true
		contactWorkflowRender(w, page)
		return
	}
	if step != "confirm" || fields["confirmed"] != "on" {
		contactWorkflowGetWithValues(cfg, w, r, fields, "contact.error.review")
		return
	}
	if !consumeContactReviewToken(r.Context(), fields["review_token"], fields, idWindow, settings) {
		recordContactStrike(r, settings)
		contactWorkflowGetWithValues(cfg, w, r, fields, "contact.error.review")
		return
	}
	if contactReviewWasDropped(r.Context(), fields["review_token"]) {
		page := contactWorkflowPageData{PageData: newPageData(cfg, r, "contact"), Type: "security", TitleKey: "contact.success", Completed: true}
		page.NoIndex = true
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Robots-Tag", "noindex, nofollow")
		contactWorkflowRender(w, page)
		return
	}
	if typeName != "security" {
		if !ordinaryContactContentAllowed(typeName, fields, r, settings) {
			recordContactStrike(r, settings)
			contactWorkflowGetWithValues(cfg, w, r, fields, "contact.error.content")
			return
		}
	}
	if !contactSecurityRateAllowed(r, fields["email"], true, settings) {
		contactRateLimitResponse(w, r)
		return
	}
	if typeName != "security" {
		if contactDuplicate(fields, settings) {
			page := contactWorkflowPageData{PageData: newPageData(cfg, r, "contact"), Type: typeName, TitleKey: "contact.success", Completed: true}
			page.NoIndex = true
			w.Header().Set("Cache-Control", "no-store")
			w.Header().Set("X-Robots-Tag", "noindex, nofollow")
			contactWorkflowRender(w, page)
			return
		}
		if err := sendOrdinaryContact(cfg, typeName, fields); err != nil {
			contactWorkflowGetWithValues(cfg, w, r, fields, "contact.error.submit")
			return
		}
		page := contactWorkflowPageData{PageData: newPageData(cfg, r, "contact"), Type: typeName, TitleKey: "contact.success", Completed: true}
		page.NoIndex = true
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Robots-Tag", "noindex, nofollow")
		contactWorkflowRender(w, page)
		return
	}
	trackingID, acknowledged, err := acceptSecurityReport(cfg, r.Context(), r, fields, idWindow)
	if err != nil {
		contactWorkflowGetWithValues(cfg, w, r, fields, "contact.error.submit")
		return
	}
	page := contactWorkflowPageData{PageData: newPageData(cfg, r, "contact"), Type: "security", TitleKey: "contact.success", TrackingID: trackingID, EmailSent: acknowledged}
	page.NoIndex = true
	page.PageTitle = i18n.Translate(page.Lang, "contact.title")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Robots-Tag", "noindex, nofollow")
	contactWorkflowRender(w, page)
}

func securityReportFields(values map[string]string) []contactWorkflowField {
	get := func(name string) string { return values[name] }
	return []contactWorkflowField{
		{Name: "name", Label: "contact.name", Type: "text", Value: get("name"), Required: true, Max: 100},
		{Name: "email", Label: "contact.email", Type: "email", Value: get("email"), Required: true, Max: 254},
		{Name: "researcher_gpg", Label: "contact.researcher_gpg", Type: "textarea", Value: get("researcher_gpg"), Max: 16384},
		{Name: "component", Label: "contact.component", Type: "text", Value: get("component"), Required: true, Max: 255},
		{Name: "endpoint", Label: "contact.endpoint", Type: "text", Value: get("endpoint"), Max: 2048},
		{Name: "severity", Label: "contact.severity", Type: "select", Value: get("severity"), Required: true, Options: []string{"critical", "high", "medium", "low", "informational"}},
		{Name: "summary", Label: "contact.summary", Type: "text", Value: get("summary"), Required: true, Max: 200},
		{Name: "steps", Label: "contact.steps", Type: "textarea", Value: get("steps"), Required: true, Max: 10000},
		{Name: "impact", Label: "contact.impact", Type: "textarea", Value: get("impact"), Required: true, Max: 5000},
		{Name: "suggested_fix", Label: "contact.suggested_fix", Type: "textarea", Value: get("suggested_fix"), Max: 5000},
		{Name: "cve_requested", Label: "contact.cve_requested", Type: "checkbox", Value: get("cve_requested")},
		{Name: "timeline_days", Label: "contact.timeline_days", Type: "number", Value: get("timeline_days"), Max: 3},
		{Name: "credit_preference", Label: "contact.credit_preference", Type: "select", Value: get("credit_preference"), Required: true, Options: []string{"name", "handle", "no", "anonymous"}},
		{Name: "credit_handle", Label: "contact.credit_handle", Type: "text", Value: get("credit_handle"), Max: 100},
		{Name: "disclosure_agreement", Label: "contact.disclosure_agreement", Type: "checkbox", Value: get("disclosure_agreement"), Required: true},
	}
}

func validateSecurityReportFields(fields map[string]string) error {
	limits := map[string]int{"name": 100, "email": 254, "researcher_gpg": 16384, "component": 255, "endpoint": 2048, "summary": 200, "steps": 10000, "impact": 5000, "suggested_fix": 5000, "credit_handle": 100, "timeline_days": 3}
	allowed := map[string]bool{"type": true, "security_id": true, "name": true, "email": true, "researcher_gpg": true, "component": true, "endpoint": true, "severity": true, "summary": true, "steps": true, "impact": true, "suggested_fix": true, "cve_requested": true, "timeline_days": true, "credit_preference": true, "credit_handle": true, "disclosure_agreement": true, "captcha_id": true, "captcha_answer": true, "website": true, "form_token": true, "step": true, "review_token": true, "confirmed": true}
	for key, value := range fields {
		if !allowed[key] || !utf8Valid(value) || strings.ContainsAny(value, "\x00\u202a\u202b\u202d\u202e\u2066\u2067\u2068\u2069\u200b\u200c\u200d") {
			return fmt.Errorf("invalid field")
		}
		if limit, ok := limits[key]; ok && len([]rune(value)) > limit {
			return fmt.Errorf("field is too long")
		}
	}
	for _, name := range []string{"name", "email", "component", "severity", "summary", "steps", "impact", "credit_preference"} {
		if fields[name] == "" {
			return fmt.Errorf("required field is missing")
		}
	}
	if !contactEmailPattern.MatchString(fields["email"]) || strings.ContainsAny(fields["name"]+fields["email"]+fields["summary"], "\r\n") {
		return fmt.Errorf("invalid contact details")
	}
	switch fields["severity"] {
	case "critical", "high", "medium", "low", "informational":
	default:
		return fmt.Errorf("invalid severity")
	}
	switch fields["credit_preference"] {
	case "name", "handle", "no", "anonymous":
	default:
		return fmt.Errorf("invalid credit preference")
	}
	if fields["credit_preference"] == "handle" && fields["credit_handle"] == "" {
		return fmt.Errorf("credit handle is required")
	}
	if fields["disclosure_agreement"] != "on" {
		return fmt.Errorf("coordinated disclosure agreement is required")
	}
	if fields["cve_requested"] != "" && fields["cve_requested"] != "on" {
		return fmt.Errorf("invalid checkbox")
	}
	if fields["timeline_days"] != "" {
		for _, ch := range fields["timeline_days"] {
			if ch < '0' || ch > '9' {
				return fmt.Errorf("invalid timeline")
			}
		}
	}
	if key := strings.TrimSpace(fields["researcher_gpg"]); key != "" {
		if strings.HasPrefix(key, "https://") {
			parsed, err := url.Parse(key)
			if err != nil || parsed.Scheme != "https" || parsed.Hostname() == "" || parsed.User != nil || len(key) > 2048 {
				return fmt.Errorf("invalid researcher public key URL")
			}
		} else if !strings.Contains(key, "-----BEGIN PGP PUBLIC KEY BLOCK-----") {
			return fmt.Errorf("invalid researcher public key")
		}
	}
	return nil
}

func normalizeResearcherKey(ctx context.Context, value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", nil
	}
	if !strings.HasPrefix(value, "https://") {
		if _, err := servicecrypto.PGPEncrypt("public key validation", value); err != nil {
			return "", err
		}
		return value, nil
	}
	if len(value) > 2048 {
		return "", fmt.Errorf("researcher key URL is too long")
	}
	u, err := url.Parse(value)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil {
		return "", fmt.Errorf("invalid researcher key URL")
	}
	addresses, err := net.LookupIP(u.Hostname())
	if err != nil || len(addresses) == 0 {
		return "", fmt.Errorf("researcher key host is unavailable")
	}
	for _, address := range addresses {
		if !address.IsGlobalUnicast() || address.IsPrivate() || address.IsLoopback() || address.IsLinkLocalUnicast() || address.IsMulticast() {
			return "", fmt.Errorf("researcher key host is not public")
		}
	}
	port := u.Port()
	if port == "" {
		port = "443"
	}
	transport := &http.Transport{DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
		host, _, splitErr := net.SplitHostPort(address)
		if splitErr != nil || !strings.EqualFold(host, u.Hostname()) {
			return nil, fmt.Errorf("unexpected researcher key host")
		}
		var lastErr error
		for _, ip := range addresses {
			conn, dialErr := (&net.Dialer{Timeout: 5 * time.Second}).DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
			if dialErr == nil {
				return conn, nil
			}
			lastErr = dialErr
		}
		return nil, lastErr
	}}
	client := &http.Client{Transport: transport, Timeout: 8 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		transport.CloseIdleConnections()
		return "", err
	}
	response, err := client.Do(request)
	if err != nil {
		transport.CloseIdleConnections()
		return "", err
	}
	defer func() { _ = response.Body.Close() }()
	defer transport.CloseIdleConnections()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return "", fmt.Errorf("researcher key could not be retrieved")
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, 16385))
	if err != nil || len(data) > 16384 {
		return "", fmt.Errorf("researcher key is too large")
	}
	key := strings.TrimSpace(string(data))
	if _, err := servicecrypto.PGPEncrypt("public key validation", key); err != nil {
		return "", fmt.Errorf("researcher key is invalid")
	}
	return key, nil
}

func issueContactFormToken(ctx context.Context) (string, error) {
	nonce := randomContactNonce()
	if nonce == "" {
		return "", fmt.Errorf("generate contact form nonce")
	}
	return signContactToken(ctx, "contact-form", fmt.Sprintf("%d.%s", time.Now().UTC().Unix(), nonce))
}

func validContactFormToken(ctx context.Context, token string, configured ...contactAntispamSettings) bool {
	settings := contactAntispamSettings{MinFill: contactFillMinimum, MaxAge: contactFormMaxAge}
	if len(configured) > 0 {
		settings = configured[0]
	}
	issued, nonce, ok := verifyContactToken(ctx, "contact-form", token)
	if !ok {
		return false
	}
	age := time.Since(time.Unix(issued, 0))
	return age >= settings.MinFill && age <= settings.MaxAge && nonce != ""
}

func issueContactReviewToken(ctx context.Context, fields map[string]string, window int64, dropped bool, configured ...contactAntispamSettings) (string, error) {
	issued := time.Now().UTC().Unix()
	nonce := randomContactNonce()
	if nonce == "" {
		return "", fmt.Errorf("generate contact review nonce")
	}
	payload := contactWorkflowReview{Issued: issued, Nonce: nonce, Fields: contactReviewFields(fields), IDWin: window}
	if dropped {
		payload.Fields["_drop"] = "1"
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	return signContactToken(ctx, "contact-review", fmt.Sprintf("%d.%s", issued, base64.RawURLEncoding.EncodeToString(body)))
}

func consumeContactReviewToken(ctx context.Context, token string, fields map[string]string, window int64, configured ...contactAntispamSettings) bool {
	settings := contactAntispamSettings{ReviewMin: contactReviewMinimum, ReviewMax: contactReviewMaximum}
	if len(configured) > 0 {
		settings = configured[0]
	}
	payload, ok := decodeContactReviewToken(ctx, token)
	if !ok || payload.IDWin != window || time.Since(time.Unix(payload.Issued, 0)) < settings.ReviewMin || time.Since(time.Unix(payload.Issued, 0)) > settings.ReviewMax {
		return false
	}
	if payload.Fields["_drop"] == "1" {
		return consumeContactToken("review", payload.Nonce, time.Now().UTC().Add(settings.ReviewMax))
	}
	if !hmac.Equal([]byte(contactFieldsDigest(payload.Fields)), []byte(contactFieldsDigest(contactReviewFields(fields)))) {
		return false
	}
	return consumeContactToken("review", payload.Nonce, time.Now().UTC().Add(settings.ReviewMax))
}

func contactReviewWasDropped(ctx context.Context, token string) bool {
	payload, ok := decodeContactReviewToken(ctx, token)
	return ok && payload.Fields["_drop"] == "1"
}

func decodeContactReviewToken(ctx context.Context, token string) (contactWorkflowReview, bool) {
	_, payload, ok := verifyContactToken(ctx, "contact-review", token)
	if !ok {
		return contactWorkflowReview{}, false
	}
	body, err := base64.RawURLEncoding.DecodeString(payload)
	if err != nil {
		return contactWorkflowReview{}, false
	}
	var review contactWorkflowReview
	if json.Unmarshal(body, &review) != nil {
		return contactWorkflowReview{}, false
	}
	return review, true
}

func contactReviewFields(fields map[string]string) map[string]string {
	copy := make(map[string]string, len(fields))
	for key, value := range fields {
		switch key {
		case "form_token", "captcha_id", "captcha_answer", "step", "review_token", "confirmed", "website":
			continue
		}
		copy[key] = strings.TrimSpace(value)
	}
	return copy
}

func contactFieldsDigest(fields map[string]string) string {
	body, _ := json.Marshal(fields)
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}

func signContactToken(ctx context.Context, purpose, payload string) (string, error) {
	secret, err := database.LoadSecret(ctx, database.SecretInstallationSecret)
	if err != nil {
		return "", err
	}
	mac := hmac.New(sha256.New, secret.Value)
	_, _ = mac.Write([]byte(purpose + "|" + payload))
	return payload + "." + hex.EncodeToString(mac.Sum(nil)), nil
}

func verifyContactToken(ctx context.Context, purpose, token string) (int64, string, bool) {
	index := strings.LastIndexByte(token, '.')
	if index <= 0 {
		return 0, "", false
	}
	payload, signature := token[:index], token[index+1:]
	provided, err := hex.DecodeString(signature)
	if err != nil || len(provided) != sha256.Size {
		return 0, "", false
	}
	secret, err := database.LoadSecret(ctx, database.SecretInstallationSecret)
	if err != nil {
		return 0, "", false
	}
	mac := hmac.New(sha256.New, secret.Value)
	_, _ = mac.Write([]byte(purpose + "|" + payload))
	if !hmac.Equal(provided, mac.Sum(nil)) {
		return 0, "", false
	}
	first, rest, has := strings.Cut(payload, ".")
	if !has {
		return 0, "", false
	}
	issued, err := time.ParseDuration(first + "s")
	if err != nil {
		return 0, "", false
	}
	return int64(issued.Seconds()), rest, true
}

func consumeContactToken(kind, token string, expiry time.Time) bool {
	contactWorkflowTokens.Lock()
	defer contactWorkflowTokens.Unlock()
	store := contactWorkflowTokens.forms
	if kind == "review" {
		store = contactWorkflowTokens.reviews
	}
	now := time.Now().UTC()
	for key, expires := range store {
		if !now.Before(expires) {
			delete(store, key)
		}
	}
	if _, exists := store[token]; exists {
		return false
	}
	if len(store) >= 50000 {
		for key := range store {
			delete(store, key)
			break
		}
	}
	store[token] = expiry
	return true
}

func randomContactNonce() string {
	data := make([]byte, 16)
	if _, err := rand.Read(data); err != nil {
		return ""
	}
	return hex.EncodeToString(data)
}

var contactRateState = struct {
	sync.Mutex
	requests map[string][]time.Time
	strikes  map[string][]time.Time
	bans     map[string]time.Time
}{requests: map[string][]time.Time{}, strikes: map[string][]time.Time{}, bans: map[string]time.Time{}}

func contactSecurityRateAllowed(r *http.Request, sender string, count bool, configured ...contactAntispamSettings) bool {
	settings := contactAntispamSettings{PerIP: 5, PerEmail: 3, Global: 60, Window: time.Hour}
	if len(configured) > 0 {
		settings = configured[0]
	}
	if settings.Window <= 0 {
		return true
	}
	now := time.Now().UTC()
	keys := []string{"global"}
	host := r.Host
	if parsedHost, _, err := net.SplitHostPort(host); err == nil {
		host = parsedHost
	}
	onion := strings.HasSuffix(strings.ToLower(host), ".onion") || strings.HasSuffix(strings.ToLower(host), ".i2p")
	ip := getClientIP(r)
	if !onion {
		keys = append(keys, "ip:"+ip)
	}
	keys = append(keys, "email:"+strings.ToLower(sender))
	limits := map[string]int{"global": settings.Global, "ip:" + ip: settings.PerIP, "email:" + strings.ToLower(sender): settings.PerEmail}
	contactRateState.Lock()
	defer contactRateState.Unlock()
	if !onion {
		if contactRateState.bans[ip].After(now) {
			return false
		}
		delete(contactRateState.bans, ip)
	}
	for key, stamps := range contactRateState.requests {
		kept := stamps[:0]
		for _, stamp := range stamps {
			if now.Sub(stamp) < settings.Window {
				kept = append(kept, stamp)
			}
		}
		if len(kept) == 0 {
			delete(contactRateState.requests, key)
		} else {
			contactRateState.requests[key] = kept
		}
	}
	for _, key := range keys {
		if limits[key] > 0 && len(contactRateState.requests[key]) >= limits[key] {
			return false
		}
	}
	if count {
		for _, key := range keys {
			contactRateState.requests[key] = append(contactRateState.requests[key], now)
		}
	}
	return true
}

func recordContactStrike(r *http.Request, settings contactAntispamSettings) {
	if settings.Strikes <= 0 || settings.Window <= 0 {
		return
	}
	host := r.Host
	if parsedHost, _, err := net.SplitHostPort(host); err == nil {
		host = parsedHost
	}
	if strings.HasSuffix(strings.ToLower(host), ".onion") || strings.HasSuffix(strings.ToLower(host), ".i2p") {
		return
	}
	ip := getClientIP(r)
	now := time.Now().UTC()
	contactRateState.Lock()
	defer contactRateState.Unlock()
	kept := contactRateState.strikes[ip][:0]
	for _, stamp := range contactRateState.strikes[ip] {
		if now.Sub(stamp) < settings.Window {
			kept = append(kept, stamp)
		}
	}
	contactRateState.strikes[ip] = append(kept, now)
	if len(contactRateState.strikes[ip]) >= settings.Strikes {
		contactRateState.bans[ip] = now.Add(settings.BanDuration)
		delete(contactRateState.strikes, ip)
	}
}

func contactRateLimitResponse(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Retry-After", "3600")
	writeEnvelopeError(w, http.StatusTooManyRequests, "RATE_LIMITED", i18n.Translate(i18n.LangFromRequest(r), "contact.errors.try_again_later"), nil)
}

func contactOriginValid(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		origin = r.Header.Get("Referer")
	}
	if origin == "" && r.Header.Get("Sec-Fetch-Site") == "" {
		return false
	}
	if origin != "" {
		u, err := url.Parse(origin)
		if err != nil || !strings.EqualFold(u.Host, r.Host) || !strings.EqualFold(u.Scheme, requestScheme(r)) {
			return false
		}
	}
	fetch := r.Header.Get("Sec-Fetch-Site")
	return fetch == "" || fetch == "same-origin"
}

func requestScheme(r *http.Request) string {
	if r.TLS != nil {
		return "https"
	}
	if strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") {
		return "https"
	}
	return "http"
}

func acceptSecurityReport(cfg *config.Config, ctx context.Context, r *http.Request, fields map[string]string, idWindow int64) (string, bool, error) {
	trackingBytes := make([]byte, 8)
	if _, err := rand.Read(trackingBytes); err != nil {
		return "", false, err
	}
	trackingID := "sec_" + hex.EncodeToString(trackingBytes)
	statusTokenBytes := make([]byte, 32)
	if _, err := rand.Read(statusTokenBytes); err != nil {
		return "", false, err
	}
	statusToken := hex.EncodeToString(statusTokenBytes)
	key, err := base64.StdEncoding.DecodeString(cfg.Server.Security.EncryptionKey)
	if err != nil || len(key) != 32 {
		return "", false, fmt.Errorf("security encryption key is unavailable")
	}
	report := map[string]string{"researcher_gpg": fields["researcher_gpg"], "component": fields["component"], "endpoint": fields["endpoint"], "severity": fields["severity"], "summary": fields["summary"], "steps": fields["steps"], "impact": fields["impact"], "suggested_fix": fields["suggested_fix"], "cve_requested": fields["cve_requested"], "timeline_days": fields["timeline_days"], "app_version": Version, "commit_hash": CommitID, "timestamp": time.Now().UTC().Format(time.RFC3339), "request_user_agent": r.UserAgent()}
	body, err := json.Marshal(report)
	if err != nil {
		return "", false, err
	}
	bodyAlgorithm := ""
	var bodyEnc []byte
	requestLanguage := i18n.LangFromEmailRequest(r)
	contactJSON, err := json.Marshal(map[string]string{"email": fields["email"], "researcher_gpg": fields["researcher_gpg"], "language": requestLanguage})
	if err != nil {
		return "", false, err
	}
	contactEnc, err := encryptContactData(key, contactJSON)
	if err != nil {
		return "", false, err
	}
	creditPref, creditName := fields["credit_preference"], ""
	switch creditPref {
	case "name":
		creditPref, creditName = "name", fields["name"]
	case "handle":
		creditPref, creditName = "handle", fields["credit_handle"]
	case "anonymous":
		creditPref = "anon"
	case "no":
		creditPref = "no"
	}
	tokenHash := sha256.Sum256([]byte(statusToken))
	input := database.SecurityReportInput{TrackingID: trackingID, IDWindow: idWindow, Severity: fields["severity"], Contact: contactEnc, CreditPref: creditPref, CreditName: creditName, CVE: fields["cve_requested"] == "on", TokenHash: hex.EncodeToString(tokenHash[:])}
	if err := database.StoreSecurityReportPrepared(ctx, input, func() ([]byte, string, error) {
		if publicKey, _, keyErr := readSecurityPublicKey(); keyErr == nil {
			encrypted, encryptErr := servicecrypto.PGPEncrypt(string(body), string(publicKey))
			if encryptErr != nil {
				return nil, "", fmt.Errorf("encrypt security report with project key: %w", encryptErr)
			}
			bodyEnc = []byte(encrypted)
			bodyAlgorithm = "pgp"
			return bodyEnc, bodyAlgorithm, nil
		}
		bodyEnc, err = encryptContactData(key, body)
		if err != nil {
			return nil, "", err
		}
		bodyAlgorithm = "aes-256-gcm"
		return bodyEnc, bodyAlgorithm, nil
	}); err != nil {
		return "", false, err
	}
	mailClient := email.Get()
	securityRecipient := strings.TrimSpace(cfg.Server.Contact.ResolveSecurity().Email)
	notified := false
	if securityRecipient != "" && mailClient != nil && mailClient.Config().Enabled {
		maintainerMessage := email.Message{To: []string{securityRecipient}, Subject: "[" + cfg.Server.Branding.Title + " security report] " + fields["severity"], OmitReplyTo: true}
		if bodyAlgorithm == "pgp" {
			maintainerMessage.Body = string(bodyEnc)
		} else {
			maintainerMessage.Body = "The project PGP key is not configured. The encrypted report is attached using AES-256-GCM and can be decrypted with the configured server encryption key."
			maintainerMessage.Attachments = []email.Attachment{{Filename: trackingID + ".enc", ContentType: "application/octet-stream", Data: bodyEnc}}
		}
		notified = mailClient.Send(maintainerMessage) == nil
	}
	statusURL := BuildURL(r, "/server/security/report/"+trackingID+"?token="+url.QueryEscape(statusToken))
	ackBody := i18n.TranslateFormat(requestLanguage, "contact.tracking_message", map[string]string{"tracking_id": trackingID}) + "\n" + i18n.Translate(requestLanguage, "security.status_title") + ": " + statusURL
	if publicKey := strings.TrimSpace(fields["researcher_gpg"]); publicKey != "" && !strings.HasPrefix(publicKey, "https://") {
		if encrypted, encryptErr := servicecrypto.PGPEncrypt(ackBody, publicKey); encryptErr == nil {
			ackBody = encrypted
		}
	}
	acknowledged := false
	if mailClient != nil && mailClient.Config().Enabled {
		acknowledged = mailClient.Send(email.Message{To: []string{fields["email"]}, Subject: i18n.Translate(requestLanguage, "contact.received") + ": " + trackingID, Body: ackBody, OmitReplyTo: true}) == nil
	}
	if logger := GetLogger(); logger != nil {
		logger.LogSecurity("security.report_received", getClientIP(r), map[string]interface{}{"tracking_id": trackingID, "notified": notified, "severity": fields["severity"], "component": sanitizeSecurityComponent(fields["component"])})
	}
	return trackingID, acknowledged, nil
}

func sanitizeSecurityComponent(value string) string {
	value = strings.TrimSpace(strings.ToValidUTF8(value, ""))
	value = strings.Map(func(r rune) rune {
		if r < 32 || r == 127 || r == '\r' || r == '\n' {
			return -1
		}
		return r
	}, value)
	if len([]rune(value)) > 255 {
		value = string([]rune(value)[:255])
	}
	return value
}

func encryptContactData(key, plaintext []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return gcm.Seal(nonce, nonce, plaintext, nil), nil
}

func contactWorkflowGetWithValues(cfg *config.Config, w http.ResponseWriter, r *http.Request, values map[string]string, errorKey string) {
	typeName := values["type"]
	fieldList, titleKey := contactFieldsForType(typeName, values)
	page := contactWorkflowPageData{PageData: newPageData(cfg, r, "contact"), Type: typeName, TitleKey: titleKey, Fields: fieldList, SecurityID: values["security_id"], ErrorKey: errorKey}
	page.Honeypot = contactAntispamConfig(cfg).Honeypot
	page.NoIndex = true
	var err error
	page.FormToken, err = issueContactFormToken(r.Context())
	if err != nil {
		http.Error(w, i18n.Translate(page.Lang, "errors.server_error"), http.StatusInternalServerError)
		return
	}
	captcha, err := IssueSimpleCaptcha(r.Context(), page.Lang)
	if err != nil {
		http.Error(w, i18n.Translate(page.Lang, "errors.server_error"), http.StatusInternalServerError)
		return
	}
	page.CaptchaID, page.CaptchaText = captcha.CaptchaID, captcha.Question
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Robots-Tag", "noindex, nofollow")
	contactWorkflowRender(w, page)
}

var contactWorkflowTemplates struct {
	sync.Once
	tmpl *template.Template
	err  error
}

func contactWorkflowTemplate() (*template.Template, error) {
	contactWorkflowTemplates.Do(func() {
		contactWorkflowTemplates.tmpl, contactWorkflowTemplates.err = template.New("contact_workflow").Funcs(templateFuncs()).ParseFS(templatesFS, "template/layout/public.tmpl", "template/partial/*.tmpl", "template/partial/public/*.tmpl", "template/page/contact_workflow.tmpl")
	})
	return contactWorkflowTemplates.tmpl, contactWorkflowTemplates.err
}

func contactWorkflowRender(w http.ResponseWriter, data contactWorkflowPageData) {
	if w.Header().Get("Content-Type") == "" {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
	}
	tmpl, err := contactWorkflowTemplate()
	if err != nil {
		http.Error(w, i18n.Translate(data.Lang, "errors.server_error"), http.StatusInternalServerError)
		return
	}
	if err := tmpl.ExecuteTemplate(w, "public.tmpl", data); err != nil {
		http.Error(w, i18n.Translate(data.Lang, "errors.server_error"), http.StatusInternalServerError)
	}
}

func contactWorkflowError(w http.ResponseWriter, r *http.Request, key string) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Robots-Tag", "noindex, nofollow")
	writeEnvelopeError(w, http.StatusBadRequest, "VALIDATION_FAILED", i18n.Translate(i18n.LangFromRequest(r), key), nil)
}

func logInvalidSecurityID(r *http.Request, supplied string) {
	sum := sha256.Sum256([]byte(supplied))
	if logger := GetLogger(); logger != nil {
		logger.LogSecurity("security.security_id_invalid", getClientIP(r), map[string]interface{}{"user_agent": r.UserAgent(), "security_id_hash": hex.EncodeToString(sum[:])[:8]})
	}
}

func utf8Valid(value string) bool { return strings.ToValidUTF8(value, "") == value }

func ordinaryContactFields(typeName string, values map[string]string) []contactWorkflowField {
	get := func(name string) string {
		if values == nil {
			return ""
		}
		return values[name]
	}
	fields := []contactWorkflowField{
		{Name: "name", Label: "contact.name", Type: "text", Value: get("name"), Required: true, Max: 100},
		{Name: "email", Label: "contact.email", Type: "email", Value: get("email"), Required: true, Max: 254},
	}
	switch typeName {
	case "abuse":
		fields = append(fields,
			contactWorkflowField{Name: "reported_url", Label: "contact.reported_url", Type: "url", Value: get("reported_url"), Required: true, Max: 2048},
			contactWorkflowField{Name: "reason", Label: "contact.reason", Type: "select", Value: get("reason"), Required: true, Options: []string{"illegal_content", "terms_violation", "harassment", "malware_phishing", "other"}},
		)
	case "dmca":
		fields = append(fields,
			contactWorkflowField{Name: "reported_url", Label: "contact.reported_url", Type: "url", Value: get("reported_url"), Required: true, Max: 2048},
			contactWorkflowField{Name: "copyrighted_work", Label: "contact.copyrighted_work", Type: "textarea", Value: get("copyrighted_work"), Required: true, Max: 2000},
			contactWorkflowField{Name: "good_faith", Label: "contact.good_faith", Type: "checkbox", Value: get("good_faith"), Required: true},
			contactWorkflowField{Name: "accuracy", Label: "contact.accuracy", Type: "checkbox", Value: get("accuracy"), Required: true},
			contactWorkflowField{Name: "signature", Label: "contact.signature", Type: "text", Value: get("signature"), Required: true, Max: 100},
		)
	case "privacy":
		fields = append(fields, contactWorkflowField{Name: "request_kind", Label: "contact.request_kind", Type: "select", Value: get("request_kind"), Required: true, Options: []string{"access", "export", "deletion", "correction", "opt_out", "other"}})
	}
	fields = append(fields,
		contactWorkflowField{Name: "subject", Label: "contact.subject", Type: "text", Value: get("subject"), Required: true, Max: 200},
		contactWorkflowField{Name: "message", Label: "contact.message", Type: "textarea", Value: get("message"), Required: true, Max: 5000},
	)
	return fields
}

func contactFieldsForType(typeName string, values map[string]string) ([]contactWorkflowField, string) {
	if typeName == "security" {
		return securityReportFields(values), "contact.security_title"
	}
	return ordinaryContactFields(typeName, values), "contact.type." + typeName
}

func validateOrdinaryContactFields(typeName string, fields map[string]string) error {
	allowed := map[string]bool{"type": true, "name": true, "email": true, "subject": true, "message": true, "website": true, "form_token": true, "captcha_id": true, "captcha_answer": true, "step": true, "review_token": true, "confirmed": true}
	limits := map[string]int{"name": 100, "email": 254, "subject": 200, "message": 5000}
	switch typeName {
	case "abuse":
		allowed["reported_url"], allowed["reason"] = true, true
		limits["reported_url"] = 2048
	case "dmca":
		for _, key := range []string{"reported_url", "copyrighted_work", "good_faith", "accuracy", "signature"} {
			allowed[key] = true
		}
		limits["reported_url"], limits["copyrighted_work"], limits["signature"] = 2048, 2000, 100
	case "privacy":
		allowed["request_kind"] = true
	default:
		return fmt.Errorf("invalid contact type")
	}
	for key, value := range fields {
		if !allowed[key] || !utf8Valid(value) || strings.ContainsAny(value, "\x00\u202a\u202b\u202d\u202e\u2066\u2067\u2068\u2069\u200b\u200c\u200d") {
			return fmt.Errorf("invalid field")
		}
		if limit, ok := limits[key]; ok && len([]rune(value)) > limit {
			return fmt.Errorf("field is too long")
		}
	}
	for _, key := range []string{"name", "email", "subject", "message"} {
		if fields[key] == "" {
			return fmt.Errorf("required field is missing")
		}
	}
	if !contactEmailPattern.MatchString(fields["email"]) || strings.ContainsAny(fields["name"]+fields["email"]+fields["subject"], "\r\n") {
		return fmt.Errorf("invalid contact details")
	}
	if typeName == "abuse" {
		switch fields["reason"] {
		case "illegal_content", "terms_violation", "harassment", "malware_phishing", "other":
		default:
			return fmt.Errorf("invalid reason")
		}
	}
	if typeName == "privacy" {
		switch fields["request_kind"] {
		case "access", "export", "deletion", "correction", "opt_out", "other":
		default:
			return fmt.Errorf("invalid request")
		}
	}
	if typeName == "dmca" && (fields["good_faith"] != "on" || fields["accuracy"] != "on" || fields["signature"] == "") {
		return fmt.Errorf("required DMCA attestations are missing")
	}
	return nil
}

var contactURLPattern = regexp.MustCompile(`(?i)(?:https?://[^\s<>]+|www\.[^\s<>]+|\b[a-z0-9-]+(?:\.[a-z0-9-]+)+\b)`)
var contactMarkupPattern = regexp.MustCompile(`(?is)<\s*/?\s*[a-z!]|&(?:lt|gt|#0*60|#0*62);|\[(?:url|img|b)(?:\s|=|\])|\[/`)

func ordinaryContactContentAllowed(typeName string, fields map[string]string, r *http.Request, configured ...contactAntispamSettings) bool {
	settings := contactAntispamSettings{MaxLinks: 10, RejectMarkup: true}
	if len(configured) > 0 {
		settings = configured[0]
	}
	textFields := []string{"name", "subject", "message", "reported_url", "reason", "copyrighted_work", "signature", "request_kind"}
	links := 0
	for _, key := range textFields {
		value := fields[key]
		if value == "" {
			continue
		}
		if settings.RejectMarkup && contactMarkupPattern.MatchString(value) {
			return false
		}
		matches := contactURLPattern.FindAllString(value, -1)
		if key == "name" || key == "subject" {
			if len(matches) > 0 {
				return false
			}
			continue
		}
		links += len(matches)
		for _, match := range matches {
			host := strings.TrimPrefix(strings.TrimPrefix(strings.TrimPrefix(strings.ToLower(match), "https://"), "http://"), "www.")
			host = strings.SplitN(host, "/", 2)[0]
			if !contactHostAllowed(host, r) {
				return false
			}
		}
	}
	if links > settings.MaxLinks {
		return false
	}
	for _, blocked := range settings.BlockedWords {
		blocked = strings.ToLower(strings.TrimSpace(blocked))
		if blocked != "" && (strings.Contains(strings.ToLower(fields["name"]), blocked) || strings.Contains(strings.ToLower(fields["subject"]), blocked) || strings.Contains(strings.ToLower(fields["message"]), blocked)) {
			return false
		}
	}
	if at := strings.LastIndex(fields["email"], "@"); at >= 0 {
		domain := strings.ToLower(strings.TrimSpace(fields["email"][at+1:]))
		for _, blocked := range settings.BlockedEmailDomains {
			if strings.EqualFold(domain, strings.TrimPrefix(strings.TrimSpace(blocked), "@")) {
				return false
			}
		}
	}
	if typeName == "abuse" || typeName == "dmca" {
		parsed, err := url.Parse(fields["reported_url"])
		if err != nil || parsed.Scheme == "" || parsed.Host == "" || !contactHostAllowed(parsed.Host, r) || parsed.Path == "" || parsed.Path == "/" || strings.HasPrefix(parsed.Path, "/api/") || parsed.Path == "/server/contact" {
			return false
		}
		if settings.RouteExists == nil || !settings.RouteExists(parsed.Path) {
			return false
		}
	}
	return true
}

func contactHostAllowed(host string, r *http.Request) bool {
	host = strings.Trim(strings.ToLower(host), ".")
	if strings.EqualFold(host, r.Host) {
		return true
	}
	if strings.EqualFold(host, strings.Split(r.Host, ":")[0]) {
		return true
	}
	for _, domain := range strings.Split(os.Getenv("DOMAIN"), ",") {
		if strings.EqualFold(host, strings.TrimSpace(domain)) {
			return true
		}
	}
	return strings.HasSuffix(host, ".onion") || strings.HasSuffix(host, ".i2p")
}

func contactDuplicate(fields map[string]string, configured ...contactAntispamSettings) bool {
	window := 24 * time.Hour
	if len(configured) > 0 {
		window = configured[0].DuplicateWindow
	}
	if window <= 0 {
		return false
	}
	keyParts := []string{strings.ToLower(strings.Join(strings.Fields(fields["email"]), " ")), strings.ToLower(strings.Join(strings.Fields(fields["subject"]), " ")), strings.ToLower(strings.Join(strings.Fields(fields["message"]), " "))}
	sum := sha256.Sum256([]byte(strings.Join(keyParts, "\x00")))
	key := hex.EncodeToString(sum[:])
	contactWorkflowTokens.Lock()
	defer contactWorkflowTokens.Unlock()
	now := time.Now().UTC()
	if contactWorkflowTokens.duplicates == nil {
		contactWorkflowTokens.duplicates = make(map[string]contactDuplicateEntry)
	}
	for duplicateKey, entry := range contactWorkflowTokens.duplicates {
		if !entry.expires.After(now) {
			delete(contactWorkflowTokens.duplicates, duplicateKey)
		}
	}
	if entry, exists := contactWorkflowTokens.duplicates[key]; exists && entry.expires.After(now) {
		entry.accessed = now
		contactWorkflowTokens.duplicates[key] = entry
		return true
	}
	contactWorkflowTokens.duplicates[key] = contactDuplicateEntry{expires: now.Add(window), accessed: now}
	for len(contactWorkflowTokens.duplicates) > 10000 {
		oldestKey := ""
		var oldest time.Time
		for candidate, entry := range contactWorkflowTokens.duplicates {
			if oldestKey == "" || entry.accessed.Before(oldest) {
				oldestKey, oldest = candidate, entry.accessed
			}
		}
		delete(contactWorkflowTokens.duplicates, oldestKey)
	}
	return false
}

func sendOrdinaryContact(cfg *config.Config, typeName string, fields map[string]string) error {
	client := email.Get()
	if client == nil || !client.Config().Enabled {
		return fmt.Errorf("email is unavailable")
	}
	role := cfg.Server.Contact.ResolveAbuse()
	if typeName == "privacy" {
		role = cfg.Server.Contact.ResolveDPO()
	}
	if role.Email == "" {
		return fmt.Errorf("contact recipient is unavailable")
	}
	body := []string{"Type: " + typeName, "Name: " + fields["name"], "Email: " + fields["email"], "Subject: " + fields["subject"], "Message:", fields["message"]}
	for _, key := range []string{"reported_url", "reason", "copyrighted_work", "good_faith", "accuracy", "signature", "request_kind"} {
		if fields[key] != "" {
			body = append(body, key+": "+fields[key])
		}
	}
	return client.Send(email.Message{To: []string{role.Email}, Subject: "[" + cfg.Server.Branding.Title + " contact] " + fields["subject"], Body: strings.Join(body, "\n"), ReplyTo: fields["email"]})
}

// SendSecurityStatusNotification emails only the tracking ID and new status.
// It decrypts the contact blob locally and never exposes report content.
func SendSecurityStatusNotification(cfg *config.Config, ctx context.Context, trackingID string, report database.SecurityReport) (bool, error) {
	client := email.Get()
	if client == nil || !client.Config().Enabled {
		return false, nil
	}
	ciphertext, err := database.GetSecurityReportContactCiphertext(ctx, trackingID)
	if err != nil {
		return false, err
	}
	var contact struct {
		Email    string `json:"email"`
		Language string `json:"language"`
	}
	var decrypted []byte
	for _, key := range ownerEncryptionKeys(cfg) {
		decrypted, err = decryptContactData(key, ciphertext)
		if err == nil {
			break
		}
	}
	if err != nil {
		return false, fmt.Errorf("decrypt security report contact: %w", err)
	}
	if err := json.Unmarshal(decrypted, &contact); err != nil || !contactEmailPattern.MatchString(contact.Email) {
		return false, fmt.Errorf("security report contact is invalid")
	}
	language := contact.Language
	if !i18n.IsSupported(language) || !languageEnabled(cfg.Server.I18N.AvailableLanguages, language) {
		language = i18n.DefaultLanguage
	}
	statusKey := "security.status." + report.Status
	message := i18n.TranslateFormat(language, "contact.tracking_message", map[string]string{"tracking_id": trackingID}) + "\n" + i18n.Translate(language, "security.status_label") + ": " + i18n.Translate(language, statusKey)
	err = client.Send(email.Message{To: []string{contact.Email}, Subject: "[" + cfg.Server.Branding.Title + " " + i18n.Translate(language, "security.status_title") + "] " + trackingID, Body: message, OmitReplyTo: true})
	if err != nil {
		return false, err
	}
	return true, nil
}

func languageEnabled(enabled []string, language string) bool {
	for _, candidate := range enabled {
		if candidate == language {
			return true
		}
	}
	return false
}

func decryptContactData(key, ciphertext []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	if len(ciphertext) < gcm.NonceSize()+gcm.Overhead() {
		return nil, fmt.Errorf("encrypted contact data is incomplete")
	}
	nonce, sealed := ciphertext[:gcm.NonceSize()], ciphertext[gcm.NonceSize():]
	return gcm.Open(nil, nonce, sealed, nil)
}
