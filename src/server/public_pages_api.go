package server

import (
	"net/http"
	"strings"

	"github.com/apimgr/api/src/common/i18n"
	"github.com/apimgr/api/src/common/sanitize"
	"github.com/apimgr/api/src/config"
	"github.com/apimgr/api/src/database"
)

func aboutAPIHandler(cfg *config.ServerConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		r = requestWithServerConfig(r, cfg)
		lang := i18n.LangFromRequest(r)
		content := builtInAboutMarkdown(cfg)
		format := "markdown"
		if body, sourceFormat, ok := operatorContentSource(r, "about"); ok {
			content = replacePageContentVariables(body, cfg)
			format = sourceFormat
			switch format {
			case "md":
				format = "markdown"
			case "txt":
				format = "text"
			}
		}
		writeEnvelopeOK(w, http.StatusOK, responseObject(
			responseField("title", i18n.Translate(lang, "about.title")),
			responseField("content", content),
			responseField("format", format),
		))
	}
}

func builtInAboutMarkdown(cfg *config.ServerConfig) string {
	title := "Universal API Toolkit"
	tagline := ""
	description := ""
	if cfg != nil {
		title = strings.TrimSpace(cfg.Server.Branding.Title)
		tagline = strings.TrimSpace(cfg.Server.Branding.Tagline)
		description = strings.TrimSpace(cfg.Server.Branding.Description)
	}
	var content strings.Builder
	content.WriteString("# " + title + "\n")
	if tagline != "" {
		content.WriteString("\n" + tagline + "\n")
	}
	if description != "" {
		content.WriteString("\n" + description + "\n")
	}
	content.WriteString("\n## Features\n")
	for _, category := range allCategories() {
		content.WriteString("\n- [" + category.Name + "](" + category.Path + "): " + category.Description)
	}
	content.WriteString("\n\n## Links\n\n- [Project repository](https://github.com/apimgr/api)\n")
	return content.String()
}

func privacyAPIHandler(cfg *config.ServerConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		r = requestWithServerConfig(r, cfg)
		lang := i18n.LangFromRequest(r)
		collection := cfg.Server.Privacy.DataCollectionContent()
		usage := cfg.Server.Privacy.DataUsageContent()
		security := cfg.Server.Privacy.DataSecurityContent()
		sharing := cfg.Server.Privacy.Data.Sharing
		thirdParties := cfg.Server.Privacy.ThirdParty.Services
		dpoName, dpoEmail := publicDPOContact(cfg)
		fields := []sanitize.TaggedField{
			responseField("title", i18n.Translate(lang, "privacy.title")),
			responseField("summary", i18n.Translate(lang, "privacy.summary")),
			responseField("stored_on_server", cfg.Server.Privacy.Data.StoredOnServer),
			responseField("data_sold", cfg.Server.Privacy.Data.Sold),
			responseField("cookies", cfg.Server.Privacy.Cookies),
			responseField("data_collection", collection),
			responseField("data_usage", usage),
			responseField("data_security", security),
			responseField("sharing", sharing),
			responseField("retention_period", cfg.Server.Privacy.Retention.PeriodFor(lang)),
			responseField("rights", responseObject(
				responseField("export_available", cfg.Server.Privacy.Retention.ExportAvailable),
				responseField("deletion_available", cfg.Server.Privacy.Retention.DeletionAvailable),
			)),
			responseField("third_parties", thirdParties),
			responseField("dpo", responseObject(responseField("name", dpoName), responseField("email", dpoEmail))),
			responseField("privacy_url", BuildURL(r, "/server/privacy")),
			responseField("preferences_url", BuildURL(r, "/server/preferences")),
		}
		if body, format, ok := operatorContentSource(r, "privacy"); ok {
			fields = append(fields,
				responseField("extension", replacePageContentVariables(body, cfg)),
				responseField("extension_format", format),
			)
		}
		writeEnvelopeOK(w, http.StatusOK, responseObject(fields...))
	}
}

func authCheckAPIHandler(cfg *config.ServerConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		operator := ""
		if cfg != nil {
			operator = cfg.Server.Token
		}
		credential, err := database.ValidateOwnerTokenReadOnly(r.Context(), database.GetServerDB(), bearerToken(r), operator)
		if err != nil {
			w.Header().Set("WWW-Authenticate", "Bearer")
			writeTokenAPIError(w, r, "UNAUTHORIZED", "errors.unauthorized", http.StatusUnauthorized)
			return
		}
		kind := credential.Kind
		if kind == "primary" || kind == "linked" {
			kind = "owner"
		}
		writeEnvelopeOK(w, http.StatusOK, responseObject(responseField("kind", kind)))
	}
}
