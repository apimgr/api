package server

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"
	"strings"

	"github.com/apimgr/api/src/config"
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

// securityHeadersMiddleware adds security headers to all responses (PART 13
// → "Security Headers"). Directive values match the spec's "everyone"
// defaults; per-project tightening via server.yml is a separate feature
// (PART 16 → "IDEA.md → Header Tightening Auto-Map") not yet implemented.
func securityHeadersMiddleware(cfg *config.Config) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			scheme := "http"
			if r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") {
				scheme = "https"
			}
			reportsURL := fmt.Sprintf("%s://%s/api/v1/server/reports/default", scheme, r.Host)
			cspReportURI := fmt.Sprintf("%s://%s/api/v1/server/reports/csp", scheme, r.Host)

			// Content Security Policy — no unsafe-inline/unsafe-eval on
			// script-src; style-src keeps unsafe-inline (inline style=""
			// attributes are unavoidable in current templates)
			cspDirectives := []string{
				"default-src 'self'",
				"script-src 'self'",
				"style-src 'self' 'unsafe-inline'",
				"img-src 'self' data: blob: https:",
				"font-src 'self' https:",
				"connect-src 'self'",
				"media-src 'self' blob:",
				"worker-src 'self' blob:",
				"manifest-src 'self'",
				"frame-src 'self'",
				"frame-ancestors 'self'",
				"base-uri 'self'",
				"form-action 'self'",
				"object-src 'none'",
			}
			if cfg.Server.SSL.Enabled {
				cspDirectives = append(cspDirectives, "upgrade-insecure-requests")
			}
			cspDirectives = append(cspDirectives, "report-to default", "report-uri "+cspReportURI)
			w.Header().Set("Content-Security-Policy", strings.Join(cspDirectives, "; "))

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

			// Blocks Flash/PDF cross-domain embedding
			w.Header().Set("X-Permitted-Cross-Domain-Policies", "none")

			// Security/perf hygiene, no compatibility cost
			w.Header().Set("Origin-Agent-Cluster", "?1")

			// Cross-origin isolation headers — "everyone" defaults; per
			// PART 13 these tighten only for compliance-flagged projects
			w.Header().Set("Cross-Origin-Opener-Policy", "unsafe-none")
			w.Header().Set("Cross-Origin-Embedder-Policy", "unsafe-none")
			w.Header().Set("Cross-Origin-Resource-Policy", "cross-origin")

			// Permissions Policy
			permissions := strings.Join([]string{
				"geolocation=()",
				"microphone=()",
				"camera=()",
				"payment=(self)",
				"usb=()",
				"magnetometer=()",
				"gyroscope=()",
				"accelerometer=()",
				"fullscreen=(self)",
				"autoplay=(self)",
				"encrypted-media=(self)",
				"picture-in-picture=(self)",
				"publickey-credentials-get=(self)",
				"storage-access=(self)",
				"web-share=(self)",
				"interest-cohort=()",
				"browsing-topics=()",
				"attribution-reporting=()",
			}, ", ")
			w.Header().Set("Permissions-Policy", permissions)

			// Reporting API (modern + legacy) and Network Error Logging —
			// same endpoint referenced by the CSP report-to/report-uri
			// directives above
			w.Header().Set("Reporting-Endpoints", fmt.Sprintf(`default="%s"`, reportsURL))
			w.Header().Set("Report-To", fmt.Sprintf(`{"group":"default","max_age":10886400,"endpoints":[{"url":"%s"}]}`, reportsURL))
			w.Header().Set("NEL", `{"report_to":"default","max_age":2592000,"include_subdomains":true}`)

			// HSTS (only if SSL is enabled) — max-age=63072000 (2 years,
			// preload-list eligible) per RFC 6797
			if cfg.Server.SSL.Enabled {
				w.Header().Set("Strict-Transport-Security", "max-age=63072000; includeSubDomains; preload")
			}

			next.ServeHTTP(w, r)
		})
	}
}
