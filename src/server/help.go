package server

import (
	"html/template"
	"net/http"
	"strings"

	"github.com/apimgr/api/src/common/i18n"
	"github.com/apimgr/api/src/common/sanitize"
	"github.com/apimgr/api/src/config"
	"github.com/apimgr/api/src/service/text"
	"github.com/apimgr/api/src/swagger"
)

type helpPageInfo struct {
	Path        string `json:"path"`
	Title       string `json:"title"`
	Description string `json:"-"`
}

type mcpHelpTool struct {
	Name        string
	Description template.HTML
}

type mcpHelpPageData struct {
	Endpoint       string
	APIAlias       string
	RootAlias      string
	Tools          []mcpHelpTool
	ProjectContent template.HTML
	ProjectText    string
}

type mcpHelpResponse struct {
	Enabled bool             `json:"enabled"`
	URL     string           `json:"url"`
	Aliases []string         `json:"aliases"`
	Auth    mcpHelpAuth      `json:"auth"`
	Tools   []mcpHelpToolAPI `json:"tools"`
	Project string           `json:"project"`
}

func (response mcpHelpResponse) TaggedResponseFields() sanitize.TaggedObject {
	return sanitize.TaggedObject{
		{Name: "enabled", Value: response.Enabled},
		{Name: "url", Value: response.URL},
		{Name: "aliases", Value: response.Aliases},
		{Name: "auth", Value: sanitize.PublicResponseValue{Value: response.Auth}},
		{Name: "tools", Value: response.Tools},
		{Name: "project", Value: response.Project},
	}
}

type mcpHelpAuth struct {
	Scheme string `json:"scheme"`
}

type mcpHelpToolAPI struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

func helpPageRegistry(language string) []helpPageInfo {
	return registeredServerPages(language).Help
}

func helpMCPPageHandler(cfg *config.ServerConfig, routes func() []swagger.Route) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		data := newPageData(cfg, r, "help-mcp")
		data.PageTitle = i18n.Translate(data.Lang, "help.mcp.title")
		data.PageDescription = i18n.Translate(data.Lang, "help.mcp.description")
		data.MCPHelp = buildMCPHelpPageData(cfg, r, routes())
		renderPage(w, "help_mcp", data)
	}
}

func helpAPIHandler(cfg *config.ServerConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		language := i18n.LangFromRequest(r)
		data := responseObject(
			responseField("title", i18n.Translate(language, "help.title")),
			responseField("content", helpMarkdown(cfg)),
			responseField("format", "markdown"),
			responseField("pages", helpPageRegistry(language)),
		)
		writeEnvelopeOK(w, http.StatusOK, data)
	}
}

func mcpHelpAPIHandler(cfg *config.ServerConfig, routes func() []swagger.Route) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		page := buildMCPHelpPageData(cfg, r, routes())
		response := mcpHelpResponse{
			Enabled: true,
			URL:     page.Endpoint,
			Aliases: []string{page.APIAlias, page.RootAlias},
			Auth:    mcpHelpAuth{Scheme: "Bearer"},
			Project: page.ProjectText,
		}
		for _, tool := range page.Tools {
			response.Tools = append(response.Tools, mcpHelpToolAPI{Name: tool.Name, Description: string(tool.Description)})
		}
		writeEnvelopeOK(w, http.StatusOK, response)
	}
}

func buildMCPHelpPageData(cfg *config.ServerConfig, r *http.Request, routes []swagger.Route) *mcpHelpPageData {
	request := requestWithServerConfig(r, cfg)
	tools := sortedMCPTools(mcpTools(cfg, routes))
	project := mcpProjectHelp(cfg, tools)
	page := &mcpHelpPageData{
		Endpoint:       BuildURL(request, APIBasePath(cfg)+"/server/mcp"),
		APIAlias:       BuildURL(request, "/api/mcp"),
		RootAlias:      BuildURL(request, "/mcp"),
		ProjectContent: template.HTML(text.MarkdownToHTML(project)),
		ProjectText:    project,
	}
	for _, tool := range tools {
		page.Tools = append(page.Tools, mcpHelpTool{Name: tool.Name, Description: template.HTML(text.MarkdownToHTML(tool.Description))})
	}
	return page
}

func helpMarkdown(cfg *config.ServerConfig) string {
	if content := nestedPageText(cfg, "content"); content != "" {
		return content
	}
	return "Use the category pages to choose a tool, or call the REST API documented in `/server/docs/swagger`. This service also provides an authenticated MCP endpoint for compatible assistants."
}

func mcpProjectHelp(cfg *config.ServerConfig, tools []mcpTool) string {
	if content := nestedPageText(cfg, "mcp"); content != "" {
		return content
	}
	var content strings.Builder
	content.WriteString("This server provides stateless utility operations. The available tools below are generated from this release's API operations; `tools/list` reflects the current token's permissions. Example requests: use `generateUUID` to generate UUIDs, `hashText` to hash supplied text, or `getCurrentTime` to look up a time by timezone. Tool inputs are processed transiently and are not retained. Network-backed lookups may contact their documented public data source. This server exposes no additional MCP resources or prompts.\n\n### Available operations\n\n")
	for _, tool := range tools {
		content.WriteString("- `")
		content.WriteString(tool.Name)
		content.WriteString("` — ")
		content.WriteString(strings.TrimSpace(tool.Description))
		content.WriteByte('\n')
	}
	return content.String()
}

func nestedPageText(cfg *config.ServerConfig, key string) string {
	if cfg == nil || cfg.Server.Pages == nil {
		return ""
	}
	help, ok := cfg.Server.Pages["help"].(map[string]interface{})
	if !ok {
		return ""
	}
	value, _ := help[key].(string)
	return strings.TrimSpace(value)
}
