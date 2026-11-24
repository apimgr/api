package graphql

// UIStylesheets returns the locally served, pinned stylesheets for the
// shared GraphiQL documentation page.
func UIStylesheets() []string {
	return []string{
		"/static/vendor/graphiql/graphiql.min.css",
		"/static/css/docs.css",
	}
}

// UIScripts returns the locally served, pinned scripts in dependency order.
func UIScripts() []string {
	return []string{
		"/static/vendor/graphiql/react.production.min.js",
		"/static/vendor/graphiql/react-dom.production.min.js",
		"/static/vendor/graphiql/graphiql.min.js",
		"/static/js/docs-graphql.js",
	}
}
