package swagger

// UIStylesheets returns the locally served, pinned stylesheets for the
// shared Swagger documentation page.
func UIStylesheets() []string {
	return []string{
		"/static/vendor/swagger-ui/swagger-ui.css",
		"/static/css/docs.css",
	}
}

// UIScripts returns the locally served, pinned scripts in dependency order.
func UIScripts() []string {
	return []string{
		"/static/vendor/swagger-ui/swagger-ui-bundle.js",
		"/static/vendor/swagger-ui/swagger-ui-standalone-preset.js",
		"/static/js/docs-swagger.js",
	}
}
