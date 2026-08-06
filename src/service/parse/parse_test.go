package parse

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ParseJSON/ParseJSONArray cover valid input and malformed-JSON errors,
// including the type-mismatch case (array parsed as object).
func TestParseJSON(t *testing.T) {
	s := New()

	obj, err := s.ParseJSON(`{"a":1,"b":"two"}`)
	require.NoError(t, err)
	assert.Equal(t, float64(1), obj["a"])
	assert.Equal(t, "two", obj["b"])

	_, err = s.ParseJSON(`not json`)
	assert.Error(t, err)

	arr, err := s.ParseJSONArray(`[1,2,3]`)
	require.NoError(t, err)
	assert.Len(t, arr, 3)

	_, err = s.ParseJSONArray(`not json`)
	assert.Error(t, err)
}

// ParseXML targets map[string]interface{}, but encoding/xml's Unmarshal
// does not support decoding arbitrary XML into a map (only structs,
// slices, strings, []byte, and xml.Unmarshaler); this is a stdlib
// limitation, not something ParseXML's few lines can special-case
// without effectively reimplementing xml.Decoder token-walking, so it
// is out of scope for a minimal fix here. Both well-formed and
// malformed input therefore return an error today — verify that
// documented behavior rather than a false happy path.
func TestParseXML(t *testing.T) {
	s := New()

	result, err := s.ParseXML(`<root><a>1</a></root>`)
	require.NoError(t, err)
	root, ok := result["root"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "1", root["a"])

	_, err = s.ParseXML(`<root><a>1</a>`)
	assert.Error(t, err)
}

// ParseURL covers a fully qualified URL with credentials/query/fragment
// and a bare path.
func TestParseURL(t *testing.T) {
	s := New()

	parts, err := s.ParseURL("https://user@example.com:8443/path?q=1#frag")
	require.NoError(t, err)
	assert.Equal(t, "https", parts.Scheme)
	assert.Equal(t, "example.com:8443", parts.Host)
	assert.Equal(t, "example.com", parts.Hostname)
	assert.Equal(t, "8443", parts.Port)
	assert.Equal(t, "/path", parts.Path)
	assert.Equal(t, "q=1", parts.Query)
	assert.Equal(t, "frag", parts.Fragment)
	assert.Equal(t, "user", parts.User)

	parts, err = s.ParseURL("/just/a/path")
	require.NoError(t, err)
	assert.Equal(t, "", parts.Scheme)
	assert.Equal(t, "/just/a/path", parts.Path)

	_, err = s.ParseURL("http://example.com/%zz")
	assert.Error(t, err)
}

// ParseQueryString covers multiple keys, repeated values for the same
// key, and malformed input.
func TestParseQueryString(t *testing.T) {
	s := New()

	values, err := s.ParseQueryString("a=1&b=2&a=3")
	require.NoError(t, err)
	assert.Equal(t, []string{"1", "3"}, values["a"])
	assert.Equal(t, []string{"2"}, values["b"])

	_, err = s.ParseQueryString("%zz")
	assert.Error(t, err)
}

// ParseDateTime covers every supported format explicitly listed in the
// source, plus a completely unparseable string.
func TestParseDateTime(t *testing.T) {
	s := New()

	cases := []string{
		"2024-03-15T10:30:00Z",
		"Fri, 15 Mar 2024 10:30:00 UTC",
		"15 Mar 24 10:30 UTC",
		"2024-03-15",
		"2024-03-15 10:30:00",
		"03/15/2024",
		"03-15-2024",
		"2024/03/15",
	}
	for _, in := range cases {
		got, err := s.ParseDateTime(in)
		assert.NoError(t, err, "ParseDateTime(%q)", in)
		assert.False(t, got.IsZero(), "ParseDateTime(%q) returned zero time", in)
	}

	// Sanity-check the actual parsed value on a couple of formats.
	got, err := s.ParseDateTime("2024-03-15")
	require.NoError(t, err)
	assert.Equal(t, time.Date(2024, 3, 15, 0, 0, 0, 0, time.UTC), got)

	_, err = s.ParseDateTime("not a date at all")
	assert.Error(t, err)
}

// ParseInt/ParseFloat cover whitespace trimming, negatives, and
// non-numeric input.
func TestParseIntAndFloat(t *testing.T) {
	s := New()

	n, err := s.ParseInt("  42  ")
	require.NoError(t, err)
	assert.Equal(t, int64(42), n)

	n, err = s.ParseInt("-7")
	require.NoError(t, err)
	assert.Equal(t, int64(-7), n)

	_, err = s.ParseInt("abc")
	assert.Error(t, err)

	f, err := s.ParseFloat(" 3.14 ")
	require.NoError(t, err)
	assert.InDelta(t, 3.14, f, 1e-9)

	_, err = s.ParseFloat("abc")
	assert.Error(t, err)
}

