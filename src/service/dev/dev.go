package dev

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"regexp"
	"strings"
)

// Service provides development utilities
type Service struct{}

// New creates a new Dev service
func New() *Service {
	return &Service{}
}

// Code formatting
func (s *Service) FormatJSON(jsonStr string) (string, error) {
	var obj interface{}
	if err := json.Unmarshal([]byte(jsonStr), &obj); err != nil {
		return "", err
	}
	formatted, err := json.MarshalIndent(obj, "", "  ")
	if err != nil {
		return "", err
	}
	return string(formatted), nil
}

func (s *Service) MinifyJSON(jsonStr string) (string, error) {
	var obj interface{}
	if err := json.Unmarshal([]byte(jsonStr), &obj); err != nil {
		return "", err
	}
	minified, err := json.Marshal(obj)
	if err != nil {
		return "", err
	}
	return string(minified), nil
}

// String manipulation for code
func (s *Service) ToCamelCase(str string) string {
	words := strings.FieldsFunc(str, func(r rune) bool {
		return r == '_' || r == '-' || r == ' '
	})

	if len(words) == 0 {
		return ""
	}

	result := strings.ToLower(words[0])
	for _, word := range words[1:] {
		if len(word) > 0 {
			result += strings.ToUpper(string(word[0])) + strings.ToLower(word[1:])
		}
	}
	return result
}

func (s *Service) ToPascalCase(str string) string {
	words := strings.FieldsFunc(str, func(r rune) bool {
		return r == '_' || r == '-' || r == ' '
	})

	var result string
	for _, word := range words {
		if len(word) > 0 {
			result += strings.ToUpper(string(word[0])) + strings.ToLower(word[1:])
		}
	}
	return result
}

func (s *Service) ToSnakeCase(str string) string {
	// Handle camelCase and PascalCase
	re := regexp.MustCompile("([a-z0-9])([A-Z])")
	str = re.ReplaceAllString(str, "${1}_${2}")

	// Handle spaces and hyphens
	str = strings.ReplaceAll(str, " ", "_")
	str = strings.ReplaceAll(str, "-", "_")

	// Convert to lowercase
	return strings.ToLower(str)
}

func (s *Service) ToKebabCase(str string) string {
	// Handle camelCase and PascalCase
	re := regexp.MustCompile("([a-z0-9])([A-Z])")
	str = re.ReplaceAllString(str, "${1}-${2}")

	// Handle spaces and underscores
	str = strings.ReplaceAll(str, " ", "-")
	str = strings.ReplaceAll(str, "_", "-")

	// Convert to lowercase
	return strings.ToLower(str)
}

func (s *Service) ToConstantCase(str string) string {
	return strings.ToUpper(s.ToSnakeCase(str))
}

// Code escaping
func (s *Service) EscapeHTML(str string) string {
	replacer := strings.NewReplacer(
		"&", "&amp;",
		"<", "&lt;",
		">", "&gt;",
		"\"", "&quot;",
		"'", "&#39;",
	)
	return replacer.Replace(str)
}

func (s *Service) UnescapeHTML(str string) string {
	replacer := strings.NewReplacer(
		"&amp;", "&",
		"&lt;", "<",
		"&gt;", ">",
		"&quot;", "\"",
		"&#39;", "'",
		"&apos;", "'",
	)
	return replacer.Replace(str)
}

func (s *Service) EscapeSQL(str string) string {
	return strings.ReplaceAll(str, "'", "''")
}

func (s *Service) EscapeRegex(str string) string {
	// Backslash must be escaped first: escaping it after the other
	// special characters would re-escape the backslashes those earlier
	// substitutions just introduced, corrupting the output.
	special := []string{"\\", ".", "+", "*", "?", "^", "$", "(", ")", "[", "]", "{", "}", "|"}
	result := str
	for _, char := range special {
		result = strings.ReplaceAll(result, char, "\\"+char)
	}
	return result
}

// Comment formatting
func (s *Service) AddLineComments(code string, commentStyle string) string {
	lines := strings.Split(code, "\n")
	var result []string

	for _, line := range lines {
		if strings.TrimSpace(line) != "" {
			result = append(result, commentStyle+" "+line)
		} else {
			result = append(result, line)
		}
	}

	return strings.Join(result, "\n")
}

func (s *Service) RemoveLineComments(code string, commentStyle string) string {
	lines := strings.Split(code, "\n")
	var result []string

	prefix := commentStyle + " "
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, prefix) {
			result = append(result, strings.TrimPrefix(trimmed, prefix))
		} else {
			result = append(result, line)
		}
	}

	return strings.Join(result, "\n")
}

