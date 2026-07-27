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
				directives := []string{
					cspDirective("default-src", "'self'", "", csp.DefaultSrcOverride),
					cspDirective("script-src", "'self'", csp.ScriptSrcExtra, csp.ScriptSrcOverride),
					cspDirective("style-src", "'self' 'unsafe-inline'", csp.StyleSrcExtra, csp.StyleSrcOverride),
					cspDirective("img-src", "'self' data: blob: https:", csp.ImgSrcExtra, csp.ImgSrcOverride),
					cspDirective("font-src", "'self' https:", csp.FontSrcExtra, csp.FontSrcOverride),
					cspDirective("connect-src", "'self'", csp.ConnectSrcExtra, csp.ConnectSrcOverride),
					"media-src 'self' blob:",
					"worker-src 'self' blob:",
					"manifest-src 'self'",
					cspDirective("frame-src", "'self'", csp.FrameSrcExtra, csp.FrameSrcOverride),
					"frame-ancestors 'self'",
					"base-uri 'self'",
					cspDirective("form-action", "'self'", csp.FormActionExtra, csp.FormActionOverride),
					"object-src 'none'",
				}
				if cfg.Server.SSL.Enabled {
					directives = append(directives, "upgrade-insecure-requests")
				}
				if csp.ReportsEnabled {
					directives = append(directives, "report-to default", "report-uri "+cspReportURI)
				}

				// Development mode auto-degrades to report-only unless the
				// operator explicitly pinned mode: enforce, per AI.md PART
				// 11 → "In development mode ... CSP runs in
				// Content-Security-Policy-Report-Only mode".
				headerName := "Content-Security-Policy"
				if csp.Mode == "report-only" || (mode.IsDevelopment() && csp.Mode != "enforce") {
					headerName = "Content-Security-Policy-Report-Only"
				}
				w.Header().Set(headerName, strings.Join(directives, "; "))
			}

			// Modern replacement for X-Frame-Options is frame-ancestors
			// above; X-Frame-Options stays set for legacy browsers
			w.Header().Set("X-Frame-Options", "SAMEORIGIN")

			// Prevent MIME sniffing
			w.Header().Set("X-Content-Type-Options", "nosniff")

			// XSS Protection (deprecated in modern browsers, kept for
			// older browser compatibility)
			w.Header().Set("X-XSS-Protection", "1; mode=block")

			// Referrer Policy
			w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")

			headers := cfg.Web.Headers

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
