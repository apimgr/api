package server

import (
	"regexp"
	"strings"

	"github.com/go-chi/chi/v5"
)

var routeMatchParameter = regexp.MustCompile(`\{[^{}]+\}`)

// RoutePatternMatches reports whether the router matches a representative path
// for a route pattern. Chi's route walker can expose trie prefixes that are not
// themselves registered endpoints, so generators and contract checks verify
// each pattern against the router before treating it as a route.
func RoutePatternMatches(router chi.Router, method, pattern string) bool {
	probePath := strings.ReplaceAll(pattern, "/*", "/route-contract-probe")
	probePath = routeMatchParameter.ReplaceAllString(probePath, "route-contract-probe")
	ctx := chi.NewRouteContext()
	if !router.Match(ctx, method, probePath) {
		return false
	}
	if canonicalRoutePattern(ctx.RoutePattern()) != canonicalRoutePattern(pattern) {
		return false
	}
	// chi treats an omitted final parameter as an empty value and can report
	// that parent path as a route. A real parent route has no such URL param.
	for _, value := range ctx.URLParams.Values {
		if value == "" {
			return false
		}
	}
	return true
}

func canonicalRoutePattern(pattern string) string {
	pattern = strings.ReplaceAll(pattern, "/*", "/{path}")
	pattern = routeMatchParameter.ReplaceAllString(pattern, "{param}")
	if len(pattern) > 1 {
		pattern = strings.TrimSuffix(pattern, "/")
	}
	return pattern
}
