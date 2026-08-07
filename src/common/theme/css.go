package theme

import (
	"fmt"
	"strings"
)

// cssVarOrder lists the ThemePalette fields in the order they should be
// emitted as CSS custom properties, paired with their `--` variable name
// suffix (e.g. Background -> --theme-background).
var cssVarOrder = []struct {
	name  string
	value func(ThemePalette) string
}{
	{"background", func(p ThemePalette) string { return p.Background }},
	{"foreground", func(p ThemePalette) string { return p.Foreground }},
	{"primary", func(p ThemePalette) string { return p.Primary }},
	{"secondary", func(p ThemePalette) string { return p.Secondary }},
	{"accent", func(p ThemePalette) string { return p.Accent }},
	{"success", func(p ThemePalette) string { return p.Success }},
	{"warning", func(p ThemePalette) string { return p.Warning }},
	{"error", func(p ThemePalette) string { return p.Error }},
	{"info", func(p ThemePalette) string { return p.Info }},
	{"surface", func(p ThemePalette) string { return p.Surface }},
	{"surface-alt", func(p ThemePalette) string { return p.SurfaceAlt }},
	{"border", func(p ThemePalette) string { return p.Border }},
	{"muted", func(p ThemePalette) string { return p.Muted }},
}

// CSSVariables renders a ThemePalette as a map of CSS custom property names
// (without the leading "--") to hex values, for callers that need to emit
// or inspect individual properties (e.g. Swagger/GraphiQL inline <style>
// generation) rather than a full CSS block.
func CSSVariables(p ThemePalette) map[string]string {
	out := make(map[string]string, len(cssVarOrder))
	for _, v := range cssVarOrder {
		out["theme-"+v.name] = v.value(p)
	}
	return out
}

// CSSBlock renders a ThemePalette as the body of a CSS custom-property
// declaration block (one "--theme-x: #hex;" line per property, indented
// two spaces), suitable for embedding inside a selector such as
// ":root", "html.theme-dark", or ".swagger-ui.theme-dark".
func CSSBlock(p ThemePalette) string {
	var b strings.Builder
	for _, v := range cssVarOrder {
		fmt.Fprintf(&b, "  --theme-%s: %s;\n", v.name, v.value(p))
	}
	return b.String()
}
