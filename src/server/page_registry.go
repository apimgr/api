package server

import (
	"net/http"

	"github.com/apimgr/api/src/common/i18n"
	"github.com/apimgr/api/src/config"
	"github.com/apimgr/api/src/paths"
	"github.com/apimgr/api/src/swagger"
)

const (
	helpHubPath        = "/server/help"
	preferencesHubPath = "/server/preferences"
)

type serverPageRegistry struct {
	HelpHubPath        string
	PreferencesHubPath string
	OwnerTokensPath    string
	Help               []helpPageInfo
	Preferences        []helpPageInfo
}

type serverPageDefinition struct {
	tree           string
	path           string
	titleKey       string
	descriptionKey string
	handler        string
}

// Each registered hub page is defined once here. The same entries drive
// route registration, hub links, sitemap exclusions, and the help API list.
var serverPageDefinitions = []serverPageDefinition{
	{
		tree:           "help",
		path:           helpHubPath + "/mcp",
		titleKey:       "help.mcp.title",
		descriptionKey: "help.mcp.description",
		handler:        "help-mcp",
	},
	{
		tree:           "preferences",
		path:           preferencesHubPath + "/sync",
		titleKey:       "page_text.text.sync.preferences.fc79211080",
		descriptionKey: "page_text.text.transfer.theme.and.language.settings.to.anot.69eb391118",
		handler:        "preferences-sync",
	},
	{
		tree:           "preferences",
		path:           preferencesHubPath + "/tokens",
		titleKey:       "page_text.text.owner.tokens.a5dc2b2bfe",
		descriptionKey: "page_text.text.manage.the.credentials.that.identify.your.an.e3e4a1b248",
		handler:        "owner-tokens",
	},
}

func registeredServerPages(language string) serverPageRegistry {
	registry := serverPageRegistry{HelpHubPath: helpHubPath, PreferencesHubPath: preferencesHubPath}
	for _, definition := range serverPageDefinitions {
		page := helpPageInfo{
			Path:        definition.path,
			Title:       i18n.Translate(language, definition.titleKey),
			Description: i18n.Translate(language, definition.descriptionKey),
		}
		switch definition.tree {
		case "help":
			registry.Help = append(registry.Help, page)
		case "preferences":
			registry.Preferences = append(registry.Preferences, page)
			if definition.path == preferencesHubPath+"/tokens" {
				registry.OwnerTokensPath = definition.path
			}
		}
	}
	return registry
}

func registerServerPageRoutes(cfg *config.ServerConfig, ownerCookieName string, routes func() []swagger.Route, register func(string, http.HandlerFunc)) {
	for _, definition := range serverPageDefinitions {
		register(definition.path, serverPageHandler(definition.handler, cfg, ownerCookieName, routes))
	}
}

func serverPageHandler(kind string, cfg *config.ServerConfig, ownerCookieName string, routes func() []swagger.Route) http.HandlerFunc {
	switch kind {
	case "help-mcp":
		return helpMCPPageHandler(cfg, routes)
	case "preferences-sync":
		return preferencesSyncPageHandler(cfg)
	case "owner-tokens":
		return ownerTokensPageHandler(cfg, ownerCookieName, paths.ProjectName)
	default:
		return http.NotFound
	}
}
