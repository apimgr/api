package dev

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// FormatJSON/MinifyJSON cover valid JSON round-tripping and invalid
// JSON error handling for both directions.
func TestFormatAndMinifyJSON(t *testing.T) {
	s := New()

	formatted, err := s.FormatJSON(`{"b":2,"a":1}`)
	assert.NoError(t, err)
	assert.Contains(t, formatted, "\n")
	assert.Contains(t, formatted, "  \"a\": 1")

	_, err = s.FormatJSON(`not json`)
	assert.Error(t, err)

	minified, err := s.MinifyJSON("{\n  \"a\": 1\n}")
	assert.NoError(t, err)
	assert.Equal(t, `{"a":1}`, minified)

	_, err = s.MinifyJSON(`{invalid`)
	assert.Error(t, err)
}

// Case-conversion functions share input fixtures spanning
// snake_case, kebab-case, and space separated sources (all split into
// words by FieldsFunc), plus an empty-string boundary. camelCase/
// PascalCase inputs are NOT split by the source's separator-only
// FieldsFunc, so they are covered separately below with their actual
// (word-preserving-case-per-segment) behavior.
func TestCaseConversions(t *testing.T) {
	s := New()

	cases := []string{"hello_world", "hello-world", "hello world"}

	for _, in := range cases {
		assert.Equal(t, "helloWorld", s.ToCamelCase(in), "ToCamelCase(%q)", in)
		assert.Equal(t, "HelloWorld", s.ToPascalCase(in), "ToPascalCase(%q)", in)
	}

	assert.Equal(t, "", s.ToCamelCase(""))
	assert.Equal(t, "", s.ToPascalCase(""))

	// Inputs with no underscore/hyphen/space separators are treated as
	// a single word: ToCamelCase lowercases it entirely, ToPascalCase
	// capitalizes only the first rune and lowercases the rest.
	assert.Equal(t, "helloworld", s.ToCamelCase("HelloWorld"))
	assert.Equal(t, "Helloworld", s.ToPascalCase("HelloWorld"))

	assert.Equal(t, "hello_world", s.ToSnakeCase("HelloWorld"))
	assert.Equal(t, "hello_world", s.ToSnakeCase("hello world"))
	assert.Equal(t, "hello_world", s.ToSnakeCase("hello-world"))

	assert.Equal(t, "hello-world", s.ToKebabCase("HelloWorld"))
	assert.Equal(t, "hello-world", s.ToKebabCase("hello world"))
	assert.Equal(t, "hello-world", s.ToKebabCase("hello_world"))

	assert.Equal(t, "HELLO_WORLD", s.ToConstantCase("helloWorld"))
}

// HTML escape/unescape cover all replaced characters and confirm the
// unescape path also handles the numeric-entity-free &apos; alias not
// produced by the escaper itself.
func TestHTMLEscaping(t *testing.T) {
	s := New()

	in := `<a href="x">it's & "that"</a>`
	escaped := s.EscapeHTML(in)
	assert.Equal(t, `&lt;a href=&quot;x&quot;&gt;it&#39;s &amp; &quot;that&quot;&lt;/a&gt;`, escaped)
	assert.Equal(t, in, s.UnescapeHTML(escaped))

	assert.Equal(t, "'", s.UnescapeHTML("&apos;"))
}

// SQL escaping covers the single-quote doubling rule and a string with
// no quotes (must pass through unchanged).
func TestEscapeSQL(t *testing.T) {
	s := New()

	assert.Equal(t, "O''Brien", s.EscapeSQL("O'Brien"))
	assert.Equal(t, "plain", s.EscapeSQL("plain"))
}

// Regex escaping covers every listed special character plus a string
// with none of them.
func TestEscapeRegex(t *testing.T) {
	s := New()

	assert.Equal(t, `\.\+\*\?\^\$\(\)\[\]\{\}\|\\`, s.EscapeRegex(`.+*?^$()[]{}|\`))
	assert.Equal(t, "plain", s.EscapeRegex("plain"))
}

// Line comment add/remove round-trip, including a blank line that must
// be left untouched by both operations.
func TestLineComments(t *testing.T) {
	s := New()

	code := "line1\n\nline2"
	commented := s.AddLineComments(code, "//")
	assert.Equal(t, "// line1\n\n// line2", commented)
	assert.Equal(t, code, s.RemoveLineComments(commented, "//"))

	// A line without the comment prefix is left untouched by removal.
	assert.Equal(t, "line1", s.RemoveLineComments("line1", "//"))
}

// Indent/Dedent round-trip, and Dedent on a line with insufficient
// leading whitespace is left untouched.
func TestIndentDedent(t *testing.T) {
	s := New()

	code := "line1\n\nline2"
	indented := s.Indent(code, 2)
	assert.Equal(t, "  line1\n\n  line2", indented)
	assert.Equal(t, code, s.Dedent(indented, 2))

	assert.Equal(t, "line1", s.Dedent("line1", 4))
}

// TemplateReplace covers multiple placeholders and an unmatched
// placeholder left in place.
func TestTemplateReplace(t *testing.T) {
	s := New()

	out := s.TemplateReplace("Hello {{name}}, you are {{age}}", map[string]string{
		"name": "Alice",
		"age":  "30",
	})
	assert.Equal(t, "Hello Alice, you are 30", out)

	out = s.TemplateReplace("Hello {{missing}}", map[string]string{"name": "Alice"})
	assert.Equal(t, "Hello {{missing}}", out)
}

// CountLines and RemoveEmptyLines/NumberLines cover single-line,
// multi-line, and blank-line-bearing inputs.
func TestLineOperations(t *testing.T) {
	s := New()

	assert.Equal(t, 1, s.CountLines("single"))
	assert.Equal(t, 3, s.CountLines("a\nb\nc"))
	assert.Equal(t, 2, s.CountLines("a\n"))

	assert.Equal(t, "a\nb", s.RemoveEmptyLines("a\n\nb\n"))

	numbered := s.NumberLines("a\nb")
	assert.Equal(t, "   1 | a\n   2 | b", numbered)
}
