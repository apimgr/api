package graphql

import (
	"fmt"
	"net/http"

	"github.com/apimgr/api/src/common/theme"
)

// ServeUI serves the GraphiQL UI with theme support
// Theme is determined from cookie (see server/theme.go)
func ServeUI(endpointURL string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Get theme from cookie (default: dark)
		theme := "dark"
		if cookie, err := r.Cookie("theme"); err == nil {
			switch cookie.Value {
			case "light":
				theme = "light"
			case "auto":
				theme = "auto"
			case "dark":
				theme = "dark"
			}
		}

		// Generate GraphiQL HTML with theme
		html := generateGraphiQLHTML(endpointURL, theme)

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(html))
	}
}

// graphiQLPaletteCSS renders one GraphiQL theme block from a shared
// theme.ThemePalette, so dark/light values always trace back to
// src/common/theme/colors.go instead of being hardcoded per-surface.
func graphiQLPaletteCSS(p theme.ThemePalette) string {
	return fmt.Sprintf(`
		body { margin: 0; background-color: %[1]s; color: %[2]s; }
		.graphiql-container { background-color: %[1]s; color: %[2]s; }
		.graphiql-container .topBar { background-color: %[3]s; border-bottom: 1px solid %[4]s; }
		.graphiql-container .doc-explorer-title { background: %[3]s; border-bottom: 1px solid %[4]s; color: %[2]s; }
		.graphiql-container .doc-explorer-contents { background-color: %[1]s; color: %[2]s; }
		.CodeMirror { background-color: %[1]s; color: %[2]s; }
		.CodeMirror-gutters { background-color: %[3]s; border-right: 1px solid %[4]s; }
		.CodeMirror-linenumber { color: %[5]s; }
		.graphiql-container .execute-button { background: %[6]s; fill: #ffffff; }
		.graphiql-container .result-window { background-color: %[1]s; }
	`, p.Background, p.Foreground, p.Surface, p.Border, p.Muted, p.Primary)
}

// generateGraphiQLHTML creates the GraphiQL UI HTML with theme support
func generateGraphiQLHTML(endpointURL, themeMode string) string {
	darkTheme := graphiQLPaletteCSS(theme.ThemePaletteDark)
	lightTheme := graphiQLPaletteCSS(theme.ThemePaletteLight)

	// Select theme CSS
	themeCSS := darkTheme
	if themeMode == "light" {
		themeCSS = lightTheme
	} else if themeMode == "auto" {
		// Auto theme uses prefers-color-scheme media query
		themeCSS = `
			@media (prefers-color-scheme: dark) {` + darkTheme + `}
			@media (prefers-color-scheme: light) {` + lightTheme + `}
		`
	}

	return fmt.Sprintf(`<!DOCTYPE html>
<html lang="en">
<head>
	<meta charset="UTF-8">
	<meta name="viewport" content="width=device-width, initial-scale=1.0">
	<title>GraphQL API - GraphiQL</title>
	<link rel="stylesheet" href="https://cdn.jsdelivr.net/npm/graphiql@3/graphiql.min.css">
	<style>
		%s
		#graphiql { height: 100vh; }
	</style>
</head>
<body>
	<div id="graphiql">Loading...</div>

	<script crossorigin src="https://unpkg.com/react@18/umd/react.production.min.js"></script>
	<script crossorigin src="https://unpkg.com/react-dom@18/umd/react-dom.production.min.js"></script>
	<script src="https://cdn.jsdelivr.net/npm/graphiql@3/graphiql.min.js"></script>

	<script>
		const fetcher = GraphiQL.createFetcher({
			url: '%s',
		});

		const root = ReactDOM.createRoot(document.getElementById('graphiql'));
		root.render(
			React.createElement(GraphiQL, {
				fetcher: fetcher,
				defaultEditorToolsVisibility: true,
			})
		);
	</script>
</body>
</html>`, themeCSS, endpointURL)
}
