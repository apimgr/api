package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// doGet builds a request against target with the given raw query string and
// runs it through h, returning the recorder for assertion.
func doGet(h http.HandlerFunc, target string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, target, nil)
	rec := httptest.NewRecorder()
	h(rec, req)
	return rec
}

// decodeJSON decodes the recorder body into a generic map for field checks.
func decodeJSON(t *testing.T, rec *httptest.ResponseRecorder) map[string]interface{} {
	t.Helper()
	var m map[string]interface{}
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&m))
	return m
}

// TestTextUUIDHandler covers default version, explicit valid versions, and
// the fallback-to-v4 behavior for unrecognized version values (per
// text.UUID's default case).
func TestTextUUIDHandler(t *testing.T) {
	tests := []struct {
		name        string
		query       string
		wantVersion int
	}{
		{"no version defaults to 4", "/api/v1/text/uuid", 4},
		{"version 1", "/api/v1/text/uuid?version=1", 1},
		{"version 4 explicit", "/api/v1/text/uuid?version=4", 4},
		{"version 7", "/api/v1/text/uuid?version=7", 7},
		{"unrecognized version falls back in service layer", "/api/v1/text/uuid?version=99", 99},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := doGet(TextUUIDHandler, tt.query)
			assert.Equal(t, http.StatusOK, rec.Code)
			m := decodeJSON(t, rec)
			assert.NotEmpty(t, m["uuid"])
			assert.EqualValues(t, tt.wantVersion, m["version"])
		})
	}
}

// TestTextUUIDsHandler covers the default count, an explicit count, and the
// zero-count-defaults-to-10 boundary (count=0 is treated as "unset" by the
// handler's own `if count == 0 { count = 10 }` check).
func TestTextUUIDsHandler(t *testing.T) {
	tests := []struct {
		name      string
		query     string
		wantCount int
	}{
		{"defaults to 10", "/api/v1/text/uuids", 10},
		{"explicit count", "/api/v1/text/uuids?count=3", 3},
		{"count=0 defaults to 10", "/api/v1/text/uuids?count=0", 10},
		{"single uuid", "/api/v1/text/uuids?count=1", 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := doGet(TextUUIDsHandler, tt.query)
			assert.Equal(t, http.StatusOK, rec.Code)
			m := decodeJSON(t, rec)
			uuids, ok := m["uuids"].([]interface{})
			require.True(t, ok)
			assert.Len(t, uuids, tt.wantCount)
			assert.EqualValues(t, tt.wantCount, m["count"])
		})
	}
}

// TestTextHashHandler covers the sha256 default algorithm, an explicit
// algorithm, the missing-input 400, and the unsupported-algorithm 400
// (routed through text.Hash's own error).
func TestTextHashHandler(t *testing.T) {
	t.Run("missing input returns 400", func(t *testing.T) {
		rec := doGet(TextHashHandler, "/api/v1/text/hash?algorithm=md5")
		assert.Equal(t, http.StatusBadRequest, rec.Code)
		m := decodeJSON(t, rec)
		assert.Equal(t, "input parameter required", m["error"])
	})

	t.Run("default algorithm is sha256", func(t *testing.T) {
		rec := doGet(TextHashHandler, "/api/v1/text/hash?input=hello")
		assert.Equal(t, http.StatusOK, rec.Code)
		m := decodeJSON(t, rec)
		assert.Equal(t, "sha256", m["algorithm"])
		assert.Equal(t, "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824", m["hash"])
	})

	t.Run("explicit md5 algorithm", func(t *testing.T) {
		rec := doGet(TextHashHandler, "/api/v1/text/hash?algorithm=md5&input=hello")
		assert.Equal(t, http.StatusOK, rec.Code)
		m := decodeJSON(t, rec)
		assert.Equal(t, "5d41402abc4b2a76b9719d911017c592", m["hash"])
	})

	t.Run("unsupported algorithm returns 400", func(t *testing.T) {
		rec := doGet(TextHashHandler, "/api/v1/text/hash?algorithm=notreal&input=hello")
		assert.Equal(t, http.StatusBadRequest, rec.Code)
		m := decodeJSON(t, rec)
		assert.Contains(t, m["error"], "unsupported algorithm")
	})
}

