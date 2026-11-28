package server

import (
	"html/template"
	"net/http"
	"sync"
	"time"

	"github.com/apimgr/api/src/common/i18n"
	"github.com/apimgr/api/src/config"
	"github.com/apimgr/api/src/database"
	"github.com/go-chi/chi/v5"
)

type securityStatusPageData struct {
	PageData
	TrackingID string
	Status     string
	Severity   string
	Comment    string
	Disclosure string
	Created    string
	Updated    string
}

type securityThanksPageData struct {
	PageData
	Researchers []database.SecurityThanks
}

var securityPublicTemplates struct {
	sync.Once
	status *template.Template
	thanks *template.Template
	err    error
}

func securityPublicPageTemplate(name string) (*template.Template, error) {
	securityPublicTemplates.Do(func() {
		base := []string{"template/layout/public.tmpl", "template/partial/*.tmpl", "template/partial/public/*.tmpl"}
		securityPublicTemplates.status, securityPublicTemplates.err = template.New("security_status").Funcs(templateFuncs()).ParseFS(templatesFS, append(base, "template/page/security_status.tmpl")...)
		if securityPublicTemplates.err != nil {
			return
		}
		securityPublicTemplates.thanks, securityPublicTemplates.err = template.New("security_thanks").Funcs(templateFuncs()).ParseFS(templatesFS, append(base, "template/page/security_thanks.tmpl")...)
	})
	if name == "security_status" {
		return securityPublicTemplates.status, securityPublicTemplates.err
	}
	return securityPublicTemplates.thanks, securityPublicTemplates.err
}

// securityReportStatusPageHandler consumes the emailed status token at most
// once per UTC day and renders only metadata approved for researcher access.
func securityReportStatusPageHandler(cfg *config.ServerConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		trackingID := chi.URLParam(r, "tracking_id")
		status, err := database.SecurityReportStatusForResearcher(r.Context(), trackingID, r.URL.Query().Get("token"), time.Now().UTC())
		if err != nil {
			http.NotFound(w, r)
			return
		}
		page := securityStatusPageData{PageData: newPageData(cfg, r, "security"), TrackingID: status.TrackingID,
			Status: status.Status, Severity: status.Severity, Comment: status.ResearcherComment,
			Disclosure: status.DisclosureDate, Created: status.CreatedAt, Updated: status.UpdatedAt}
		page.PageTitle = i18n.Translate(page.Lang, "security.status_title")
		page.PageDescription = i18n.Translate(page.Lang, "security.status_description")
		page.NoIndex = true
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Robots-Tag", "noindex, nofollow")
		securityPublicRender(w, r, "security_status", page)
	}
}

// securityThanksPageHandler lists only researchers who opted into public
// credit and the disclosure metadata stored in security_thanks.
func securityThanksPageHandler(cfg *config.ServerConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rows, err := database.ListSecurityThanks(r.Context())
		if err != nil {
			http.Error(w, i18n.Translate(i18n.LangFromRequest(r), "errors.server_error"), http.StatusInternalServerError)
			return
		}
		page := securityThanksPageData{PageData: newPageData(cfg, r, "security"), Researchers: rows}
		page.PageTitle = i18n.Translate(page.Lang, "security.thanks_title")
		page.PageDescription = i18n.Translate(page.Lang, "security.thanks_description")
		w.Header().Set("Cache-Control", "public, max-age=300")
		securityPublicRender(w, r, "security_thanks", page)
	}
}

func securityPublicRender(w http.ResponseWriter, r *http.Request, name string, data any) {
	tmpl, err := securityPublicPageTemplate(name)
	if err != nil {
		http.Error(w, i18n.Translate(i18n.LangFromRequest(r), "errors.server_error"), http.StatusInternalServerError)
		return
	}
	if err := tmpl.ExecuteTemplate(w, "public.tmpl", data); err != nil {
		http.Error(w, i18n.Translate(i18n.LangFromRequest(r), "errors.server_error"), http.StatusInternalServerError)
	}
}
