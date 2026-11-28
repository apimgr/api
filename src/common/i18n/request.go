package i18n

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// CookieName is the cookie used to persist a visitor's language choice.
const CookieName = "lang"

// QueryParam is the query-string parameter that selects a language for the
// current request and, when present, persists it to CookieName.
const QueryParam = "lang"

type contextKey int

const langContextKey contextKey = 0
const enabledLanguagesContextKey contextKey = 1

// LangFromRequest resolves the effective language for r using the fixed
// priority chain from AI.md PART 30:
//
//	?lang= query param -> lang cookie -> Accept-Language header -> "en"
//
// A value outside the request's enabled language set is skipped (never
// surfaced as an error), and the final result is always an enabled code.
func LangFromRequest(r *http.Request) string {
	enabled := enabledLanguagesFromContext(r.Context())
	if q := r.URL.Query().Get(QueryParam); q != "" {
		if lang, ok := normalizeEnabled(q, enabled); ok {
			return lang
		}
		return DefaultLanguage
	}

	if c, err := r.Cookie(CookieName); err == nil && c.Value != "" {
		if lang, ok := normalizeEnabled(c.Value, enabled); ok {
			return lang
		}
	}

	if al := r.Header.Get("Accept-Language"); al != "" {
		if lang, ok := langFromAcceptLanguageEnabled(al, enabled); ok {
			return lang
		}
	}

	return DefaultLanguage
}

// LangFromEmailRequest selects the submitter language using PART 17's
// email-specific order: the lang cookie first, then Accept-Language. Query
// parameters are intentionally ignored because they only select the page's
// rendered language, not the submitter's saved preference.
func LangFromEmailRequest(r *http.Request) string {
	enabled := enabledLanguagesFromContext(r.Context())
	if c, err := r.Cookie(CookieName); err == nil && c.Value != "" {
		if lang, ok := normalizeEnabled(c.Value, enabled); ok {
			return lang
		}
	}
	if header := r.Header.Get("Accept-Language"); header != "" {
		if lang, ok := langFromAcceptLanguageEnabled(header, enabled); ok {
			return lang
		}
	}
	return DefaultLanguage
}

func normalizeEnabled(tag string, enabled []string) (string, bool) {
	base := strings.ToLower(strings.TrimSpace(tag))
	if i := strings.IndexAny(base, "-_"); i != -1 {
		base = base[:i]
	}
	if IsSupported(base) {
		for _, code := range enabled {
			if code == base {
				return base, true
			}
		}
	}
	return "", false
}

func langFromAcceptLanguageEnabled(header string, enabled []string) (string, bool) {
	type candidate struct {
		lang string
		q    float64
	}
	var candidates []candidate

	for _, part := range strings.Split(header, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		tag := part
		q := 1.0
		if i := strings.IndexByte(part, ';'); i != -1 {
			tag = strings.TrimSpace(part[:i])
			params := part[i+1:]
			if j := strings.Index(params, "q="); j != -1 {
				if parsed, err := strconv.ParseFloat(strings.TrimSpace(params[j+2:]), 64); err == nil {
					q = parsed
				}
			}
		}
		if base, ok := normalizeEnabled(tag, enabled); ok {
			candidates = append(candidates, candidate{lang: base, q: q})
		}
	}

	if len(candidates) == 0 {
		return "", false
	}

	best := candidates[0]
	for _, c := range candidates[1:] {
		if c.q > best.q {
			best = c
		}
	}
	return best.lang, true
}

// LanguageMiddleware resolves the request language via LangFromRequest and
// stores it in the request context. It does not persist a preference because
// it has no consent decision; use LanguageMiddlewareForConsent for that.
func LanguageMiddleware(next http.Handler) http.Handler {
	return LanguageMiddlewareFor(SupportedLanguageCodes())(next)
}

// LanguageMiddlewareFor negotiates requests against the configured enabled
// language set without persisting preferences.
func LanguageMiddlewareFor(enabled []string) func(http.Handler) http.Handler {
	return languageMiddlewareFor(enabled, nil)
}

// LanguageMiddlewareForConsent negotiates the request language and persists a
// query-selected language only when the caller confirms Preferences consent.
func LanguageMiddlewareForConsent(enabled []string, consent func(*http.Request) bool) func(http.Handler) http.Handler {
	return languageMiddlewareFor(enabled, consent)
}

func languageMiddlewareFor(enabled []string, consent func(*http.Request) bool) func(http.Handler) http.Handler {
	if len(enabled) == 0 {
		enabled = SupportedLanguageCodes()
	}
	allowed := append([]string(nil), enabled...)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := context.WithValue(r.Context(), enabledLanguagesContextKey, allowed)
			r = r.WithContext(ctx)
			lang := LangFromRequest(r)

			if q := r.URL.Query().Get(QueryParam); q != "" {
				if normalized, ok := normalizeEnabled(q, allowed); ok && normalized == lang {
					if consent != nil && consent(r) {
						http.SetCookie(w, &http.Cookie{
							Name:     CookieName,
							Value:    lang,
							Path:     "/",
							MaxAge:   int((365 * 24 * time.Hour).Seconds()),
							HttpOnly: false,
							Secure:   r.TLS != nil,
							SameSite: http.SameSiteLaxMode,
						})
					}
				}
			}

			ctx = context.WithValue(r.Context(), langContextKey, lang)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func enabledLanguagesFromContext(ctx context.Context) []string {
	if enabled, ok := ctx.Value(enabledLanguagesContextKey).([]string); ok && len(enabled) > 0 {
		return enabled
	}
	return SupportedLanguageCodes()
}

// IsEnabledForRequest reports whether lang is both embedded and enabled for r.
func IsEnabledForRequest(r *http.Request, lang string) bool {
	if r == nil {
		return IsSupported(lang)
	}
	_, ok := normalizeEnabled(lang, enabledLanguagesFromContext(r.Context()))
	return ok
}

// LangFromContext returns the language stored by LanguageMiddleware, or
// DefaultLanguage if none is present (e.g. outside an HTTP request, such as
// CLI/agent output).
func LangFromContext(ctx context.Context) string {
	if lang, ok := ctx.Value(langContextKey).(string); ok && IsSupported(lang) {
		return lang
	}
	return DefaultLanguage
}
