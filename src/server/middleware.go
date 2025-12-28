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

// securityHeadersMiddleware adds security headers to all responses
func securityHeadersMiddleware(cfg *config.Config) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Content Security Policy
			csp := strings.Join([]string{
				"default-src 'self'",
				"script-src 'self' 'unsafe-inline' 'unsafe-eval'", // TODO: Remove unsafe-inline/eval in production
				"style-src 'self' 'unsafe-inline'",                 // TODO: Remove unsafe-inline with nonces
				"img-src 'self' data: https:",
				"font-src 'self' data:",
				"connect-src 'self'",
				"frame-ancestors 'none'",
				"base-uri 'self'",
				"form-action 'self'",
			}, "; ")
			w.Header().Set("Content-Security-Policy", csp)

			// Prevent clickjacking
			w.Header().Set("X-Frame-Options", "DENY")

			// Prevent MIME sniffing
			w.Header().Set("X-Content-Type-Options", "nosniff")

			// XSS Protection (legacy, but doesn't hurt)
			w.Header().Set("X-XSS-Protection", "1; mode=block")

			// Referrer Policy
			w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")

			// Permissions Policy
			permissions := strings.Join([]string{
				"geolocation=()",
				"microphone=()",
				"camera=()",
				"payment=()",
				"usb=()",
				"magnetometer=()",
				"gyroscope=()",
				"accelerometer=()",
			}, ", ")
			w.Header().Set("Permissions-Policy", permissions)

			// HSTS (only if SSL is enabled)
			if cfg.Server.SSL.Enabled {
				// max-age=31536000 (1 year), includeSubDomains
				w.Header().Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains; preload")
			}

			next.ServeHTTP(w, r)
		})
	}
}

// maintenanceModeMiddleware checks if maintenance mode is enabled
func maintenanceModeMiddleware(dataDir string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Allow health checks even in maintenance mode
			if r.URL.Path == "/healthz" || r.URL.Path == "/api/v1/healthz" {
				next.ServeHTTP(w, r)
				return
			}

			// Check for maintenance mode file
			maintenanceFile := fmt.Sprintf("%s/maintenance", dataDir)
			if fileExists(maintenanceFile) {
				w.WriteHeader(http.StatusServiceUnavailable)
				w.Header().Set("Content-Type", "application/json")
				fmt.Fprintf(w, `{"error":"Service is in maintenance mode","status":503}`)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// fileExists checks if a file exists
func fileExists(path string) bool {
	_, err := http.Dir(".").Open(path)
	return err == nil
}
