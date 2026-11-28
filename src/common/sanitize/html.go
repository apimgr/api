package sanitize

import (
	"bytes"
	"strings"

	"golang.org/x/net/html"
)

// SanitizeHTMLResponse keeps the HTML element structure while sanitizing
// rendered text and attribute values. Script and style contents are left
// intact because rewriting source code can change executable behavior.
func SanitizeHTMLResponse(body []byte, production bool) ([]byte, error) {
	document, err := html.Parse(bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	var walk func(*html.Node)
	walk = func(node *html.Node) {
		if node.Type == html.ElementNode {
			attrs := node.Attr[:0]
			for _, attr := range node.Attr {
				if production && strings.EqualFold(attr.Key, "dev_only") {
					continue
				}
				attrs = append(attrs, html.Attribute{Namespace: attr.Namespace, Key: attr.Key, Val: SanitizeResponseText(attr.Val, attr.Key)})
			}
			node.Attr = attrs
			if node.Data == "script" || node.Data == "style" {
				if node.Data == "script" && strings.EqualFold(attributeValue(node, "type"), "application/ld+json") && node.FirstChild != nil && node.FirstChild.Type == html.TextNode {
					if sanitized, err := SanitizeResponseJSON([]byte(node.FirstChild.Data), production); err == nil {
						node.FirstChild.Data = strings.TrimSuffix(string(sanitized), "\n")
					}
				}
				return
			}
		}
		if node.Type == html.TextNode {
			node.Data = SanitizeResponseText(node.Data, "")
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(document)
	var output bytes.Buffer
	if err := html.Render(&output, document); err != nil {
		return nil, err
	}
	return output.Bytes(), nil
}

func attributeValue(node *html.Node, name string) string {
	for _, attr := range node.Attr {
		if strings.EqualFold(attr.Key, name) {
			return attr.Val
		}
	}
	return ""
}
