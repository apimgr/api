package server

import (
	"context"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/apimgr/api/src/config"
)

// originalPeerContextKey stores the original TCP peer address (before any
// X-Forwarded-For/X-Real-IP rewrite) so trust-gated consumers can always
// evaluate the real connecting peer, never a header-supplied value
const originalPeerContextKey contextKey = "originalPeer"

// alwaysTrustedProxyCIDRs are trusted without any configuration -
// loopback, RFC 1918 IPv4 private ranges, RFC 4193 IPv6 unique-local, and
// link-local ranges
var alwaysTrustedProxyCIDRs = []string{
	"127.0.0.0/8",
	"::1/128",
	"10.0.0.0/8",
	"172.16.0.0/12",
	"192.168.0.0/16",
	"fc00::/7",
	"169.254.0.0/16",
	"fe80::/10",
}

// trustedProxyResolver resolves and caches the trusted_proxies.additional
// allow-list, re-resolving DNS names every 5 minutes
type trustedProxyResolver struct {
	mu          sync.RWMutex
	cidrs       []*net.IPNet
	ips         map[string]bool
	lastResolve time.Time
	entries     []string
}

var proxyResolver = &trustedProxyResolver{}

// resolve refreshes the trusted-proxy allow-list from the configured
// entries, re-resolving DNS names at most once every 5 minutes
func (t *trustedProxyResolver) resolve(entries []string) {
	t.mu.Lock()
	sameEntries := len(entries) == len(t.entries)
	if sameEntries {
		for i, e := range entries {
			if e != t.entries[i] {
				sameEntries = false
				break
			}
		}
	}
	if sameEntries && time.Since(t.lastResolve) < 5*time.Minute {
		t.mu.Unlock()
		return
	}
	t.mu.Unlock()

	cidrs := make([]*net.IPNet, 0, len(entries))
	ips := make(map[string]bool, len(entries))

	for _, entry := range entries {
		if strings.Contains(entry, "/") {
			if _, ipnet, err := net.ParseCIDR(entry); err == nil {
				cidrs = append(cidrs, ipnet)
			}
			continue
		}

		if ip := net.ParseIP(entry); ip != nil {
			ips[ip.String()] = true
			continue
		}

		// Treat as a DNS name - resolve to IPs
		resolved, err := net.LookupIP(entry)
		if err != nil {
			continue
		}
		for _, ip := range resolved {
			ips[ip.String()] = true
		}
	}

	t.mu.Lock()
	t.cidrs = cidrs
	t.ips = ips
	t.entries = entries
	t.lastResolve = time.Now()
	t.mu.Unlock()
}

// trusts reports whether ip matches the resolved allow-list
func (t *trustedProxyResolver) trusts(ip net.IP) bool {
	t.mu.RLock()
	defer t.mu.RUnlock()

	if t.ips[ip.String()] {
		return true
	}
	for _, ipnet := range t.cidrs {
		if ipnet.Contains(ip) {
			return true
		}
	}
	return false
}

// isTrustedPeer reports whether the given peer address (IP, with or
// without a port) is trusted to set X-Forwarded-*/X-Real-IP and related
// proxy headers - the always-trusted private ranges plus the configured
// server.trusted_proxies.additional allow-list
func isTrustedPeer(cfg *config.Config, addr string) bool {
	host := addr
	if h, _, err := net.SplitHostPort(addr); err == nil {
		host = h
	}

	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}

	for _, cidr := range alwaysTrustedProxyCIDRs {
		_, ipnet, err := net.ParseCIDR(cidr)
		if err != nil {
			continue
		}
		if ipnet.Contains(ip) {
			return true
		}
	}

	proxyResolver.resolve(cfg.Server.TrustedProxies.Additional)
	return proxyResolver.trusts(ip)
}

// realIPMiddleware resolves the client IP from proxy headers, but only
// when the immediate TCP peer is a trusted proxy. The original peer
// address is preserved in the request context (under originalPeerContextKey)
// so trust-gated consumers elsewhere always evaluate the true connecting
// peer, never a header-supplied value. Untrusted peers fall through
// unmodified - RemoteAddr stays the direct connection.
func realIPMiddleware(cfg *config.Config) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			originalPeer := r.RemoteAddr
			ctx := context.WithValue(r.Context(), originalPeerContextKey, originalPeer)
			r = r.WithContext(ctx)

			if isTrustedPeer(cfg, originalPeer) {
				if clientIP := resolveForwardedClientIP(r); clientIP != "" {
					r.RemoteAddr = clientIP
				}
			}

			next.ServeHTTP(w, r)
		})
	}
}

// resolveForwardedClientIP extracts the client IP from proxy headers,
// checked in priority order. Only called after the immediate peer has
// already been verified as trusted.
func resolveForwardedClientIP(r *http.Request) string {
	headers := []string{"X-Real-IP", "X-Forwarded-For", "CF-Connecting-IP", "True-Client-IP", "X-Client-IP"}

	for _, header := range headers {
		value := r.Header.Get(header)
		if value == "" {
			continue
		}

		// X-Forwarded-For may contain a comma-separated chain -
		// the first entry is the original client
		if header == "X-Forwarded-For" {
			parts := strings.Split(value, ",")
			value = strings.TrimSpace(parts[0])
		}

		if net.ParseIP(value) != nil {
			return value
		}
	}

	return ""
}

// originalPeerAddr returns the original TCP peer address stored in the
// request context by realIPMiddleware, falling back to RemoteAddr when
// the middleware has not run (e.g. in tests)
func originalPeerAddr(r *http.Request) string {
	if v, ok := r.Context().Value(originalPeerContextKey).(string); ok && v != "" {
		return v
	}
	return r.RemoteAddr
}
