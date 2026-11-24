package server

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"net/http"
	"strings"
	"time"
	"unicode"

	"github.com/apimgr/api/src/common/i18n"
	"github.com/apimgr/api/src/config"
)

const sitemapNamespace = "http://www.sitemaps.org/schemas/sitemap/0.9"

type sitemapDocument struct {
	XMLName xml.Name     `xml:"urlset"`
	XMLNS   string       `xml:"xmlns,attr"`
	URLs    []sitemapURL `xml:"url"`
}

type sitemapURL struct {
	Location   string `xml:"loc"`
	LastMod    string `xml:"lastmod,omitempty"`
	ChangeFreq string `xml:"changefreq"`
	Priority   string `xml:"priority"`
}

type atomDocument struct {
	XMLName  xml.Name    `xml:"feed"`
	XMLNS    string      `xml:"xmlns,attr"`
	Title    string      `xml:"title"`
	Subtitle string      `xml:"subtitle,omitempty"`
	ID       string      `xml:"id"`
	Updated  string      `xml:"updated"`
	Links    []atomLink  `xml:"link"`
	Entries  []atomEntry `xml:"entry"`
}

type atomLink struct {
	Href string `xml:"href,attr"`
	Rel  string `xml:"rel,attr,omitempty"`
	Type string `xml:"type,attr,omitempty"`
}

type atomEntry struct {
	Title   string `xml:"title"`
	ID      string `xml:"id"`
	Updated string `xml:"updated"`
}

type rssDocument struct {
	XMLName xml.Name   `xml:"rss"`
	Version string     `xml:"version,attr"`
	Channel rssChannel `xml:"channel"`
}

type rssChannel struct {
	Title       string    `xml:"title"`
	Link        string    `xml:"link"`
	Description string    `xml:"description"`
	LastBuild   string    `xml:"lastBuildDate"`
	Items       []rssItem `xml:"item"`
}

type rssItem struct {
	Title string `xml:"title"`
	Link  string `xml:"link"`
	GUID  string `xml:"guid"`
}

func sitemapHandler(cfg *config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		r = requestWithServerConfig(r, cfg)
		lastmod := ""
		for _, layout := range []string{time.RFC3339, "2006-01-02T15:04:05Z", "2006-01-02"} {
			if built, err := time.Parse(layout, BuildDate); err == nil {
				lastmod = built.UTC().Format("2006-01-02")
				break
			}
		}
		paths := publicSitemapPaths()
		doc := sitemapDocument{XMLNS: sitemapNamespace}
		for _, path := range paths {
			if !robotsAllowsPath(cfg, path) {
				continue
			}
			entry := sitemapURL{Location: BuildURL(r, path), LastMod: lastmod, ChangeFreq: "weekly", Priority: "0.8"}
			switch path {
			case "/":
				entry.ChangeFreq = "daily"
				entry.Priority = "1.0"
			case "/server/docs/swagger", "/server/docs/graphql":
				entry.Priority = "0.7"
			}
			doc.URLs = append(doc.URLs, entry)
		}
		modified := time.Now().UTC().Truncate(time.Second)
		if lastmod != "" {
			modified, _ = time.Parse("2006-01-02", lastmod)
		}
		writeXMLDocument(w, r, doc, modified, "application/xml; charset=utf-8")
	}
}

func publicSitemapPaths() []string {
	paths := []string{
		"/", "/text", "/crypto", "/datetime", "/network", "/categories",
		"/convert", "/dev", "/docker", "/fun", "/generate", "/geo",
		"/image", "/language", "/lorem", "/math", "/osint", "/parse",
		"/research", "/system", "/testing", "/validate", "/weather",
		"/server/about", "/server/privacy", "/server/help", "/server/terms", "/server/security", "/server/security/policy",
		"/server/docs/swagger", "/server/docs/graphql",
	}
	for _, page := range toolPages() {
		paths = append(paths, "/"+page.category+"/"+page.tool)
	}
	return paths
}

