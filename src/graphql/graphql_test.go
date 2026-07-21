package graphql

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestBuildSchema exercises the static schema definition: shape, field
// presence, and that each resolver's Resolve function actually produces the
// documented output (not just that it's non-nil).
func TestBuildSchema(t *testing.T) {
	schema := BuildSchema()
	require.NotNil(t, schema.Query)
	require.NotNil(t, schema.Mutation)

	assert.Equal(t, "Query", schema.Query.Name)
	assert.Equal(t, "Mutation", schema.Mutation.Name)

	t.Run("health resolver", func(t *testing.T) {
		field, ok := schema.Query.Fields["health"]
		require.True(t, ok)
		result, err := field.Resolve(nil)
		require.NoError(t, err)
		data := result.(map[string]interface{})
		assert.Equal(t, "ok", data["status"])
	})

	t.Run("version resolver", func(t *testing.T) {
		field, ok := schema.Query.Fields["version"]
		require.True(t, ok)
		result, err := field.Resolve(nil)
		require.NoError(t, err)
		data := result.(map[string]interface{})
		assert.Contains(t, data, "version")
		assert.Contains(t, data, "commit_id")
		assert.Contains(t, data, "build_date")
	})

	t.Run("textUppercase query resolver converts case", func(t *testing.T) {
		field, ok := schema.Query.Fields["textUppercase"]
		require.True(t, ok)
		result, err := field.Resolve(map[string]interface{}{"text": "hello"})
		require.NoError(t, err)
		assert.Equal(t, "HELLO", result)
	})

	t.Run("textUppercase query resolver panics on missing arg", func(t *testing.T) {
		field := schema.Query.Fields["textUppercase"]
		assert.Panics(t, func() {
			_, _ = field.Resolve(map[string]interface{}{})
		})
	})

	t.Run("generateUUID resolver returns a UUID-shaped string", func(t *testing.T) {
		field, ok := schema.Query.Fields["generateUUID"]
		require.True(t, ok)
		result, err := field.Resolve(nil)
		require.NoError(t, err)
		s, ok := result.(string)
		require.True(t, ok)
		assert.Len(t, s, 36)
	})

	t.Run("mutation textUppercase", func(t *testing.T) {
		field, ok := schema.Mutation.Fields["textUppercase"]
		require.True(t, ok)
		result, err := field.Resolve(map[string]interface{}{"text": "abc"})
		require.NoError(t, err)
		data := result.(map[string]interface{})
		assert.Equal(t, "ABC", data["result"])
	})

	t.Run("mutation textLowercase", func(t *testing.T) {
		field, ok := schema.Mutation.Fields["textLowercase"]
		require.True(t, ok)
		result, err := field.Resolve(map[string]interface{}{"text": "ABC"})
		require.NoError(t, err)
		data := result.(map[string]interface{})
		assert.Equal(t, "abc", data["result"])
	})

	t.Run("mutation textLowercase panics on missing arg", func(t *testing.T) {
		field := schema.Mutation.Fields["textLowercase"]
		assert.Panics(t, func() {
			_, _ = field.Resolve(map[string]interface{}{})
		})
	})

	t.Run("mutation bcryptHash placeholder", func(t *testing.T) {
		field, ok := schema.Mutation.Fields["bcryptHash"]
		require.True(t, ok)
		result, err := field.Resolve(map[string]interface{}{"password": "secret"})
		require.NoError(t, err)
		data := result.(map[string]interface{})
		assert.Equal(t, "hashed", data["result"])
	})
}

// TestGenerateSchemaSDL checks the SDL text contains the type/field
// declarations the rest of the package (and any client) depends on.
func TestGenerateSchemaSDL(t *testing.T) {
	sdl := GenerateSchemaSDL()

	for _, want := range []string{
		"type Query", "type Mutation", "type Health", "type Version",
		"type TextResult", "health: Health!", "textUppercase(text: String!): String!",
		"bcryptHash(password: String!): TextResult!",
	} {
		assert.Contains(t, sdl, want)
	}
}

// TestServeSchema verifies the introspection endpoint serves the SDL as
// plain text with the correct content type, regardless of HTTP method.
func TestServeSchema(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/graphql/schema", nil)
	rec := httptest.NewRecorder()

	ServeSchema(rec, req)

	resp := rec.Result()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "text/plain", resp.Header.Get("Content-Type"))
	assert.Contains(t, rec.Body.String(), "type Query")
}

// TestHandleQuery covers method restriction, malformed JSON, and the
// pattern-matched "health"/"version"/default response branches of
// executeQuery via the HTTP handler.
func TestHandleQuery(t *testing.T) {
	t.Run("rejects non-POST", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/graphql", nil)
		rec := httptest.NewRecorder()
		HandleQuery(rec, req)
		assert.Equal(t, http.StatusMethodNotAllowed, rec.Code)
	})

	t.Run("rejects invalid JSON body", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/graphql", strings.NewReader("{not json"))
		rec := httptest.NewRecorder()
		HandleQuery(rec, req)
		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})

	t.Run("health query returns health data", func(t *testing.T) {
		body, _ := json.Marshal(Request{Query: "{ health { status } }"})
		req := httptest.NewRequest(http.MethodPost, "/graphql", bytes.NewReader(body))
		rec := httptest.NewRecorder()
		HandleQuery(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))

		var resp Response
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
		data := resp.Data.(map[string]interface{})
		assert.Contains(t, data, "health")
	})

	t.Run("version query returns version data", func(t *testing.T) {
		body, _ := json.Marshal(Request{Query: "{ version { version } }"})
		req := httptest.NewRequest(http.MethodPost, "/graphql", bytes.NewReader(body))
		rec := httptest.NewRecorder()
		HandleQuery(rec, req)

		var resp Response
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
		data := resp.Data.(map[string]interface{})
		assert.Contains(t, data, "version")
	})

	t.Run("unrecognized query gets default fallback response", func(t *testing.T) {
		body, _ := json.Marshal(Request{Query: "{ unknownField }"})
		req := httptest.NewRequest(http.MethodPost, "/graphql", bytes.NewReader(body))
		rec := httptest.NewRecorder()
		HandleQuery(rec, req)

		var resp Response
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
		data := resp.Data.(map[string]interface{})
		assert.Contains(t, data, "message")
	})

	t.Run("empty body decodes to empty query and gets default response", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/graphql", strings.NewReader("{}"))
		rec := httptest.NewRecorder()
		HandleQuery(rec, req)
		assert.Equal(t, http.StatusOK, rec.Code)
	})
}
