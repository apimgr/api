package parse

import (
	"encoding/csv"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"golang.org/x/net/html"
	"gopkg.in/yaml.v3"
)

// Service provides parsing utilities
type Service struct{}

// New creates a new Parse service
func New() *Service {
	return &Service{}
}

// JSON parsing
func (s *Service) ParseJSON(jsonStr string) (map[string]interface{}, error) {
	var result map[string]interface{}
	err := json.Unmarshal([]byte(jsonStr), &result)
	return result, err
}

func (s *Service) ParseJSONArray(jsonStr string) ([]interface{}, error) {
	var result []interface{}
	err := json.Unmarshal([]byte(jsonStr), &result)
	return result, err
}

// xmlNode is a generic XML element tree used to decode arbitrary XML into a
// map[string]interface{}. encoding/xml cannot unmarshal directly into a
// generic map (it only supports structs/known types), so this walks the
// element tree itself: attributes are keyed as "@name", text content as
// "#text" (or the bare string when there are no attributes/children), and
// repeated child element names collapse into a slice.
type xmlNode struct {
	XMLName xml.Name
	Attrs   []xml.Attr `xml:",any,attr"`
	Content string     `xml:",chardata"`
	Nodes   []xmlNode  `xml:",any"`
}

// xmlNodeToValue converts a single xmlNode into its map/string representation.
func xmlNodeToValue(n xmlNode) interface{} {
	if len(n.Nodes) == 0 {
		text := strings.TrimSpace(n.Content)
		if len(n.Attrs) == 0 {
			return text
		}
		leaf := make(map[string]interface{}, len(n.Attrs)+1)
		for _, a := range n.Attrs {
			leaf["@"+a.Name.Local] = a.Value
		}
		if text != "" {
			leaf["#text"] = text
		}
		return leaf
	}

	m := make(map[string]interface{}, len(n.Attrs)+len(n.Nodes))
	for _, a := range n.Attrs {
		m["@"+a.Name.Local] = a.Value
	}
	for _, child := range n.Nodes {
		value := xmlNodeToValue(child)
		key := child.XMLName.Local
		if existing, ok := m[key]; ok {
			if arr, ok := existing.([]interface{}); ok {
				m[key] = append(arr, value)
			} else {
				m[key] = []interface{}{existing, value}
			}
		} else {
			m[key] = value
		}
	}
	return m
}

// XML parsing
func (s *Service) ParseXML(xmlStr string) (map[string]interface{}, error) {
	var root xmlNode
	if err := xml.Unmarshal([]byte(xmlStr), &root); err != nil {
		return nil, err
	}
	value := xmlNodeToValue(root)
	body, ok := value.(map[string]interface{})
	if !ok {
		body = map[string]interface{}{"#text": value}
	}
	return map[string]interface{}{root.XMLName.Local: body}, nil
}

// URL parsing
func (s *Service) ParseURL(urlStr string) (*URLParts, error) {
	u, err := url.Parse(urlStr)
	if err != nil {
		return nil, err
	}

	return &URLParts{
		Scheme:   u.Scheme,
		Host:     u.Host,
		Hostname: u.Hostname(),
		Port:     u.Port(),
		Path:     u.Path,
		Query:    u.RawQuery,
		Fragment: u.Fragment,
		User:     u.User.Username(),
	}, nil
}

type URLParts struct {
	Scheme   string `json:"scheme"`
	Host     string `json:"host"`
	Hostname string `json:"hostname"`
	Port     string `json:"port"`
	Path     string `json:"path"`
	Query    string `json:"query"`
	Fragment string `json:"fragment"`
	User     string `json:"user"`
}

// Query string parsing
func (s *Service) ParseQueryString(query string) (map[string][]string, error) {
	values, err := url.ParseQuery(query)
	if err != nil {
		return nil, err
	}
	return values, nil
}

// Date/Time parsing
func (s *Service) ParseDateTime(dateStr string) (time.Time, error) {
	// Try common formats
	formats := []string{
		time.RFC3339,
		time.RFC1123,
		time.RFC822,
		"2006-01-02",
		"2006-01-02 15:04:05",
		"01/02/2006",
		"01-02-2006",
		"2006/01/02",
	}

	for _, format := range formats {
		if t, err := time.Parse(format, dateStr); err == nil {
			return t, nil
		}
	}

	return time.Time{}, fmt.Errorf("unable to parse date: %s", dateStr)
}