// TestTextHashAllHandler confirms the missing-input error and that a
// populated input yields all five algorithms plus the echoed input.
func TestTextHashAllHandler(t *testing.T) {
	t.Run("missing input returns 400", func(t *testing.T) {
		rec := doGet(TextHashAllHandler, "/api/v1/text/hash/all")
		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})

	t.Run("happy path returns all algorithms", func(t *testing.T) {
		rec := doGet(TextHashAllHandler, "/api/v1/text/hash/all?input=hello")
		assert.Equal(t, http.StatusOK, rec.Code)
		m := decodeJSON(t, rec)
		for _, alg := range []string{"md5", "sha1", "sha256", "sha384", "sha512"} {
			assert.NotEmpty(t, m[alg], "expected %s to be present", alg)
		}
		assert.Equal(t, "hello", m["input"])
	})
}

// TestTextEncodingHandlers table-drives every reversible encode/decode pair
// (base64, base64url, base32, hex, url) through a round trip, plus the
// shared missing-input 400 behavior each handler implements identically.
func TestTextEncodingHandlers(t *testing.T) {
	type pair struct {
		name        string
		encode      http.HandlerFunc
		decode      http.HandlerFunc
		encodePath  string
		decodePath  string
		resultField string
	}

	pairs := []pair{
		{"base64", TextBase64EncodeHandler, TextBase64DecodeHandler, "/api/v1/text/base64/encode", "/api/v1/text/base64/decode", "encoded"},
		{"base64url", TextBase64URLEncodeHandler, TextBase64URLDecodeHandler, "/api/v1/text/base64url/encode", "/api/v1/text/base64url/decode", "encoded"},
		{"base32", TextBase32EncodeHandler, TextBase32DecodeHandler, "/api/v1/text/base32/encode", "/api/v1/text/base32/decode", "encoded"},
		{"hex", TextHexEncodeHandler, TextHexDecodeHandler, "/api/v1/text/hex/encode", "/api/v1/text/hex/decode", "encoded"},
		{"url", TextURLEncodeHandler, TextURLDecodeHandler, "/api/v1/text/url/encode", "/api/v1/text/url/decode", "encoded"},
	}

	for _, p := range pairs {
		t.Run(p.name+" missing input on encode", func(t *testing.T) {
			rec := doGet(p.encode, p.encodePath)
			assert.Equal(t, http.StatusBadRequest, rec.Code)
		})

		t.Run(p.name+" missing input on decode", func(t *testing.T) {
			rec := doGet(p.decode, p.decodePath)
			assert.Equal(t, http.StatusBadRequest, rec.Code)
		})

		t.Run(p.name+" round trip", func(t *testing.T) {
			encRec := doGet(p.encode, p.encodePath+"?input="+"Hello%2C+World%21")
			require.Equal(t, http.StatusOK, encRec.Code)
			encMap := decodeJSON(t, encRec)
			encoded, ok := encMap[p.resultField].(string)
			require.True(t, ok)
			require.NotEmpty(t, encoded)

			decRec := doGet(p.decode, p.decodePath+"?input="+encoded)
			require.Equal(t, http.StatusOK, decRec.Code)
			decMap := decodeJSON(t, decRec)
			assert.Equal(t, "Hello, World!", decMap["decoded"])
		})
	}
}

// TestTextDecodeHandlers_InvalidInput confirms malformed input on each
// decode endpoint surfaces the underlying decode error as a 400, not a
// panic or 500.
func TestTextDecodeHandlers_InvalidInput(t *testing.T) {
	tests := []struct {
		name    string
		handler http.HandlerFunc
		query   string
	}{
		{"base64 invalid", TextBase64DecodeHandler, "/x?input=not-valid-base64!!!"},
		{"base64url invalid", TextBase64URLDecodeHandler, "/x?input=not%20valid"},
		{"base32 invalid", TextBase32DecodeHandler, "/x?input=111"},
		{"hex invalid odd length", TextHexDecodeHandler, "/x?input=abc"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := doGet(tt.handler, tt.query)
			assert.Equal(t, http.StatusBadRequest, rec.Code)
			m := decodeJSON(t, rec)
			assert.NotEmpty(t, m["error"])
		})
	}
}

