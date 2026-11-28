package server

import (
	"html/template"
	"net/http"
	"strings"

	"github.com/apimgr/api/src/common/i18n"
	"github.com/apimgr/api/src/config"
	"github.com/apimgr/api/src/service/text"
)

const securityPolicyMarkdown = `## Scope

This policy covers the application served at {fqdn}, its API, and its published Tor or I2P addresses.

## Out of scope

Denial of service and volumetric testing, social engineering, physical attacks, findings in third-party services or dependencies without an exploitable path in this application, automated scanner output without demonstrated impact, and missing best-practice headers on non-sensitive pages are out of scope.

## Coordinated disclosure

Please report vulnerabilities privately through [{app_name} security](/server/security) and allow 90 days from the report to publish. The window ends early once a fix is released and may be extended by agreement.

## Safe harbor

Good-faith research that follows this policy, avoids privacy violations and service disruption, accesses only what is needed to demonstrate the issue, and stops when it has done so is authorized. No legal action will be taken for such research.

## Reporting

Use the private reporting channels at [{app_name} security](/server/security). Do not disclose vulnerability details publicly before coordinating with the maintainers.
`

func securityPolicyContent(cfg *config.ServerConfig, r *http.Request) string {
	content := strings.TrimSpace(cfg.Web.Security.Policy)
	if content == "" {
		content = securityPolicyMarkdown
	}
	content = strings.ReplaceAll(content, "{app_name}", cfg.Server.Branding.Title)
	content = strings.ReplaceAll(content, "{fqdn}", BuildURL(r, ""))
	return content
}

const defaultTermsMarkdown = `## Acceptance

Using {app_name} means accepting these terms; stop using it if you do not agree.

## Acceptable use

No unlawful use, no abuse of other people, no attempt to bypass rate limits or access controls, and no testing beyond the security policy at /server/security/policy.

## Liability

The service is provided as is, without warranty of any kind; to the extent the law allows, the operator is not liable for loss arising from its use.

## Changes

The terms may change; the current text on this page applies from the moment it is published.

## Governing law

The laws of the United States, without regard to conflict-of-law rules, unless the operator's own terms name another.
`

func termsMarkdown(appName string) string {
	return strings.ReplaceAll(defaultTermsMarkdown, "{app_name}", appName)
}

func termsPageHandler(cfg *config.ServerConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		r = requestWithServerConfig(r, cfg)
		data := newPageData(cfg, r, "terms")
		data.PageTitle = i18n.Translate(data.Lang, "nav.terms")
		data.PageDescription = data.PageTitle + " for " + data.SiteTitle
		data.TermsContent = template.HTML(text.MarkdownToHTML(termsMarkdown(cfg.Server.Branding.Title)))
		if configured := operatorHTML(r, "terms"); configured != "" {
			data.TermsContent = configured
		}
		data.TermsContent = cardContentHeadings(data.TermsContent)
		renderPage(w, "terms", data)
	}
}

func termsAPIHandler(cfg *config.ServerConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		r = requestWithServerConfig(r, cfg)
		lang := i18n.LangFromRequest(r)
		content := termsMarkdown(cfg.Server.Branding.Title)
		format := "markdown"
		if body, sourceFormat, ok := operatorContentSource(r, "terms"); ok {
			content = replacePageContentVariables(body, cfg)
			format = sourceFormat
			switch format {
			case "md":
				format = "markdown"
			case "txt":
				format = "text"
			}
		}
		writeEnvelopeOK(w, http.StatusOK, responseObject(responseField("title", i18n.Translate(lang, "nav.terms")), responseField("content", content), responseField("format", format)))
	}
}

func securityPolicyPageHandler(cfg *config.ServerConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		r = requestWithServerConfig(r, cfg)
		data := newPageData(cfg, r, "security-policy")
		data.PageTitle = i18n.Translate(data.Lang, "security.policy_link")
		data.PageDescription = i18n.TranslateFormat(data.Lang, "security.lead", map[string]string{"app_name": data.SiteTitle})
		data.SecurityPolicy = cardContentHeadings(template.HTML(text.MarkdownToHTML(securityPolicyContent(cfg, r))))
		renderPage(w, "security_policy", data)
	}
}

func securityPolicyAPIHandler(cfg *config.ServerConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		r = requestWithServerConfig(r, cfg)
		writeEnvelopeOK(w, http.StatusOK, responseObject(responseField("policy", securityPolicyContent(cfg, r)), responseField("format", "markdown")))
	}
}
