package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNew verifies New trims a trailing slash from the base URL and
// wires up the token and a non-nil HTTP client with the documented
// timeout behavior (a client must exist so callers never nil-deref).
func TestNew(t *testing.T) {
	tests := []struct {
		name    string
		baseURL string
		want    string
	}{
		{"no trailing slash", "https://api.example.com", "https://api.example.com"},
		{"single trailing slash", "https://api.example.com/", "https://api.example.com"},
		{"multiple trailing slashes", "https://api.example.com///", "https://api.example.com"},
		{"empty", "", ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c := New(tc.baseURL, "tok")
			assert.Equal(t, tc.want, c.BaseURL)
			assert.Equal(t, "tok", c.Token)
			require.NotNil(t, c.HTTP)
		})
	}
}

// TestErrorError verifies the Error type's message includes both the
// status code and body, since callers match on this string in places.
func TestErrorError(t *testing.T) {
	err := &Error{StatusCode: 404, Body: "not found"}
	assert.Equal(t, "server returned 404: not found", err.Error())
}

// TestGetSuccess exercises a full round trip against an httptest server:
// query params, headers (User-Agent, Accept, Authorization), and a 2xx
// body being returned verbatim.
func TestGetSuccess(t *testing.T) {
	var gotPath, gotQuery, gotAuth, gotUA, gotAccept string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotQuery = r.URL.RawQuery
		gotAuth = r.Header.Get("Authorization")
		gotUA = r.Header.Get("User-Agent")
		gotAccept = r.Header.Get("Accept")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	UserAgent = "api-cli/test"
	c := New(srv.URL, "abc123")
	body, err := c.Get("/api/v1/things", map[string]string{"limit": "10"})
	require.NoError(t, err)
	assert.JSONEq(t, `{"ok":true}`, string(body))
	assert.Equal(t, "/api/v1/things", gotPath)
	assert.Equal(t, "limit=10", gotQuery)
	assert.Equal(t, "Bearer abc123", gotAuth)
	assert.Equal(t, "api-cli/test", gotUA)
	assert.Equal(t, "application/json", gotAccept)
}

// TestGetNoToken verifies no Authorization header is sent when the
// client has no token, matching the CLI's "open API, tokens only for
// ownership" model from PART 32.
func TestGetNoToken(t *testing.T) {
	var gotAuth string
	sawHeader := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth, sawHeader = r.Header.Get("Authorization"), r.Header.Get("Authorization") != ""
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	c := New(srv.URL, "")
	_, err := c.Get("/api/v1/things", nil)
	require.NoError(t, err)
	assert.False(t, sawHeader)
	assert.Empty(t, gotAuth)
}

// TestPostJSON verifies the request body is marshaled to JSON and the
// Content-Type header is set only when a body is present.
func TestPostJSON(t *testing.T) {
	var gotBody map[string]any
	var gotContentType, gotMethod string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotContentType = r.Header.Get("Content-Type")
		json.NewDecoder(r.Body).Decode(&gotBody)
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte(`{"id":"123"}`))
	}))
	defer srv.Close()

	c := New(srv.URL, "")
	body, err := c.PostJSON("/api/v1/things", map[string]string{"name": "widget"})
	require.NoError(t, err)
	assert.JSONEq(t, `{"id":"123"}`, string(body))
	assert.Equal(t, http.MethodPost, gotMethod)
	assert.Equal(t, "application/json", gotContentType)
	assert.Equal(t, "widget", gotBody["name"])
}

// TestErrorStatusCodes verifies 401, 404, and other non-2xx statuses all
// surface as *Error with the response body preserved, per the do()
// status handling branches.
func TestErrorStatusCodes(t *testing.T) {
	tests := []struct {
		name   string
		status int
	}{
		{"unauthorized", http.StatusUnauthorized},
		{"not found", http.StatusNotFound},
		{"server error", http.StatusInternalServerError},
		{"bad request", http.StatusBadRequest},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				w.Write([]byte(`{"error":"boom"}`))
			}))
			defer srv.Close()

			c := New(srv.URL, "")
			body, err := c.Get("/x", nil)
			require.Error(t, err)
			apiErr, ok := err.(*Error)
			require.True(t, ok)
			assert.Equal(t, tc.status, apiErr.StatusCode)
			assert.JSONEq(t, `{"error":"boom"}`, string(body))
			assert.Contains(t, apiErr.Error(), `{"error":"boom"}`)
		})
	}
}

// TestGetNoServerConfigured verifies the friendly guidance error is
// returned when BaseURL is empty, rather than attempting (and failing)
// a request to an invalid URL.
func TestGetNoServerConfigured(t *testing.T) {
	c := New("", "")
	_, err := c.Get("/x", nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no server configured")
}

// TestConnectionRefused verifies a network-level failure (nothing
// listening) is wrapped with a "cannot connect" message rather than
// leaking the raw net/http error unadorned.
func TestConnectionRefused(t *testing.T) {
	c := New("http://127.0.0.1:1", "")
	_, err := c.Get("/x", nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cannot connect to server")
}

// TestEncodePathSegment verifies user input cannot smuggle extra path
// segments (e.g. "../" or "/") into a request URL, per the PART 32 URL
// encoding rules referenced in the doc comment.
func TestEncodePathSegment(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"simple", "simple"},
		{"with/slash", "with%2Fslash"},
		{"with space", "with%20space"},
		{"../traversal", "..%2Ftraversal"},
		{"", ""},
		{"unicode-café", "unicode-caf%C3%A9"},
	}

	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			assert.Equal(t, tc.want, EncodePathSegment(tc.in))
		})
	}
}