// TestTextCaseHandlers table-drives the six case-conversion endpoints,
// each of which shares the same missing-input/happy-path contract.
func TestTextCaseHandlers(t *testing.T) {
	tests := []struct {
		name    string
		handler http.HandlerFunc
		path    string
		input   string
		want    string
	}{
		{"lower", TextCaseLowerHandler, "/api/v1/text/case/lower", "HELLO", "hello"},
		{"upper", TextCaseUpperHandler, "/api/v1/text/case/upper", "hello", "HELLO"},
		{"title", TextCaseTitleHandler, "/api/v1/text/case/title", "hello world", "Hello World"},
		{"camel", TextCaseCamelHandler, "/api/v1/text/case/camel", "hello world", "helloWorld"},
		{"snake", TextCaseSnakeHandler, "/api/v1/text/case/snake", "hello world", "hello_world"},
		{"kebab", TextCaseKebabHandler, "/api/v1/text/case/kebab", "hello world", "hello-world"},
	}

	for _, tt := range tests {
		t.Run(tt.name+" missing input", func(t *testing.T) {
			rec := doGet(tt.handler, tt.path)
			assert.Equal(t, http.StatusBadRequest, rec.Code)
		})

		t.Run(tt.name+" happy path", func(t *testing.T) {
			rec := doGet(tt.handler, tt.path+"?input="+url.QueryEscape(tt.input))
			assert.Equal(t, http.StatusOK, rec.Code)
			m := decodeJSON(t, rec)
			assert.Equal(t, tt.want, m["result"])
			assert.Equal(t, tt.input, m["input"])
		})
	}
}

// TestTextReverseHandler covers missing input, a normal string, and the
// single-character boundary (reversing a length-1 string is a no-op).
func TestTextReverseHandler(t *testing.T) {
	t.Run("missing input", func(t *testing.T) {
		rec := doGet(TextReverseHandler, "/api/v1/text/reverse")
		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})

	t.Run("normal string", func(t *testing.T) {
		rec := doGet(TextReverseHandler, "/api/v1/text/reverse?input=abc")
		assert.Equal(t, http.StatusOK, rec.Code)
		m := decodeJSON(t, rec)
		assert.Equal(t, "cba", m["result"])
	})

	t.Run("single character is idempotent", func(t *testing.T) {
		rec := doGet(TextReverseHandler, "/api/v1/text/reverse?input=x")
		m := decodeJSON(t, rec)
		assert.Equal(t, "x", m["result"])
	})
}

// TestTextStatsHandler confirms missing-input 400 and that a populated
// string yields a non-empty stats payload passed straight through from the
// service layer.
func TestTextStatsHandler(t *testing.T) {
	t.Run("missing input", func(t *testing.T) {
		rec := doGet(TextStatsHandler, "/api/v1/text/stats")
		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})

	t.Run("happy path", func(t *testing.T) {
		rec := doGet(TextStatsHandler, "/api/v1/text/stats?input=hello%20world")
		assert.Equal(t, http.StatusOK, rec.Code)
		m := decodeJSON(t, rec)
		assert.NotEmpty(t, m)
	})
}

// TestTextROT13Handler confirms the classic self-inverse property: applying
// ROT13 twice via the handler returns the original text.
func TestTextROT13Handler(t *testing.T) {
	rec := doGet(TextROT13Handler, "/api/v1/text/rot13?input=Hello")
	require.Equal(t, http.StatusOK, rec.Code)
	m := decodeJSON(t, rec)
	rotated, ok := m["result"].(string)
	require.True(t, ok)
	assert.NotEqual(t, "Hello", rotated)

	rec2 := doGet(TextROT13Handler, "/api/v1/text/rot13?input="+rotated)
	m2 := decodeJSON(t, rec2)
	assert.Equal(t, "Hello", m2["result"])
}

// TestTextLoremHandlers covers the default counts and explicit small counts
// for words/sentences/paragraphs, and the zero-count-defaults boundary.
func TestTextLoremHandlers(t *testing.T) {
	t.Run("words default 10", func(t *testing.T) {
		rec := doGet(TextLoremWordsHandler, "/api/v1/text/lorem/words")
		assert.Equal(t, http.StatusOK, rec.Code)
		m := decodeJSON(t, rec)
		words, ok := m["words"].([]interface{})
		require.True(t, ok)
		assert.Len(t, words, 10)
		assert.EqualValues(t, 10, m["count"])
	})

	t.Run("words explicit count", func(t *testing.T) {
		rec := doGet(TextLoremWordsHandler, "/api/v1/text/lorem/words?count=2")
		m := decodeJSON(t, rec)
		words, ok := m["words"].([]interface{})
		require.True(t, ok)
		assert.Len(t, words, 2)
	})

	t.Run("sentences default 5", func(t *testing.T) {
		rec := doGet(TextLoremSentencesHandler, "/api/v1/text/lorem/sentences")
		assert.Equal(t, http.StatusOK, rec.Code)
		m := decodeJSON(t, rec)
		sentences, ok := m["sentences"].([]interface{})
		require.True(t, ok)
		assert.Len(t, sentences, 5)
	})

	t.Run("paragraphs default 3", func(t *testing.T) {
		rec := doGet(TextLoremParagraphsHandler, "/api/v1/text/lorem/paragraphs")
		assert.Equal(t, http.StatusOK, rec.Code)
		m := decodeJSON(t, rec)
		paragraphs, ok := m["paragraphs"].([]interface{})
		require.True(t, ok)
		assert.Len(t, paragraphs, 3)
	})
}

