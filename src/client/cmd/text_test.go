package cmd

import (
	"testing"

	"github.com/apimgr/api/src/client/api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestTextCommands_HappyPath table-drives every text subcommand's success
// path, including the optional-arg chaining rules for uuid and lorem
// (a count only ever gets appended when a preceding optional arg is also
// present).
func TestTextCommands_HappyPath(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		wantPath string
	}{
		{"uuid", nil, "/api/v1/text/uuid"},
		{"uuid", []string{"4"}, "/api/v1/text/uuid/4"},
		{"uuid", []string{"4", "5"}, "/api/v1/text/uuid/4/5"},
		{"hash", []string{"sha256", "hello"}, "/api/v1/text/hash/sha256/hello"},
		{"hash-all", []string{"hello"}, "/api/v1/text/hash/multi/hello"},
		{"encode", []string{"base64", "hello"}, "/api/v1/text/encode/base64/hello"},
		{"decode", []string{"base64", "aGVsbG8="}, "/api/v1/text/decode/base64/aGVsbG8="},
		{"case", []string{"upper", "hello"}, "/api/v1/text/case/upper/hello"},
		{"lorem", nil, "/api/v1/text/lorem"},
		{"lorem", []string{"words"}, "/api/v1/text/lorem/words"},
		{"lorem", []string{"words", "10"}, "/api/v1/text/lorem/words/10"},
		{"rot13", []string{"hello"}, "/api/v1/text/rot13/hello"},
		{"reverse", []string{"hello"}, "/api/v1/text/reverse/hello"},
	}

	for _, tc := range tests {
		t.Run(tc.name+"_"+tc.wantPath, func(t *testing.T) {
			srv, rec := newRecordingServer(t, 200, `{"result":"ok"}`)
			client := api.New(srv.URL, "")

			_, err := runCommand(t, client, "text", tc.name, tc.args)
			require.NoError(t, err)
			assert.Equal(t, "GET", rec.Method)
			assert.Equal(t, tc.wantPath, rec.Path)
		})
	}
}

// TestTextCommands_UuidCountWithoutVersionIsIgnored documents that a count
// argument without a preceding version argument is not appended by itself:
// text uuid only ever reads args[1] once args[0] is non-empty, so passing
// count as args[0] is interpreted as (and encoded as) the version segment.
func TestTextCommands_UuidCountWithoutVersionIsIgnored(t *testing.T) {
	srv, rec := newRecordingServer(t, 200, `{}`)
	client := api.New(srv.URL, "")

	_, err := runCommand(t, client, "text", "uuid", []string{"5"})
	require.NoError(t, err)
	assert.Equal(t, "/api/v1/text/uuid/5", rec.Path)
}

// TestTextCommands_Stats verifies `text stats` POSTs a JSON body containing
// the input under the "text" key, unlike every other text subcommand which
// is a GET.
func TestTextCommands_Stats(t *testing.T) {
	srv, rec := newRecordingServer(t, 200, `{"chars":5}`)
	client := api.New(srv.URL, "")

	_, err := runCommand(t, client, "text", "stats", []string{"hello"})
	require.NoError(t, err)

	assert.Equal(t, "POST", rec.Method)
	assert.Equal(t, "/api/v1/text/stats", rec.Path)
	assert.JSONEq(t, `{"text":"hello"}`, string(rec.Body))
}

// TestTextCommands_MissingRequiredArgs verifies every required-arg command
// fails fast without contacting the server.
func TestTextCommands_MissingRequiredArgs(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{"hash", nil},
		{"hash", []string{"sha256"}},
		{"hash-all", nil},
		{"encode", []string{"base64"}},
		{"decode", []string{"base64"}},
		{"case", []string{"upper"}},
		{"rot13", nil},
		{"reverse", nil},
		{"stats", nil},
	}

	for _, tc := range tests {
		t.Run(tc.name+"_missing", func(t *testing.T) {
			srv, rec := newRecordingServer(t, 200, `{}`)
			client := api.New(srv.URL, "")

			_, err := runCommand(t, client, "text", tc.name, tc.args)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "missing required argument")
			assert.Empty(t, rec.Method)
		})
	}
}

// TestTextCommands_EncodePathSegmentPreventsPathInjection verifies input
// containing path-traversal characters is percent-encoded into a single
// path segment rather than smuggling extra path components into the
// request URL (PART 32 URL Encoding rule).
func TestTextCommands_EncodePathSegmentPreventsPathInjection(t *testing.T) {
	srv, rec := newRecordingServer(t, 200, `{}`)
	client := api.New(srv.URL, "")

	_, err := runCommand(t, client, "text", "reverse", []string{"../../etc/passwd"})
	require.NoError(t, err)

	assert.Equal(t, "/api/v1/text/reverse/..%2F..%2Fetc%2Fpasswd", rec.Path)
	assert.NotContains(t, rec.Path, "/etc/passwd")
}
