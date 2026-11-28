package server

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"html/template"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode"

	"github.com/apimgr/api/src/common/i18n"
	"github.com/apimgr/api/src/config"
)

const sitemapNamespace = "http://www.sitemaps.org/schemas/sitemap/0.9"

type verificationMetaTag struct {
	Name     string
	Property string
	Content  string
}

func canonicalPageURL(r *http.Request, configured string) string {
	if parsed, err := url.Parse(strings.TrimSpace(configured)); err == nil && parsed.Scheme == "https" && parsed.Host != "" && parsed.User == nil {
		parsed.Path = strings.TrimRight(parsed.Path, "/") + "/" + strings.TrimLeft(r.URL.Path, "/")
		parsed.RawQuery = ""
		parsed.Fragment = ""
		return parsed.String()
	}
	return BuildURL(r, r.URL.Path)
}

func absoluteAssetURL(r *http.Request, value string) string {
	if value == "" || strings.HasPrefix(value, "https://") || strings.HasPrefix(value, "http://") || strings.HasPrefix(value, "data:") {
		return value
	}
	return BuildURL(r, value)
}

func validSEOMetaTags(tags map[string]string) map[string]string {
	valid := make(map[string]string)
	for name, content := range tags {
		if !validSEOAttribute(name) || len(content) > 1024 {
			continue
		}
		switch strings.ToLower(name) {
		case "description", "keywords", "author", "robots", "viewport":
			continue
		}
		valid[name] = content
	}
	return valid
}

func validSEOAttribute(value string) bool {
	if value == "" || len(value) > 64 {
		return false
	}
	for _, character := range value {
		if (character < 'a' || character > 'z') &&
			(character < 'A' || character > 'Z') &&
			(character < '0' || character > '9') &&
			!strings.ContainsRune(":_.-", character) {
			return false
		}
	}
	return true
}

func validSEOVerification(config config.VerificationConfig) []verificationMetaTag {
	var tags []verificationMetaTag
	appendTag := func(name, property, content string, valid bool) {
		if valid && strings.TrimSpace(content) != "" {
			tags = append(tags, verificationMetaTag{Name: name, Property: property, Content: content})
		}
	}
	appendTag("google-site-verification", "", config.Google, validPattern(config.Google, 43, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_-"))
	appendTag("msvalidate.01", "", config.Bing, validPattern(config.Bing, 32, "0123456789abcdefABCDEF"))
	appendTag("yandex-verification", "", config.Yandex, validPattern(config.Yandex, 32, "0123456789abcdef"))
	appendTag("baidu-site-verification", "", config.Baidu, validPattern(config.Baidu, 64, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_-"))
	appendTag("p:domain_verify", "", config.Pinterest, validPattern(config.Pinterest, 64, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_-"))
	appendTag("", "fb:domain_verification", config.Facebook, validPattern(config.Facebook, 64, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_-"))
	for _, custom := range config.Custom {
		if (custom.Name == "") == (custom.Property == "") || (custom.Name != "" && !validSEOAttribute(custom.Name)) || (custom.Property != "" && !validSEOAttribute(custom.Property)) || len(custom.Content) > 256 || strings.TrimSpace(custom.Content) == "" {
			continue
		}
		appendTag(custom.Name, custom.Property, custom.Content, true)
	}
	return tags
}

func validPattern(value string, maximum int, allowed string) bool {
	if value == "" || len(value) > maximum {
		return false
	}
	for _, character := range value {
		if !strings.ContainsRune(allowed, character) {
			return false
		}
	}
	return true
}

func publicIndexableRoute(path string) bool {
	if path == "/server/help/mcp" {
		return true
	}
	for _, publicPath := range publicSitemapPaths() {
		if path == publicPath {
			return true
		}
	}
	return false
}

func robotsTagMiddleware(cfg *config.ServerConfig) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if publicIndexableRoute(r.URL.Path) && (cfg == nil || !cfg.Server.SEO.NoIndex) {
				w.Header().Set("X-Robots-Tag", "index, follow")
			} else {
				w.Header().Set("X-Robots-Tag", "noindex, nofollow")
			}
			next.ServeHTTP(w, r)
		})
	}
}

func normalizedTitleSeparator(separator string) string {
	if separator == "" || len(separator) > 8 {
		return " - "
	}
	return separator
}

func websiteJSONLD(name, description, canonical string) template.HTML {
	body, err := json.Marshal(map[string]string{
		"@context":    "https://schema.org",
		"@type":       "WebSite",
		"name":        name,
		"description": description,
		"url":         canonical,
	})
	if err != nil {
		return "{}"
	}
	return template.HTML(body)
}

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

func sitemapHandler(cfg *config.ServerConfig) http.HandlerFunc {
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
		registeredPages := registeredServerPages(i18n.LangFromRequest(r)).Preferences
		privatePages := make(map[string]struct{}, len(registeredPages))
		for _, page := range registeredPages {
			privatePages[page.Path] = struct{}{}
		}
		doc := sitemapDocument{XMLNS: sitemapNamespace}
		for _, path := range paths {
			if _, registered := privatePages[path]; registered {
				continue
			}
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
			if cfg.Server.SEO.Sitemap.MaxURLs > 0 && len(doc.URLs) >= cfg.Server.SEO.Sitemap.MaxURLs {
				break
			}
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
		"/server/about", "/server/privacy", "/server/dpo", "/server/help", "/server/terms", "/server/security", "/server/security/policy",
		"/server/help/mcp",
		"/server/docs/swagger", "/server/docs/graphql",
	}
	for _, page := range toolPages() {
		paths = append(paths, "/"+page.category+"/"+page.tool)
	}
	return paths
}

func robotsAllowsPath(cfg *config.ServerConfig, path string) bool {
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

func feedHandler(cfg *config.ServerConfig, rss bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		r = requestWithServerConfig(r, cfg)
		origin := BuildURL(r, "")
		title := cfg.Server.SEO.Feed.Title
		if title == "" {
			title = cfg.Server.Branding.Title
		}
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