// Number parsing
func (s *Service) ParseInt(str string) (int64, error) {
	return strconv.ParseInt(strings.TrimSpace(str), 10, 64)
}

func (s *Service) ParseFloat(str string) (float64, error) {
	return strconv.ParseFloat(strings.TrimSpace(str), 64)
}

func (s *Service) ParseBool(str string) (bool, error) {
	str = strings.ToLower(strings.TrimSpace(str))
	switch str {
	case "true", "yes", "1", "on":
		return true, nil
	case "false", "no", "0", "off":
		return false, nil
	default:
		return false, fmt.Errorf("invalid boolean value: %s", str)
	}
}

// CSV parsing (simple)
func (s *Service) ParseCSVLine(line string) []string {
	var result []string
	var current strings.Builder
	inQuotes := false

	for i := 0; i < len(line); i++ {
		char := line[i]

		switch char {
		case '"':
			inQuotes = !inQuotes
		case ',':
			if !inQuotes {
				result = append(result, current.String())
				current.Reset()
			} else {
				current.WriteByte(char)
			}
		default:
			current.WriteByte(char)
		}
	}

	result = append(result, current.String())
	return result
}

// ParseCSV parses a full RFC 4180 CSV document (via the stdlib encoding/csv
// reader) using the first row as column headers, returning one map per
// remaining row keyed by header name.
func (s *Service) ParseCSV(csvStr string) ([]map[string]string, error) {
	reader := csv.NewReader(strings.NewReader(csvStr))
	reader.FieldsPerRecord = -1

	records, err := reader.ReadAll()
	if err != nil {
		return nil, err
	}
	if len(records) == 0 {
		return nil, fmt.Errorf("csv document has no rows")
	}

	headers := records[0]
	rows := make([]map[string]string, 0, len(records)-1)
	for _, record := range records[1:] {
		row := make(map[string]string, len(headers))
		for i, header := range headers {
			if i < len(record) {
				row[header] = record[i]
			} else {
				row[header] = ""
			}
		}
		rows = append(rows, row)
	}
	return rows, nil
}

// User-Agent parsing (basic)
func (s *Service) ParseUserAgent(ua string) *UserAgent {
	result := &UserAgent{
		Raw: ua,
	}

	// Detect browser
	if strings.Contains(ua, "Chrome") {
		result.Browser = "Chrome"
	} else if strings.Contains(ua, "Firefox") {
		result.Browser = "Firefox"
	} else if strings.Contains(ua, "Safari") && !strings.Contains(ua, "Chrome") {
		result.Browser = "Safari"
	} else if strings.Contains(ua, "Edge") {
		result.Browser = "Edge"
	} else if strings.Contains(ua, "MSIE") || strings.Contains(ua, "Trident") {
		result.Browser = "Internet Explorer"
	}

	// Detect OS.
	//
	// iOS is checked before "Mac OS": real iPhone/iPad Safari user
	// agents always include the compatibility string "like Mac OS X",
	// so checking "Mac OS" first would permanently misdetect every
	// actual iOS device as macOS.
	if strings.Contains(ua, "Windows") {
		result.OS = "Windows"
	} else if strings.Contains(ua, "iOS") || strings.Contains(ua, "iPhone") || strings.Contains(ua, "iPad") {
		result.OS = "iOS"
	} else if strings.Contains(ua, "Mac OS") {
		result.OS = "macOS"
	} else if strings.Contains(ua, "Linux") {
		result.OS = "Linux"
	} else if strings.Contains(ua, "Android") {
		result.OS = "Android"
	}

	// Detect device type
	if strings.Contains(ua, "Mobile") || strings.Contains(ua, "Android") || strings.Contains(ua, "iPhone") {
		result.Device = "Mobile"
	} else if strings.Contains(ua, "Tablet") || strings.Contains(ua, "iPad") {
		result.Device = "Tablet"
	} else {
		result.Device = "Desktop"
	}

	return result
}

type UserAgent struct {
	Raw     string `json:"raw"`
	Browser string `json:"browser"`
	OS      string `json:"os"`
	Device  string `json:"device"`
}

