package httputil

import (
	"bytes"
	"strings"

	nethtml "golang.org/x/net/html"
)

// RenderNoJSHTML removes executable scripts and inline event handlers from
// already server-rendered HTML so terminal browsers can navigate and submit
// ordinary forms without JavaScript.
func RenderNoJSHTML(source string) string {
	doc, err := nethtml.Parse(strings.NewReader(source))
	if err != nil {
		return source
	}
	var clean func(*nethtml.Node)
	clean = func(parent *nethtml.Node) {
		for child := parent.FirstChild; child != nil; {
			next := child.NextSibling
			if child.Type == nethtml.ElementNode && child.Data == "script" {
				parent.RemoveChild(child)
				child = next
				continue
			}
			if child.Type == nethtml.ElementNode {
				attrs := child.Attr[:0]
				for _, attr := range child.Attr {
					if strings.HasPrefix(strings.ToLower(attr.Key), "on") ||
						(strings.EqualFold(attr.Key, "href") && strings.HasPrefix(strings.TrimSpace(strings.ToLower(attr.Val)), "javascript:")) {
						continue
					}
					attrs = append(attrs, attr)
				}
				child.Attr = attrs
			}
			clean(child)
			child = next
		}
	}
	clean(doc)
	var output bytes.Buffer
	if err := nethtml.Render(&output, doc); err != nil {
		return source
	}
	return output.String()
}
