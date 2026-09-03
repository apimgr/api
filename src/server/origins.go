package server

import (
	"net/http"
	"os"
	"strings"
	"sync"

	"github.com/apimgr/api/src/config"
)

// originSet is a thread-safe, order-preserving, deduplicated collection of
// reverse-proxy-learned origins (step 3 of the CORS Allow-list Resolution
// Order). Ordering is kept stable so CORS/CSP output and tests are
// deterministic across requests.
type originSet struct {
	mu    sync.RWMutex
	order []string
	seen  map[string]bool
}

// learnedOriginStore accumulates X-Forwarded-Host values seen from trusted
// reverse-proxy peers over the life of the process.
var learnedOriginStore = &originSet{seen: map[string]bool{}}

// add records origin if it hasn't been seen before.
func (s *originSet) add(origin string) {
	if origin == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.seen[origin] {
		return
	}
	if s.seen == nil {
		s.seen = map[string]bool{}
	}
	s.seen[origin] = true
	s.order = append(s.order, origin)
}

// snapshot returns a copy of the currently recorded origins, in the order
// they were first learned.
func (s *originSet) snapshot() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]string, len(s.order))
	copy(out, s.order)
	return out
}

// resetLearnedOrigins clears the learned-origin store. Test-only helper so
// each test case starts from a clean slate.
func resetLearnedOrigins() {
	learnedOriginStore.mu.Lock()
	defer learnedOriginStore.mu.Unlock()
	learnedOriginStore.order = nil
	learnedOriginStore.seen = map[string]bool{}
}

// firstForwardedValue returns the first entry of a comma-separated proxy
// header value (X-Forwarded-Host/X-Forwarded-Proto may carry a chain when
// requests pass through multiple hops), trimmed of surrounding whitespace.
func firstForwardedValue(header string) string {
	if header == "" {
		return ""
	}
	parts := strings.Split(header, ",")
	return strings.TrimSpace(parts[0])
}

// domainEnvOrigins parses the comma-separated DOMAIN environment variable
// into "https://" origins, per AI.md PART 16 CORS Allow-list Resolution
// Order step 2 ("every DOMAIN env hostname as an https:// origin").
func domainEnvOrigins() []string {
	raw := os.Getenv("DOMAIN")
	if raw == "" {
		return nil
	}
	var origins []string
	for _, part := range strings.Split(raw, ",") {
		host := strings.TrimSpace(part)
		if host == "" {
			continue
		}
		origins = append(origins, "https://"+host)
	}
	return origins
}

// originalPeerAddr returns the pre-rewrite TCP peer address recorded by
// realIPMiddleware, falling back to r.RemoteAddr when unset (e.g. in tests
// that don't run the full middleware chain).
func originalPeerAddr(r *http.Request) string {
	if v, ok := r.Context().Value(originalPeerContextKey).(string); ok && v != "" {
		return v
	}
	return r.RemoteAddr
}

// learnedOrigins implements steps 2 and 3 of the CORS Allow-list Resolution
// Order (AI.md PART 16): every DOMAIN env hostname, plus any host learned
// from a trusted reverse-proxy's X-Forwarded-Host header. This is also the
// exact `{learned_origins}` set that AI.md PART 11's CSP auto-detection
// injects into connect-src/frame-ancestors/form-action — it deliberately
// excludes the operator's explicit server.cors.allowed_origins list (step 1)
// and the "*" fallback (step 4), since those aren't "learned", they're
// configured or defaulted.
func learnedOrigins(cfg *config.Config, r *http.Request) []string {
	result := domainEnvOrigins()

	if r != nil {
		peer := originalPeerAddr(r)
		if isTrustedPeer(cfg, peer) {
			if fwHost := firstForwardedValue(r.Header.Get("X-Forwarded-Host")); fwHost != "" {
				scheme := "https"
				if proto := firstForwardedValue(r.Header.Get("X-Forwarded-Proto")); proto != "" {
					scheme = proto
				}
				learnedOriginStore.add(scheme + "://" + fwHost)
			}
		}
	}

	result = append(result, learnedOriginStore.snapshot()...)

	seen := map[string]bool{}
	deduped := make([]string, 0, len(result))
	for _, o := range result {
		if o == "" || seen[o] {
			continue
		}
		seen[o] = true
		deduped = append(deduped, o)
	}
	return deduped
}

// resolveCORSOrigins implements the full CORS Allow-list Resolution Order
// from AI.md PART 16:
//  1. explicit server.cors.allowed_origins ("" as the sole entry disables
//     CORS entirely and stops resolution)
//  2. every DOMAIN env hostname as an https:// origin
//  3. reverse-proxy-learned hosts via X-Forwarded-Host, trusted peers only
//  4. default "*" if nothing else resolved
//
// A "*" anywhere in the explicit config list short-circuits to pure
// wildcard mode, since mixing a literal "*" with specific origins in the
// same Access-Control-Allow-Origin response is not meaningful and would
// make "credentials never sent with *" ambiguous to enforce.
func resolveCORSOrigins(cfg *config.Config, r *http.Request) (origins []string, disabled bool) {
	explicit := cfg.Web.CORS.AllowedOrigins

	if len(explicit) == 1 && explicit[0] == "" {
		return nil, true
	}

	for _, o := range explicit {
		if o == "*" {
			return []string{"*"}, false
		}
	}

	seen := map[string]bool{}
	result := make([]string, 0, len(explicit))
	add := func(o string) {
		if o == "" || seen[o] {
			return
		}
		seen[o] = true
		result = append(result, o)
	}

	for _, o := range explicit {
		add(o)
	}
	for _, o := range learnedOrigins(cfg, r) {
		add(o)
	}

	if len(result) == 0 {
		return []string{"*"}, false
	}
	return result, false
}
