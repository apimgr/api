package server

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"html/template"
	"log/slog"
	"net/http"
	"path"
	"strings"

	"github.com/apimgr/api/src/config"
)

// csrfContextKey carries the per-request CSRF token so handlers and templates
// can echo it into a hidden form field.
type csrfContextKey struct{}

// csrfFormField is the hidden input name checked when the token does not
// arrive in the configured header. It is fixed by AI.md PART 16 and is
// deliberately independent of the configurable header name.
const csrfFormField = "csrf_token"

// csrfMultipartMemory is the in-memory budget used when the middleware has to
// parse a multipart body to find the hidden token field. Anything larger
// spills to a temp file, exactly as the handlers' own parse would do.
const csrfMultipartMemory = 1 << 20

// csrfMiddleware implements the stateless double-submit cookie pattern from
// AI.md PART 16 → "CSRF Protection". There are no server-side sessions, so
// the token is never stored: the cookie value and the echoed header/form
// value are compared in constant time.
//
// Validation runs if and only if the method is POST/PUT/PATCH/DELETE AND no
// bearer credential is present. Bypasses are limited to the four the spec
// lists: a bearer header, a safe method, a WebSocket upgrade, and an
// operator-declared exempt path. There is deliberately NO Origin- or
// Referer-based bypass — those headers can be absent or spoofed by
// non-browser clients, and the double-submit check is cheap.
func csrfMiddleware(cfg *config.Config) func(http.Handler) http.Handler {
	csrfCfg := cfg.Server.CSRF.Normalized()

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !cfg.Server.CSRF.Enabled {
				next.ServeHTTP(w, r)
				return
			}

			cookieToken := csrfCookieValue(r, csrfCfg.CookieName)
			if cookieToken == "" {
				// Token regenerated whenever the cookie is absent, which also
				// covers the post-revocation case (revocation clears it).
				issued, err := newCSRFToken(csrfCfg.TokenLength)
				if err != nil {
					writeEnvelopeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Unable to issue a CSRF token", nil)
					return
				}
				setCSRFCookie(w, r, csrfCfg, issued)
				cookieToken = issued
			}

			if csrfValidationRequired(r, csrfCfg) {
				presented := csrfPresentedToken(r, csrfCfg.HeaderName)
				if presented == "" {
					csrfReject(w, r, "missing token")
					return
				}
				if subtle.ConstantTimeCompare([]byte(presented), []byte(cookieToken)) != 1 {
					csrfReject(w, r, "token mismatch")
					return
				}
			}

			ctx := context.WithValue(r.Context(), csrfContextKey{}, cookieToken)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// csrfValidationRequired applies the PART 16 "When CSRF Validation Runs"
// table. Everything the table lists as a bypass returns false here.
func csrfValidationRequired(r *http.Request, cfg config.CSRFConfig) bool {
	switch r.Method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
	default:
		return false
	}
	if hasBearerToken(r) || r.Header.Get("X-API-Token") != "" {
		return false
	}
	if strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
		return false
	}
	if isPublicAPIPath(r.URL.Path) {
		return false
	}
	return !csrfPathExempt(r.URL.Path, cfg.ExemptPaths)
}

// isPublicAPIPath reports whether the path is part of the programmatic API
// surface, which bypasses CSRF automatically.
//
// PART 8 mandates that "/api/..." routes accept the Authorization header ONLY
// and MUST ignore cookies — no ambient authority for programmatic endpoints.
// A route that never reads a cookie cannot be the target of a forged
// cookie-carried request, which is the only thing CSRF defends against. PART 11
// states the same conclusion from the other direction: bypasses for bearer,
// public and exempt paths are automatic. Requiring a token here would break
// every documented tokenless client (curl, the CLI, SDKs) without closing any
// attack path; cross-origin abuse of these endpoints is handled by the
// Sec-Fetch-Site layer and per-IP rate limiting instead.
func isPublicAPIPath(requestPath string) bool {
	return requestPath == "/api" || strings.HasPrefix(requestPath, "/api/")
}

