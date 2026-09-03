package server

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Every public page shares the same layout and partials, so executing one of
// them against a zero-value-ish PageData exercises head/header/nav/footer.
// html/template resolves field references at execution time, not parse time,
// so a partial naming a field PageData does not have still parses cleanly and
// only fails once a request renders it - which is exactly how the old header's
// .User reference survived a green build.
func renderTestPage(t *testing.T, page string) string {
	t.Helper()
	require.NoError(t, initTemplates())

	tmpl, ok := pageTemplates[page]
	require.True(t, ok, "template %q not registered", page)

	data := PageData{
		SiteTitle:  "api",
		Lang:       "en",
		LangDir:    "ltr",
		Theme:      "dark",
		ThemeClass: "theme-dark",
		Layout:     "public",
		CSRFToken:  "test-token",
		FaviconURL: "/static/images/favicon.ico",
	}

	var buf bytes.Buffer
	require.NoError(t, tmpl.ExecuteTemplate(&buf, "public.tmpl", data))
	return buf.String()
}

func TestPublicLayoutExecutesWithoutMissingFields(t *testing.T) {
	for _, page := range []string{"index", "about", "preferences"} {
		t.Run(page, func(t *testing.T) {
			out := renderTestPage(t, page)
			assert.Contains(t, out, "<html")
			assert.Contains(t, out, "</html>")
		})
	}
}

// PART 16 "Header Layout": one header row carrying all four zones, and no
// second nav row underneath it.
func TestHeaderRendersSingleRowWithFourZones(t *testing.T) {
	out := renderTestPage(t, "index")

	assert.Equal(t, 1, strings.Count(out, `<header class="header"`))
	assert.Contains(t, out, `class="site-brand"`)
	assert.Contains(t, out, `class="nav-links"`)
	assert.Contains(t, out, `class="header-actions"`)
	assert.Contains(t, out, `href="/server/preferences"`)

	// The links must sit inside the header, not in a row of their own.
	header := out[strings.Index(out, `<header class="header"`):strings.Index(out, "</header>")]
	assert.Contains(t, header, `class="nav-links"`)
	assert.NotContains(t, out, `<nav class="nav" id="navigation"`)
}

// PART 16 "Theme Cycle Logic": the POST target is the mode AFTER the one in
// effect, computed per render - never a fixed value.
func TestThemeToggleTargetsNextMode(t *testing.T) {
	out := renderTestPage(t, "index")

	assert.Contains(t, out, `<form action="/server/preferences" method="POST" class="theme-toggle-form">`)
	assert.Contains(t, out, `<input type="hidden" name="theme" value="light">`)
}

func TestNextThemeCycles(t *testing.T) {
	assert.Equal(t, "light", NextTheme("dark"))
	assert.Equal(t, "auto", NextTheme("light"))
	assert.Equal(t, "dark", NextTheme("auto"))
	assert.Equal(t, "dark", NextTheme(""))
}
