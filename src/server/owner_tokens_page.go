package server

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"html/template"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/apimgr/api/src/common/i18n"
	"github.com/apimgr/api/src/config"
	"github.com/apimgr/api/src/database"
	"github.com/go-chi/chi/v5"
)

type ownerTokensPageData struct {
	PageData
	OwnerCookie    bool
	PrimaryToken   *database.OwnerToken
	LinkedTokens   []database.OwnerToken
	RevealedToken  string
	RevealedCard   string
	ResourcePlural string
	ErrorMessage   string
	SuccessMessage string
}

var ownerTokensTemplates struct {
	sync.Once
	tmpl *template.Template
	err  error
}

func ownerTokenPageTemplate() (*template.Template, error) {
	ownerTokensTemplates.Do(func() {
		ownerTokensTemplates.tmpl, ownerTokensTemplates.err = template.New("preferences_tokens").Funcs(templateFuncs()).ParseFS(templatesFS,
			"template/layout/public.tmpl", "template/partial/*.tmpl", "template/partial/public/*.tmpl", "template/page/preferences_tokens.tmpl")
	})
	return ownerTokensTemplates.tmpl, ownerTokensTemplates.err
}

// ownerTokensPageHandler renders the cookie-authenticated token-management page.
func ownerTokensPageHandler(cfg *config.Config, cookieName, projectName string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !validOwnerCookieName(cookieName, projectName) {
			http.Error(w, i18n.Translate(i18n.LangFromRequest(r), "errors.server_error"), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		data := ownerTokensPageData{PageData: newPageData(cfg, r, "preferences"), ResourcePlural: i18n.Translate(i18n.LangFromRequest(r), "owner_tokens.resources")}
		data.PageTitle = i18n.Translate(data.Lang, "page_text.text.owner.tokens.a5dc2b2bfe")
		data.PageDescription = i18n.Translate(data.Lang, "page_text.text.manage.the.credentials.that.identify.your.an.e3e4a1b248")
		data.CancelURL = "/server/preferences"
		credential, token, err := webOwnerCredential(r, cfg, cookieName)
		if err == nil && credential.Kind == "primary" {
			data.OwnerCookie = true
			setOwnerTokenCookie(w, r, cfg, cookieName, token)
			tokens, listErr := database.ListOwnerTokens(r.Context(), database.GetServerDB(), credential.OwnerID)
			if listErr == nil {
				for i := range tokens {
					if tokens[i].Kind == "primary" {
						data.PrimaryToken = &tokens[i]
					} else {
						data.LinkedTokens = append(data.LinkedTokens, tokens[i])
					}
				}
			}
			if data.PrimaryToken == nil {
				data.OwnerCookie = false
				clearOwnerTokenCookie(w, r, cookieName)
			}
			if card, ok := consumeOwnerTokenView(w, r, projectName); ok {
				var revealErr error
				if card == "primary" {
					data.RevealedToken, revealErr = database.RevealOwnerToken(r.Context(), database.GetServerDB(), credential.OwnerID, credential.TokenID, ownerEncryptionKeys(cfg)...)
				} else if id, parseErr := strconv.ParseInt(card, 10, 64); parseErr == nil {
					data.RevealedToken, revealErr = database.RevealLinkedOwnerToken(r.Context(), database.GetServerDB(), credential.OwnerID, id, ownerEncryptionKeys(cfg)...)
				}
				if revealErr == nil {
					data.RevealedCard = card
				}
			}
		} else if cookie, cookieErr := r.Cookie(cookieName); cookieErr == nil && cookie.Value != "" {
			clearOwnerTokenCookie(w, r, cookieName)
		}
		switch r.URL.Query().Get("status") {
		case "created":
			data.SuccessMessage = i18n.Translate(data.Lang, "owner_tokens.created_notice")
		case "updated":
			data.SuccessMessage = i18n.Translate(data.Lang, "owner_tokens.updated_notice")
		case "rotated":
			data.SuccessMessage = i18n.Translate(data.Lang, "owner_tokens.rotated_notice")
		case "revoked":
			data.SuccessMessage = i18n.Translate(data.Lang, "owner_tokens.revoked_notice")
		case "forgotten":
			data.SuccessMessage = i18n.Translate(data.Lang, "owner_tokens.forgotten_notice")
		case "error":
			data.ErrorMessage = i18n.Translate(data.Lang, "owner_tokens.action_failed")
		}
		tmpl, parseErr := ownerTokenPageTemplate()
		if parseErr != nil {
			http.Error(w, i18n.Translate(data.Lang, "errors.server_error"), http.StatusInternalServerError)
			return
		}
		if err := tmpl.ExecuteTemplate(w, "public.tmpl", data); err != nil {
			return
		}
	}
}

func ownerTokensActionHandler(cfg *config.Config, cookieName, projectName, action string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !validOwnerCookieName(cookieName, projectName) {
			http.Error(w, i18n.Translate(i18n.LangFromRequest(r), "errors.server_error"), http.StatusInternalServerError)
			return
		}
		if r.Method != http.MethodPost {
			http.Error(w, i18n.Translate(i18n.LangFromRequest(r), "errors.method_not_allowed"), http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		_ = r.ParseForm()
		if action == "forget" {
			clearOwnerTokenCookie(w, r, cookieName)
			redirectTokenPage(w, r, "forgotten")
			return
		}
		credential, _, authErr := webOwnerCredential(r, cfg, cookieName)
		if action == "create" {
			if authErr == nil {
				redirectTokenPage(w, r, "error")
				return
			}
			raw, owner, err := database.IssueOwnerToken(r.Context(), database.GetServerDB(), ownerEncryptionKey(cfg))
			if err != nil {
				redirectTokenPage(w, r, "error")
				return
			}
			setOwnerTokenCookie(w, r, cfg, cookieName, raw)
			setOwnerTokenView(w, r, projectName, "primary")
			writeTokenAudit(r, "token.created", owner, map[string]any{"source": "web"})
			redirectTokenCard(w, r, "created", "primary")
			return
		}
		if action == "restore" {
			raw := strings.TrimSpace(r.FormValue("token"))
			restored, err := validateConfiguredOwnerToken(r.Context(), cfg, raw)
			if err != nil || restored.Kind != "primary" {
				count, reset, recordErr := recordAuthenticationFailure(r)
				if recordErr == nil && count >= cfg.Server.RateLimit.AuthFailures.Requests {
					writeRateLimitExceeded(w, r, cfg.Server.RateLimit.AuthFailures.Requests, reset)
					return
				}
				redirectTokenPage(w, r, "error")
				return
			}
			setOwnerTokenCookie(w, r, cfg, cookieName, raw)
			redirectTokenPage(w, r, "")
			return
		}
		if authErr != nil || credential.Kind != "primary" {
			clearOwnerTokenCookie(w, r, cookieName)
			redirectTokenPage(w, r, "error")
			return
		}
		if cookie, err := r.Cookie(cookieName); err == nil {
			setOwnerTokenCookie(w, r, cfg, cookieName, cookie.Value)
		}
		db := database.GetServerDB()
		id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
		status, revealCard := "", ""
		switch action {
		case "reveal":
			revealCard = "primary"
		case "rotate":
			raw, err := database.RotateOwnerToken(r.Context(), db, ownerEncryptionKey(cfg), credential.OwnerID, credential.TokenID)
			if err != nil {
				status = "error"
			} else {
				setOwnerTokenCookie(w, r, cfg, cookieName, raw)
				revealCard = "primary"
				status = "rotated"
			}
		case "revoke":
			items, err := database.ListOwnerTokens(r.Context(), db, credential.OwnerID)
			if err != nil || len(items) == 0 || subtleLast4(r.FormValue("confirm_last4"), items[0].Last4) != 1 || database.RevokeOwner(r.Context(), db, credential.OwnerID) != nil {
				status = "error"
			} else {
				clearOwnerTokenCookie(w, r, cookieName)
				status = "revoked"
			}
		case "linked-create":
			exp, err := parseOwnerTokenExpiry(expiryFormValue(r))
			if err != nil {
				status = "error"
				break
			}
			policy, _ := cfg.Server.Tokens.Normalized()
			raw, item, err := database.CreateLinkedOwnerToken(r.Context(), db, ownerEncryptionKey(cfg), credential.OwnerID, r.FormValue("name"), exp, policy.MaxLinked)
			if err != nil {
				status = "error"
			} else {
				revealCard = strconv.FormatInt(item.ID, 10)
				_ = raw
				status = "created"
			}
		case "linked-reveal":
			revealCard = strconv.FormatInt(id, 10)
		case "linked-rotate":
			raw, err := database.RotateLinkedOwnerToken(r.Context(), db, ownerEncryptionKey(cfg), credential.OwnerID, id)
			if err != nil {
				status = "error"
			} else {
				revealCard = strconv.FormatInt(id, 10)
				_ = raw
				status = "rotated"
			}
		case "linked-update":
			exp, err := parseOwnerTokenExpiry(expiryFormValue(r))
			if err != nil || database.UpdateLinkedOwnerToken(r.Context(), db, credential.OwnerID, id, r.FormValue("name"), exp) != nil {
				status = "error"
			} else {
				status = "updated"
			}
		case "linked-revoke":
			if database.RevokeLinkedOwnerToken(r.Context(), db, credential.OwnerID, id) != nil {
				status = "error"
			} else {
				status = "revoked"
			}
		default:
			status = "error"
		}
		if revealCard != "" {
			setOwnerTokenView(w, r, projectName, revealCard)
		}
		writeTokenAudit(r, "token.web_action", credential, map[string]any{"action": action, "token_id": id})
		if revealCard != "" {
			redirectTokenCard(w, r, status, revealCard)
		} else {
			redirectTokenPage(w, r, status)
		}
	}
}

func validOwnerCookieName(cookieName, projectName string) bool {
	const marker = "_owner_token_"
	if !strings.HasPrefix(cookieName, projectName+marker) {
		return false
	}
	suffix := strings.TrimPrefix(cookieName, projectName+marker)
	derived, err := ownerTokenCookieName(projectName, suffix)
	return err == nil && derived == cookieName
}

func webOwnerCredential(r *http.Request, cfg *config.Config, cookieName string) (database.OwnerCredential, string, error) {
	c, err := r.Cookie(cookieName)
	if err != nil || c.Value == "" {
		return database.OwnerCredential{}, "", database.ErrInvalidOwnerToken
	}
	credential, err := validateConfiguredOwnerToken(r.Context(), cfg, c.Value)
	if err != nil || credential.Kind != "primary" {
		return database.OwnerCredential{}, "", database.ErrInvalidOwnerToken
	}
	return credential, c.Value, nil
}

func expiryFormValue(r *http.Request) string {
	if r.FormValue("expiry") == "custom" {
		return r.FormValue("expiry_date")
	}
	return r.FormValue("expiry")
}
func redirectTokenPage(w http.ResponseWriter, r *http.Request, status string) {
	target := "/server/preferences/tokens"
	if status != "" {
		target += "?status=" + status
	}
	http.Redirect(w, r, target, http.StatusSeeOther)
}

func redirectTokenCard(w http.ResponseWriter, r *http.Request, status, card string) {
	target := "/server/preferences/tokens"
	if status != "" {
		target += "?status=" + status
	}
	if card == "primary" {
		target += "#primary"
	} else {
		target += "#token-" + card
	}
	http.Redirect(w, r, target, http.StatusSeeOther)
}

func ownerViewMAC(ctx context.Context, payload string, secret []byte) string {
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write([]byte(payload))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}
func setOwnerTokenView(w http.ResponseWriter, r *http.Request, projectName, card string) {
	secret, err := database.LoadSecret(r.Context(), database.SecretCookieSigningKey)
	if err != nil || len(secret.Value) == 0 {
		return
	}
	payload := card + "." + strconv.FormatInt(time.Now().Add(time.Minute).Unix(), 10)
	value := payload + "." + ownerViewMAC(r.Context(), payload, secret.Value)
	proto, _, _ := GetURLVars(r)
	http.SetCookie(w, &http.Cookie{Name: projectName + "_token_view", Value: value, Path: "/server/preferences/tokens", MaxAge: 60, HttpOnly: true, Secure: strings.EqualFold(proto, "https"), SameSite: http.SameSiteStrictMode})
}
func consumeOwnerTokenView(w http.ResponseWriter, r *http.Request, projectName string) (string, bool) {
	name := projectName + "_token_view"
	c, err := r.Cookie(name)
	proto, _, _ := GetURLVars(r)
	http.SetCookie(w, &http.Cookie{Name: name, Value: "", Path: "/server/preferences/tokens", MaxAge: -1, Expires: time.Unix(1, 0).UTC(), HttpOnly: true, Secure: strings.EqualFold(proto, "https"), SameSite: http.SameSiteStrictMode})
	if err != nil {
		return "", false
	}
	parts := strings.Split(c.Value, ".")
	if len(parts) != 3 {
		return "", false
	}
	expiry, e := strconv.ParseInt(parts[1], 10, 64)
	if e != nil || expiry < time.Now().Unix() || expiry > time.Now().Add(time.Minute*2).Unix() {
		return "", false
	}
	secret, e := database.LoadSecret(r.Context(), database.SecretCookieSigningKey)
	if e != nil {
		return "", false
	}
	payload := parts[0] + "." + parts[1]
	valid := hmac.Equal([]byte(parts[2]), []byte(ownerViewMAC(r.Context(), payload, secret.Value)))
	if !valid && len(secret.Previous) > 0 && secret.PreviousExpiresAt.After(time.Now()) {
		valid = hmac.Equal([]byte(parts[2]), []byte(ownerViewMAC(r.Context(), payload, secret.Previous)))
	}
	if !valid {
		return "", false
	}
	return parts[0], true
}

func subtleLast4(value, expected string) int {
	if len(value) != len(expected) {
		return 0
	}
	return subtle.ConstantTimeCompare([]byte(value), []byte(expected))
}
