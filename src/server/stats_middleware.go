package server

import (
	"net/http"

	"github.com/apimgr/api/src/server/handler"
)

// healthStatsMiddleware feeds the public aggregate counters reported by
// /server/healthz (PART 13 stats: requests_total, requests_24h,
// active_connections). It records nothing per-client, so it is safe to run on
// every route including the overlay networks.
func healthStatsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		done := handler.RequestStarted()
		defer done()
		next.ServeHTTP(w, r)
	})
}