// Indentation
func (s *Service) Indent(code string, spaces int) string {
	indent := strings.Repeat(" ", spaces)
	lines := strings.Split(code, "\n")
	var result []string

	for _, line := range lines {
		if strings.TrimSpace(line) != "" {
			result = append(result, indent+line)
		} else {
			result = append(result, line)
		}
	}

	return strings.Join(result, "\n")
}

func (s *Service) Dedent(code string, spaces int) string {
	prefix := strings.Repeat(" ", spaces)
	lines := strings.Split(code, "\n")
	var result []string

	for _, line := range lines {
		if strings.HasPrefix(line, prefix) {
			result = append(result, strings.TrimPrefix(line, prefix))
		} else {
			result = append(result, line)
		}
	}

	return strings.Join(result, "\n")
}

// Template string
func (s *Service) TemplateReplace(template string, values map[string]string) string {
	result := template
	for key, value := range values {
		placeholder := fmt.Sprintf("{{%s}}", key)
		result = strings.ReplaceAll(result, placeholder, value)
	}
	return result
}

// Line operations
func (s *Service) CountLines(code string) int {
	return len(strings.Split(code, "\n"))
}

func (s *Service) RemoveEmptyLines(code string) string {
	lines := strings.Split(code, "\n")
	var result []string

	for _, line := range lines {
		if strings.TrimSpace(line) != "" {
			result = append(result, line)
		}
	}

	return strings.Join(result, "\n")
}

func (s *Service) NumberLines(code string) string {
	lines := strings.Split(code, "\n")
	var result []string

	for i, line := range lines {
		result = append(result, fmt.Sprintf("%4d | %s", i+1, line))
	}

	return strings.Join(result, "\n")
}

// FormatCSS re-indents a CSS document by brace depth. This is a pragmatic
// whitespace normalizer, not a full CSS AST parser: it walks the raw text
// character by character, breaking a new line on every "{", "}", and ";",
// and indenting two spaces per open brace depth.
func (s *Service) FormatCSS(css string) string {
	var out strings.Builder
	var cur strings.Builder
	depth := 0

	flush := func() {
		trimmed := strings.TrimSpace(cur.String())
		if trimmed != "" {
			out.WriteString(strings.Repeat("  ", depth))
			out.WriteString(trimmed)
			out.WriteString("\n")
		}
		cur.Reset()
	}

	for _, r := range css {
		switch r {
		case '{':
			trimmed := strings.TrimSpace(cur.String())
			out.WriteString(strings.Repeat("  ", depth))
			out.WriteString(trimmed)
			out.WriteString(" {\n")
			cur.Reset()
			depth++
		case '}':
			flush()
			if depth > 0 {
				depth--
			}
			out.WriteString(strings.Repeat("  ", depth))
			out.WriteString("}\n")
		case ';':
			cur.WriteRune(';')
			flush()
		default:
			cur.WriteRune(r)
		}
	}
	flush()

	return strings.TrimRight(out.String(), "\n") + "\n"
}

// MinifyCSS strips comments and collapses all whitespace, including the
// whitespace immediately surrounding structural punctuation.
func (s *Service) MinifyCSS(css string) string {
	commentRe := regexp.MustCompile(`/\*[\s\S]*?\*/`)
	css = commentRe.ReplaceAllString(css, "")

	whitespaceRe := regexp.MustCompile(`\s+`)
	css = whitespaceRe.ReplaceAllString(strings.TrimSpace(css), " ")

	punctRe := regexp.MustCompile(`\s*([{}:;,])\s*`)
	css = punctRe.ReplaceAllString(css, "$1")

	return strings.TrimSuffix(css, ";")
}

// htmlVoidElements holds the HTML5 void elements, which never receive a
// matching closing tag and therefore never increase indent depth.
var htmlVoidElements = map[string]bool{
	"area": true, "base": true, "br": true, "col": true,
	"embed": true, "hr": true, "img": true, "input": true,
	"link": true, "meta": true, "param": true, "source": true,
	"track": true, "wbr": true,
}

// htmlTagNameRe extracts the tag name from an opening or closing HTML tag.
var htmlTagNameRe = regexp.MustCompile(`^<\/?([a-zA-Z][a-zA-Z0-9-]*)`)

// htmlTagName returns the lowercased tag name of a tag token, or "" if the
// token is not a recognizable tag (e.g. a comment or doctype).
func htmlTagName(tok string) string {
	m := htmlTagNameRe.FindStringSubmatch(tok)
	if len(m) > 1 {
		return strings.ToLower(m[1])
	}
	return ""
}

