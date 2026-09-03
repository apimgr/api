package server

import (
	"net/http"
	"strings"
)

// buildCookieName is the name of the build-stamp cookie required by AI.md
// PART 9 "Version-Change Purge (Clear-Site-Data)": `{project_name}_build`,
// with {project_name} hardcoded to the internal name per PART 8 (a renamed
// binary must not change cookie/config identifiers).
const buildCookieName = "api_build"

// buildCookieMaxAge is the one-year Max-Age the spec mandates for the
// build-stamp cookie.
const buildCookieMaxAge = 31536000

// assetStamp returns the running build's asset stamp,
// `{project_version}-{short_commit}` per AI.md PART 9 "Asset Version-Busting".
// This is the single source of truth for the stamp: the build-stamp cookie,
// the asset `?v=` query value, and the /sw.js + /manifest.json ETag all
// derive from it, so a new build changes every one of them together.
func assetStamp() string {
	return Version + "-" + CommitID
}

// versionPurge implements AI.md PART 9 "Version-Change Purge
// (Clear-Site-Data)". It is the forced recovery path for a browser that
// already cached HTML or a service worker from an older build: when the
// request's build-stamp cookie does not match the running stamp, the
// response evicts the HTTP cache, the Cache API caches, and any registered
// service worker in one shot.
//
// The value is `"cache", "storage"` ONLY. `"cookies"` is FORBIDDEN here — it
// would destroy the owner_token cookie and every cookie-stored preference
// (theme, language, consent). The separate token-revocation Clear-Site-Data
// response deliberately DOES include "cookies"; the two contexts must never
// be merged.
//
// The same response re-sets the cookie to the running stamp, so the next
// request matches and no purge loop is possible. A first-ever visit (no
// cookie) never purges.
//
// Callers: the HTML response middleware only — never static assets, never
// API responses, never /sw.js or /manifest.json.
func versionPurge(w http.ResponseWriter, r *http.Request) {
	stamp := assetStamp()
	if c, err := r.Cookie(buildCookieName); err == nil && c.Value != stamp {
		w.Header().Set("Clear-Site-Data", `"cache", "storage"`)
	}
	// Essential cookie (no consent required) — the stamp is public build
	// metadata, so it is deliberately not HttpOnly. Secure is set only on a
	// TLS request: browsers silently drop a Secure cookie sent over plain
	// HTTP, which would disable the purge entirely on the Tor hidden service
	// and I2P eepsite, both of which serve HTTP by design (PART 31).
	http.SetCookie(w, &http.Cookie{
		Name:     buildCookieName,
		Value:    stamp,
		Path:     "/",
		MaxAge:   buildCookieMaxAge,
		Secure:   r.TLS != nil,
		SameSite: http.SameSiteLaxMode,
	})
}

// versionPurgeWriter defers versionPurge until the response Content-Type is
// known, so the purge applies to HTML documents only. Handlers set
// Content-Type before WriteHeader, so inspecting it there is the earliest
// point at which "is this an HTML response" can be answered without
// duplicating the routing table.
type versionPurgeWriter struct {
	http.ResponseWriter
	request *http.Request
	applied bool
}

// applyOnce runs versionPurge exactly once, and only for HTML documents.
func (w *versionPurgeWriter) applyOnce() {
	if w.applied {
		return
	}
	w.applied = true
	if strings.HasPrefix(w.Header().Get("Content-Type"), "text/html") {
		versionPurge(w.ResponseWriter, w.request)
	}
}

func (w *versionPurgeWriter) WriteHeader(status int) {
	w.applyOnce()
	w.ResponseWriter.WriteHeader(status)
}

func (w *versionPurgeWriter) Write(b []byte) (int, error) {
	w.applyOnce()
	return w.ResponseWriter.Write(b)
}

// Flush keeps streaming handlers working through the wrapper.
func (w *versionPurgeWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// versionPurgeMiddleware applies the version-change purge to HTML responses
// per AI.md PART 9. Non-HTML responses (static assets, API JSON, /sw.js,
// /manifest.json) pass through untouched because the Content-Type check in
// applyOnce never matches them.
func versionPurgeMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(&versionPurgeWriter{ResponseWriter: w, request: r}, r)
	})
}
