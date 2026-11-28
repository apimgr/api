package server

import (
	"context"
	"net/http"
	"net/netip"

	"github.com/apimgr/api/src/config"
)

type allowlistedContextKey struct{}

func allowlistMiddleware(cfg *config.ServerConfig) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			current := config.Get()
			if current == nil {
				current = cfg
			}
			addr, err := netip.ParseAddr(getClientIP(r))
			if current != nil && err == nil && isAllowlistedIP(addr, current.Server.Security.Allowlist) {
				r = r.WithContext(context.WithValue(r.Context(), allowlistedContextKey{}, true))
			}
			next.ServeHTTP(w, r)
		})
	}
}

func isAllowlistedIP(addr netip.Addr, entries []config.AllowlistEntry) bool {
	for _, entry := range entries {
		if prefix, err := netip.ParsePrefix(entry.CIDR); err == nil && prefix.Contains(addr) {
			return true
		}
		if allowed, err := netip.ParseAddr(entry.CIDR); err == nil && allowed == addr {
			return true
		}
	}
	return false
}

func isAllowlistedRequest(r *http.Request) bool {
	allowed, _ := r.Context().Value(allowlistedContextKey{}).(bool)
	return allowed
}