// Email parsing
func (s *Service) ParseEmail(email string) (*EmailParts, error) {
	parts := strings.Split(email, "@")
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid email format")
	}

	return &EmailParts{
		Local:  parts[0],
		Domain: parts[1],
		Full:   email,
	}, nil
}

type EmailParts struct {
	Local  string `json:"local"`
	Domain string `json:"domain"`
	Full   string `json:"full"`
}

// ParseEnv parses ".env"-style KEY=VALUE lines. Blank lines and lines
// starting with "#" (after trimming) are skipped as comments, a leading
// "export " prefix on the key is stripped, and a value wrapped in matching
// single or double quotes has the quotes removed with no further escape
// processing. A line with no "=" is skipped best-effort; an error is only
// returned when the input is non-empty after trimming but produces zero
// valid KEY=VALUE pairs.
func (s *Service) ParseEnv(raw string) (map[string]string, error) {
	result := make(map[string]string)
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		idx := strings.Index(line, "=")
		if idx < 0 {
			continue
		}
		key := strings.TrimSpace(line[:idx])
		if key == "" {
			continue
		}
		value := strings.TrimSpace(line[idx+1:])
		result[key] = unquoteEnvValue(value)
	}
	if len(result) == 0 && strings.TrimSpace(raw) != "" {
		return nil, fmt.Errorf("no valid KEY=VALUE pairs found")
	}
	return result, nil
}

// unquoteEnvValue strips a matching pair of surrounding single or double
// quotes from a .env value, with no further escape processing.
func unquoteEnvValue(value string) string {
	if len(value) >= 2 {
		first, last := value[0], value[len(value)-1]
		if (first == '"' && last == '"') || (first == '\'' && last == '\'') {
			return value[1 : len(value)-1]
		}
	}
	return value
}

// HTMLSummary is a structural summary of an HTML document: title, meta
// tags, headings, links, images, and form count. It is a summary, not a
// lossy full-DOM-to-map conversion.
type HTMLSummary struct {
	Title     string            `json:"title"`
	Meta      map[string]string `json:"meta"`
	Headings  []HTMLHeading     `json:"headings"`
	Links     []string          `json:"links"`
	Images    []string          `json:"images"`
	FormCount int               `json:"form_count"`
}

// HTMLHeading is a single heading (<h1>-<h6>) extracted from an HTML
// document.
type HTMLHeading struct {
	Level int    `json:"level"`
	Text  string `json:"text"`
}

// ParseHTML walks a parsed HTML document tree and returns a structural
// summary: title, meta name/property -> content pairs, headings, deduped
// links and images in document order, and a form count.
func (s *Service) ParseHTML(raw string) (*HTMLSummary, error) {
	doc, err := html.Parse(strings.NewReader(raw))
	if err != nil {
		return nil, err
	}

	summary := &HTMLSummary{Meta: make(map[string]string)}
	seenLinks := make(map[string]bool)
	seenImages := make(map[string]bool)

	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode {
			switch n.Data {
			case "title":
				if summary.Title == "" && n.FirstChild != nil {
					summary.Title = strings.TrimSpace(htmlText(n))
				}
			case "meta":
				name := htmlAttr(n, "name")
				if name == "" {
					name = htmlAttr(n, "property")
				}
				content := htmlAttr(n, "content")
				if name != "" && content != "" {
					summary.Meta[name] = content
				}
			case "h1", "h2", "h3", "h4", "h5", "h6":
				level, _ := strconv.Atoi(n.Data[1:])
				summary.Headings = append(summary.Headings, HTMLHeading{
					Level: level,
					Text:  strings.TrimSpace(htmlText(n)),
				})
			case "a":
				if href := htmlAttr(n, "href"); href != "" && !seenLinks[href] {
					seenLinks[href] = true
					summary.Links = append(summary.Links, href)
				}
			case "img":
				if src := htmlAttr(n, "src"); src != "" && !seenImages[src] {
					seenImages[src] = true
					summary.Images = append(summary.Images, src)
				}
			case "form":
				summary.FormCount++
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)

	return summary, nil
}

// htmlAttr returns the value of attribute name on node n, or "" if absent.
func htmlAttr(n *html.Node, name string) string {
	for _, a := range n.Attr {
		if a.Key == name {
			return a.Val
		}
	}
	return ""
}

// htmlText concatenates all text node content under n.
func htmlText(n *html.Node) string {
	if n.Type == html.TextNode {
		return n.Data
	}
	var sb strings.Builder
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		sb.WriteString(htmlText(c))
	}
	return sb.String()
}

