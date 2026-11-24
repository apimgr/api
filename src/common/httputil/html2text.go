package httputil

import (
	"fmt"
	"html"
	"regexp"
	"strings"
	"unicode/utf8"

	nethtml "golang.org/x/net/html"
)

var tagStripPattern = regexp.MustCompile(`<[^>]*>`)
var linkReferencePattern = regexp.MustCompile(`(\S+)\s+(\[[^\]]+\])`)

// HTML2TextConverter renders a complete HTML response as readable terminal
// text. Forms and executable or decorative content are omitted.
func HTML2TextConverter(source string, width int) string {
	if width < 1 {
		width = 80
	}
	if width > 200 {
		width = 200
	}
	doc, err := nethtml.Parse(strings.NewReader(source))
	if err != nil {
		return strings.TrimSpace(html.UnescapeString(tagStripPattern.ReplaceAllString(source, ""))) + "\n"
	}
	var out strings.Builder
	convertHTMLNode(&out, doc, width, 0)
	text := strings.TrimSpace(out.String())
	if text == "" {
		return "\n"
	}
	return text + "\n"
}

func convertHTMLNode(out *strings.Builder, node *nethtml.Node, width, indent int) {
	if node.Type == nethtml.ElementNode {
		switch node.Data {
		case "script", "style", "form", "button", "input", "textarea", "select", "option", "noscript", "svg", "canvas", "iframe":
			return
		case "h1":
			text := nodeText(node)
			if text != "" {
				line := strings.Repeat("═", width)
				fmt.Fprintf(out, "%s\n%s\n%s\n\n", line, centerText(strings.ToUpper(text), width), line)
			}
			return
		case "h2":
			if text := nodeText(node); text != "" {
				fmt.Fprintf(out, "─── %s ───\n\n", text)
			}
			return
		case "h3":
			if text := nodeText(node); text != "" {
				fmt.Fprintf(out, "► %s\n\n", text)
			}
			return
		case "p":
			if text := inlineText(node); text != "" {
				fmt.Fprintf(out, "%s\n\n", wordWrap(text, width-indent))
			}
			return
		case "ul", "ol":
			convertList(out, node, width, indent, node.Data == "ol")
			return
		case "li":
			if text := nodeText(node); text != "" {
				fmt.Fprintf(out, "%s• %s\n", strings.Repeat(" ", indent), text)
			}
			return
		case "a":
			text := nodeText(node)
			href := nodeAttribute(node, "href")
			if text != "" {
				if href != "" && href != text {
					fmt.Fprintf(out, "%s [%s]", text, href)
				} else {
					out.WriteString(text)
				}
			}
			return
		case "strong", "b":
			if text := nodeText(node); text != "" {
				fmt.Fprintf(out, "*%s*", text)
			}
			return
		case "em", "i":
			if text := nodeText(node); text != "" {
				fmt.Fprintf(out, "_%s_", text)
			}
			return
		case "code":
			fmt.Fprintf(out, "`%s`", nodeText(node))
			return
		case "pre":
			for _, line := range strings.Split(strings.TrimRight(nodeText(node), "\n"), "\n") {
				fmt.Fprintf(out, "    %s\n", line)
			}
			out.WriteByte('\n')
			return
		case "table":
			convertTable(out, node, width)
			return
		case "hr":
			fmt.Fprintf(out, "%s\n\n", strings.Repeat("─", width))
			return
		case "blockquote":
			for _, line := range strings.Split(nodeText(node), "\n") {
				fmt.Fprintf(out, "│ %s\n", line)
			}
			out.WriteByte('\n')
			return
		case "br":
			out.WriteByte('\n')
			return
		case "tr", "td", "th":
			return
		}
	}
	if node.Type == nethtml.TextNode {
		text := strings.Join(strings.Fields(node.Data), " ")
		if text != "" {
			if out.Len() > 0 {
				last := out.String()[out.Len()-1]
				if last != ' ' && last != '\n' {
					out.WriteByte(' ')
				}
			}
			out.WriteString(text)
		}
		return
	}
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		convertHTMLNode(out, child, width, indent)
	}
}

func inlineText(node *nethtml.Node) string {
	var render func(*nethtml.Node) string
	render = func(current *nethtml.Node) string {
		if current.Type == nethtml.TextNode {
			return current.Data
		}
		if current.Type == nethtml.ElementNode {
			switch current.Data {
			case "script", "style", "form", "button", "input", "textarea", "select", "option", "noscript", "svg", "canvas", "iframe":
				return ""
			case "br":
				return " "
			}
		}
		var content strings.Builder
		for child := current.FirstChild; child != nil; child = child.NextSibling {
			content.WriteString(render(child))
		}
		text := content.String()
		switch current.Data {
		case "strong", "b":
			return "*" + text + "*"
		case "em", "i":
			return "_" + text + "_"
		case "code":
			return "`" + text + "`"
		case "a":
			href := nodeAttribute(current, "href")
			if href != "" && strings.TrimSpace(text) != href {
				return text + " [" + href + "]"
			}
		}
		return text
	}
	return strings.Join(strings.Fields(render(node)), " ")
}

