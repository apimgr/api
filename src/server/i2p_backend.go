package server

import (
	"context"
	"net/http"
	"strings"
)

type i2pBackendContextKey struct{}

// I2PBackend marks requests accepted by the dedicated SAM-forwarded loopback
// listener and removes headers that must never be treated as peer identity.
func I2PBackend(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for name := range r.Header {
			lower := strings.ToLower(name)
			if strings.HasPrefix(lower, "x-i2p-") || strings.HasPrefix(lower, "x-forwarded-") || lower == "forwarded" || lower == "x-real-ip" || lower == "x-real-host" || lower == "x-original-host" || lower == "x-url-scheme" {
				r.Header.Del(name)
			}
		}
		ctx := context.WithValue(r.Context(), i2pBackendContextKey{}, true)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func isI2PBackendRequest(r *http.Request) bool {
	if r == nil {
		return false
	}
	trusted, _ := r.Context().Value(i2pBackendContextKey{}).(bool)
	return trusted
}
