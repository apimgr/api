package server

import (
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/apimgr/api/src/config"
	"github.com/apimgr/api/src/mode"
	"github.com/google/uuid"
)

// requestIDMiddleware generates a unique request ID for each request
func requestIDMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestID := ""
		invalidHeader := ""
		for _, header := range []string{"X-Request-ID", "X-Correlation-ID", "X-Trace-ID"} {
			candidate := strings.TrimSpace(r.Header.Get(header))
			if candidate == "" {
				continue
			}
			if isValidRequestUUID(candidate) {
				requestID = strings.ToLower(candidate)
			} else {
				invalidHeader = header
			}
			break
		}
		if requestID == "" {
			generated, err := uuid.NewRandom()
			if err != nil {
				slog.Error("secure request ID generation failed", "error", err)
				http.Error(w, http.StatusText(http.StatusServiceUnavailable), http.StatusServiceUnavailable)
				return
			}
			requestID = generated.String()
		}
		if invalidHeader != "" {
			slog.Warn("invalid request ID; generating a new UUIDv4", "header", invalidHeader, "request_id", requestID)
		}

		// Add to response headers
		w.Header().Set("X-Request-ID", requestID)
		r.Header.Set("X-Request-ID", requestID)

		// Add to context for use in handlers
		ctx := r.Context()
		ctx = contextWithRequestID(ctx, requestID)

		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func isValidRequestUUID(value string) bool {
	id, err := uuid.Parse(value)
	return err == nil && len(value) == 36 && id.Version() == 4 && id.Variant() == uuid.RFC4122 && strings.EqualFold(value, id.String())
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
// API and compatibility routes are excluded because this validation only
// protects browser-facing frontend handlers. Sec-Fetch-Site also shares the
// configured CSRF exempt-path list. This project has no browser flow
// authenticated by the operator token, so Sec-Fetch-User has no applicable
// route; owner-token pages do not use operator credentials.
func secFetchValidationMiddleware(cfg *config.ServerConfig) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !cfg.Web.Headers.SecFetchValidation {
				next.ServeHTTP(w, r)
				return
			}
			if isPublicAPIPath(r.URL.Path) {
				next.ServeHTTP(w, r)
				return
			}

			stateChanging := r.Method == http.MethodPost || r.Method == http.MethodPut ||
				r.Method == http.MethodPatch || r.Method == http.MethodDelete

			if stateChanging {
				// Sec-Fetch-Site rejects cross-site frontend state changes
				// unless the route is exempt or the caller presents a Bearer
				// credential that the browser cannot attach automatically.
				if r.Header.Get("Sec-Fetch-Site") == "cross-site" && !hasBearerToken(r) &&
					!csrfPathExempt(r.URL.Path, cfg.Server.CSRF.ExemptPaths) {
					writeSecFetchRejected(w, r)
					return
				}
			}

			// Sec-Fetch-Dest follows the same origin set as CSP
			// frame-ancestors. A learned or same-origin parent is permitted;
			// an iframe with no verifiable allowed parent is rejected.
			if r.Header.Get("Sec-Fetch-Dest") == "iframe" && !frameAncestorAllowed(cfg, r) {
				writeSecFetchRejected(w, r)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

func frameAncestorAllowed(cfg *config.ServerConfig, r *http.Request) bool {
	if r.Header.Get("Sec-Fetch-Site") == "same-origin" {
		return true
	}
	parentOrigin, ok := canonicalHTTPOrigin(r.Referer())
	if !ok {
		return false
	}
	if selfOrigin, ok := canonicalHTTPOrigin(requestBaseURL(r)); ok && parentOrigin == selfOrigin {
		return true
	}
	for _, origin := range learnedOrigins(cfg, r) {
		if allowed, ok := canonicalHTTPOrigin(origin); ok && parentOrigin == allowed {
			return true
		}
	}
	return false
}

func canonicalHTTPOrigin(value string) (string, bool) {
	parsed, err := url.Parse(value)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Hostname() == "" {
		return "", false
	}
	scheme := strings.ToLower(parsed.Scheme)
	host := strings.ToLower(parsed.Hostname())
	port := parsed.Port()
	if (scheme == "http" && port == "80") || (scheme == "https" && port == "443") {
		port = ""
	}
	if port != "" {
		host = net.JoinHostPort(host, port)
	} else if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	return scheme + "://" + host, true
}

// writeSecFetchRejected writes the canonical PART 14 error envelope for a
// request blocked by Sec-Fetch-* validation.
func writeSecFetchRejected(w http.ResponseWriter, r *http.Request) {
	writeLocalizedAPIError(w, r, http.StatusForbidden, "SEC_FETCH_REJECTED")
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
func securityHeadersMiddleware(cfg *config.ServerConfig) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r = requestWithServerConfig(r, cfg)
			reportsURL := BuildURL(r, APIBasePath(cfg)+"/server/reports/default")
			nelURL := BuildURL(r, APIBasePath(cfg)+"/server/reports/nel")
			cspReportURI := BuildURL(r, APIBasePath(cfg)+"/server/reports/csp")

			csp := cfg.Web.CSP
			if trackingAllowed(cfg, r) {
				tracking := buildTrackingOutput(cfg.Server.Tracking)
				csp.ScriptSrcExtra = appendCSPSource(csp.ScriptSrcExtra, tracking.ScriptHost)
				csp.ConnectSrcExtra = appendCSPSource(csp.ConnectSrcExtra, tracking.ConnectHost)
				csp.ImgSrcExtra = appendCSPSource(csp.ImgSrcExtra, tracking.ImageHost)
			}
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
				if cfg.Server.SSL.Enabled && !isI2PBackendRequest(r) {
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

			// Permissions Policy is project-declared and always emitted; the
			// internal value cannot be changed through deployment config.
			if permissions := cfg.Web.PermissionsPolicy.Header(); permissions != "" {
				w.Header().Set("Permissions-Policy", permissions)
			}

			// Reporting API (modern + legacy) and Network Error Logging use
			// separate groups so NEL reports reach their dedicated endpoint.
			w.Header().Set("Reporting-Endpoints", fmt.Sprintf(`default="%s", nel="%s"`, reportsURL, nelURL))
			w.Header().Set("Report-To", fmt.Sprintf(`{"group":"default","max_age":10886400,"endpoints":[{"url":"%s"}]}, {"group":"nel","max_age":10886400,"endpoints":[{"url":"%s"}]}`, reportsURL, nelURL))
			if headers.NEL.Enabled {
				w.Header().Set("NEL", fmt.Sprintf(
					`{"report_to":"nel","max_age":%d,"include_subdomains":%t,"success_fraction":%s,"failure_fraction":%s}`,
					headers.NEL.MaxAgeSeconds, headers.NEL.IncludeSubdomains,
					strconv.FormatFloat(headers.NEL.SampleRate, 'g', -1, 64),
					strconv.FormatFloat(headers.NEL.SampleRate, 'g', -1, 64),
				))
			}

			// HSTS (only if SSL is enabled and the operator hasn't
			// disabled it) — per RFC 6797
			if !isI2PBackendRequest(r) && cfg.Server.SSL.Enabled && cfg.Web.HSTS.Enabled && cfg.Web.HSTS.MaxAgeSeconds > 0 {
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

func appendCSPSource(existing, source string) string {
	if strings.TrimSpace(source) == "" {
		return existing
	}
	for _, current := range strings.Fields(existing) {
		if current == source {
			return existing
		}
	}
	if strings.TrimSpace(existing) == "" {
		return source
	}
	return strings.TrimSpace(existing) + " " + source
}