// FormatHTML re-indents an HTML document by tag depth. This is a pragmatic
// whitespace normalizer, not a full HTML5 parser: it tokenizes the document
// into tags and text runs, indenting two spaces per open (non-void, non-
// self-closing) tag depth.
func (s *Service) FormatHTML(html string) string {
	tokenRe := regexp.MustCompile(`<[^>]+>|[^<]+`)
	tokens := tokenRe.FindAllString(html, -1)

	var out strings.Builder
	depth := 0

	for _, tok := range tokens {
		switch {
		case strings.HasPrefix(tok, "</"):
			if depth > 0 {
				depth--
			}
			out.WriteString(strings.Repeat("  ", depth))
			out.WriteString(strings.TrimSpace(tok))
			out.WriteString("\n")
		case strings.HasPrefix(tok, "<!"):
			out.WriteString(strings.Repeat("  ", depth))
			out.WriteString(strings.TrimSpace(tok))
			out.WriteString("\n")
		case strings.HasPrefix(tok, "<"):
			out.WriteString(strings.Repeat("  ", depth))
			out.WriteString(strings.TrimSpace(tok))
			out.WriteString("\n")
			if !strings.HasSuffix(tok, "/>") && !htmlVoidElements[htmlTagName(tok)] {
				depth++
			}
		default:
			trimmed := strings.TrimSpace(tok)
			if trimmed != "" {
				out.WriteString(strings.Repeat("  ", depth))
				out.WriteString(trimmed)
				out.WriteString("\n")
			}
		}
	}

	return strings.TrimRight(out.String(), "\n") + "\n"
}

// MinifyHTML strips comments and collapses whitespace between and within
// tags, including the whitespace runs left over between elements.
func (s *Service) MinifyHTML(html string) string {
	commentRe := regexp.MustCompile(`<!--[\s\S]*?-->`)
	html = commentRe.ReplaceAllString(html, "")

	betweenTagsRe := regexp.MustCompile(`>\s+<`)
	html = betweenTagsRe.ReplaceAllString(html, "><")

	whitespaceRe := regexp.MustCompile(`\s+`)
	html = whitespaceRe.ReplaceAllString(strings.TrimSpace(html), " ")

	return html
}

// FormatJS re-indents a JavaScript source by brace depth. This is a
// pragmatic whitespace normalizer, not a full JS parser: it walks the raw
// text rune by rune, tracking single/double/backtick string literals so
// braces and semicolons inside strings are never treated as structural,
// and breaks a new line on every structural "{", "}", ";", and newline.
func (s *Service) FormatJS(js string) string {
	var out strings.Builder
	var cur strings.Builder
	depth := 0
	inString := false
	var stringChar rune

	flush := func() {
		trimmed := strings.TrimSpace(cur.String())
		if trimmed != "" {
			out.WriteString(strings.Repeat("  ", depth))
			out.WriteString(trimmed)
			out.WriteString("\n")
		}
		cur.Reset()
	}

	runes := []rune(js)
	for i := 0; i < len(runes); i++ {
		r := runes[i]

		if inString {
			cur.WriteRune(r)
			if r == '\\' && i+1 < len(runes) {
				i++
				cur.WriteRune(runes[i])
				continue
			}
			if r == stringChar {
				inString = false
			}
			continue
		}

		switch r {
		case '\'', '"', '`':
			inString = true
			stringChar = r
			cur.WriteRune(r)
		case '{':
			trimmed := strings.TrimSpace(cur.String())
			out.WriteString(strings.Repeat("  ", depth))
			if trimmed != "" {
				out.WriteString(trimmed)
				out.WriteString(" ")
			}
			out.WriteString("{\n")
			cur.Reset()
			depth++
		case '}':
			flush()
			if depth > 0 {
				depth--
			}
			out.WriteString(strings.Repeat("  ", depth))
			out.WriteString("}\n")
		case ';':
			cur.WriteRune(';')
			flush()
		case '\n':
			cur.WriteRune(' ')
		default:
			cur.WriteRune(r)
		}
	}
	flush()

	return strings.TrimRight(out.String(), "\n") + "\n"
}

