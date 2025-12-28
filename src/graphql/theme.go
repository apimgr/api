package graphql

import (
	"fmt"
	"net/http"
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

// generateGraphiQLHTML creates the GraphiQL UI HTML with theme support
func generateGraphiQLHTML(endpointURL, theme string) string {
	// GraphiQL theme colors
	darkTheme := `
		body { margin: 0; background-color: #1e1e1e; color: #d4d4d4; }
		.graphiql-container { background-color: #1e1e1e; color: #d4d4d4; }
		.graphiql-container .topBar { background-color: #2d2d2d; border-bottom: 1px solid #3e3e42; }
		.graphiql-container .doc-explorer-title { background: #2d2d2d; border-bottom: 1px solid #3e3e42; color: #d4d4d4; }
		.graphiql-container .doc-explorer-contents { background-color: #1e1e1e; color: #d4d4d4; }
		.CodeMirror { background-color: #1e1e1e; color: #d4d4d4; }
		.CodeMirror-gutters { background-color: #2d2d2d; border-right: 1px solid #3e3e42; }
		.CodeMirror-linenumber { color: #858585; }
		.graphiql-container .execute-button { background: #0e639c; fill: #ffffff; }
		.graphiql-container .result-window { background-color: #1e1e1e; }
	`

	lightTheme := `
		body { margin: 0; background-color: #ffffff; color: #1e1e1e; }
		.graphiql-container { background-color: #ffffff; color: #1e1e1e; }
		.graphiql-container .topBar { background-color: #f5f5f5; border-bottom: 1px solid #e0e0e0; }
		.graphiql-container .doc-explorer-title { background: #f5f5f5; border-bottom: 1px solid #e0e0e0; color: #1e1e1e; }
		.graphiql-container .doc-explorer-contents { background-color: #ffffff; color: #1e1e1e; }
		.CodeMirror { background-color: #ffffff; color: #1e1e1e; }
		.CodeMirror-gutters { background-color: #f5f5f5; border-right: 1px solid #e0e0e0; }
		.CodeMirror-linenumber { color: #858585; }
		.graphiql-container .execute-button { background: #0078d4; fill: #ffffff; }
		.graphiql-container .result-window { background-color: #ffffff; }
	`

	// Select theme CSS
	themeCSS := darkTheme
	if theme == "light" {
		themeCSS = lightTheme
	} else if theme == "auto" {
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
