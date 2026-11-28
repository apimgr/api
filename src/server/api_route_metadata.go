package server

import (
	"net/http"
	"strings"

	"github.com/apimgr/api/src/common/i18n"
	"github.com/apimgr/api/src/swagger"
)

// apiRouteDefinition attaches the PART 14 text-response metadata to the same
// registered route records consumed by Swagger, llms.txt, and MCP discovery.
func apiRouteDefinition(method, path string) swagger.Route {
	return BuildAPIRouteMetadata(method, path)
}

// BuildAPIRouteMetadata returns the canonical route metadata used by API
// catalogs and generated discovery documents.
func BuildAPIRouteMetadata(method, path string) swagger.Route {
	mountedWhen := "always"
	switch {
	case path == "/healthz":
		mountedWhen = "server.healthz.root.enabled"
	case path == "/metrics" || strings.HasPrefix(path, "/metrics/"):
		mountedWhen = "server.metrics.root.enabled"
	case strings.HasPrefix(path, "/api/metrics") || strings.Contains(path, "/server/metrics"):
		mountedWhen = "server.metrics.enabled"
	}
	return swagger.Route{
		Method:      method,
		Path:        path,
		Primary:     apiPrimaryValue(path),
		TextMessage: apiRouteMessage(method, ""),
		MountedWhen: mountedWhen,
		Internal:    isInternalAPIRoute(path),
	}
}

func apiPrimaryValue(path string) string {
	path = strings.TrimPrefix(path, "/api/")
	if _, remainder, found := strings.Cut(path, "/"); found {
		path = remainder
	}
	// These API responses expose their user-facing value in the `text` field.
	// Other endpoints remain structured unless their registered route declares
	// a primary field.
	switch path {
	case "fun/joke", "fun/dad-joke", "fun/programming-joke", "fun/quote", "fun/fact", "fun/riddle", "fun/trivia", "fun/motivational", "fun/insult", "fun/compliment":
		return "text"
	default:
		return ""
	}
}

func apiRouteMessage(method, configured string, language ...string) string {
	if configured != "" {
		return configured
	}
	lang := i18n.DefaultLanguage
	if len(language) > 0 && language[0] != "" {
		lang = language[0]
	}
	switch strings.ToUpper(method) {
	case http.MethodGet:
		return i18n.Translate(lang, "api.text.retrieved")
	case http.MethodPut, http.MethodPatch:
		return i18n.Translate(lang, "api.text.updated")
	case http.MethodDelete:
		return i18n.Translate(lang, "api.text.deleted")
	default:
		return i18n.Translate(lang, "api.text.ok")
	}
}
