package swagger

import (
	"fmt"
	"net/http"

	"github.com/apimgr/api/src/common/theme"
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

// swaggerPaletteCSS renders one Swagger UI theme block from a shared
// theme.ThemePalette, so dark/light values always trace back to
// src/common/theme/colors.go instead of being hardcoded per-surface.
func swaggerPaletteCSS(p theme.ThemePalette) string {
	return fmt.Sprintf(`
		.swagger-ui { background-color: %[1]s; color: %[2]s; }
		.swagger-ui .topbar { background-color: %[3]s; border-bottom: 1px solid %[4]s; }
		.swagger-ui .info .title { color: %[2]s; }
		.swagger-ui .opblock-tag { color: %[2]s; background: %[3]s; border-color: %[4]s; }
		.swagger-ui .opblock { background: %[3]s; border-color: %[4]s; }
		.swagger-ui .opblock .opblock-summary { background: %[5]s; }
		.swagger-ui .opblock .opblock-summary-description { color: %[2]s; }
		.swagger-ui .btn { background: %[6]s; color: #ffffff; border-color: %[7]s; }
		.swagger-ui .model-box { background: %[5]s; }
		.swagger-ui section.models { border-color: %[4]s; }
		.swagger-ui .model { color: %[2]s; }
		.swagger-ui .parameter__name { color: %[8]s; }
		.swagger-ui .parameter__type { color: %[9]s; }
		.swagger-ui .response-col_status { color: %[2]s; }
		.swagger-ui table thead tr th { color: %[2]s; border-color: %[4]s; }
		.swagger-ui table tbody tr td { color: %[2]s; border-color: %[4]s; }
	`, p.Background, p.Foreground, p.Surface, p.Border, p.SurfaceAlt, p.Primary, p.Accent, p.Info, p.Success)
}

// generateSwaggerHTML creates the Swagger UI HTML with theme support
func generateSwaggerHTML(specURL, themeMode string) string {
	darkTheme := swaggerPaletteCSS(theme.ThemePaletteDark)
	lightTheme := swaggerPaletteCSS(theme.ThemePaletteLight)

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