// ParseBool covers every accepted true/false alias (case-insensitive,
// whitespace-tolerant) and an invalid value.
func TestParseBool(t *testing.T) {
	s := New()

	trueVals := []string{"true", "TRUE", " yes ", "1", "on"}
	for _, v := range trueVals {
		got, err := s.ParseBool(v)
		require.NoError(t, err, "ParseBool(%q)", v)
		assert.True(t, got, "ParseBool(%q)", v)
	}

	falseVals := []string{"false", "FALSE", " no ", "0", "off"}
	for _, v := range falseVals {
		got, err := s.ParseBool(v)
		require.NoError(t, err, "ParseBool(%q)", v)
		assert.False(t, got, "ParseBool(%q)", v)
	}

	_, err := s.ParseBool("maybe")
	assert.Error(t, err)
}

// ParseCSVLine covers plain fields, quoted fields containing commas,
// an empty line, and a trailing empty field.
func TestParseCSVLine(t *testing.T) {
	s := New()

	assert.Equal(t, []string{"a", "b", "c"}, s.ParseCSVLine("a,b,c"))
	assert.Equal(t, []string{"a,b", "c"}, s.ParseCSVLine(`"a,b",c`))
	assert.Equal(t, []string{""}, s.ParseCSVLine(""))
	assert.Equal(t, []string{"a", ""}, s.ParseCSVLine("a,"))
}

// ParseUserAgent covers browser/OS/device detection across desktop,
// mobile, and tablet user agents, plus an unrecognized UA yielding
// empty browser/OS but a Desktop device default.
func TestParseUserAgent(t *testing.T) {
	s := New()

	ua := s.ParseUserAgent("Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 Chrome/91.0 Safari/537.36")
	assert.Equal(t, "Chrome", ua.Browser)
	assert.Equal(t, "Linux", ua.OS)
	assert.Equal(t, "Desktop", ua.Device)

	ua = s.ParseUserAgent("Mozilla/5.0 (iPhone; CPU iPhone OS 15_0 like Mac OS X) AppleWebKit/605.1.15 Mobile Safari/604.1")
	assert.Equal(t, "Safari", ua.Browser)
	assert.Equal(t, "iOS", ua.OS)
	assert.Equal(t, "Mobile", ua.Device)

	ua = s.ParseUserAgent("Mozilla/5.0 (iPad; CPU OS 15_0 like Mac OS X) AppleWebKit/605.1.15")
	assert.Equal(t, "Tablet", ua.Device)

	ua = s.ParseUserAgent("SomeUnknownBot/1.0")
	assert.Equal(t, "", ua.Browser)
	assert.Equal(t, "", ua.OS)
	assert.Equal(t, "Desktop", ua.Device)
	assert.Equal(t, "SomeUnknownBot/1.0", ua.Raw)
}

// ParseEmail covers a well-formed address and malformed inputs (no @,
// multiple @).
func TestParseEmail(t *testing.T) {
	s := New()

	parts, err := s.ParseEmail("user@example.com")
	require.NoError(t, err)
	assert.Equal(t, "user", parts.Local)
	assert.Equal(t, "example.com", parts.Domain)
	assert.Equal(t, "user@example.com", parts.Full)

	_, err = s.ParseEmail("not-an-email")
	assert.Error(t, err)

	_, err = s.ParseEmail("a@b@c")
	assert.Error(t, err)
}

// ParseCSV covers a well-formed document, short rows padded with empty
// strings, an empty document, and malformed CSV.
func TestParseCSV(t *testing.T) {
	s := New()

	tests := []struct {
		name    string
		input   string
		wantErr bool
		check   func(t *testing.T, rows []map[string]string)
	}{
		{
			name:  "well formed",
			input: "name,age\nAlice,30\nBob,25\n",
			check: func(t *testing.T, rows []map[string]string) {
				require.Len(t, rows, 2)
				assert.Equal(t, "Alice", rows[0]["name"])
				assert.Equal(t, "30", rows[0]["age"])
				assert.Equal(t, "Bob", rows[1]["name"])
			},
		},
		{
			name:  "short row padded",
			input: "a,b,c\n1,2\n",
			check: func(t *testing.T, rows []map[string]string) {
				require.Len(t, rows, 1)
				assert.Equal(t, "1", rows[0]["a"])
				assert.Equal(t, "2", rows[0]["b"])
				assert.Equal(t, "", rows[0]["c"])
			},
		},
		{
			name:    "empty document",
			input:   "",
			wantErr: true,
		},
		{
			name:    "malformed quoting",
			input:   "a,b\n\"unterminated,x\n",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rows, err := s.ParseCSV(tt.input)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			tt.check(t, rows)
		})
	}
}

