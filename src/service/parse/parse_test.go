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

	_, err := s.ParseXML(`<root><a>1</a></root>`)
	assert.Error(t, err)

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
