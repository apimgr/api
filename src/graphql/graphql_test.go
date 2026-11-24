package graphql

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
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

	assert.Equal(t, "Query", schema.Query.Name)

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

}

// TestGenerateSchemaSDL checks the SDL text contains the type/field
// declarations the rest of the package (and any client) depends on.
func TestGenerateSchemaSDL(t *testing.T) {
	sdl := GenerateSchemaSDL()

	for _, want := range []string{
		"type Query", "type Health", "type Version",
		"health: Health!", "textUppercase(text: String!): String!",
	} {
		assert.Contains(t, sdl, want)
	}
	assert.NotContains(t, sdl, "type Mutation")
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
	t.Run("rejects methods other than GET and POST", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPatch, "/graphql", nil)
		rec := httptest.NewRecorder()
		HandleQuery(rec, req)
		assert.Equal(t, http.StatusMethodNotAllowed, rec.Code)
	})

	t.Run("rejects invalid JSON body", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/graphql", strings.NewReader("{not json"))
		rec := httptest.NewRecorder()
		HandleQuery(rec, req)
		assert.Equal(t, http.StatusBadRequest, rec.Code)
		var response map[string]interface{}
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &response))
		assert.Equal(t, false, response["ok"])
		assert.Equal(t, "INVALID_JSON", response["error"])
		assert.NotEmpty(t, response["message"])
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
		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Nil(t, resp.Data)
		assert.Equal(t, "GRAPHQL_VALIDATION_FAILED", resp.Errors[0].Extensions["code"])
	})

	t.Run("parse errors use the GraphQL response map", func(t *testing.T) {
		body, _ := json.Marshal(Request{Query: "{ health"})
		req := httptest.NewRequest(http.MethodPost, "/graphql", bytes.NewReader(body))
		rec := httptest.NewRecorder()
		HandleQuery(rec, req)

		var resp Response
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
		assert.Equal(t, http.StatusOK, rec.Code)
		require.NotEmpty(t, resp.Errors)
		assert.Equal(t, "GRAPHQL_PARSE_FAILED", resp.Errors[0].Extensions["code"])
	})

	t.Run("GET query returns GraphQL data", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/graphql?query=%7B%20health%20%7B%20status%20%7D%20%7D", nil)
		rec := httptest.NewRecorder()
		HandleQuery(rec, req)

		var resp Response
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))
		assert.Contains(t, resp.Data.(map[string]interface{}), "health")
	})

	t.Run("GET supports variables and operationName", func(t *testing.T) {
		params := url.Values{}
		params.Set("query", "query Upper($t: String!) { textUppercase(text: $t) }")
		params.Set("variables", `{"t":"abc"}`)
		params.Set("operationName", "Upper")
		req := httptest.NewRequest(http.MethodGet, "/graphql?"+params.Encode(), nil)
		rec := httptest.NewRecorder()
		HandleQuery(rec, req)

		var resp Response
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Equal(t, "ABC", resp.Data.(map[string]interface{})["textUppercase"])
	})

	t.Run("GET rejects an unknown operationName", func(t *testing.T) {
		params := url.Values{}
		params.Set("query", "query Upper { version { version } }")
		params.Set("operationName", "Other")
		req := httptest.NewRequest(http.MethodGet, "/graphql?"+params.Encode(), nil)
		req.Header.Set("Accept-Language", "fr")
		rec := httptest.NewRecorder()
		HandleQuery(rec, req)

		var resp Response
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
		assert.Equal(t, http.StatusOK, rec.Code)
		require.NotEmpty(t, resp.Errors)
		assert.Equal(t, "GRAPHQL_VALIDATION_FAILED", resp.Errors[0].Extensions["code"])
		assert.Contains(t, resp.Errors[0].Message, "Opération inconnue")
	})

	t.Run("GET without a query returns a human-readable usage message", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/graphql", nil)
		req.Header.Set("Accept-Language", "fr")
		rec := httptest.NewRecorder()
		HandleQuery(rec, req)
		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Contains(t, rec.Header().Get("Content-Type"), "text/plain")
		assert.Contains(t, rec.Body.String(), "Fournissez un paramètre query")
	})

	t.Run("GET rejects malformed variables as a canonical transport error", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/graphql?query=%7B%20health%20%7D&variables=not-json", nil)
		req.Header.Set("Accept-Language", "fr")
		rec := httptest.NewRecorder()
		HandleQuery(rec, req)
		assert.Equal(t, http.StatusBadRequest, rec.Code)
		var response map[string]interface{}
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &response))
		assert.Equal(t, "INVALID_JSON", response["error"])
		assert.Contains(t, response["message"], "paramètre variables")
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

	t.Run("mutation is rejected by the query-only API", func(t *testing.T) {
		body, _ := json.Marshal(Request{
			Query:     `mutation($t: String!) { textLowercase(text: $t) { result } }`,
			Variables: map[string]interface{}{"t": "ABC"},
		})
		req := httptest.NewRequest(http.MethodPost, "/graphql", bytes.NewReader(body))
		req.Header.Set("Accept-Language", "fr")
		rec := httptest.NewRecorder()
		HandleQuery(rec, req)

		var resp Response
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Nil(t, resp.Data)
		require.NotEmpty(t, resp.Errors)
		assert.Equal(t, "GRAPHQL_VALIDATION_FAILED", resp.Errors[0].Extensions["code"])
		assert.Contains(t, resp.Errors[0].Message, "accepte uniquement les opérations de lecture")
	})

	t.Run("GET mutation is rejected by the query-only API", func(t *testing.T) {
		query := url.QueryEscape("mutation { textUppercase(text: \"abc\") }")
		req := httptest.NewRequest(http.MethodGet, "/graphql?query="+query, nil)
		rec := httptest.NewRecorder()
		HandleQuery(rec, req)

		var resp Response
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Nil(t, resp.Data)
		require.NotEmpty(t, resp.Errors)
		assert.Equal(t, "GRAPHQL_VALIDATION_FAILED", resp.Errors[0].Extensions["code"])
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
