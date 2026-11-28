package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/apimgr/api/src/common/i18n"
	"github.com/apimgr/api/src/config"
	"github.com/apimgr/api/src/swagger"
	"github.com/go-chi/chi/v5"
)

// llmsHandler serves the route-derived AI discovery document. Links are
// resolved for the current request so proxy prefixes and overlay hosts stay
// consistent with the URLs rendered elsewhere by the server.
func llmsHandler(cfg *config.ServerConfig, routes chi.Routes) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		r = requestWithServerConfig(r, cfg)
		var endpoints []string
		seen := make(map[string]struct{})
		apiVersion := strings.TrimPrefix(APIBasePath(cfg), "/api/")
		openAPISpec := swagger.GenerateSpecForAPIVersion(Version, "", apiVersion)
		err := chi.Walk(routes, func(method, pattern string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
			if !strings.HasPrefix(pattern, "/api/") || strings.Contains(pattern, "/metrics") || strings.Contains(pattern, "/admin/") {
				return nil
			}
			definition := apiRouteDefinition(method, pattern)
			line := fmt.Sprintf("- %s %s", method, pattern)
			line += "\n  Text response: default message=" + definition.TextMessage
			if definition.Primary != "" {
				line += "; primary=" + definition.Primary
			}
			if cfg.Web.LLMS.IncludeSchemas {
				if item, ok := openAPISpec.Paths[pattern]; ok {
					if operation := openAPIOperation(item, method); operation != nil {
						operation.Primary = definition.Primary
						operation.TextMessage = definition.TextMessage
						if encoded, err := json.Marshal(operation); err == nil {
							line += "\n  Schema: " + string(encoded)
						}
					}
				}
			}
			if _, ok := seen[line]; ok {
				return nil
			}
			seen[line] = struct{}{}
			endpoints = append(endpoints, line)
			return nil
		})
		if err != nil {
			http.Error(w, i18n.Translate(i18n.LangFromRequest(r), "errors.server_error"), http.StatusInternalServerError)
			return
		}
		sort.Strings(endpoints)
		name := llmsSingleLine(cfg.Server.Branding.Title)
		if name == "" {
			name = "api"
		}
		description := llmsSingleLine(cfg.Server.Branding.Description)
		apiBase := BuildURL(r, APIBasePath(cfg))
		lines := []string{
			"# " + name,
			"> " + description,
			"",
			"## API",
			"Base URL: " + apiBase,
			"Authentication: The REST API has public endpoints and does not require credentials. An owner Bearer token links resources to its owner; MCP requires a valid Bearer token.",
			fmt.Sprintf("Rate limit: reads %d requests per %d seconds; writes %d requests per %d seconds", cfg.Server.RateLimit.Read.Requests, cfg.Server.RateLimit.Read.Window, cfg.Server.RateLimit.Write.Requests, cfg.Server.RateLimit.Write.Window),
			"MCP: " + BuildURL(r, "/mcp") + " (Streamable HTTP; Bearer token required)",
		}
		if cfg.Web.LLMS.IncludeEndpoints {
			lines = append(lines, "", "## Endpoints", "- GET "+BuildURL(r, "/server/healthz")+" - Health check", "- GET "+BuildURL(r, "/server/about")+" - Server information")
			lines = append(lines, "- POST "+BuildURL(r, APIBasePath(cfg)+"/server/mcp")+" - MCP Streamable HTTP endpoint (Bearer token required)")
			lines = append(lines, endpoints...)
		}
		lines = append(lines, "", "## Capabilities")
		categories := make(map[string]struct{})
		for _, page := range toolPages() {
			categories[page.category] = struct{}{}
		}
		categoryNames := make([]string, 0, len(categories))
		for category := range categories {
			categoryNames = append(categoryNames, category)
		}
		sort.Strings(categoryNames)
		for _, category := range categoryNames {
			lines = append(lines, "- "+category+" tools via /"+category)
		}
		lines = append(lines, "", "## Contact", "API issues: "+llmsSingleLine(cfg.Server.Contact.ResolveGeneral().Email))
		if contact := llmsSingleLine(cfg.Server.Contact.ResolveSecurity().Email); contact != "" {
			lines = append(lines, "Security: "+contact)
		}
		body := strings.Join(lines, "\n") + "\n"
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("Cache-Control", "public, max-age=300")
		w.Header().Set("Vary", "Host, X-Forwarded-Host, X-Forwarded-Proto, X-Forwarded-Port, X-Forwarded-Prefix")
		w.WriteHeader(http.StatusOK)
		if r.Method != http.MethodHead {
			_, _ = w.Write([]byte(body))
		}
	}
}

func openAPIOperation(item swagger.PathItem, method string) *swagger.Operation {
	switch strings.ToUpper(method) {
	case http.MethodGet:
		return item.Get
	case http.MethodPost:
		return item.Post
	case http.MethodPut:
		return item.Put
	case http.MethodPatch:
		return item.Patch
	case http.MethodDelete:
		return item.Delete
	default:
		return nil
	}
}

func llmsSingleLine(value string) string {
	return strings.Join(strings.Fields(value), " ")
}