func robotsAllowsPath(cfg *config.Config, path string) bool {
	allowed := false
	for _, rule := range cfg.Web.Robots.Allow {
		rule = strings.TrimSpace(rule)
		if rule != "" && strings.HasPrefix(path, rule) {
			allowed = true
			break
		}
	}
	if !allowed {
		return false
	}
	for _, rule := range cfg.Web.Robots.Deny {
		rule = strings.TrimSpace(rule)
		if rule != "" && strings.HasPrefix(path, rule) {
			return false
		}
	}
	return true
}

func feedHandler(cfg *config.Config, rss bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		r = requestWithServerConfig(r, cfg)
		origin := BuildURL(r, "")
		title := cfg.Server.Branding.Title
		if title == "" {
			title = "api"
		}
		updated := time.Now().UTC().Truncate(5 * time.Minute)
		var document any
		if rss {
			document = rssDocument{
				Version: "2.0",
				Channel: rssChannel{
					Title: title, Link: origin, Description: cfg.Server.Branding.Description,
					LastBuild: updated.Format(time.RFC1123Z), Items: []rssItem{},
				},
			}
		} else {
			feedURL := BuildURL(r, "/feed.xml")
			document = atomDocument{
				XMLNS: "http://www.w3.org/2005/Atom", Title: title,
				Subtitle: cfg.Server.Branding.Description,
				ID:       origin, Updated: updated.Format(time.RFC3339),
				Links:   []atomLink{{Href: origin}, {Href: feedURL, Rel: "self", Type: "application/atom+xml"}},
				Entries: []atomEntry{},
			}
		}
		contentType := "application/atom+xml; charset=utf-8"
		if rss {
			contentType = "application/rss+xml; charset=utf-8"
		}
		writeXMLDocument(w, r, document, updated, contentType)
	}
}

func writeXMLDocument(w http.ResponseWriter, r *http.Request, document any, modified time.Time, contentType string) {
	document = cleanXMLValues(document)
	body, err := xml.MarshalIndent(document, "", "  ")
	if err != nil {
		http.Error(w, i18n.Translate(i18n.LangFromRequest(r), "errors.server_error"), http.StatusInternalServerError)
		return
	}
	body = append([]byte(xml.Header), body...)
	lastModified := modified.UTC().Truncate(time.Second)
	if lastModified.IsZero() {
		lastModified = time.Now().UTC().Truncate(time.Second)
	}
	digest := sha256.Sum256(body)
	etag := `"` + hex.EncodeToString(digest[:]) + `"`
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "public, max-age=300")
	w.Header().Set("ETag", etag)
	w.Header().Set("Last-Modified", lastModified.Format(http.TimeFormat))
	w.Header().Set("Vary", "Host, X-Forwarded-Host, X-Forwarded-Proto, X-Forwarded-Port, X-Forwarded-Prefix")
	if ifNoneMatch := r.Header.Get("If-None-Match"); ifNoneMatch != "" {
		if ifNoneMatch == etag || ifNoneMatch == "*" {
			w.WriteHeader(http.StatusNotModified)
			return
		}
	} else if since, err := http.ParseTime(r.Header.Get("If-Modified-Since")); err == nil && !lastModified.After(since) {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	w.WriteHeader(http.StatusOK)
	if r.Method != http.MethodHead {
		_, _ = w.Write(body)
	}
}

func cleanXMLValues(document any) any {
	clean := func(value string) string {
		return strings.Map(func(char rune) rune {
			if char == '\t' || char == '\n' || char == '\r' || char >= 0x20 && char <= 0xd7ff || char >= 0xe000 && char <= 0xfffd || char >= 0x10000 && char <= 0x10ffff && !unicode.IsControl(char) {
				return char
			}
			return -1
		}, value)
	}
	switch value := document.(type) {
	case sitemapDocument:
		for i := range value.URLs {
			value.URLs[i].Location = clean(value.URLs[i].Location)
		}
		return value
	case atomDocument:
		value.Title = clean(value.Title)
		value.Subtitle = clean(value.Subtitle)
		value.ID = clean(value.ID)
		for i := range value.Links {
			value.Links[i].Href = clean(value.Links[i].Href)
		}
		return value
	case rssDocument:
		value.Channel.Title = clean(value.Channel.Title)
		value.Channel.Link = clean(value.Channel.Link)
		value.Channel.Description = clean(value.Channel.Description)
		return value
	}
	return document
}
