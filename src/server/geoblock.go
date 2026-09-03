package server

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/apimgr/api/src/config"
	"github.com/apimgr/api/src/database"
	"github.com/apimgr/api/src/geoip"
)

// geoIPMiddleware enforces the server.geoip country rules (AI.md PART 19).
// It runs after rate limiting so a blocked-country request still consumes its
// rate limit budget, and before authentication so country blocking never
// substitutes for a real auth check. Every uncertainty fails open: the
// decision helper returns Blocked=false for disabled GeoIP, unparseable or
// private addresses, allowlisted addresses, an unconfigured rule set, a
// missing country database, and an unknown country.
func geoIPMiddleware(cfg *config.Config) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !cfg.Server.GeoIP.Enabled {
				next.ServeHTTP(w, r)
				return
			}

			clientIP := getClientIP(r)
			decision := geoip.Get().CheckCountry(clientIP, cfg.Server.GeoIP, cfg.Server.Security.Allowlist)
			if !decision.Blocked {
				next.ServeHTTP(w, r)
				return
			}

			logCountryBlocked(r, clientIP, decision)
			writeCountryBlocked(w)
		})
	}
}

// logCountryBlocked records the block in the structured log and in the audit
// log as the PART 11 `security.country_blocked` event.
func logCountryBlocked(r *http.Request, clientIP string, decision geoip.CountryDecision) {
	slog.Warn("geoip: request blocked by country rules",
		"ip", clientIP,
		"country", decision.CountryCode,
		"reason", decision.Reason,
		"path", r.URL.Path)

	if err := database.WriteAuditEvent(r.Context(), "security.country_blocked", "", clientIP, map[string]any{
		"country": decision.CountryCode,
		"reason":  decision.Reason,
		"path":    r.URL.Path,
	}); err != nil && err != context.Canceled {
		slog.Warn("geoip: failed to record the country block audit event", "error", err)
	}
}

// writeCountryBlocked writes the canonical 403 body. The response never
// reveals which rule matched or which country list is configured.
func writeCountryBlocked(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusForbidden)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"ok":      false,
		"error":   "FORBIDDEN",
		"message": "Access denied",
	})
}
