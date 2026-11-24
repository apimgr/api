package graphql

import (
	"reflect"
	"testing"
)

func TestUIAssetsArePinnedLocalFiles(t *testing.T) {
	wantCSS := []string{"/static/vendor/graphiql/graphiql.min.css", "/static/css/docs.css"}
	wantJS := []string{
		"/static/vendor/graphiql/react.production.min.js",
		"/static/vendor/graphiql/react-dom.production.min.js",
		"/static/vendor/graphiql/graphiql.min.js",
		"/static/js/docs-graphql.js",
	}
	if got := UIStylesheets(); !reflect.DeepEqual(got, wantCSS) {
		t.Fatalf("UIStylesheets() = %v, want %v", got, wantCSS)
	}
	if got := UIScripts(); !reflect.DeepEqual(got, wantJS) {
		t.Fatalf("UIScripts() = %v, want %v", got, wantJS)
	}
}
