package cmd

import (
	"testing"

	"github.com/apimgr/api/src/client/api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRawGet_HappyPath verifies raw get issues a GET to the given path with
// key=value pairs turned into query parameters.
func TestRawGet_HappyPath(t *testing.T) {
	srv, rec := newRecordingServer(t, 200, `{"ok":true}`)
	client := api.New(srv.URL, "")

	_, err := runCommand(t, client, "raw", "get", []string{"/api/v1/text/uuid", "count=3", "version=4"})
	require.NoError(t, err)

	assert.Equal(t, "GET", rec.Method)
	assert.Equal(t, "/api/v1/text/uuid", rec.Path)
	assert.Equal(t, "3", rec.Query.Get("count"))
	assert.Equal(t, "4", rec.Query.Get("version"))
}

// TestRawGet_PathWithoutLeadingSlashIsNormalized verifies a bare path
// (no leading "/") is prefixed automatically.
func TestRawGet_PathWithoutLeadingSlashIsNormalized(t *testing.T) {
	srv, rec := newRecordingServer(t, 200, `{}`)
	client := api.New(srv.URL, "")

	_, err := runCommand(t, client, "raw", "get", []string{"api/v1/system/health"})
	require.NoError(t, err)
	assert.Equal(t, "/api/v1/system/health", rec.Path)
}

// TestRawGet_MalformedKeyValuePairsAreIgnored verifies a token with no "="
// is silently skipped rather than corrupting the query string or erroring.
func TestRawGet_MalformedKeyValuePairsAreIgnored(t *testing.T) {
	srv, rec := newRecordingServer(t, 200, `{}`)
	client := api.New(srv.URL, "")

	_, err := runCommand(t, client, "raw", "get", []string{"/api/v1/system/health", "not-a-pair", "real=1"})
	require.NoError(t, err)
	assert.Equal(t, "1", rec.Query.Get("real"))
	assert.Empty(t, rec.Query.Get("not-a-pair"))
}

// TestRawGet_MissingPath verifies the required path argument is enforced.
func TestRawGet_MissingPath(t *testing.T) {
	srv, rec := newRecordingServer(t, 200, `{}`)
	client := api.New(srv.URL, "")

	_, err := runCommand(t, client, "raw", "get", nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing required argument")
	assert.Empty(t, rec.Method)
}

// TestRawPost_HappyPath verifies raw post issues a POST with a JSON body
// built from key=value pairs.
func TestRawPost_HappyPath(t *testing.T) {
	srv, rec := newRecordingServer(t, 200, `{"ok":true}`)
	client := api.New(srv.URL, "")

	_, err := runCommand(t, client, "raw", "post", []string{"/api/v1/text/stats", "text=hello world"})
	require.NoError(t, err)

	assert.Equal(t, "POST", rec.Method)
	assert.Equal(t, "/api/v1/text/stats", rec.Path)
	assert.JSONEq(t, `{"text":"hello world"}`, string(rec.Body))
}

// TestRawPost_PathWithoutLeadingSlashIsNormalized mirrors the get case for
// post.
func TestRawPost_PathWithoutLeadingSlashIsNormalized(t *testing.T) {
	srv, rec := newRecordingServer(t, 200, `{}`)
	client := api.New(srv.URL, "")

	_, err := runCommand(t, client, "raw", "post", []string{"api/v1/text/stats", "text=hi"})
	require.NoError(t, err)
	assert.Equal(t, "/api/v1/text/stats", rec.Path)
}

// TestRawPost_MalformedKeyValuePairsAreIgnored mirrors the get case: a
// token without "=" contributes nothing to the JSON payload.
func TestRawPost_MalformedKeyValuePairsAreIgnored(t *testing.T) {
	srv, rec := newRecordingServer(t, 200, `{}`)
	client := api.New(srv.URL, "")

	_, err := runCommand(t, client, "raw", "post", []string{"/api/v1/text/stats", "malformed", "text=hi"})
	require.NoError(t, err)
	assert.JSONEq(t, `{"text":"hi"}`, string(rec.Body))
}

// TestRawPost_MissingPath verifies the required path argument is enforced.
func TestRawPost_MissingPath(t *testing.T) {
	srv, rec := newRecordingServer(t, 200, `{}`)
	client := api.New(srv.URL, "")

	_, err := runCommand(t, client, "raw", "post", nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing required argument")
	assert.Empty(t, rec.Method)
}
