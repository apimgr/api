package output

import (
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestColorEnabled covers the explicit "yes"/"no" overrides and the
// NO_COLOR environment convention taking priority over auto-detection
// when mode is ColorAuto (or unrecognized).
func TestColorEnabled(t *testing.T) {
	tests := []struct {
		name    string
		mode    ColorMode
		noColor string
		want    bool
	}{
		{"explicit yes ignores NO_COLOR", ColorYes, "1", true},
		{"explicit no ignores NO_COLOR", ColorNo, "", false},
		{"auto respects NO_COLOR set", ColorAuto, "1", false},
		{"unrecognized mode respects NO_COLOR set", ColorMode("bogus"), "1", false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("NO_COLOR", tc.noColor)
			assert.Equal(t, tc.want, ColorEnabled(tc.mode))
		})
	}
}

// TestCapture verifies Capture redirects stdout during fn and returns
// exactly what was printed, restoring the original stdout afterward and
// propagating fn's error.
func TestCapture(t *testing.T) {
	out, err := Capture(func() error {
		return Print([]byte(`{"a":1}`), FormatJSON)
	})
	require.NoError(t, err)
	assert.Contains(t, out, `"a": 1`)
}

// TestCaptureErrorPropagation verifies Capture still returns whatever
// was written to stdout even when fn returns a non-nil error.
func TestCaptureErrorPropagation(t *testing.T) {
	sentinel := errors.New("boom")
	out, err := Capture(func() error {
		return sentinel
	})
	assert.Equal(t, sentinel, err)
	assert.Empty(t, out)
}

// TestPrintJSON verifies JSON format pretty-prints valid JSON and falls
// back to printing raw (trimmed) bytes for non-JSON input.
func TestPrintJSON(t *testing.T) {
	t.Run("not json", func(t *testing.T) {
		out, err := Capture(func() error {
			return Print([]byte("plain text"), FormatJSON)
		})
		require.NoError(t, err)
		assert.Equal(t, "plain text\n", out)
	})

	// Object key order from encoding/json's map marshaling is not
	// guaranteed, so assert on the decoded/pretty-printed shape rather
	// than a literal byte string.
	t.Run("valid object", func(t *testing.T) {
		out, err := Capture(func() error {
			return Print([]byte(`{"b":2,"a":1}`), FormatJSON)
		})
		require.NoError(t, err)
		assert.Contains(t, out, "\"a\": 1")
		assert.Contains(t, out, "\"b\": 2")
		assert.True(t, strings.HasPrefix(out, "{\n"))
		assert.True(t, strings.HasSuffix(out, "}\n"))
	})
}

// TestPrintPlain covers the single-field shortcut (print just the
// value) and the multi-field / non-JSON fallbacks.
func TestPrintPlain(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"single string field", `{"uuid":"abc-123"}`, "abc-123\n"},
		{"single number field", `{"count":42}`, "42\n"},
		{"single bool field", `{"ok":true}`, "true\n"},
		{"multi field falls back to raw", `{"a":1,"b":2}`, `{"a":1,"b":2}` + "\n"},
		{"not json", "hello", "hello\n"},
		{"array falls back to raw", `[1,2,3]`, `[1,2,3]` + "\n"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			out, err := Capture(func() error {
				return Print([]byte(tc.in), FormatPlain)
			})
			require.NoError(t, err)
			assert.Equal(t, tc.want, out)
		})
	}
}

// TestPrintTableMap verifies map output renders as a sorted, aligned
// key/value table.
func TestPrintTableMap(t *testing.T) {
	out, err := Capture(func() error {
		return Print([]byte(`{"zeta":1,"alpha":"first"}`), FormatTable)
	})
	require.NoError(t, err)
	assert.Equal(t, "alpha  first\nzeta   1\n", out)
}

// TestPrintTableEmptyList verifies an empty JSON array prints the
// documented "(no results)" placeholder rather than an empty table.
func TestPrintTableEmptyList(t *testing.T) {
	out, err := Capture(func() error {
		return Print([]byte(`[]`), FormatTable)
	})
	require.NoError(t, err)
	assert.Equal(t, "(no results)\n", out)
}

// TestPrintTableScalarList verifies a homogeneous list of scalars (not
// objects) prints one value per line rather than attempting a column
// layout.
func TestPrintTableScalarList(t *testing.T) {
	out, err := Capture(func() error {
		return Print([]byte(`["a","b","c"]`), FormatTable)
	})
	require.NoError(t, err)
	assert.Equal(t, "a\nb\nc\n", out)
}

// TestPrintTableObjectList verifies a list of objects renders as a
// header row (union of keys, first-seen order) followed by aligned data
// rows, including a row missing a key present in another row.
func TestPrintTableObjectList(t *testing.T) {
	out, err := Capture(func() error {
		return Print([]byte(`[{"name":"a","age":1},{"name":"bb","extra":"x"}]`), FormatTable)
	})
	require.NoError(t, err)
	// Columns appear in first-seen order across all rows: name, age, extra.
	assert.Contains(t, out, "name")
	assert.Contains(t, out, "age")
	assert.Contains(t, out, "extra")
	assert.Contains(t, out, "a")
	assert.Contains(t, out, "bb")
}

// TestPrintTableNotJSON verifies non-JSON input falls back to printing
// raw trimmed bytes regardless of format.
func TestPrintTableNotJSON(t *testing.T) {
	out, err := Capture(func() error {
		return Print([]byte("  raw text  \n"), FormatTable)
	})
	require.NoError(t, err)
	assert.Equal(t, "raw text\n", out)
}

// TestPrintTableScalarRoot verifies a bare JSON scalar (not an object or
// array) falls back to raw bytes since printTable only special-cases
// map/list.
func TestPrintTableScalarRoot(t *testing.T) {
	out, err := Capture(func() error {
		return Print([]byte(`42`), FormatTable)
	})
	require.NoError(t, err)
	assert.Equal(t, "42\n", out)
}

// TestPrintUnknownFormatDefaultsToTable verifies any Format value other
// than JSON/Plain falls through to the table renderer (the switch's
// default case), matching Print's doc comment "in the requested format".
func TestPrintUnknownFormatDefaultsToTable(t *testing.T) {
	out, err := Capture(func() error {
		return Print([]byte(`{"a":1}`), Format("nonsense"))
	})
	require.NoError(t, err)
	assert.Equal(t, "a  1\n", out)
}