// ParseINI parses a common subset of INI documents: lines starting with
// ";" or "#" (after trimming) are comments, "[section]" lines start a new
// section (with "" as the key for content appearing before any section
// header), and "key=value" (or "key = value") lines set a key within the
// current section. Nested sections and multi-line values are not
// supported. An error is only returned when the input is non-empty after
// trimming but produces zero sections or key=value pairs.
func (s *Service) ParseINI(raw string) (map[string]map[string]string, error) {
	result := make(map[string]map[string]string)
	section := ""

	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, ";") || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			section = strings.TrimSpace(line[1 : len(line)-1])
			if _, ok := result[section]; !ok {
				result[section] = make(map[string]string)
			}
			continue
		}
		idx := strings.Index(line, "=")
		if idx < 0 {
			continue
		}
		key := strings.TrimSpace(line[:idx])
		if key == "" {
			continue
		}
		value := strings.TrimSpace(line[idx+1:])
		if _, ok := result[section]; !ok {
			result[section] = make(map[string]string)
		}
		result[section][key] = value
	}

	if len(result) == 0 && strings.TrimSpace(raw) != "" {
		return nil, fmt.Errorf("no valid INI sections or key=value pairs found")
	}
	return result, nil
}

// logTimeLayouts are the fixed-width timestamp layouts tried, in order,
// against the start of each log line.
var logTimeLayouts = []string{
	time.RFC3339,
	"2006-01-02 15:04:05",
	"Jan _2 15:04:05",
	"[02/Jan/2006:15:04:05 -0700]",
}

// logLevelTokens maps a recognized level token (uppercase) to its
// canonical uppercase form.
var logLevelTokens = map[string]string{
	"TRACE":   "TRACE",
	"DEBUG":   "DEBUG",
	"INFO":    "INFO",
	"WARN":    "WARN",
	"WARNING": "WARN",
	"ERROR":   "ERROR",
	"ERR":     "ERROR",
	"FATAL":   "FATAL",
	"PANIC":   "PANIC",
}

// logLevelRe matches the first whole-word, case-insensitive level token.
var logLevelRe = regexp.MustCompile(`(?i)\b(TRACE|DEBUG|INFO|WARN|WARNING|ERROR|ERR|FATAL|PANIC)\b`)

// LogEntry is one best-effort parsed line from a log file.
type LogEntry struct {
	Raw       string     `json:"raw"`
	Timestamp *time.Time `json:"timestamp,omitempty"`
	Level     string     `json:"level,omitempty"`
	Message   string     `json:"message"`
}

// ParseLogLines applies best-effort heuristic extraction to each non-blank
// line of a log file: a leading timestamp is matched against a small set
// of common layouts, then the first whole-word level token found in the
// remaining text sets Level and the text after it becomes Message. Lines
// with no recognizable timestamp or level are never rejected — Message
// falls back to the remaining (or full raw) text. Only an entirely empty
// input is an error.
func (s *Service) ParseLogLines(raw string) ([]LogEntry, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, fmt.Errorf("input is empty")
	}

	var entries []LogEntry
	for _, line := range strings.Split(raw, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		entries = append(entries, parseLogLine(line))
	}
	return entries, nil
}

// parseLogLine extracts an optional timestamp and level from a single log
// line, returning the remaining text as Message.
func parseLogLine(line string) LogEntry {
	entry := LogEntry{Raw: line, Message: line}
	remaining := line

	for _, layout := range logTimeLayouts {
		n := len(layout)
		if n > len(remaining) {
			continue
		}
		if t, err := time.Parse(layout, remaining[:n]); err == nil {
			entry.Timestamp = &t
			remaining = strings.TrimSpace(remaining[n:])
			break
		}
	}

	entry.Message = remaining
	if loc := logLevelRe.FindStringIndex(remaining); loc != nil {
		token := strings.ToUpper(remaining[loc[0]:loc[1]])
		entry.Level = logLevelTokens[token]
		entry.Message = strings.TrimSpace(remaining[loc[1]:])
	}

	return entry
}

