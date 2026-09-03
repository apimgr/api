package metrics

import (
	"crypto/subtle"
	"net/http"
	"strings"
)

const bearerPrefix = "Bearer "

// AuthMiddleware wraps a metrics handler (Prometheus, Grafana, or Loki) with
// per-service bearer-token authentication, per AI.md PART 20 "Authentication".
//
// Rules, in order:
//  1. allowUnauthenticated bypasses all checks (a firewalled-only escape
//     hatch - server.metrics.auth.allow_unauthenticated).
//  2. An empty token means the service is disabled: every request gets a
//     403 with an empty body. Callers should log this once at startup, not
//     per request.
//  3. Otherwise, the Authorization header must be "Bearer <token>", compared
//     in constant time; anything else gets 401.
//
// Tokens are accepted only via the Authorization header - unlike the
// general API rules, metrics endpoints never accept a "?token=" query
// param, since query strings routinely leak into proxy/access logs.
func AuthMiddleware(token string, allowUnauthenticated bool, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if allowUnauthenticated {
			next.ServeHTTP(w, r)
			return
		}
		if token == "" {
			w.WriteHeader(http.StatusForbidden)
			return
		}

		// Every bearer check is an authentication attempt: the method label
		// is the fixed mechanism name and the status label is only
		// success/failure, never the specific reason the check failed.
		m := Get()

		auth := r.Header.Get("Authorization")
		if !strings.HasPrefix(auth, bearerPrefix) {
			m.RecordAuthAttempt("api_token", "failure")
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		supplied := auth[len(bearerPrefix):]
		if len(supplied) != len(token) || subtle.ConstantTimeCompare([]byte(supplied), []byte(token)) != 1 {
			m.RecordAuthAttempt("api_token", "failure")
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		m.RecordAuthAttempt("api_token", "success")

		next.ServeHTTP(w, r)
	})
}