// MinifyJS strips line and block comments and collapses whitespace, while
// preserving the contents of single/double/backtick string literals
// verbatim (including any comment-like or whitespace-like characters
// inside them).
func (s *Service) MinifyJS(js string) string {
	var out strings.Builder
	inString := false
	var stringChar rune
	lastWasSpace := true

	runes := []rune(js)
	for i := 0; i < len(runes); i++ {
		r := runes[i]

		if inString {
			out.WriteRune(r)
			if r == '\\' && i+1 < len(runes) {
				i++
				out.WriteRune(runes[i])
				continue
			}
			if r == stringChar {
				inString = false
			}
			lastWasSpace = false
			continue
		}

		switch {
		case r == '\'' || r == '"' || r == '`':
			inString = true
			stringChar = r
			out.WriteRune(r)
			lastWasSpace = false
		case r == '/' && i+1 < len(runes) && runes[i+1] == '/':
			for i < len(runes) && runes[i] != '\n' {
				i++
			}
		case r == '/' && i+1 < len(runes) && runes[i+1] == '*':
			i += 2
			for i+1 < len(runes) && !(runes[i] == '*' && runes[i+1] == '/') {
				i++
			}
			i++
		case r == ' ' || r == '\t' || r == '\n' || r == '\r':
			if !lastWasSpace {
				out.WriteRune(' ')
				lastWasSpace = true
			}
		default:
			out.WriteRune(r)
			lastWasSpace = false
		}
	}

	return strings.TrimSpace(out.String())
}

// sqlKeywords lists the clause/join keywords that FormatSQL breaks a new
// line before. Multi-word keywords are ordered longest-first so the regex
// alternation matches "GROUP BY" before a bare "GROUP" could.
var sqlKeywords = []string{
	"UNION ALL", "UNION", "GROUP BY", "ORDER BY",
	"LEFT JOIN", "RIGHT JOIN", "INNER JOIN", "OUTER JOIN",
	"INSERT INTO", "DELETE FROM", "SELECT", "FROM", "WHERE",
	"HAVING", "JOIN", "LIMIT", "OFFSET", "VALUES", "UPDATE", "SET",
}

var sqlKeywordRe = regexp.MustCompile(`(?i)\b(` + strings.Join(sqlKeywords, "|") + `)\b`)
var sqlAndOrRe = regexp.MustCompile(`(?i)\b(AND|OR)\b`)
var sqlWhitespaceRe = regexp.MustCompile(`\s+`)

// FormatSQL breaks a SQL query onto multiple lines by clause keyword, with
// AND/OR conditions indented under their clause. This is a pragmatic
// keyword-based line-breaker, not a full SQL parser.
func (s *Service) FormatSQL(sqlStr string) string {
	collapsed := sqlWhitespaceRe.ReplaceAllString(strings.TrimSpace(sqlStr), " ")

	broken := sqlKeywordRe.ReplaceAllStringFunc(collapsed, func(m string) string {
		return "\n" + strings.ToUpper(m)
	})
	broken = sqlAndOrRe.ReplaceAllStringFunc(broken, func(m string) string {
		return "\n  " + strings.ToUpper(m)
	})

	var result []string
	for _, line := range strings.Split(broken, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		indent := len(line) - len(strings.TrimLeft(line, " "))
		result = append(result, strings.Repeat(" ", indent)+trimmed)
	}

	return strings.Join(result, "\n") + "\n"
}

// isBlankCharData reports whether an XML character-data token is nothing
// but whitespace (the original document's own indentation) so it can be
// dropped before re-encoding with a fresh indent.
func isBlankCharData(tok xml.Token) bool {
	cd, ok := tok.(xml.CharData)
	return ok && len(strings.TrimSpace(string(cd))) == 0
}

// FormatXML re-indents an XML document using a real encoding/xml decode/
// re-encode round-trip: the original document's own whitespace-only text
// nodes are dropped, then every token is re-emitted through xml.Encoder
// with two-space indentation.
func (s *Service) FormatXML(xmlStr string) (string, error) {
	decoder := xml.NewDecoder(strings.NewReader(xmlStr))
	var buf bytes.Buffer
	encoder := xml.NewEncoder(&buf)
	encoder.Indent("", "  ")

	for {
		tok, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", err
		}
		if isBlankCharData(tok) {
			continue
		}
		if err := encoder.EncodeToken(tok); err != nil {
			return "", err
		}
	}
	if err := encoder.Flush(); err != nil {
		return "", err
	}

	return buf.String(), nil
}

// MinifyXML decodes and re-encodes an XML document with no indentation,
// dropping the original document's whitespace-only text nodes.
func (s *Service) MinifyXML(xmlStr string) (string, error) {
	decoder := xml.NewDecoder(strings.NewReader(xmlStr))
	var buf bytes.Buffer
	encoder := xml.NewEncoder(&buf)

	for {
		tok, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", err
		}
		if isBlankCharData(tok) {
			continue
		}
		if err := encoder.EncodeToken(tok); err != nil {
			return "", err
		}
	}
	if err := encoder.Flush(); err != nil {
		return "", err
	}

	return buf.String(), nil
}
