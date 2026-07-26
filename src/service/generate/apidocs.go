package generate

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/apimgr/api/src/swagger"
)

// APIDocs renders API documentation for the given version and base URL.
// format "json" returns the same OpenAPI structure served at /openapi.json
// (reusing swagger.GenerateSpec, not duplicating its logic); any other
// format (default "markdown") walks the spec's paths and renders a
// human-readable Markdown document, one section per path with its methods,
// summaries, and descriptions.
func (s *Service) APIDocs(format, version, baseURL string) (string, error) {
	spec := swagger.GenerateSpec(version, baseURL)

	if strings.ToLower(strings.TrimSpace(format)) == "json" {
		out, err := json.MarshalIndent(spec, "", "  ")
		if err != nil {
			return "", fmt.Errorf("failed to marshal OpenAPI spec: %w", err)
		}
		return string(out), nil
	}

	return renderAPIDocsMarkdown(spec), nil
}

// renderAPIDocsMarkdown walks a swagger.Spec's paths and renders a
// human-readable Markdown document, one section per path.
func renderAPIDocsMarkdown(spec *swagger.Spec) string {
	var b strings.Builder

	fmt.Fprintf(&b, "# %s\n\n", spec.Info.Title)
	if spec.Info.Description != "" {
		fmt.Fprintf(&b, "%s\n\n", spec.Info.Description)
	}
	fmt.Fprintf(&b, "Version: %s\n\n", spec.Info.Version)

	paths := make([]string, 0, len(spec.Paths))
	for p := range spec.Paths {
		paths = append(paths, p)
	}
	sort.Strings(paths)

	for _, path := range paths {
		item := spec.Paths[path]
		fmt.Fprintf(&b, "## %s\n\n", path)

		type methodOp struct {
			method string
			op     *swagger.Operation
		}
		ops := []methodOp{
			{"GET", item.Get},
			{"POST", item.Post},
			{"PUT", item.Put},
			{"DELETE", item.Delete},
			{"PATCH", item.Patch},
			{"OPTIONS", item.Options},
		}

		for _, mo := range ops {
			if mo.op == nil {
				continue
			}
			fmt.Fprintf(&b, "### %s %s\n\n", mo.method, path)
			if mo.op.Summary != "" {
				fmt.Fprintf(&b, "%s\n\n", mo.op.Summary)
			}
			if mo.op.Description != "" {
				fmt.Fprintf(&b, "%s\n\n", mo.op.Description)
			}
			if len(mo.op.Parameters) > 0 {
				b.WriteString("Parameters:\n\n")
				for _, p := range mo.op.Parameters {
					required := ""
					if p.Required {
						required = " (required)"
					}
					fmt.Fprintf(&b, "- `%s` (%s)%s: %s\n", p.Name, p.In, required, p.Description)
				}
				b.WriteString("\n")
			}
		}
	}

	return b.String()
}
