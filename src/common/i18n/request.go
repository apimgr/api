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

// LangFromRequest resolves the effective language for r using the fixed
// priority chain from AI.md PART 30:
//
//	?lang= query param -> lang cookie -> Accept-Language header -> "en"
//
// An unsupported value at any stage is skipped (never surfaced as an
// error), and the final result is always one of the seven supported
// language codes.
func LangFromRequest(r *http.Request) string {
	if q := r.URL.Query().Get(QueryParam); q != "" {
		if lang, ok := normalizeSupported(q); ok {
			return lang
		}
	}

	if c, err := r.Cookie(CookieName); err == nil && c.Value != "" {
		if lang, ok := normalizeSupported(c.Value); ok {
			return lang
		}
	}

	if al := r.Header.Get("Accept-Language"); al != "" {
		if lang, ok := langFromAcceptLanguage(al); ok {
			return lang
		}
	}

	return DefaultLanguage
}

// normalizeSupported reduces a BCP 47 tag (e.g. "en-US", "zh-Hans-CN") to
// its base language subtag and reports whether that subtag is one of the
// seven supported languages.
func normalizeSupported(tag string) (string, bool) {
	base := strings.ToLower(strings.TrimSpace(tag))
	if i := strings.IndexAny(base, "-_"); i != -1 {
		base = base[:i]
	}
	if IsSupported(base) {
		return base, true
	}
	return "", false
}

// langFromAcceptLanguage parses an RFC 9110 Accept-Language header and
// returns the highest-quality supported language, if any.
func langFromAcceptLanguage(header string) (string, bool) {
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
		if base, ok := normalizeSupported(tag); ok {
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

// LanguageMiddleware resolves the request language via LangFromRequest,
// stores it in the request context (retrievable with LangFromContext), and
// — when the resolution came from an explicit ?lang= query parameter —
// persists the choice to CookieName for one year.
func LanguageMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		lang := LangFromRequest(r)

		if q := r.URL.Query().Get(QueryParam); q != "" {
			if normalized, ok := normalizeSupported(q); ok && normalized == lang {
				http.SetCookie(w, &http.Cookie{
					Name:     CookieName,
					Value:    lang,
					Path:     "/",
					MaxAge:   int((365 * 24 * time.Hour).Seconds()),
					HttpOnly: true,
					Secure:   r.TLS != nil,
					SameSite: http.SameSiteLaxMode,
				})
			}
		}

		ctx := context.WithValue(r.Context(), langContextKey, lang)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
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