func convertList(out *strings.Builder, node *nethtml.Node, width, indent int, ordered bool) {
	index := 0
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		if child.Type != nethtml.ElementNode || child.Data != "li" {
			continue
		}
		index++
		text := nodeText(child)
		marker := "•"
		if ordered {
			marker = fmt.Sprintf("%d.", index)
		}
		prefix := strings.Repeat(" ", indent+2) + marker + " "
		wrapped := wordWrap(text, width-utf8.RuneCountInString(prefix))
		lines := strings.Split(wrapped, "\n")
		for i, line := range lines {
			if i == 0 {
				out.WriteString(prefix)
			} else {
				out.WriteString(strings.Repeat(" ", utf8.RuneCountInString(prefix)))
			}
			out.WriteString(line)
			out.WriteByte('\n')
		}
	}
	out.WriteByte('\n')
}

func convertTable(out *strings.Builder, table *nethtml.Node, width int) {
	rows := make([][]string, 0)
	var collectRows func(*nethtml.Node)
	collectRows = func(node *nethtml.Node) {
		if node.Type == nethtml.ElementNode && node.Data == "tr" {
			row := make([]string, 0)
			for cell := node.FirstChild; cell != nil; cell = cell.NextSibling {
				if cell.Type == nethtml.ElementNode && (cell.Data == "td" || cell.Data == "th") {
					row = append(row, nodeText(cell))
				}
			}
			if len(row) > 0 {
				rows = append(rows, row)
			}
			return
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			collectRows(child)
		}
	}
	collectRows(table)
	if len(rows) == 0 {
		return
	}
	columns := 0
	for _, row := range rows {
		if len(row) > columns {
			columns = len(row)
		}
	}
	if columns == 0 {
		return
	}
	colWidth := (width - columns - 1) / columns
	if colWidth < 4 {
		colWidth = 4
	}
	separator := strings.Repeat("─", colWidth+2)
	for rowIndex, row := range rows {
		if rowIndex == 0 {
			writeTableBorder(out, separator, columns, "┬", "┌", "┐")
		}
		out.WriteString("│")
		for col := 0; col < columns; col++ {
			cell := ""
			if col < len(row) {
				cell = wordWrap(row[col], colWidth)
			}
			cell = strings.ReplaceAll(cell, "\n", " ")
			fmt.Fprintf(out, " %-*s │", colWidth, cell)
		}
		out.WriteByte('\n')
		if rowIndex == 0 {
			writeTableBorder(out, separator, columns, "┼", "├", "┤")
		}
	}
	writeTableBorder(out, separator, columns, "┴", "└", "┘")
	out.WriteByte('\n')
}

func writeTableBorder(out *strings.Builder, segment string, columns int, middle, left, right string) {
	out.WriteString(left)
	for col := 0; col < columns; col++ {
		if col > 0 {
			out.WriteString(middle)
		}
		out.WriteString(segment)
	}
	out.WriteString(right)
	out.WriteByte('\n')
}

func nodeText(node *nethtml.Node) string {
	var out strings.Builder
	var visit func(*nethtml.Node)
	visit = func(current *nethtml.Node) {
		if current.Type == nethtml.TextNode {
			out.WriteString(current.Data)
			out.WriteByte(' ')
			return
		}
		if current.Type == nethtml.ElementNode {
			switch current.Data {
			case "script", "style", "form", "button", "input", "textarea", "select", "option", "noscript", "svg", "canvas", "iframe":
				return
			}
		}
		for child := current.FirstChild; child != nil; child = child.NextSibling {
			visit(child)
		}
	}
	visit(node)
	return strings.Join(strings.Fields(out.String()), " ")
}

func nodeAttribute(node *nethtml.Node, key string) string {
	for _, attr := range node.Attr {
		if strings.EqualFold(attr.Key, key) {
			return strings.TrimSpace(attr.Val)
		}
	}
	return ""
}

func wordWrap(text string, width int) string {
	if width < 1 {
		width = 1
	}
	text = linkReferencePattern.ReplaceAllString(text, "$1\uE000$2")
	words := strings.Fields(text)
	var lines []string
	line := ""
	for _, word := range words {
		wordWidth := utf8.RuneCountInString(word)
		if line == "" {
			line = word
			continue
		}
		if utf8.RuneCountInString(line)+1+wordWidth > width {
			lines = append(lines, line)
			line = word
			continue
		}
		line += " " + word
	}
	if line != "" {
		lines = append(lines, line)
	}
	return strings.ReplaceAll(strings.Join(lines, "\n"), "\uE000", " ")
}

func centerText(text string, width int) string {
	length := utf8.RuneCountInString(text)
	if length >= width {
		return text
	}
	left := (width - length) / 2
	return strings.Repeat(" ", left) + text
}
