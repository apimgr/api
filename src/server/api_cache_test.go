package server

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/apimgr/api/src/swagger"
	"github.com/stretchr/testify/require"
)

func TestAPICachePolicyUsesRouteMetadata(t *testing.T) {
	routes := []swagger.Route{
		{Method: http.MethodGet, Path: "/api/v1/items/{id}"},
		{Method: http.MethodPost, Path: "/api/v1/items/{id}"},
		{Method: http.MethodGet, Path: "/api/v1/internal", Internal: true},
		{Method: http.MethodGet, Path: "/healthz"},
		{Method: "*", Path: "/mcp"},
	}
	respond := func(status int, contentType, cacheControl string) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			if contentType != "" {
				w.Header().Set("Content-Type", contentType)
			}
			if cacheControl != "" {
				w.Header().Set("Cache-Control", cacheControl)
			}
			w.WriteHeader(status)
			_, _ = w.Write([]byte(`{"ok":true}`))
		})
	}
	run := func(method, target, authorization string, status int, contentType, cacheControl string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, target, nil)
		if authorization != "" {
			req.Header.Set("Authorization", authorization)
		}
		recorder := httptest.NewRecorder()
		apiCachePolicyMiddleware(func() []swagger.Route { return routes })(respond(status, contentType, cacheControl)).ServeHTTP(recorder, req)
		return recorder
	}

	public := run(http.MethodGet, "/api/v1/items/123", "", http.StatusOK, "application/json", "")
	require.Equal(t, publicAPICacheControl, public.Header().Get("Cache-Control"))

	private := run(http.MethodGet, "/api/v1/items/123", "Bearer token", http.StatusOK, "application/json", "")
	require.Equal(t, privateAPICacheControl, private.Header().Get("Cache-Control"))

	internal := run(http.MethodGet, "/api/v1/internal", "", http.StatusOK, "text/plain", "")
	require.Equal(t, privateAPICacheControl, internal.Header().Get("Cache-Control"))

	write := run(http.MethodPost, "/api/v1/items/123", "", http.StatusOK, "application/json", "")
	require.Equal(t, privateAPICacheControl, write.Header().Get("Cache-Control"))

	errorResponse := run(http.MethodGet, "/api/v1/items/123", "", http.StatusInternalServerError, "text/plain; charset=utf-8", "")
	require.Equal(t, apiErrorCacheControl, errorResponse.Header().Get("Cache-Control"))

	explicitPrivate := run(http.MethodGet, "/api/v1/items/123", "", http.StatusOK, "application/json", "no-store")
	require.Equal(t, "no-store", explicitPrivate.Header().Get("Cache-Control"))

	rootAlias := run(http.MethodPost, "/mcp", "Bearer token", http.StatusOK, "application/json", "")
	require.Equal(t, privateAPICacheControl, rootAlias.Header().Get("Cache-Control"))

	rootHealthHTML := run(http.MethodGet, "/healthz", "", http.StatusOK, "text/html; charset=utf-8", "")
	require.Empty(t, rootHealthHTML.Header().Get("Cache-Control"))
}
