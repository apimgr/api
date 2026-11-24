package httputil

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestHTML2TextConverterPreservesStructureAndSkipsControls(t *testing.T) {
	got := HTML2TextConverter(`<!doctype html><html><body><h1>Tool page</h1><p>A useful <strong>paragraph</strong> with a <a href="/next">link</a>.</p><ul><li>First</li><li>Second</li></ul><form><input value="secret"><button>Submit</button></form><script>alert(1)</script><style>body{display:none}</style></body></html>`, 40)

	assert.Contains(t, got, "TOOL PAGE")
	assert.Contains(t, got, "*paragraph*")
	assert.Contains(t, got, "link [/next]")
	assert.Contains(t, got, "• First")
	assert.NotContains(t, got, "Submit")
	assert.NotContains(t, got, "secret")
	assert.NotContains(t, got, "alert")
	assert.NotContains(t, got, "display:none")
	assert.True(t, strings.HasSuffix(got, "\n"))
}

func TestHTML2TextConverterWrapsByDisplayWidth(t *testing.T) {
	got := HTML2TextConverter("<p>alpha beta gamma delta epsilon</p>", 12)
	assert.Contains(t, got, "alpha beta\ngamma delta\nepsilon")
}

func TestClientDetection(t *testing.T) {
	tests := []struct {
		name           string
		userAgent      string
		project        string
		cli            bool
		textBrowser    bool
		httpTool       bool
		nonInteractive bool
	}{
		{name: "project cli", userAgent: "api-cli/1.0", project: "api", cli: true},
		{name: "lynx", userAgent: "Lynx/2.9", textBrowser: true},
		{name: "curl", userAgent: "curl/8.0", httpTool: true, nonInteractive: true},
		{name: "browser", userAgent: "Mozilla/5.0"},
		{name: "empty agent", httpTool: true, nonInteractive: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequest("GET", "/", nil)
			r.Header.Set("User-Agent", tt.userAgent)
			assert.Equal(t, tt.cli, IsOurCLI(r, tt.project))
			assert.Equal(t, tt.textBrowser, IsTextBrowser(r))
			assert.Equal(t, tt.httpTool, IsHTTPTool(r))
			assert.Equal(t, tt.nonInteractive, IsNonInteractiveClient(r, tt.project))
		})
	}
}

func TestRenderNoJSHTMLRemovesScriptsAndInlineHandlers(t *testing.T) {
	got := RenderNoJSHTML(`<html><body><a href="/next" onclick="run()">Next</a><script>run()</script><form><button>Save</button></form></body></html>`)
	assert.Contains(t, got, `href="/next"`)
	assert.Contains(t, got, "Save")
	assert.NotContains(t, got, "onclick")
	assert.NotContains(t, got, "<script")
	assert.NotContains(t, got, "run()")
}

func TestHTML2TextConverterFallbackAndEmptyDocument(t *testing.T) {
	assert.Equal(t, "plain text\n", HTML2TextConverter("<p>plain text</p>", 0))
	assert.Equal(t, "\n", HTML2TextConverter("<form><input></form>", 80))
}