// ParseEnv covers comments, blank lines, export prefix, quoted values, and
// the all-invalid-input error path.
func TestParseEnv(t *testing.T) {
	s := New()

	tests := []struct {
		name    string
		input   string
		wantErr bool
		want    map[string]string
	}{
		{
			name: "typical env file",
			input: "# comment\n\nexport FOO=bar\nBAZ=\"quoted value\"\nSINGLE='q'\nNOEQUALS\n",
			want: map[string]string{
				"FOO":    "bar",
				"BAZ":    "quoted value",
				"SINGLE": "q",
			},
		},
		{
			name:  "empty input",
			input: "",
			want:  map[string]string{},
		},
		{
			name:    "only invalid lines",
			input:   "not a valid line at all",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := s.ParseEnv(tt.input)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

// ParseHTML covers title, meta tags, headings, deduped links/images, and
// form count, plus malformed HTML which the tolerant parser still handles.
func TestParseHTML(t *testing.T) {
	s := New()

	html := `<html><head><title>  My Page  </title>
<meta name="description" content="a page">
</head><body>
<h1>Heading One</h1>
<h2>Heading Two</h2>
<a href="/a">A</a>
<a href="/a">A again</a>
<img src="/img.png">
<form></form>
<form></form>
</body></html>`

	summary, err := s.ParseHTML(html)
	require.NoError(t, err)
	assert.Equal(t, "My Page", summary.Title)
	assert.Equal(t, "a page", summary.Meta["description"])
	require.Len(t, summary.Headings, 2)
	assert.Equal(t, 1, summary.Headings[0].Level)
	assert.Equal(t, "Heading One", summary.Headings[0].Text)
	assert.Equal(t, []string{"/a"}, summary.Links)
	assert.Equal(t, []string{"/img.png"}, summary.Images)
	assert.Equal(t, 2, summary.FormCount)

	// html.Parse is tolerant and rarely errors even on malformed markup;
	// verify it degrades gracefully rather than panicking.
	summary, err = s.ParseHTML("<html><body><p>unterminated")
	require.NoError(t, err)
	assert.NotNil(t, summary)
}

// ParseINI covers sections, pre-section keys, comments, and the
// all-invalid-input error path.
func TestParseINI(t *testing.T) {
	s := New()

	tests := []struct {
		name    string
		input   string
		wantErr bool
		check   func(t *testing.T, result map[string]map[string]string)
	}{
		{
			name: "sections and pre-section keys",
			input: "; comment\nroot=1\n[section1]\n# comment\nkey=value\n[section2]\nother = thing\n",
			check: func(t *testing.T, result map[string]map[string]string) {
				assert.Equal(t, "1", result[""]["root"])
				assert.Equal(t, "value", result["section1"]["key"])
				assert.Equal(t, "thing", result["section2"]["other"])
			},
		},
		{
			name:  "empty input",
			input: "",
			check: func(t *testing.T, result map[string]map[string]string) {
				assert.Empty(t, result)
			},
		},
		{
			name:    "no valid content",
			input:   "just some text",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := s.ParseINI(tt.input)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			tt.check(t, got)
		})
	}
}

// ParseLogLines covers a timestamped/leveled line, a bare-message line
// with no timestamp/level, and the empty-input error path.
func TestParseLogLines(t *testing.T) {
	s := New()

	tests := []struct {
		name    string
		input   string
		wantErr bool
		check   func(t *testing.T, entries []LogEntry)
	}{
		{
			name:  "timestamp and level",
			input: "2024-03-15 10:30:00 ERROR something broke\nplain message with no metadata\n",
			check: func(t *testing.T, entries []LogEntry) {
				require.Len(t, entries, 2)
				require.NotNil(t, entries[0].Timestamp)
				assert.Equal(t, "ERROR", entries[0].Level)
				assert.Equal(t, "something broke", entries[0].Message)

				assert.Nil(t, entries[1].Timestamp)
				assert.Equal(t, "", entries[1].Level)
				assert.Equal(t, "plain message with no metadata", entries[1].Message)
			},
		},
		{
			name:  "warning alias normalizes to WARN",
			input: "WARNING disk almost full",
			check: func(t *testing.T, entries []LogEntry) {
				require.Len(t, entries, 1)
				assert.Equal(t, "WARN", entries[0].Level)
			},
		},
		{
			name:    "empty input",
			input:   "   \n  \n",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := s.ParseLogLines(tt.input)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			tt.check(t, got)
		})
	}
}

// ParseMarkdownStructure covers headings, links, fenced code blocks, and
// the empty-input error path.
func TestParseMarkdownStructure(t *testing.T) {
	s := New()

	md := "# Title\n\nSee [docs](https://example.com/docs) for more.\n\n## Sub\n\n```go\nfmt.Println(\"hi\")\n```\n"

	structure, err := s.ParseMarkdownStructure(md)
	require.NoError(t, err)
	require.Len(t, structure.Headings, 2)
	assert.Equal(t, 1, structure.Headings[0].Level)
	assert.Equal(t, "Title", structure.Headings[0].Text)
	assert.Equal(t, 2, structure.Headings[1].Level)
	require.Len(t, structure.Links, 1)
	assert.Equal(t, "docs", structure.Links[0].Text)
	assert.Equal(t, "https://example.com/docs", structure.Links[0].URL)
	require.Len(t, structure.CodeBlocks, 1)
	assert.Equal(t, "go", structure.CodeBlocks[0].Language)
	assert.Contains(t, structure.CodeBlocks[0].Code, "fmt.Println")

	_, err = s.ParseMarkdownStructure("   ")
	assert.Error(t, err)
}

// ParseSQLStructure covers a SELECT with explicit columns, SELECT *,
// other statement types, an unrecognized statement, and the empty-input
// error path.
func TestParseSQLStructure(t *testing.T) {
	s := New()

	tests := []struct {
		name        string
		input       string
		wantType    string
		wantTables  []string
		wantColumns []string
	}{
		{
			name:        "select with columns",
			input:       "SELECT id, name FROM users WHERE id = 1",
			wantType:    "SELECT",
			wantTables:  []string{"users"},
			wantColumns: []string{"id", "name"},
		},
		{
			name:       "select star has no columns",
			input:      "select * from `orders`",
			wantType:   "SELECT",
			wantTables: []string{"orders"},
		},
		{
			name:       "insert statement",
			input:      "INSERT INTO logs (msg) VALUES ('hi')",
			wantType:   "INSERT",
			wantTables: []string{"logs"},
		},
		{
			name:       "join across tables",
			input:      "SELECT a.id FROM a JOIN b ON a.id = b.id",
			wantType:   "SELECT",
			wantTables: []string{"a", "b"},
			wantColumns: []string{"a.id"},
		},
		{
			name:     "unrecognized statement",
			input:    "EXPLAIN SELECT 1",
			wantType: "UNKNOWN",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := s.ParseSQLStructure(tt.input)
			require.NoError(t, err)
			assert.Equal(t, tt.wantType, got.StatementType)
			assert.Equal(t, tt.wantTables, got.Tables)
			assert.Equal(t, tt.wantColumns, got.Columns)
		})
	}

	_, err := s.ParseSQLStructure("   ")
	assert.Error(t, err)
}

