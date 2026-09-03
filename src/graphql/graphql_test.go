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
		assert.Contains(t, []string{"healthy", "degraded", "unhealthy"}, data["status"])
		assert.IsType(t, int64(0), data["uptime"])
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

	t.Run("textUppercase query resolver errors on missing arg", func(t *testing.T) {
		field := schema.Query.Fields["textUppercase"]
		_, err := field.Resolve(map[string]interface{}{})
		assert.Error(t, err)
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

	t.Run("mutation textLowercase errors on missing arg", func(t *testing.T) {
		field := schema.Mutation.Fields["textLowercase"]
		_, err := field.Resolve(map[string]interface{}{})
		assert.Error(t, err)
	})

	t.Run("mutation bcryptHash returns a real bcrypt hash", func(t *testing.T) {
		field, ok := schema.Mutation.Fields["bcryptHash"]
		require.True(t, ok)
		result, err := field.Resolve(map[string]interface{}{"password": "secret"})
		require.NoError(t, err)
		data := result.(map[string]interface{})
		hash, ok := data["result"].(string)
		require.True(t, ok)
		assert.NotEqual(t, "hashed", hash)
		assert.True(t, strings.HasPrefix(hash, "$2"))
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

	t.Run("unrecognized field returns a GraphQL field error", func(t *testing.T) {
		body, _ := json.Marshal(Request{Query: "{ unknownField }"})
		req := httptest.NewRequest(http.MethodPost, "/graphql", bytes.NewReader(body))
		rec := httptest.NewRecorder()
		HandleQuery(rec, req)

		var resp Response
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
		require.NotEmpty(t, resp.Errors)
		assert.Contains(t, resp.Errors[0].Message, "unknownField")
	})

	t.Run("empty body decodes to empty query and gets default response", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/graphql", strings.NewReader("{}"))
		rec := httptest.NewRecorder()
		HandleQuery(rec, req)
		assert.Equal(t, http.StatusOK, rec.Code)
	})

	t.Run("query with a literal string argument resolves through the parser", func(t *testing.T) {
		body, _ := json.Marshal(Request{Query: `{ textUppercase(text: "abc") }`})
		req := httptest.NewRequest(http.MethodPost, "/graphql", bytes.NewReader(body))
		rec := httptest.NewRecorder()
		HandleQuery(rec, req)

		var resp Response
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
		data := resp.Data.(map[string]interface{})
		assert.Equal(t, "ABC", data["textUppercase"])
	})

	t.Run("mutation with a variable argument resolves through the parser", func(t *testing.T) {
		body, _ := json.Marshal(Request{
			Query:     `mutation($t: String!) { textLowercase(text: $t) { result } }`,
			Variables: map[string]interface{}{"t": "ABC"},
		})
		req := httptest.NewRequest(http.MethodPost, "/graphql", bytes.NewReader(body))
		rec := httptest.NewRecorder()
		HandleQuery(rec, req)

		var resp Response
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
		data := resp.Data.(map[string]interface{})
		result := data["textLowercase"].(map[string]interface{})
		assert.Equal(t, "abc", result["result"])
	})
}

// TestParserArgumentValues exercises the argument/value grammar (strings,
// numbers, booleans, null, and variables) directly against the hand-rolled
// parser, independent of any particular schema field.
func TestParserArgumentValues(t *testing.T) {
	t.Run("string, numeric, boolean, and null literals parse", func(t *testing.T) {
		op, err := newDocParser(`{ field(a: "x", b: 42, c: true, d: false, e: null) }`).parseDocument()
		require.NoError(t, err)
		require.Len(t, op.Selections, 1)
		args := op.Selections[0].Arguments
		assert.Equal(t, "x", args["a"].resolve(nil))
		assert.Equal(t, 42, args["b"].resolve(nil))
		assert.Equal(t, true, args["c"].resolve(nil))
		assert.Equal(t, false, args["d"].resolve(nil))
		assert.Nil(t, args["e"].resolve(nil))
	})

	t.Run("negative and floating point numbers parse", func(t *testing.T) {
		op, err := newDocParser(`{ field(a: -3, b: 1.5) }`).parseDocument()
		require.NoError(t, err)
		args := op.Selections[0].Arguments
		assert.Equal(t, -3, args["a"].resolve(nil))
		assert.Equal(t, 1.5, args["b"].resolve(nil))
	})

	t.Run("variable argument resolves from the variables map", func(t *testing.T) {
		op, err := newDocParser(`query($v: String!) { field(a: $v) }`).parseDocument()
		require.NoError(t, err)
		val := op.Selections[0].Arguments["a"].resolve(map[string]interface{}{"v": "hello"})
		assert.Equal(t, "hello", val)
	})

	t.Run("escaped string literal decodes escape sequences", func(t *testing.T) {
		op, err := newDocParser(`{ field(a: "line1\nline2\ttab\"quote\"") }`).parseDocument()
		require.NoError(t, err)
		assert.Equal(t, "line1\nline2\ttab\"quote\"", op.Selections[0].Arguments["a"].resolve(nil))
	})

	t.Run("unterminated argument list is a syntax error", func(t *testing.T) {
		_, err := newDocParser(`{ field(a: "x"`).parseDocument()
		assert.Error(t, err)
	})

	t.Run("unterminated string literal is a syntax error", func(t *testing.T) {
		_, err := newDocParser(`{ field(a: "x) }`).parseDocument()
		assert.Error(t, err)
	})

	t.Run("empty query is a syntax error", func(t *testing.T) {
		_, err := newDocParser("").parseDocument()
		assert.Error(t, err)
	})
}
