package server

import (
	"net/http"
	"strings"

	"github.com/apimgr/api/src/swagger"
)

const (
	publicAPICacheControl  = "public, max-age=60"
	privateAPICacheControl = "private, no-store"
	apiErrorCacheControl   = "no-store"
)

// apiCachePolicyMiddleware applies PART 9's cache defaults to routes in the
// server's route metadata. Handler-level no-store policies remain in force for
// sensitive public operations such as contact forms and guest preferences.
func apiCachePolicyMiddleware(routes func() []swagger.Route) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			route, found := apiCacheRouteForRequest(routes(), r.Method, r.URL.Path)
			if !found {
				next.ServeHTTP(w, r)
				return
			}

			private := route.Internal || r.Method != http.MethodGet && r.Method != http.MethodHead ||
				strings.TrimSpace(r.Header.Get("Authorization")) != ""
			defaultPublic := false
			if private {
				w.Header().Set("Cache-Control", privateAPICacheControl)
			} else if w.Header().Get("Cache-Control") == "" {
				w.Header().Set("Cache-Control", publicAPICacheControl)
				defaultPublic = true
			}

			capture := &apiCachePolicyWriter{ResponseWriter: w}
			next.ServeHTTP(capture, r)
			if defaultPublic && strings.HasPrefix(strings.ToLower(capture.Header().Get("Content-Type")), "text/html") &&
				capture.Header().Get("Cache-Control") == publicAPICacheControl {
				capture.Header().Del("Cache-Control")
			}
		})
	}
}

type apiCachePolicyWriter struct {
	http.ResponseWriter
	status int
}

func (w *apiCachePolicyWriter) WriteHeader(status int) {
	if w.status != 0 {
		return
	}
	w.status = status
	if status >= http.StatusMultipleChoices {
		w.Header().Set("Cache-Control", apiErrorCacheControl)
	}
	w.ResponseWriter.WriteHeader(status)
}

func (w *apiCachePolicyWriter) Write(body []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.ResponseWriter.Write(body)
}

func (w *apiCachePolicyWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}

func (w *apiCachePolicyWriter) Flush() {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	if flusher, ok := w.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

func apiCacheRouteForRequest(routes []swagger.Route, method, path string) (swagger.Route, bool) {
	for _, route := range routes {
		methodMatches := route.Method == "*" || strings.EqualFold(route.Method, method) ||
			method == http.MethodHead && strings.EqualFold(route.Method, http.MethodGet)
		if !methodMatches {
			continue
		}
		if apiRoutePatternMatches(route.Path, path) {
			return route, true
		}
	}
	return swagger.Route{}, false
}

func apiRoutePatternMatches(pattern, path string) bool {
	pattern = strings.Trim(pattern, "/")
	path = strings.Trim(path, "/")
	if pattern == "" || path == "" {
		return pattern == path
	}
	patternParts := strings.Split(pattern, "/")
	pathParts := strings.Split(path, "/")
	for index, patternPart := range patternParts {
		if patternPart == "*" || strings.HasPrefix(patternPart, "*") {
			return index < len(pathParts)
		}
		if index >= len(pathParts) {
			return false
		}
		if strings.HasPrefix(patternPart, "{") && strings.HasSuffix(patternPart, "}") {
			continue
		}
		if patternPart != pathParts[index] {
			return false
		}
	}
	return len(patternParts) == len(pathParts)
}
