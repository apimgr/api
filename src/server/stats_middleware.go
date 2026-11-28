package server

import (
	"net/http"
	"path"
	"strings"

	"github.com/apimgr/api/src/server/handler"
)

// healthStatsMiddleware feeds the public aggregate counters reported by
// /server/healthz (PART 13 stats: requests_total, requests_24h,
// active_connections). It records nothing per-client, so it is safe to run on
// every route including the overlay networks.
func healthStatsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		done := handler.RequestStartedCounted(!isHealthStatsExcludedPath(r.URL.Path))
		defer done()
		next.ServeHTTP(w, r)
	})
}

func isHealthStatsExcludedPath(requestPath string) bool {
	cleanPath := path.Clean(requestPath)
	cleanPath = strings.TrimSuffix(cleanPath, "/")
	if strings.HasSuffix(cleanPath, "/healthz") {
		return true
	}
	return cleanPath == "/metrics" || strings.HasPrefix(cleanPath, "/metrics/") ||
		cleanPath == "/server/metrics" || strings.HasPrefix(cleanPath, "/server/metrics/") ||
		strings.HasSuffix(cleanPath, "/server/metrics") || strings.Contains(cleanPath, "/server/metrics/") ||
		cleanPath == "/api/metrics" || strings.HasPrefix(cleanPath, "/api/metrics/")
}
