package server

import (
	"crypto/sha256"
	"fmt"
	stdhtml "html"
	"html/template"
	"log"
	"net/http"
	"net/url"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/apimgr/api/src/common/i18n"
	"github.com/apimgr/api/src/config"
	"github.com/apimgr/api/src/i2p"
	"github.com/apimgr/api/src/paths"
	"github.com/apimgr/api/src/service/text"
	"github.com/apimgr/api/src/tor"
	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

const maxPageContentBytes = 256 * 1024

var pageContentLogged sync.Map

var operatorContentEntries = map[string]bool{
	"header": true, "about": true, "privacy": true, "terms": true, "footer": true,
}

var strictPageContentTags = map[string]bool{
	"p": true, "br": true, "span": true, "div": true, "strong": true, "b": true,
	"em": true, "i": true, "u": true, "s": true, "small": true, "a": true,
	"img": true, "ul": true, "ol": true, "li": true, "h1": true, "h2": true,
	"h3": true, "h4": true, "h5": true, "h6": true,
}

// operatorHTML resolves and sanitizes configured public-page content.
func operatorHTML(r *http.Request, name string) template.HTML {
	body, format, ok := operatorContentSource(r, name)
	if !ok || strings.TrimSpace(body) == "" {
		return ""
	}
	cfg, _ := r.Context().Value(serverConfigKey).(*config.Config)
	if cfg == nil {
		return ""
	}
	raw := body
	switch format {
	case "md":
		body = text.MarkdownToHTML(body)
	case "html":
	case "txt":
		body = plainTextHTML(body)
	default:
		return ""
	}
	strict := name == "header" || name == "footer"
	rendered := replacePageContentVariables(body, cfg)
	sanitized := text.SanitizeHTML(sanitizeOperatorContent(rendered, strict))
	logPageContentPreview(name, raw, rendered, sanitized)
	return template.HTML(sanitized)
}

// LogConfiguredPageContent previews every configured language during startup
// and after the configuration watcher applies a reload.
func LogConfiguredPageContent(cfg *config.Config) {
	logConfiguredPageContent(cfg)
}

// logConfiguredPageContent previews every configured language during server
// initialization, before any request renders the operator-supplied fragment.
func logConfiguredPageContent(cfg *config.Config) {
	if cfg == nil {
		return
	}
	for name, entry := range cfg.Server.Branding.Content {
		if !operatorContentEntries[name] {
			logInvalidPageContentOnce(name, "entry", "unknown content area")
			continue
		}
		format := entry.Format
		if format == "" {
			format = "md"
		}
		validFormat := format == "md" || format == "html" || format == "txt"
		if name == "header" || name == "footer" {
			validFormat = format == "html"
		}
		if !validFormat {
			logInvalidPageContentOnce(name, "format", format)
			continue
		}
		values := pageContentValues(entry.Body)
		if len(values) == 0 || !validPageContentBody(entry.Body) {
			logInvalidPageContentOnce(name, "body", "must be a string or a language-to-string map")
		}
		for _, body := range values {
			if len(body) > maxPageContentBytes {
				logInvalidPageContentOnce(name, "body", "exceeds 256 KiB")
				continue
			}
			rendered := body
			switch format {
			case "md":
				rendered = text.MarkdownToHTML(rendered)
			case "txt":
				rendered = plainTextHTML(rendered)
			}
			rendered = replacePageContentVariables(rendered, cfg)
			sanitized := sanitizeOperatorContent(rendered, name == "header" || name == "footer")
			logPageContentPreview(name, body, rendered, sanitized)
		}
	}
}

func validPageContentBody(value interface{}) bool {
	switch body := value.(type) {
	case string:
		return true
	case map[string]interface{}:
		if len(body) == 0 {
			return false
		}
		for language, raw := range body {
			if language != "default" && !i18n.IsSupported(strings.ToLower(language)) {
				return false
			}
			if _, ok := raw.(string); !ok {
				return false
			}
		}
		return true
	default:
		return false
	}
}

func pageContentValues(value interface{}) []string {
	switch body := value.(type) {
	case string:
		return []string{body}
	case map[string]interface{}:
		values := make([]string, 0, len(body))
		for _, raw := range body {
			if text, ok := raw.(string); ok {
				values = append(values, text)
			}
		}
		return values
	default:
		return nil
	}
}

// operatorContentSource resolves the configured source for rendered HTML and
// API responses. It keeps configuration access in one place while preserving
// the source format for endpoints that must return Markdown or text.
func operatorContentSource(r *http.Request, name string) (string, string, bool) {
	if !operatorContentEntries[name] {
		return "", "", false
	}
	cfg, _ := r.Context().Value(serverConfigKey).(*config.Config)
	if cfg == nil {
		return "", "", false
	}
	entry, ok := cfg.Server.Branding.Content[name]
	if !ok {
		return "", "", false
	}
	format := entry.Format
	if format == "" {
		format = "md"
	}
	validFormat := format == "md" || format == "html" || format == "txt"
	if name == "header" || name == "footer" {
		validFormat = format == "html"
	}
	if !validFormat {
		logInvalidPageContentOnce(name, "format", format)
		return "", format, false
	}
	if !validPageContentBody(entry.Body) {
		logInvalidPageContentOnce(name, "body", "must be a string or a language-to-string map")
		return "", format, false
	}
	body, ok := pageContentBody(entry.Body, i18n.LangFromRequest(r))
	if !ok {
		logInvalidPageContentOnce(name, "body", "missing or not a string")
		return "", format, false
	}
	if len(body) > maxPageContentBytes {
		logInvalidPageContentOnce(name, "body", "exceeds 256 KiB")
		return "", format, false
	}
	return body, format, true
}

func logInvalidPageContentOnce(name, field, reason string) {
	key := strings.Join([]string{name, field}, ":")
	if _, loaded := pageContentLogged.LoadOrStore("invalid:"+key, struct{}{}); !loaded {
		log.Printf("Warning: ignoring server.branding.content.%s: invalid %s (%s)", name, field, reason)
	}
}

func logPageContentPreview(name, raw, rendered, sanitized string) {
	key := "preview:" + name
	digest := fmt.Sprintf("%x", sha256.Sum256([]byte(raw)))
	if previous, loaded := pageContentLogged.Load(key); loaded && previous == digest {
		return
	}
	pageContentLogged.Store(key, digest)
	log.Printf("Page content %s raw: %q", name, raw)
	log.Printf("Page content %s sanitized: %q", name, sanitized)
	if rendered != sanitized {
		log.Printf("Warning: server.branding.content.%s was sanitized", name)
	}
}

func pageContentBody(value interface{}, lang string) (string, bool) {
	switch body := value.(type) {
	case string:
		return body, true
	case map[string]interface{}:
		if value, ok := body[lang].(string); ok {
			return value, true
		}
		if value, ok := body["default"].(string); ok {
			return value, true
		}
	}
	return "", false
}

func plainTextHTML(value string) string {
	paragraphs := strings.Split(strings.TrimSpace(value), "\n\n")
	for i, paragraph := range paragraphs {
		paragraphs[i] = "<p>" + stdhtml.EscapeString(strings.TrimSpace(paragraph)) + "</p>"
	}
	return strings.Join(paragraphs, "\n")
}

func replacePageContentVariables(value string, cfg *config.Config) string {
	variables := map[string]string{
		"{app_name}":        cfg.Server.Branding.Title,
		"{project_name}":    paths.ProjectName,
		"{project_org}":     paths.OrgName,
		"{project_version}": Version,
		"{current_year}":    fmt.Sprint(time.Now().Year()),
		"{build_datetime}":  footerBuildDate(),
		"{onion_address}":   "",
		"{i2p_address}":     "",
	}
	if manager := tor.Get(); manager != nil && manager.Running() {
		variables["{onion_address}"] = manager.OnionAddress()
	}
	if manager := i2p.Get(); manager != nil {
		snapshot := manager.Snapshot()
		if snapshot.Enabled && snapshot.Running && strings.TrimSpace(snapshot.Address) != "" {
			variables["{i2p_address}"] = snapshot.Address
		}
	}
	for key, replacement := range variables {
		value = strings.ReplaceAll(value, key, replacement)
	}
	return value
}

var pageContentTags = map[string]bool{
	"p": true, "br": true, "span": true, "div": true, "strong": true, "b": true,
	"em": true, "i": true, "a": true, "img": true, "ul": true, "ol": true,
	"li": true, "h1": true, "h2": true, "h3": true, "h4": true, "h5": true,
	"h6": true, "table": true, "thead": true, "tbody": true, "tr": true,
	"th": true, "td": true, "pre": true, "code": true, "blockquote": true,
	"hr": true, "dl": true, "dt": true, "dd": true, "sup": true, "sub": true,
}

func sanitizeOperatorContent(value string, strict bool) string {
	context := &html.Node{Type: html.ElementNode, Data: "div", DataAtom: atom.Div}
	nodes, err := html.ParseFragment(strings.NewReader(value), context)
	if err != nil {
		return ""
	}
	var out strings.Builder
	for _, node := range nodes {
		writeSanitizedNode(&out, node, strict)
	}
	return out.String()
}

func writeSanitizedNode(out *strings.Builder, node *html.Node, strict bool) {
	switch node.Type {
	case html.TextNode:
		out.WriteString(stdhtml.EscapeString(node.Data))
	case html.ElementNode:
		if blockedPageContentTags[node.Data] {
			return
		}
		allowed := pageContentTags[node.Data]
		if strict {
			allowed = strictPageContentTags[node.Data]
		}
		if !allowed {
			for child := node.FirstChild; child != nil; child = child.NextSibling {
				writeSanitizedNode(out, child, strict)
			}
			return
		}
		out.WriteByte('<')
		out.WriteString(node.Data)
		for _, attr := range node.Attr {
			if node.Data == "a" && attr.Key == "rel" && hasTargetBlank(node) {
				continue
			}
			if safePageContentAttribute(node.Data, attr.Key, attr.Val) {
				out.WriteByte(' ')
				out.WriteString(attr.Key)
				out.WriteString(`="`)
				out.WriteString(stdhtml.EscapeString(attr.Val))
				out.WriteByte('"')
			}
		}
		if node.Data == "a" && hasTargetBlank(node) {
			out.WriteString(` rel="noopener noreferrer"`)
		}
		out.WriteByte('>')
		if node.Data != "br" && node.Data != "hr" && node.Data != "img" {
			for child := node.FirstChild; child != nil; child = child.NextSibling {
				writeSanitizedNode(out, child, strict)
			}
			out.WriteString("</")
			out.WriteString(node.Data)
			out.WriteByte('>')
		}
	}
}

func hasTargetBlank(node *html.Node) bool {
	for _, attribute := range node.Attr {
		if attribute.Key == "target" && attribute.Val == "_blank" {
			return true
		}
	}
	return false
}

var blockedPageContentTags = map[string]bool{
	"script": true, "style": true, "iframe": true, "frame": true, "object": true,
	"embed": true, "form": true, "input": true, "button": true, "noscript": true,
	"link": true, "meta": true, "base": true,
}

func safePageContentAttribute(tag, name, value string) bool {
	if name == "class" || name == "id" {
		return true
	}
	if tag == "a" && name == "href" {
		if strings.ContainsAny(value, "\\\r\n") {
			return false
		}
		parsed, err := url.Parse(value)
		if err != nil || parsed.Scheme == "javascript" || parsed.Scheme == "data" || parsed.Host != "" && parsed.Scheme == "" || linksToContact(parsed) {
			return false
		}
		return parsed.Scheme == "" || parsed.Scheme == "https" || parsed.Scheme == "http" || parsed.Scheme == "mailto"
	}
	if tag == "img" && (name == "src" || name == "alt" || name == "title" || name == "width" || name == "height") {
		if name != "src" {
			return true
		}
		if strings.ContainsAny(value, "\\\r\n") {
			return false
		}
		parsed, err := url.Parse(value)
		return err == nil && (parsed.Scheme == "https" || parsed.Scheme == "" && parsed.Host == "") && !strings.HasPrefix(value, "//")
	}
	if tag == "a" && name == "target" {
		return value == "_blank" || value == "_self"
	}
	if tag == "a" && name == "rel" {
		return safeRelTokens(value)
	}
	if tag == "a" && name == "title" {
		return true
	}
	return false
}

func linksToContact(parsed *url.URL) bool {
	decoded, err := url.PathUnescape(parsed.EscapedPath())
	if err != nil {
		return true
	}
	return path.Clean(decoded) == "/server/contact"
}

func safeRelTokens(value string) bool {
	for _, token := range strings.Fields(strings.ToLower(value)) {
		if token != "noopener" && token != "noreferrer" && token != "nofollow" {
			return false
		}
	}
	return true
}
