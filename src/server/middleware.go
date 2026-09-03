package server

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/apimgr/api/src/config"
	"github.com/apimgr/api/src/mode"
)

// requestIDMiddleware generates a unique request ID for each request
func requestIDMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Check if request ID already exists (from load balancer/proxy)
		requestID := r.Header.Get("X-Request-ID")
		if requestID == "" {
			// Generate new request ID
			b := make([]byte, 16)
			rand.Read(b)
			requestID = hex.EncodeToString(b)
		}

		// Add to response headers
		w.Header().Set("X-Request-ID", requestID)

		// Add to context for use in handlers
		ctx := r.Context()
		ctx = contextWithRequestID(ctx, requestID)

		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// hasBearerToken reports whether the request carries an Authorization:
// Bearer credential — the signal the spec uses to exempt API-client
// requests from Sec-Fetch-Site cross-site rejection (a browser cannot
// auto-attach a Bearer header the way it auto-attaches cookies).
func hasBearerToken(r *http.Request) bool {
	return strings.HasPrefix(strings.ToLower(r.Header.Get("Authorization")), "bearer ")
}

// secFetchValidationMiddleware validates the browser-set Sec-Fetch-*
// request headers as a defense-in-depth layer against CSRF and
// clickjacking, per AI.md "Sec-Fetch-* Request Validation". It runs before
// handlers execute and is deliberately separate from
// securityHeadersMiddleware, which only emits response headers. Gated on
// cfg.Web.Headers.SecFetchValidation.
//
// Validation is present-and-bad reject only: an absent Sec-Fetch-* header
// is always treated as legacy-browser pass-through, never a rejection.
//
// The CSRF token check itself lives in csrfMiddleware and owns the
// `server.csrf.exempt_paths` allow-list; this layer stays deliberately
// simpler and does not consult it, so a path exempted from the token check
// is still subject to Sec-Fetch-Site rejection unless it presents a bearer
// credential. Similarly, there is no per-path `frame-ancestors` config — the
// CSP frame-ancestors directive defaults to 'self' plus any auto-detected
// {learned_origins} (DOMAIN env + reverse-proxy-detected hosts, see PART 11
// → "Content Security Policy" → "Auto-detection") in securityHeadersMiddleware.
// The Sec-Fetch-Dest check here is deliberately stricter than that CSP
// directive: it rejects ALL cross-site iframe embeds outright, since this
// project has no per-path allow-list config to check a learned origin
// against.
func secFetchValidationMiddleware(cfg *config.Config) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !cfg.Web.Headers.SecFetchValidation {
				next.ServeHTTP(w, r)
				return
			}

			stateChanging := r.Method == http.MethodPost || r.Method == http.MethodPut ||
				r.Method == http.MethodPatch || r.Method == http.MethodDelete

			if stateChanging {
				// Sec-Fetch-Site: reject a cross-site state-changer unless
				// the caller proved possession of a Bearer credential (a
				// browser cannot auto-attach that the way it auto-attaches
				// cookies). The CSRF exempt_paths allow-list is not
				// consulted here (see doc comment above).
				if r.Header.Get("Sec-Fetch-Site") == "cross-site" && !hasBearerToken(r) {
					writeSecFetchRejected(w)
					return
				}

				// Sec-Fetch-Mode: block form-based navigation CSRF against
				// the JSON API surface. GET/HEAD navigations are excluded
				// by the stateChanging gate above — opening an API URL in
				// a browser still returns JSON normally.
				if r.Header.Get("Sec-Fetch-Mode") == "navigate" && strings.HasPrefix(r.URL.Path, "/api/") {
					writeSecFetchRejected(w)
					return
				}

				// Sec-Fetch-User: only meaningful for authenticated
				// state-changing navigations (spec: "sensitive
				// operator-token flows only"). This project has no
				// accounts/sessions, so a Bearer credential is the closest
				// available "authenticated" signal. The check only runs
				// once Sec-Fetch-Mode confirms the browser participates in
				// the Sec-Fetch-* family (present as "navigate"), so a
				// missing/bad Sec-Fetch-User here is present-and-bad for a
				// confirmed-participating browser, not a legacy omission.
				if hasBearerToken(r) && r.Header.Get("Sec-Fetch-Mode") == "navigate" {
					if user := r.Header.Get("Sec-Fetch-User"); user != "" && user != "?1" {
						writeSecFetchRejected(w)
						return
					}
				}
			}

			// Sec-Fetch-Dest: block a cross-site iframe embed attempt. This
			// check is intentionally stricter than the CSP frame-ancestors
			// directive (which now also allows auto-detected
			// {learned_origins} — see doc comment above): there's no
			// per-path allow-list here, so any cross-site framing
			// destination is outright disallowed.
			if r.Header.Get("Sec-Fetch-Dest") == "iframe" && r.Header.Get("Sec-Fetch-Site") == "cross-site" {
				writeSecFetchRejected(w)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// writeSecFetchRejected writes the canonical PART 14 error envelope for a
// request blocked by Sec-Fetch-* validation.
func writeSecFetchRejected(w http.ResponseWriter) {
	writeEnvelopeError(w, http.StatusForbidden, "SEC_FETCH_REJECTED", "Request blocked by Sec-Fetch validation", nil)
}

// cspDirective builds one CSP directive from the built-in default value,
// applying an operator override (replaces default) or extra (appends to
// default) per AI.md PART 11 → "Content Security Policy" → Configuration.
func cspDirective(name, def, extra, override string) string {
	if override != "" {
		return name + " " + override
	}
	if extra != "" {
		return name + " " + def + " " + extra
	}
	return name + " " + def
}

// securityHeadersMiddleware adds security headers to all responses per
// AI.md PART 11 → "Security Headers" / "Content Security Policy" /
// "Permissions-Policy Configuration", fully driven by cfg.Web.* so
// server.yml can tighten or extend every directive without a code change.
func securityHeadersMiddleware(cfg *config.Config) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			scheme := "http"
			if r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") {
				scheme = "https"
			}
			reportsURL := fmt.Sprintf("%s://%s/api/v1/server/reports/default", scheme, r.Host)
			cspReportURI := fmt.Sprintf("%s://%s/api/v1/server/reports/csp", scheme, r.Host)

			csp := cfg.Web.CSP
			if csp.Enabled {
				// Auto-detection per AI.md PART 11 → "Content Security
				// Policy" → "Auto-detection": connect-src, frame-ancestors,
				// and form-action pick up the same {learned_origins} set as
				// the CORS Allow-list Resolution Order (PART 16) — DOMAIN
				// env entries plus reverse-proxy-detected hosts — so the
				// operator never has to list their own domain in
				// connect_src_extra.
				learned := strings.Join(learnedOrigins(cfg, r), " ")
				connectSrcDefault := "'self'"
				frameAncestorsDefault := "'self'"
				formActionDefault := "'self'"
				if learned != "" {
					connectSrcDefault += " " + learned
					frameAncestorsDefault += " " + learned
					formActionDefault += " " + learned
				}

				directives := []string{
					cspDirective("default-src", "'self'", "", csp.DefaultSrcOverride),
					cspDirective("script-src", "'self'", csp.ScriptSrcExtra, csp.ScriptSrcOverride),
					cspDirective("style-src", "'self' 'unsafe-inline'", csp.StyleSrcExtra, csp.StyleSrcOverride),
					cspDirective("img-src", "'self' data: blob: https:", csp.ImgSrcExtra, csp.ImgSrcOverride),
					cspDirective("font-src", "'self' https:", csp.FontSrcExtra, csp.FontSrcOverride),
					cspDirective("connect-src", connectSrcDefault, csp.ConnectSrcExtra, csp.ConnectSrcOverride),
					"media-src 'self' blob:",
					"worker-src 'self' blob:",
					"manifest-src 'self'",
					cspDirective("frame-src", "'self'", csp.FrameSrcExtra, csp.FrameSrcOverride),
					cspDirective("frame-ancestors", frameAncestorsDefault, "", ""),
					"base-uri 'self'",
					cspDirective("form-action", formActionDefault, csp.FormActionExtra, csp.FormActionOverride),
					"object-src 'none'",
				}
				if cfg.Server.SSL.Enabled {
					directives = append(directives, "upgrade-insecure-requests")
				}
				if csp.ReportsEnabled {
					directives = append(directives, "report-to default", "report-uri "+cspReportURI)
				}

				// The non-production modes auto-degrade to report-only unless
				// the operator explicitly pinned mode: enforce, per AI.md PART
				// 11 → "In development mode ... CSP runs in
				// Content-Security-Policy-Report-Only mode". Debug is the
				// second non-production mode and shares that behaviour.
				headerName := "Content-Security-Policy"
				if csp.Mode == "report-only" || (mode.IsVerboseMode() && csp.Mode != "enforce") {
					headerName = "Content-Security-Policy-Report-Only"
				}
				w.Header().Set(headerName, strings.Join(directives, "; "))
			}

			// Modern replacement for X-Frame-Options is frame-ancestors
			// above; X-Frame-Options stays set for legacy browsers.
			//
			// AI.md's IDEA.md → Header Tightening Auto-Map PCI-DSS row also
			// calls for frame-ancestors='none'/X-Frame-Options=DENY "on
			// payment pages". This project has no payment pages (IDEA.md
			// non-goals: no accounts, no checkout flow), so that per-page
			// override is intentionally not implemented — see
			// src/config/ideamap.go doc comment.
			w.Header().Set("X-Frame-Options", "SAMEORIGIN")

			// Prevent MIME sniffing
			w.Header().Set("X-Content-Type-Options", "nosniff")

			// XSS Protection (deprecated in modern browsers, kept for
			// older browser compatibility)
			w.Header().Set("X-XSS-Protection", "1; mode=block")

			headers := cfg.Web.Headers

			// Referrer Policy — config-driven so the IDEA.md → Header
			// Tightening Auto-Map can tighten it per AI.md PART 11; empty
			// (pre-upgrade server.yml with no key yet) falls back to the
			// same default this used to be hardcoded to.
			referrerPolicy := headers.ReferrerPolicy
			if referrerPolicy == "" {
				referrerPolicy = "strict-origin-when-cross-origin"
			}
			w.Header().Set("Referrer-Policy", referrerPolicy)

			// Blocks Flash/PDF cross-domain embedding
			if headers.CrossDomainPolicies != "" {
				w.Header().Set("X-Permitted-Cross-Domain-Policies", headers.CrossDomainPolicies)
			}

			// Security/perf hygiene, no compatibility cost
			if headers.OriginAgentCluster {
				w.Header().Set("Origin-Agent-Cluster", "?1")
			}

			// Cross-origin isolation headers — "everyone" defaults; per
			// PART 11 these tighten only for compliance-flagged projects
			// via the IDEA.md → Header Tightening Auto-Map
			if headers.COOP != "" {
				w.Header().Set("Cross-Origin-Opener-Policy", headers.COOP)
			}
			if headers.COEP != "" {
				w.Header().Set("Cross-Origin-Embedder-Policy", headers.COEP)
			}
			if headers.CORP != "" {
				w.Header().Set("Cross-Origin-Resource-Policy", headers.CORP)
			}

			// "" = omit (browser default applies); "off" = privacy-strict
			if headers.DNSPrefetchControl != "" {
				w.Header().Set("X-DNS-Prefetch-Control", headers.DNSPrefetchControl)
			}

			// Permissions Policy — fully config-driven per AI.md PART 11 →
			// "Permissions-Policy Configuration"
			if permissions := cfg.Web.PermissionsPolicy.Header(); permissions != "" {
				w.Header().Set("Permissions-Policy", permissions)
			}

			// Reporting API (modern + legacy) and Network Error Logging —
			// same endpoint referenced by the CSP report-to/report-uri
			// directives above
			w.Header().Set("Reporting-Endpoints", fmt.Sprintf(`default="%s"`, reportsURL))
			w.Header().Set("Report-To", fmt.Sprintf(`{"group":"default","max_age":10886400,"endpoints":[{"url":"%s"}]}`, reportsURL))
			if headers.NEL.Enabled {
				w.Header().Set("NEL", fmt.Sprintf(
					`{"report_to":"default","max_age":%d,"include_subdomains":%t,"success_fraction":%s,"failure_fraction":%s}`,
					headers.NEL.MaxAgeSeconds, headers.NEL.IncludeSubdomains,
					strconv.FormatFloat(headers.NEL.SampleRate, 'g', -1, 64),
					strconv.FormatFloat(headers.NEL.SampleRate, 'g', -1, 64),
				))
			}

			// HSTS (only if SSL is enabled and the operator hasn't
			// disabled it) — per RFC 6797
			if cfg.Server.SSL.Enabled && cfg.Web.HSTS.Enabled && cfg.Web.HSTS.MaxAgeSeconds > 0 {
				hsts := fmt.Sprintf("max-age=%d", cfg.Web.HSTS.MaxAgeSeconds)
				if cfg.Web.HSTS.IncludeSubdomains {
					hsts += "; includeSubDomains"
				}
				if cfg.Web.HSTS.Preload {
					hsts += "; preload"
				}
				w.Header().Set("Strict-Transport-Security", hsts)
			}

			// Privacy signal — Sec-GPC (Global Privacy Control) is honored
			// as an inbound opt-out signal per AI.md PART 11 → "Privacy
			// Signal Headers"; DNT is off by default (dead in modern
			// browsers) but operator-configurable.
			gpcOptOut := headers.HonorSecGPC && r.Header.Get("Sec-GPC") == "1"
			dntOptOut := headers.HonorDNT && r.Header.Get("DNT") == "1"
			if gpcOptOut || dntOptOut {
				if logger := GetLogger(); logger != nil {
					logger.LogSecurity("compliance.gpc_honored", getClientIP(r), map[string]interface{}{
						"sec_gpc": gpcOptOut,
						"dnt":     dntOptOut,
						"path":    r.URL.Path,
					})
				}
				ctx := contextWithPrivacyOptOut(r.Context(), true)
				r = r.WithContext(ctx)
			}

			next.ServeHTTP(w, r)
		})
	}
}
