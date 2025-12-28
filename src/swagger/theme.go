package swagger

import (
	"fmt"
	"net/http"
)

// ServeUI serves the Swagger UI with theme support
// Theme is determined from cookie (see server/theme.go)
func ServeUI(specURL string) http.HandlerFunc {
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

		// Generate Swagger UI HTML with theme
		html := generateSwaggerHTML(specURL, theme)

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(html))
	}
}

// generateSwaggerHTML creates the Swagger UI HTML with theme support
func generateSwaggerHTML(specURL, theme string) string {
	// Swagger UI theme colors
	darkTheme := `
		.swagger-ui { background-color: #1e1e1e; color: #d4d4d4; }
		.swagger-ui .topbar { background-color: #2d2d2d; border-bottom: 1px solid #3e3e42; }
		.swagger-ui .info .title { color: #d4d4d4; }
		.swagger-ui .opblock-tag { color: #d4d4d4; background: #2d2d2d; border-color: #3e3e42; }
		.swagger-ui .opblock { background: #2d2d2d; border-color: #3e3e42; }
		.swagger-ui .opblock .opblock-summary { background: #252526; }
		.swagger-ui .opblock .opblock-summary-description { color: #d4d4d4; }
		.swagger-ui .btn { background: #0e639c; color: #ffffff; border-color: #1177bb; }
		.swagger-ui .model-box { background: #2d2d2d; }
		.swagger-ui section.models { border-color: #3e3e42; }
		.swagger-ui .model { color: #d4d4d4; }
		.swagger-ui .parameter__name { color: #9cdcfe; }
		.swagger-ui .parameter__type { color: #4ec9b0; }
		.swagger-ui .response-col_status { color: #d4d4d4; }
		.swagger-ui table thead tr th { color: #d4d4d4; border-color: #3e3e42; }
		.swagger-ui table tbody tr td { color: #d4d4d4; border-color: #3e3e42; }
	`

	lightTheme := `
		.swagger-ui { background-color: #ffffff; color: #1e1e1e; }
		.swagger-ui .topbar { background-color: #f5f5f5; border-bottom: 1px solid #e0e0e0; }
		.swagger-ui .info .title { color: #1e1e1e; }
		.swagger-ui .opblock-tag { color: #1e1e1e; background: #f5f5f5; border-color: #e0e0e0; }
		.swagger-ui .opblock { background: #ffffff; border-color: #e0e0e0; }
		.swagger-ui .opblock .opblock-summary { background: #fafafa; }
		.swagger-ui .opblock .opblock-summary-description { color: #1e1e1e; }
		.swagger-ui .btn { background: #0078d4; color: #ffffff; border-color: #005a9e; }
		.swagger-ui .model-box { background: #fafafa; }
		.swagger-ui section.models { border-color: #e0e0e0; }
		.swagger-ui .model { color: #1e1e1e; }
		.swagger-ui .parameter__name { color: #0000ff; }
		.swagger-ui .parameter__type { color: #008000; }
		.swagger-ui .response-col_status { color: #1e1e1e; }
		.swagger-ui table thead tr th { color: #1e1e1e; border-color: #e0e0e0; }
		.swagger-ui table tbody tr td { color: #1e1e1e; border-color: #e0e0e0; }
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
	<title>API Documentation - Swagger UI</title>
	<link rel="stylesheet" type="text/css" href="https://cdn.jsdelivr.net/npm/swagger-ui-dist@5/swagger-ui.css">
	<style>
		%s

		/* Additional styling */
		.swagger-ui .topbar { display: none; }
		body { margin: 0; padding: 0; }
	</style>
</head>
<body>
	<div id="swagger-ui"></div>

	<script src="https://cdn.jsdelivr.net/npm/swagger-ui-dist@5/swagger-ui-bundle.js"></script>
	<script src="https://cdn.jsdelivr.net/npm/swagger-ui-dist@5/swagger-ui-standalone-preset.js"></script>
	<script>
		window.onload = function() {
			window.ui = SwaggerUIBundle({
				url: '%s',
				dom_id: '#swagger-ui',
				deepLinking: true,
				presets: [
					SwaggerUIBundle.presets.apis,
					SwaggerUIStandalonePreset
				],
				plugins: [
					SwaggerUIBundle.plugins.DownloadUrl
				],
				layout: "StandaloneLayout"
			});
		};
	</script>
</body>
</html>`, themeCSS, specURL)
}
