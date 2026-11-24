package server

import (
	"net/http"

	"github.com/apimgr/api/src/config"
)

func serverBodyLimitMiddleware(cfg *config.Config) func(http.Handler) http.Handler {
	limit := cfg.Server.Limits.MaxBodySizeBytes()
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Body != nil {
				r.Body = http.MaxBytesReader(w, r.Body, limit)
			}
			next.ServeHTTP(w, r)
		})
	}
}