// mdHeadingLineRe matches ATX-style Markdown headings ("# Title").
var mdHeadingLineRe = regexp.MustCompile(`(?m)^(#{1,6})\s+(.+)$`)

// mdLinkRe matches an inline Markdown link "[text](url)".
var mdLinkRe = regexp.MustCompile(`\[([^\]]*)\]\(([^)]*)\)`)

// mdCodeBlockRe matches a fenced ``` code block, capturing an optional
// language token and the code body.
var mdCodeBlockRe = regexp.MustCompile("(?s)```([a-zA-Z0-9_+-]*)\n(.*?)```")

// MarkdownHeading is a single ATX-style heading extracted from Markdown.
type MarkdownHeading struct {
	Level int    `json:"level"`
	Text  string `json:"text"`
}

// MarkdownLink is a single inline "[text](url)" link extracted from
// Markdown. Reference-style links ("[text][ref]") are not supported.
type MarkdownLink struct {
	Text string `json:"text"`
	URL  string `json:"url"`
}

// MarkdownCodeBlock is a single fenced code block extracted from
// Markdown.
type MarkdownCodeBlock struct {
	Language string `json:"language,omitempty"`
	Code     string `json:"code"`
}

// MarkdownStructure is the structural extraction result of
// ParseMarkdownStructure.
type MarkdownStructure struct {
	Headings   []MarkdownHeading   `json:"headings"`
	Links      []MarkdownLink      `json:"links"`
	CodeBlocks []MarkdownCodeBlock `json:"code_blocks"`
}

// ParseMarkdownStructure extracts document structure (ATX headings, inline
// "[text](url)" links, and fenced ``` code blocks) from Markdown as data.
// This is distinct in purpose from MarkdownToHTML/MarkdownTOC in the text
// service, which convert/render Markdown rather than extracting its
// structure. Reference-style links and non-fenced (indented) code blocks
// are not supported.
func (s *Service) ParseMarkdownStructure(raw string) (*MarkdownStructure, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, fmt.Errorf("input is empty")
	}

	structure := &MarkdownStructure{}

	for _, m := range mdHeadingLineRe.FindAllStringSubmatch(raw, -1) {
		structure.Headings = append(structure.Headings, MarkdownHeading{
			Level: len(m[1]),
			Text:  strings.TrimSpace(m[2]),
		})
	}

	for _, m := range mdLinkRe.FindAllStringSubmatch(raw, -1) {
		structure.Links = append(structure.Links, MarkdownLink{Text: m[1], URL: m[2]})
	}

	for _, m := range mdCodeBlockRe.FindAllStringSubmatch(raw, -1) {
		structure.CodeBlocks = append(structure.CodeBlocks, MarkdownCodeBlock{
			Language: m[1],
			Code:     m[2],
		})
	}

	return structure, nil
}

// sqlFirstWordRe matches the first keyword token of a SQL statement.
var sqlFirstWordRe = regexp.MustCompile(`(?i)^\s*([a-zA-Z]+)`)

// sqlTableRe matches a table name following FROM, INTO, UPDATE, or JOIN,
// with an optional pair of surrounding backticks.
var sqlTableRe = regexp.MustCompile("(?i)\\b(?:FROM|INTO|UPDATE|JOIN)\\s+`?([a-zA-Z_][a-zA-Z0-9_.]*)`?")

// sqlSelectRe captures the column list between SELECT and the first FROM.
var sqlSelectRe = regexp.MustCompile(`(?is)^\s*SELECT\s+(.*?)\s+FROM\b`)

// SQLStructure is the best-effort structural extraction result of
// ParseSQLStructure.
type SQLStructure struct {
	StatementType string   `json:"statement_type"`
	Tables        []string `json:"tables"`
	Columns       []string `json:"columns,omitempty"`
}

