package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAPIJSONToText(t *testing.T) {
	tests := []struct {
		name   string
		method string
		body   string
		want   string
	}{
		{
			name:   "object preserves field order and flattens nested values",
			method: http.MethodGet,
			body:   `{"ok":true,"data":{"name":"Ada","stats":{"requests":2},"tags":["one","two"]}}`,
			want:   "OK: Retrieved successfully\nname: Ada\nstats.requests: 2\ntags: one, two\n",
		},
		{
			name:   "scalar list",
			method: http.MethodGet,
			body:   `{"ok":true,"data":["a","b"]}`,
			want:   "OK: 2 items found\na\nb\n",
		},
		{
			name:   "object list",
			method: http.MethodGet,
			body:   `{"ok":true,"data":[{"id":1,"name":"a"},{"id":2,"name":"b"}]}`,
			want:   "OK: 2 items found\nid: 1\nname: a\n\nid: 2\nname: b\n",
		},
		{
			name:   "error retains code and message",
			method: http.MethodGet,
			body:   `{"ok":false,"error":"INVALID","message":"bad input"}`,
			want:   "ERROR: INVALID: bad input\n",
		},
		{
			name:   "empty post uses create message",
			method: http.MethodPost,
			body:   `{"ok":true,"data":null}`,
			want:   "OK: OK\n",
		},
		{
			name:   "declared primary value is bare",
			method: http.MethodGet,
			body:   `{"ok":true,"data":{"text":"answer","kind":"joke"}}`,
			want:   "answer\n",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			metadata := []string{}
			if test.name == "declared primary value is bare" {
				metadata = []string{"text", "Retrieved successfully"}
			}
			got, err := apiJSONToText([]byte(test.body), test.method, "/api/v1/example", metadata...)
			require.NoError(t, err)
			assert.Equal(t, test.want, got)
		})
	}
}

func TestAPIJSONToTextUsesRequestLanguage(t *testing.T) {
	got, err := apiJSONToText([]byte(`{"ok":true,"data":null}`), http.MethodGet, "/api/v1/example", "", "", "es")
	require.NoError(t, err)
	assert.Equal(t, "Correcto: Recuperado correctamente\n", got)
}

func TestAPITextMiddlewareNegotiation(t *testing.T) {
	handler := apiTextMiddleware("api")(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true,"data":{"value":"result"}}`))
	}))
	tests := []struct {
		name        string
		path        string
		userAgent   string
		accept      string
		wantType    string
		wantContent string
	}{
		{name: "explicit text", path: "/api/v1/test", userAgent: "Mozilla/5.0", accept: "text/plain", wantType: "text/plain", wantContent: "OK: Retrieved successfully"},
		{name: "curl defaults to text", path: "/api/v1/test", userAgent: "curl/8.0", wantType: "text/plain", wantContent: "OK: Retrieved successfully"},
		{name: "explicit JSON wins for curl", path: "/api/v1/test", userAgent: "curl/8.0", accept: "application/json", wantType: "application/json", wantContent: `"value":"result"`},
		{name: "txt suffix always text", path: "/api/v1/test.txt", userAgent: "Mozilla/5.0", accept: "application/json", wantType: "text/plain", wantContent: "OK: Retrieved successfully"},
		{name: "MCP keeps JSON", path: "/api/v1/server/mcp", userAgent: "curl/8.0", wantType: "application/json", wantContent: `"value":"result"`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, test.path, nil)
			req.Header.Set("User-Agent", test.userAgent)
			if test.accept != "" {
				req.Header.Set("Accept", test.accept)
			}
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, req)
			assert.Contains(t, response.Header().Get("Content-Type"), test.wantType)
			assert.Contains(t, response.Body.String(), test.wantContent)
			if strings.Contains(test.wantType, "text/plain") {
				assert.True(t, strings.HasSuffix(response.Body.String(), "\n"))
			}
		})
	}
}

func TestAPIRoutePatternMatchesTextAliases(t *testing.T) {
	assert.True(t, apiRoutePatternMatches("/api/v1/fun/joke", "/api/v1/fun/joke"))
	assert.True(t, apiRoutePatternMatches("/api/v1/crypto/hash/{algorithm}/{input}", "/api/v1/crypto/hash/sha256/value"))
	assert.True(t, apiRoutePatternMatches("/api/v1/research/doi/*", "/api/v1/research/doi/10.1000/example"))
	assert.False(t, apiRoutePatternMatches("/api/v1/fun/joke", "/api/v1/fun/quote"))
}
