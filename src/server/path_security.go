package server

import (
	"net/http"
	"path"
	"strings"

	"github.com/apimgr/api/src/common/i18n"
)

// urlNormalizationMiddleware collapses repeated slashes while retaining the
// raw request target for the following traversal check.
func urlNormalizationMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		original := r.URL.Path
		leading := strings.HasPrefix(original, "/")
		trailing := original != "/" && strings.HasSuffix(original, "/")
		parts := strings.FieldsFunc(original, func(r rune) bool { return r == '/' })
		normalized := strings.Join(parts, "/")
		if leading {
			normalized = "/" + normalized
		}
		if normalized == "" && leading {
			normalized = "/"
		}
		if trailing && !strings.HasSuffix(normalized, "/") {
			normalized += "/"
		}
		r.URL.Path = normalized
		next.ServeHTTP(w, r)
	})
}

// pathSecurityMiddleware rejects raw and decoded traversal before routing,
// then canonicalizes dot-free paths and preserves directory trailing slashes.
func pathSecurityMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		original := r.URL.Path
		rawPath := r.URL.RawPath
		if rawPath == "" {
			rawPath = original
		}
		if strings.Contains(original, "..") || strings.Contains(rawPath, "..") ||
			strings.Contains(strings.ToLower(rawPath), "%2e") {
			http.Error(w, i18n.Translate(i18n.LangFromRequest(r), "errors.bad_request"), http.StatusBadRequest)
			return
		}
		cleaned := path.Clean(original)
		if !strings.HasPrefix(cleaned, "/") {
			cleaned = "/" + cleaned
		}
		if original != "/" && strings.HasSuffix(original, "/") {
			canonical := *r.URL
			canonical.Path = strings.TrimSuffix(cleaned, "/")
			canonical.RawPath = ""
			http.Redirect(w, r, canonical.String(), http.StatusMovedPermanently)
			return
		}
		r.URL.Path = cleaned
		r.URL.RawPath = ""
		next.ServeHTTP(w, r)
	})
}