// ParseSQLStructure performs best-effort structural extraction (statement
// type, referenced tables, and SELECT column list) from a single SQL
// statement. This is NOT a real SQL parser or validator and is not
// dialect-aware — it never rejects input as invalid SQL syntax, it only
// reports what it can confidently extract; an unrecognized statement type
// yields "UNKNOWN" rather than an error.
func (s *Service) ParseSQLStructure(raw string) (*SQLStructure, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, fmt.Errorf("input is empty")
	}

	structure := &SQLStructure{StatementType: "UNKNOWN"}

	if m := sqlFirstWordRe.FindStringSubmatch(raw); m != nil {
		switch strings.ToUpper(m[1]) {
		case "SELECT", "INSERT", "UPDATE", "DELETE", "CREATE", "ALTER", "DROP":
			structure.StatementType = strings.ToUpper(m[1])
		}
	}

	seenTables := make(map[string]bool)
	for _, m := range sqlTableRe.FindAllStringSubmatch(raw, -1) {
		if !seenTables[m[1]] {
			seenTables[m[1]] = true
			structure.Tables = append(structure.Tables, m[1])
		}
	}

	if structure.StatementType == "SELECT" {
		if m := sqlSelectRe.FindStringSubmatch(raw); m != nil {
			columnList := strings.TrimSpace(m[1])
			if columnList != "*" {
				for _, col := range strings.Split(columnList, ",") {
					structure.Columns = append(structure.Columns, strings.TrimSpace(col))
				}
			}
		}
	}

	return structure, nil
}

// ParseTOML parses a limited subset of TOML: top-level "key = value" pairs
// appearing before any table header populate the root map; "[table]" and
// "[table.subtable]" headers (dot-separated) create/select nested tables
// by path; subsequent "key = value" lines set keys in the current table.
// Quoted strings, booleans (true/false), integers, floats, and single-line
// arrays ("[...]", a simple top-level comma split with no nested-array
// support) are supported. Blank lines and "#"-prefixed comments are
// skipped. Multi-line strings, inline tables ("{...}"), dates, and arrays
// of tables ("[[...]]") are NOT supported.
func (s *Service) ParseTOML(raw string) (map[string]interface{}, error) {
	root := make(map[string]interface{})
	current := root

	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			current = tomlTable(root, strings.TrimSpace(line[1:len(line)-1]))
			continue
		}
		idx := strings.Index(line, "=")
		if idx < 0 {
			continue
		}
		key := strings.TrimSpace(line[:idx])
		if key == "" {
			continue
		}
		current[key] = tomlValue(strings.TrimSpace(line[idx+1:]))
	}

	return root, nil
}

// tomlTable walks (creating as needed) a dot-separated table path from
// root, returning the nested map for the final path segment.
func tomlTable(root map[string]interface{}, path string) map[string]interface{} {
	current := root
	for _, part := range strings.Split(path, ".") {
		part = strings.TrimSpace(part)
		next, ok := current[part].(map[string]interface{})
		if !ok {
			next = make(map[string]interface{})
			current[part] = next
		}
		current = next
	}
	return current
}

// tomlValue parses a single TOML scalar or simple array value according to
// the supported subset documented on ParseTOML.
func tomlValue(value string) interface{} {
	if len(value) >= 2 && value[0] == '"' && value[len(value)-1] == '"' {
		return value[1 : len(value)-1]
	}
	if value == "true" {
		return true
	}
	if value == "false" {
		return false
	}
	if len(value) >= 2 && value[0] == '[' && value[len(value)-1] == ']' {
		inner := strings.TrimSpace(value[1 : len(value)-1])
		if inner == "" {
			return []interface{}{}
		}
		parts := strings.Split(inner, ",")
		result := make([]interface{}, 0, len(parts))
		for _, part := range parts {
			result = append(result, tomlValue(strings.TrimSpace(part)))
		}
		return result
	}
	if i, err := strconv.ParseInt(value, 10, 64); err == nil {
		return i
	}
	if f, err := strconv.ParseFloat(value, 64); err == nil {
		return f
	}
	return value
}

// ParseYAML parses a YAML document into a generic map using
// gopkg.in/yaml.v3, which natively decodes mappings into
// map[string]interface{} (unlike yaml.v2's map[interface{}]interface{}).
func (s *Service) ParseYAML(raw string) (map[string]interface{}, error) {
	var out map[string]interface{}
	if err := yaml.Unmarshal([]byte(raw), &out); err != nil {
		return nil, err
	}
	return out, nil
}