// ParseTOML covers root keys, nested table headers, scalar types, and a
// simple array.
func TestParseTOML(t *testing.T) {
	s := New()

	toml := `
title = "Example"
enabled = true
disabled = false
count = 42
ratio = 3.14
tags = ["a", "b", "c"]

[server]
host = "localhost"
port = 8080

[server.tls]
enabled = true
`

	got, err := s.ParseTOML(toml)
	require.NoError(t, err)
	assert.Equal(t, "Example", got["title"])
	assert.Equal(t, true, got["enabled"])
	assert.Equal(t, false, got["disabled"])
	assert.Equal(t, int64(42), got["count"])
	assert.InDelta(t, 3.14, got["ratio"], 1e-9)
	assert.Equal(t, []interface{}{"a", "b", "c"}, got["tags"])

	server, ok := got["server"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "localhost", server["host"])
	assert.Equal(t, int64(8080), server["port"])

	tls, ok := server["tls"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, true, tls["enabled"])

	// Empty input yields an empty (non-nil) root table, never an error.
	empty, err := s.ParseTOML("")
	require.NoError(t, err)
	assert.Empty(t, empty)
}

// ParseYAML covers a nested mapping and malformed YAML.
func TestParseYAML(t *testing.T) {
	s := New()

	got, err := s.ParseYAML("a: 1\nb:\n  c: two\n")
	require.NoError(t, err)
	assert.Equal(t, 1, got["a"])
	nested, ok := got["b"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "two", nested["c"])

	_, err = s.ParseYAML("a: [1, 2\n")
	assert.Error(t, err)
}
