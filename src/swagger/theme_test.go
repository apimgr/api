package swagger

import (
	"reflect"
	"testing"
)

func TestUIAssetsArePinnedLocalFiles(t *testing.T) {
	wantCSS := []string{"/static/vendor/swagger-ui/swagger-ui.css", "/static/css/docs.css"}
	wantJS := []string{
		"/static/vendor/swagger-ui/swagger-ui-bundle.js",
		"/static/vendor/swagger-ui/swagger-ui-standalone-preset.js",
		"/static/js/docs-swagger.js",
	}
	if got := UIStylesheets(); !reflect.DeepEqual(got, wantCSS) {
		t.Fatalf("UIStylesheets() = %v, want %v", got, wantCSS)
	}
	if got := UIScripts(); !reflect.DeepEqual(got, wantJS) {
		t.Fatalf("UIScripts() = %v, want %v", got, wantJS)
	}
}
