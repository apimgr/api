package server

import (
	"net/http"

	"github.com/apimgr/api/src/config"
)

// corsAllowedHeaders is the full set of headers this server accepts in a
// CORS preflight, per AI.md PART 16 → "CORS Headers" → Access-Control-
// Allow-Headers. Every supported auth header is listed by name, in sync
// with PART 8 → "Auth Token Headers (All Headers Supported)" — a wildcard
// is invalid here because Authorization is never covered by "*" and
// wildcards can't be combined with credentials.
const corsAllowedHeaders = "Content-Type, Accept, X-Requested-With, Authorization, X-API-Key, X-Api-Key, API-Key, ApiKey, X-Auth-Token, X-Access-Token, X-Token, Token, X-CSRF-Token, X-XSRF-Token, X-Session-ID, X-Service-Token, X-Internal-Token"

// corsAllowedMethods is the fixed method list from AI.md PART 16 → "CORS
// Headers" → Access-Control-Allow-Methods.
const corsAllowedMethods = "GET, POST, PUT, PATCH, DELETE, OPTIONS"

// corsMiddleware implements the CORS Allow-list Resolution Order from
// AI.md PART 16, replacing the previously static go-chi/cors handler. A
// hand-rolled implementation is required because the resolved allow-list
// is dynamic per-request (DOMAIN env + trusted-proxy-learned hosts), and
// the exact literal "*" vs. reflected-origin header semantics in the
// Behavior table aren't reproducible through go-chi/cors's dynamic
// AllowOriginFunc mode, which always reflects the request Origin rather
// than emitting a literal "*".
func corsMiddleware(cfg *config.Config) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origins, disabled := resolveCORSOrigins(cfg, r)
			if disabled {
				next.ServeHTTP(w, r)
				return
			}

			reqOrigin := r.Header.Get("Origin")
			isWildcard := len(origins) == 1 && origins[0] == "*"
			allowed := isWildcard || originInList(origins, reqOrigin)

			if reqOrigin != "" && allowed {
				if isWildcard {
					w.Header().Set("Access-Control-Allow-Origin", "*")
				} else {
					w.Header().Set("Access-Control-Allow-Origin", reqOrigin)
					w.Header().Add("Vary", "Origin")
					w.Header().Set("Access-Control-Allow-Credentials", "true")
				}
			}

			if r.Method == http.MethodOptions && reqOrigin != "" {
				if allowed {
					w.Header().Set("Access-Control-Allow-Methods", corsAllowedMethods)
					w.Header().Set("Access-Control-Allow-Headers", corsAllowedHeaders)
					w.Header().Set("Access-Control-Max-Age", "86400")
				}
				w.WriteHeader(http.StatusNoContent)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// originInList reports whether origin is present in origins.
func originInList(origins []string, origin string) bool {
	if origin == "" {
		return false
	}
	for _, o := range origins {
		if o == origin {
			return true
		}
	}
	return false
}