// TestTextIDHandlers covers the three parameterless ID-generator endpoints:
// each must return 200, a non-empty ID, and two consecutive calls must
// produce distinct values (uniqueness, not just non-empty).
func TestTextIDHandlers(t *testing.T) {
	tests := []struct {
		name    string
		handler http.HandlerFunc
		path    string
		field   string
	}{
		{"ulid", TextULIDHandler, "/api/v1/text/ulid", "ulid"},
		{"nanoid", TextNanoIDHandler, "/api/v1/text/nanoid", "nanoid"},
		{"ksuid", TextKSUIDHandler, "/api/v1/text/ksuid", "ksuid"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec1 := doGet(tt.handler, tt.path)
			assert.Equal(t, http.StatusOK, rec1.Code)
			m1 := decodeJSON(t, rec1)
			id1, ok := m1[tt.field].(string)
			require.True(t, ok)
			assert.NotEmpty(t, id1)

			rec2 := doGet(tt.handler, tt.path)
			m2 := decodeJSON(t, rec2)
			id2 := m2[tt.field].(string)
			assert.NotEqual(t, id1, id2, "consecutive %s calls should be unique", tt.name)
		})
	}
}

// TestRegisterTextRoutes mounts all text routes on a real chi.Router and
// confirms a representative sample resolve through the mux end-to-end
// (rather than testing the handler funcs directly), catching route-path
// typos that direct handler tests can't.
func TestRegisterTextRoutes(t *testing.T) {
	r := chi.NewRouter()
	RegisterTextRoutes(r)

	paths := []string{
		"/api/v1/text/uuid",
		"/api/v1/text/uuids",
		"/api/v1/text/hash?input=x",
		"/api/v1/text/hash/all?input=x",
		"/api/v1/text/base64/encode?input=x",
		"/api/v1/text/base64/decode?input=eA==",
		"/api/v1/text/base64url/encode?input=x",
		"/api/v1/text/base64url/decode?input=eA==",
		"/api/v1/text/base32/encode?input=x",
		"/api/v1/text/base32/decode?input=NQ======",
		"/api/v1/text/hex/encode?input=x",
		"/api/v1/text/hex/decode?input=78",
		"/api/v1/text/url/encode?input=x",
		"/api/v1/text/url/decode?input=x",
		"/api/v1/text/case/lower?input=X",
		"/api/v1/text/case/upper?input=x",
		"/api/v1/text/case/title?input=x",
		"/api/v1/text/case/camel?input=x",
		"/api/v1/text/case/snake?input=x",
		"/api/v1/text/case/kebab?input=x",
		"/api/v1/text/reverse?input=x",
		"/api/v1/text/stats?input=x",
		"/api/v1/text/rot13?input=x",
		"/api/v1/text/lorem/words",
		"/api/v1/text/lorem/sentences",
		"/api/v1/text/lorem/paragraphs",
		"/api/v1/text/ulid",
		"/api/v1/text/nanoid",
		"/api/v1/text/ksuid",
	}

	for _, p := range paths {
		t.Run(p, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, p, nil)
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, req)
			assert.Equal(t, http.StatusOK, rec.Code, "expected route %s to be registered and succeed", p)
		})
	}

	t.Run("unregistered path is 404", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/text/does-not-exist", nil)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		assert.Equal(t, http.StatusNotFound, rec.Code)
	})
}

// TestJSONError_ContentType confirms the shared jsonError helper always sets
// the JSON content type even on error responses, not just success ones.
func TestJSONError_ContentType(t *testing.T) {
	rec := httptest.NewRecorder()
	jsonError(rec, "boom", http.StatusTeapot)

	assert.Equal(t, http.StatusTeapot, rec.Code)
	assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))
	m := decodeJSON(t, rec)
	assert.Equal(t, "boom", m["error"])
}

// TestJSONResponse_ContentType confirms the shared jsonResponse helper sets
// the JSON content type and serializes arbitrary data.
func TestJSONResponse_ContentType(t *testing.T) {
	rec := httptest.NewRecorder()
	jsonResponse(rec, map[string]string{"ok": "yes"})

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))
	m := decodeJSON(t, rec)
	assert.Equal(t, "yes", m["ok"])
}