// csrfPathExempt reports whether the request path matches one of the
// operator-declared glob patterns in server.csrf.exempt_paths.
func csrfPathExempt(requestPath string, patterns []string) bool {
	for _, pattern := range patterns {
		if pattern == "" {
			continue
		}
		if matched, err := path.Match(pattern, requestPath); err == nil && matched {
			return true
		}
		// A trailing /* pattern is also treated as covering every nested
		// segment, since path.Match stops at a separator.
		if strings.HasSuffix(pattern, "/*") && strings.HasPrefix(requestPath, strings.TrimSuffix(pattern, "*")) {
			return true
		}
	}
	return false
}

// csrfCookieValue reads the double-submit cookie, returning "" when absent.
func csrfCookieValue(r *http.Request, name string) string {
	cookie, err := r.Cookie(name)
	if err != nil || cookie.Value == "" {
		return ""
	}
	return cookie.Value
}

// csrfPresentedToken returns the token echoed by the client, preferring the
// configured header and falling back to the hidden form field.
func csrfPresentedToken(r *http.Request, headerName string) string {
	if value := r.Header.Get(headerName); value != "" {
		return value
	}
	if value := r.Header.Get("X-XSRF-Token"); value != "" {
		return value
	}
	contentType := strings.ToLower(r.Header.Get("Content-Type"))
	if strings.HasPrefix(contentType, "application/x-www-form-urlencoded") {
		if err := r.ParseForm(); err == nil {
			return r.PostFormValue(csrfFormField)
		}
		return ""
	}
	// ParseForm never reads a multipart body, so the upload forms need an
	// explicit ParseMultipartForm. Downstream handlers call it again with their
	// own limit, which is a no-op once r.MultipartForm is populated, so the
	// uploaded file is still available to them.
	if strings.HasPrefix(contentType, "multipart/form-data") {
		if err := r.ParseMultipartForm(csrfMultipartMemory); err == nil {
			return r.PostFormValue(csrfFormField)
		}
	}
	return ""
}

// setCSRFCookie writes the double-submit cookie. It is SameSite=Strict and
// deliberately NOT HttpOnly, because the page's own JavaScript has to read it
// to populate the header on fetch requests.
func setCSRFCookie(w http.ResponseWriter, r *http.Request, cfg config.CSRFConfig, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     cfg.CookieName,
		Value:    token,
		Path:     "/",
		Secure:   csrfCookieSecure(r, cfg.Secure),
		HttpOnly: false,
		SameSite: http.SameSiteStrictMode,
	})
}

// csrfCookieSecure resolves the "auto" | "true" | "false" secure setting;
// "auto" sets the attribute whenever the request proto is https.
func csrfCookieSecure(r *http.Request, setting string) bool {
	switch setting {
	case "true":
		return true
	case "false":
		return false
	default:
		return r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
	}
}

// newCSRFToken generates a URL-safe token with the configured entropy.
func newCSRFToken(length int) (string, error) {
	buf := make([]byte, length)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// csrfReject writes the canonical 403 body and records the PART 11
// security.csrf_failure event with IP, endpoint and reason.
func csrfReject(w http.ResponseWriter, r *http.Request, reason string) {
	slog.Warn("security.csrf_failure",
		"ip", getClientIP(r),
		"endpoint", r.URL.Path,
		"method", r.Method,
		"reason", reason)
	writeEnvelopeError(w, http.StatusForbidden, "CSRF_FAILED", "CSRF token validation failed", nil)
}

// CSRFTokenFromContext returns the token issued for this request, or "" when
// CSRF protection is disabled.
func CSRFTokenFromContext(ctx context.Context) string {
	token, _ := ctx.Value(csrfContextKey{}).(string)
	return token
}

// csrfFieldHTML renders the hidden input that server-rendered forms include,
// so no template has to build the markup by hand.
func csrfFieldHTML(token string) template.HTML {
	if token == "" {
		return ""
	}
	return template.HTML(`<input type="hidden" name="` + csrfFormField + `" value="` + template.HTMLEscapeString(token) + `">`)
}
