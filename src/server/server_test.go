package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/apimgr/api/src/config"
	"github.com/apimgr/api/src/paths"
)

// newTestConfig points the paths package at fresh per-test temp
// directories and loads a real default config through it, so New() and
// the handlers under test see production-shaped config values instead of
// a hand-rolled struct that could drift from defaultConfig().
func newTestConfig(t *testing.T) *config.Config {
	t.Helper()
	configDir := t.TempDir()
	dataDir := t.TempDir()
	logDir := t.TempDir()
	paths.Init(configDir, dataDir, logDir)

	cfg, err := config.Load()
	require.NoError(t, err)
	require.NotNil(t, cfg)
	return cfg
}

// reqWithParams builds a request carrying chi URL params, mimicking what
// the chi router injects before invoking a leaf handler, so handlers can
// be unit tested directly without standing up the full router tree.
func reqWithParams(method, target string, params map[string]string, body *bytes.Buffer) *http.Request {
	var req *http.Request
	if body != nil {
		req = httptest.NewRequest(method, target, body)
	} else {
		req = httptest.NewRequest(method, target, nil)
	}

	rctx := chi.NewRouteContext()
	for k, v := range params {
		rctx.URLParams.Add(k, v)
	}
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
}

// decodeJSON decodes the recorder body into a generic map for assertions
// that only need to check a handful of keys.
func decodeJSON(t *testing.T, rec *httptest.ResponseRecorder) map[string]interface{} {
	t.Helper()
	var out map[string]interface{}
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&out))
	return out
}

// newTestServer builds the real router via New(). initTemplates() parses
// the spec-mandated layout/public.tmpl plus the mandatory partial set
// (partial/*.tmpl, partial/public/*.tmpl) against every publicPages entry,
// all of which now have a corresponding template/page/*.tmpl file, so
// New() no longer panics; the recover here guards against a future
// regression reintroducing a template-wiring gap.
func newTestServer(t *testing.T, cfg *config.Config) *http.Server {
	t.Helper()
	var srv *http.Server
	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("New(cfg) panicked: %v", r)
			}
		}()
		srv = New(cfg)
	}()
	return srv
}

// TestNew_RouterServesCoreEndpoints builds the real router via New() with
// a production-shaped config and drives representative routes through it
// end-to-end (via httptest.NewServer), covering static/special files,
// health/version, and a nested API route registration all at once.
func TestNew_RouterServesCoreEndpoints(t *testing.T) {
	cfg := newTestConfig(t)
	srv := newTestServer(t, cfg)
	require.NotNil(t, srv)
	require.NotNil(t, srv.Handler)

	ts := httptest.NewServer(srv.Handler)
	defer ts.Close()

	tests := []struct {
		name       string
		method     string
		path       string
		wantStatus int
	}{
		{"home page", http.MethodGet, "/", http.StatusOK},
		{"robots.txt", http.MethodGet, "/robots.txt", http.StatusOK},
		{"security.txt", http.MethodGet, "/security.txt", http.StatusOK},
		{"well-known security.txt", http.MethodGet, "/.well-known/security.txt", http.StatusOK},
		{"manifest.json", http.MethodGet, "/manifest.json", http.StatusOK},
		{"healthz frontend", http.MethodGet, "/server/healthz", http.StatusOK},
		{"healthz api", http.MethodGet, "/api/v1/server/healthz", http.StatusOK},
		{"healthz unversioned alias", http.MethodGet, "/api/healthz", http.StatusOK},
		{"metrics", http.MethodGet, "/metrics", http.StatusOK},
		{"api v1 uuid", http.MethodGet, "/api/v1/text/uuid", http.StatusOK},
		{"api v1 docker placeholder", http.MethodGet, "/api/v1/docker/version", http.StatusNotImplemented},
		{"unknown route 404", http.MethodGet, "/this-route-does-not-exist", http.StatusNotFound},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, err := http.NewRequest(tt.method, ts.URL+tt.path, nil)
			require.NoError(t, err)
			resp, err := ts.Client().Do(req)
			require.NoError(t, err)
			defer resp.Body.Close()
			assert.Equal(t, tt.wantStatus, resp.StatusCode, "path %s", tt.path)
		})
	}
}

// TestNew_SecurityHeadersApplied confirms the security headers middleware
// registered in New() actually fires on a real request through the
// assembled router (not just in isolation).
func TestNew_SecurityHeadersApplied(t *testing.T) {
	cfg := newTestConfig(t)
	srv := newTestServer(t, cfg)

	req := httptest.NewRequest(http.MethodGet, "/robots.txt", nil)
	rec := httptest.NewRecorder()
	srv.Handler.ServeHTTP(rec, req)

	assert.Equal(t, "nosniff", rec.Header().Get("X-Content-Type-Options"))
	assert.NotEmpty(t, rec.Header().Get("X-Request-ID"))
}

// TestNew_HealthzRootAlias covers the config-gated optional "/healthz"
// root alias: present when enabled, absent (404) when not.
func TestNew_HealthzRootAlias(t *testing.T) {
	t.Run("enabled", func(t *testing.T) {
		cfg := newTestConfig(t)
		cfg.Server.Healthz.Root.Enabled = true
		srv := newTestServer(t, cfg)

		req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
		rec := httptest.NewRecorder()
		srv.Handler.ServeHTTP(rec, req)
		assert.Equal(t, http.StatusOK, rec.Code)
	})

	t.Run("disabled", func(t *testing.T) {
		cfg := newTestConfig(t)
		cfg.Server.Healthz.Root.Enabled = false
		srv := newTestServer(t, cfg)

		req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
		rec := httptest.NewRecorder()
		srv.Handler.ServeHTTP(rec, req)
		assert.Equal(t, http.StatusNotFound, rec.Code)
	})
}

// TestNewPageData covers the FQDN-based base URL derivation, including
// the "localhost"/empty-FQDN fallback branch.
func TestNewPageData(t *testing.T) {
	tests := []struct {
		name    string
		fqdn    string
		port    string
		wantURL string
	}{
		{"empty fqdn falls back to localhost", "", "8080", "http://localhost:8080"},
		{"explicit localhost fqdn", "localhost", "9090", "http://localhost:9090"},
		{"real fqdn used as-is", "example.com", "443", "http://example.com:443"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{}
			cfg.Server.FQDN = tt.fqdn
			cfg.Server.Port = tt.port
			cfg.Server.Branding.Title = "Test Title"
			cfg.Web.UI.Theme = "dark"

			data := newPageData(cfg, "home")

			assert.Equal(t, tt.wantURL, data.BaseURL)
			assert.Equal(t, "Test Title", data.SiteTitle)
			assert.Equal(t, "dark", data.Theme)
			assert.Equal(t, "home", data.ActivePage)
			assert.Equal(t, "🛠️", data.SiteIcon)
		})
	}
}

// TestGetBaseURL covers all three branches: localhost fallback, plain
// http with a real FQDN, and https once SSL is enabled (SSL takes
// priority over the plain-http FQDN branch).
func TestGetBaseURL(t *testing.T) {
	tests := []struct {
		name    string
		fqdn    string
		ssl     bool
		wantURL string
	}{
		{"empty fqdn", "", false, "http://localhost:8080"},
		{"localhost fqdn", "localhost", false, "http://localhost:8080"},
		{"real fqdn no ssl", "api.example.com", false, "http://api.example.com:8080"},
		{"real fqdn with ssl", "api.example.com", true, "https://api.example.com:8080"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{}
			cfg.Server.FQDN = tt.fqdn
			cfg.Server.Port = "8080"
			cfg.Server.SSL.Enabled = tt.ssl

			assert.Equal(t, tt.wantURL, getBaseURL(cfg))
		})
	}
}

// TestJSONResponse verifies Content-Type and that the payload round-trips
// through JSON encoding unmodified.
func TestJSONResponse(t *testing.T) {
	rec := httptest.NewRecorder()
	jsonResponse(rec, map[string]interface{}{"foo": "bar", "n": 42})

	assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))
	got := decodeJSON(t, rec)
	assert.Equal(t, "bar", got["foo"])
	assert.Equal(t, float64(42), got["n"])
}

// TestTextResponse verifies Content-Type and body are set exactly as
// given, with no transformation.
func TestTextResponse(t *testing.T) {
	rec := httptest.NewRecorder()
	textResponse(rec, "hello world")

	assert.Equal(t, "text/plain; charset=utf-8", rec.Header().Get("Content-Type"))
	assert.Equal(t, "hello world", rec.Body.String())
}

// TestErrorResponse covers that the status code, Content-Type, and JSON
// error envelope are all set correctly for a range of status codes.
func TestErrorResponse(t *testing.T) {
	tests := []struct {
		name    string
		message string
		status  int
	}{
		{"bad request", "invalid input", http.StatusBadRequest},
		{"internal error", "boom", http.StatusInternalServerError},
		{"empty message", "", http.StatusBadRequest},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			errorResponse(rec, tt.message, tt.status)

			assert.Equal(t, tt.status, rec.Code)
			assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))
			got := decodeJSON(t, rec)
			assert.Equal(t, tt.message, got["error"])
		})
	}
}

// TestApiUUIDHandler covers default (v4) and explicit version params, and
// that the JSON envelope contains a non-empty uuid plus the echoed
// version.
func TestApiUUIDHandler(t *testing.T) {
	tests := []struct {
		name        string
		versionParm string
		wantVersion float64
	}{
		{"no version param defaults to 4", "", 4},
		{"explicit v4", "4", 4},
		{"explicit v1", "1", 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			params := map[string]string{}
			if tt.versionParm != "" {
				params["version"] = tt.versionParm
			}
			req := reqWithParams(http.MethodGet, "/api/v1/text/uuid", params, nil)
			rec := httptest.NewRecorder()

			apiUUIDHandler(rec, req)

			assert.Equal(t, http.StatusOK, rec.Code)
			got := decodeJSON(t, rec)
			assert.NotEmpty(t, got["uuid"])
			assert.Equal(t, tt.wantVersion, got["version"])
		})
	}
}

// TestApiUUIDHandler_UnrecognizedVersionFallsBackToV4 covers that an
// unrecognized version number is not an error — text.UUID's default case
// silently falls back to a v4 UUID rather than rejecting the request.
func TestApiUUIDHandler_UnrecognizedVersionFallsBackToV4(t *testing.T) {
	req := reqWithParams(http.MethodGet, "/api/v1/text/uuid/99", map[string]string{"version": "99"}, nil)
	rec := httptest.NewRecorder()

	apiUUIDHandler(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	got := decodeJSON(t, rec)
	assert.NotEmpty(t, got["uuid"])
	assert.Equal(t, float64(99), got["version"])
}

// TestApiUUIDTextHandler mirrors the JSON case but for the plain-text
// variant.
func TestApiUUIDTextHandler(t *testing.T) {
	t.Run("valid", func(t *testing.T) {
		req := reqWithParams(http.MethodGet, "/api/v1/text/uuid.txt", nil, nil)
		rec := httptest.NewRecorder()
		apiUUIDTextHandler(rec, req)
		assert.Equal(t, http.StatusOK, rec.Code)
		assert.NotEmpty(t, rec.Body.String())
	})

	t.Run("unrecognized version still succeeds via v4 fallback", func(t *testing.T) {
		req := reqWithParams(http.MethodGet, "/api/v1/text/uuid/99.txt", map[string]string{"version": "99"}, nil)
		rec := httptest.NewRecorder()
		apiUUIDTextHandler(rec, req)
		assert.Equal(t, http.StatusOK, rec.Code)
		assert.False(t, strings.HasPrefix(rec.Body.String(), "Error:"))
	})
}

// TestApiUUIDBatchHandler covers default count/version, an explicit
// count, and the boundary case of a zero count, which text.UUIDs clamps
// up to 1 rather than returning an empty batch.
func TestApiUUIDBatchHandler(t *testing.T) {
	tests := []struct {
		name      string
		version   string
		count     string
		wantCount float64
	}{
		{"defaults", "", "", 10},
		{"explicit count 3", "4", "3", 3},
		{"zero count clamped to 1", "4", "0", 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			params := map[string]string{}
			if tt.version != "" {
				params["version"] = tt.version
			}
			if tt.count != "" {
				params["count"] = tt.count
			}
			req := reqWithParams(http.MethodGet, "/api/v1/text/uuid/4/3", params, nil)
			rec := httptest.NewRecorder()

			apiUUIDBatchHandler(rec, req)

			assert.Equal(t, http.StatusOK, rec.Code)
			got := decodeJSON(t, rec)
			assert.Equal(t, tt.wantCount, got["count"])
			uuids, ok := got["uuids"].([]interface{})
			require.True(t, ok)
			assert.Len(t, uuids, int(tt.wantCount))
		})
	}
}

// TestApiHashHandler covers a supported algorithm, an unsupported one
// (must 400 with a JSON error, never 500), and an empty input.
func TestApiHashHandler(t *testing.T) {
	tests := []struct {
		name       string
		algorithm  string
		input      string
		wantStatus int
	}{
		{"sha256 of hello", "sha256", "hello", http.StatusOK},
		{"md5 of empty string", "md5", "", http.StatusOK},
		{"unsupported algorithm", "not-a-real-algo", "hello", http.StatusBadRequest},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := reqWithParams(http.MethodGet, "/api/v1/text/hash/x/y", map[string]string{
				"algorithm": tt.algorithm,
				"input":     tt.input,
			}, nil)
			rec := httptest.NewRecorder()

			apiHashHandler(rec, req)

			assert.Equal(t, tt.wantStatus, rec.Code)
			got := decodeJSON(t, rec)
			if tt.wantStatus == http.StatusOK {
				assert.NotEmpty(t, got["hash"])
			} else {
				assert.NotEmpty(t, got["error"])
			}
		})
	}
}

// TestApiHashMultiHandler covers that every requested algorithm shows up
// in the map, including for an empty input string.
func TestApiHashMultiHandler(t *testing.T) {
	req := reqWithParams(http.MethodGet, "/api/v1/text/hash/multi/hello", map[string]string{"input": "hello"}, nil)
	rec := httptest.NewRecorder()

	apiHashMultiHandler(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	got := decodeJSON(t, rec)
	hashes, ok := got["hashes"].(map[string]interface{})
	require.True(t, ok)
	assert.NotEmpty(t, hashes)
}

// TestApiEncodeDecodeHandler_RoundTrip covers every supported encoding
// round-tripping correctly, plus the unsupported-encoding error path for
// both encode and decode.
func TestApiEncodeDecodeHandler_RoundTrip(t *testing.T) {
	encodings := []string{"base64", "base64url", "base32", "hex", "base16", "url"}
	const input = "Hello, World! / test+data="

	for _, enc := range encodings {
		t.Run(enc, func(t *testing.T) {
			encReq := reqWithParams(http.MethodGet, "/api/v1/text/encode/x/y", map[string]string{
				"encoding": enc,
				"input":    input,
			}, nil)
			encRec := httptest.NewRecorder()
			apiEncodeHandler(encRec, encReq)
			require.Equal(t, http.StatusOK, encRec.Code)
			encGot := decodeJSON(t, encRec)
			encoded, _ := encGot["output"].(string)
			require.NotEmpty(t, encoded)

			decReq := reqWithParams(http.MethodGet, "/api/v1/text/decode/x/y", map[string]string{
				"encoding": enc,
				"input":    encoded,
			}, nil)
			decRec := httptest.NewRecorder()
			apiDecodeHandler(decRec, decReq)
			require.Equal(t, http.StatusOK, decRec.Code)
			decGot := decodeJSON(t, decRec)
			assert.Equal(t, input, decGot["output"])
		})
	}

	t.Run("unsupported encoding on encode", func(t *testing.T) {
		req := reqWithParams(http.MethodGet, "/api/v1/text/encode/x/y", map[string]string{
			"encoding": "rot47",
			"input":    "abc",
		}, nil)
		rec := httptest.NewRecorder()
		apiEncodeHandler(rec, req)
		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})

	t.Run("unsupported encoding on decode", func(t *testing.T) {
		req := reqWithParams(http.MethodGet, "/api/v1/text/decode/x/y", map[string]string{
			"encoding": "rot47",
			"input":    "abc",
		}, nil)
		rec := httptest.NewRecorder()
		apiDecodeHandler(rec, req)
		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})

	t.Run("malformed base64 fails to decode", func(t *testing.T) {
		req := reqWithParams(http.MethodGet, "/api/v1/text/decode/x/y", map[string]string{
			"encoding": "base64",
			"input":    "%%%not-base64%%%",
		}, nil)
		rec := httptest.NewRecorder()
		apiDecodeHandler(rec, req)
		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})
}

// TestApiEncodeTextHandler_UnsupportedEncoding covers the text-response
// error path, which (unlike the JSON handler) always answers 200 with an
// "Error: ..." body.
func TestApiEncodeTextHandler_UnsupportedEncoding(t *testing.T) {
	req := reqWithParams(http.MethodGet, "/api/v1/text/encode/x/y.txt", map[string]string{
		"encoding": "nope",
		"input":    "abc",
	}, nil)
	rec := httptest.NewRecorder()
	apiEncodeTextHandler(rec, req)

	assert.Equal(t, "Error: unsupported encoding", rec.Body.String())
}

// TestApiDecodeTextHandler_DecodeError covers the text-response decode
// error path.
func TestApiDecodeTextHandler_DecodeError(t *testing.T) {
	req := reqWithParams(http.MethodGet, "/api/v1/text/decode/x/y.txt", map[string]string{
		"encoding": "base64",
		"input":    "%%%",
	}, nil)
	rec := httptest.NewRecorder()
	apiDecodeTextHandler(rec, req)

	assert.True(t, strings.HasPrefix(rec.Body.String(), "Error:"))
}

// TestApiCaseHandler covers every supported style plus the unsupported
// style error path.
func TestApiCaseHandler(t *testing.T) {
	tests := []struct {
		name       string
		style      string
		input      string
		wantOutput string
		wantStatus int
	}{
		{"lower", "lower", "HeLLo", "hello", http.StatusOK},
		{"upper", "upper", "HeLLo", "HELLO", http.StatusOK},
		{"snake", "snake", "Hello World", "hello_world", http.StatusOK},
		{"unsupported style", "sarcasm", "Hello", "", http.StatusBadRequest},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := reqWithParams(http.MethodGet, "/api/v1/text/case/x/y", map[string]string{
				"style": tt.style,
				"input": tt.input,
			}, nil)
			rec := httptest.NewRecorder()
			apiCaseHandler(rec, req)

			assert.Equal(t, tt.wantStatus, rec.Code)
			if tt.wantStatus == http.StatusOK {
				got := decodeJSON(t, rec)
				assert.Equal(t, tt.wantOutput, got["output"])
			}
		})
	}
}

// TestApiLoremHandler_Defaults covers the default type ("paragraphs") and
// default count (5) when neither URL param is present.
func TestApiLoremHandler_Defaults(t *testing.T) {
	req := reqWithParams(http.MethodGet, "/api/v1/text/lorem", nil, nil)
	rec := httptest.NewRecorder()
	apiLoremHandler(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	got := decodeJSON(t, rec)
	assert.Equal(t, "paragraphs", got["type"])
	assert.Equal(t, float64(5), got["count"])
}

// TestApiLoremHandler_Words covers the words branch with an explicit
// count.
func TestApiLoremHandler_Words(t *testing.T) {
	req := reqWithParams(http.MethodGet, "/api/v1/text/lorem/words/3", map[string]string{
		"type":  "words",
		"count": "3",
	}, nil)
	rec := httptest.NewRecorder()
	apiLoremHandler(rec, req)

	got := decodeJSON(t, rec)
	words, ok := got["text"].([]interface{})
	require.True(t, ok)
	assert.Len(t, words, 3)
}

// TestApiTextStatsHandler covers a valid JSON body and the invalid-body
// error path.
func TestApiTextStatsHandler(t *testing.T) {
	t.Run("valid body", func(t *testing.T) {
		body := bytes.NewBufferString(`{"text":"hello world"}`)
		req := reqWithParams(http.MethodPost, "/api/v1/text/stats", nil, body)
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		apiTextStatsHandler(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
	})

	t.Run("invalid json body", func(t *testing.T) {
		body := bytes.NewBufferString(`not json`)
		req := reqWithParams(http.MethodPost, "/api/v1/text/stats", nil, body)
		rec := httptest.NewRecorder()

		apiTextStatsHandler(rec, req)

		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})

	t.Run("empty body", func(t *testing.T) {
		req := reqWithParams(http.MethodPost, "/api/v1/text/stats", nil, bytes.NewBuffer(nil))
		rec := httptest.NewRecorder()

		apiTextStatsHandler(rec, req)

		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})
}

// TestApiROT13Handler_Idempotent verifies ROT13 applied twice returns the
// original input (a genuine algorithmic property, not just a fixed
// example).
func TestApiROT13Handler_Idempotent(t *testing.T) {
	const input = "Hello, World!"

	req1 := reqWithParams(http.MethodGet, "/api/v1/text/rot13/x", map[string]string{"input": input}, nil)
	rec1 := httptest.NewRecorder()
	apiROT13Handler(rec1, req1)
	got1 := decodeJSON(t, rec1)
	once, _ := got1["output"].(string)
	require.NotEqual(t, input, once)

	req2 := reqWithParams(http.MethodGet, "/api/v1/text/rot13/x", map[string]string{"input": once}, nil)
	rec2 := httptest.NewRecorder()
	apiROT13Handler(rec2, req2)
	got2 := decodeJSON(t, rec2)
	assert.Equal(t, input, got2["output"])
}

// TestApiReverseHandler covers a normal string, an empty string, and a
// palindrome (output equals input).
func TestApiReverseHandler(t *testing.T) {
	tests := []struct {
		name       string
		input      string
		wantOutput string
	}{
		{"normal string", "abc", "cba"},
		{"empty string", "", ""},
		{"palindrome", "level", "level"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := reqWithParams(http.MethodGet, "/api/v1/text/reverse/x", map[string]string{"input": tt.input}, nil)
			rec := httptest.NewRecorder()
			apiReverseHandler(rec, req)

			got := decodeJSON(t, rec)
			assert.Equal(t, tt.wantOutput, got["output"])
		})
	}
}

// bcryptHashForTest generates a bcrypt hash via the same handler-level
// entry point used elsewhere in this file, at a low cost to keep tests
// fast.
func bcryptHashForTest(t *testing.T, password string) string {
	t.Helper()
	req := reqWithParams(http.MethodGet, "/api/v1/crypto/bcrypt/x", map[string]string{
		"password": password,
		"cost":     "4",
	}, nil)
	rec := httptest.NewRecorder()
	apiBcryptHandler(rec, req)
	got := decodeJSON(t, rec)
	hash, _ := got["hash"].(string)
	require.NotEmpty(t, hash)
	return hash
}

// TestApiBcryptHandler_AndVerify covers hash generation at an explicit
// cost plus round-tripping through the GET-param verify handler, both
// for the correct password (valid=true) and a wrong one (valid=false).
func TestApiBcryptHandler_AndVerify(t *testing.T) {
	req := reqWithParams(http.MethodGet, "/api/v1/crypto/bcrypt/x", map[string]string{
		"password": "correct horse",
		"cost":     "4",
	}, nil)
	rec := httptest.NewRecorder()
	apiBcryptHandler(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	got := decodeJSON(t, rec)
	hash, _ := got["hash"].(string)
	require.NotEmpty(t, hash)
	assert.Equal(t, float64(4), got["cost"])

	t.Run("correct password verifies true", func(t *testing.T) {
		vReq := reqWithParams(http.MethodGet, "/api/v1/crypto/bcrypt/verify/x/y", map[string]string{
			"password": "correct horse",
			"hash":     hash,
		}, nil)
		vRec := httptest.NewRecorder()
		apiBcryptVerifyGetHandler(vRec, vReq)
		vGot := decodeJSON(t, vRec)
		assert.Equal(t, true, vGot["valid"])
	})

	t.Run("wrong password verifies false", func(t *testing.T) {
		vReq := reqWithParams(http.MethodGet, "/api/v1/crypto/bcrypt/verify/x/y", map[string]string{
			"password": "wrong password",
			"hash":     hash,
		}, nil)
		vRec := httptest.NewRecorder()
		apiBcryptVerifyGetHandler(vRec, vReq)
		vGot := decodeJSON(t, vRec)
		assert.Equal(t, false, vGot["valid"])
	})
}

// TestApiBcryptVerifyHandler_PostBody covers the JSON-body verify
// endpoint including the invalid-body error path.
func TestApiBcryptVerifyHandler_PostBody(t *testing.T) {
	hash := bcryptHashForTest(t, "swordfish")

	t.Run("valid body", func(t *testing.T) {
		body := bytes.NewBufferString(`{"password":"swordfish","hash":"` + hash + `"}`)
		req := reqWithParams(http.MethodPost, "/api/v1/crypto/bcrypt/verify", nil, body)
		rec := httptest.NewRecorder()
		apiBcryptVerifyHandler(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
		got := decodeJSON(t, rec)
		assert.Equal(t, true, got["valid"])
	})

	t.Run("invalid body", func(t *testing.T) {
		body := bytes.NewBufferString(`not json`)
		req := reqWithParams(http.MethodPost, "/api/v1/crypto/bcrypt/verify", nil, body)
		rec := httptest.NewRecorder()
		apiBcryptVerifyHandler(rec, req)

		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})
}

// TestApiPasswordHandler covers the default length and an explicit
// length, and that the generated password's length matches the request.
func TestApiPasswordHandler(t *testing.T) {
	tests := []struct {
		name       string
		lengthParm string
		wantLen    float64
	}{
		{"default length", "", 16},
		{"explicit length 24", "24", 24},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			params := map[string]string{}
			if tt.lengthParm != "" {
				params["length"] = tt.lengthParm
			}
			req := reqWithParams(http.MethodGet, "/api/v1/crypto/password", params, nil)
			rec := httptest.NewRecorder()
			apiPasswordHandler(rec, req)

			assert.Equal(t, http.StatusOK, rec.Code)
			got := decodeJSON(t, rec)
			pw, _ := got["password"].(string)
			assert.Len(t, pw, int(tt.wantLen))
		})
	}
}

// TestApiPINHandler covers an explicit PIN length, and that the PIN is
// digits-only.
func TestApiPINHandler(t *testing.T) {
	req := reqWithParams(http.MethodGet, "/api/v1/crypto/pin/6", map[string]string{"length": "6"}, nil)
	rec := httptest.NewRecorder()
	apiPINHandler(rec, req)

	got := decodeJSON(t, rec)
	pin, _ := got["pin"].(string)
	assert.Len(t, pin, 6)
	for _, c := range pin {
		assert.True(t, c >= '0' && c <= '9', "PIN must be digits only, got %q", pin)
	}
}

// TestApiTOTPGenerateAndVerify exercises the full TOTP lifecycle: generate
// a secret, derive the current code from it, then verify that code — and
// confirm an obviously-wrong code fails verification.
func TestApiTOTPGenerateAndVerify(t *testing.T) {
	genReq := reqWithParams(http.MethodGet, "/api/v1/crypto/totp/secret", nil, nil)
	genRec := httptest.NewRecorder()
	apiTOTPGenerateHandler(genRec, genReq)
	require.Equal(t, http.StatusOK, genRec.Code)
	genGot := decodeJSON(t, genRec)
	secret, _ := genGot["secret"].(string)
	require.NotEmpty(t, secret)

	codeReq := reqWithParams(http.MethodGet, "/api/v1/crypto/totp/code/x", map[string]string{"secret": secret}, nil)
	codeRec := httptest.NewRecorder()
	apiTOTPCodeHandler(codeRec, codeReq)
	require.Equal(t, http.StatusOK, codeRec.Code)
	codeGot := decodeJSON(t, codeRec)
	code, _ := codeGot["code"].(string)
	require.NotEmpty(t, code)

	t.Run("correct code verifies true", func(t *testing.T) {
		vReq := reqWithParams(http.MethodGet, "/api/v1/crypto/totp/verify/x/y", map[string]string{
			"secret": secret,
			"code":   code,
		}, nil)
		vRec := httptest.NewRecorder()
		apiTOTPVerifyHandler(vRec, vReq)
		vGot := decodeJSON(t, vRec)
		assert.Equal(t, true, vGot["valid"])
	})

	t.Run("wrong code verifies false", func(t *testing.T) {
		vReq := reqWithParams(http.MethodGet, "/api/v1/crypto/totp/verify/x/y", map[string]string{
			"secret": secret,
			"code":   "000000",
		}, nil)
		vRec := httptest.NewRecorder()
		apiTOTPVerifyHandler(vRec, vReq)
		vGot := decodeJSON(t, vRec)
		assert.Equal(t, false, vGot["valid"])
	})
}

// TestApiTOTPCodeHandler_InvalidSecret covers a malformed base32 secret
// producing a 400 rather than a panic.
func TestApiTOTPCodeHandler_InvalidSecret(t *testing.T) {
	req := reqWithParams(http.MethodGet, "/api/v1/crypto/totp/code/x", map[string]string{"secret": "not valid base32!!"}, nil)
	rec := httptest.NewRecorder()
	apiTOTPCodeHandler(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

// TestApiRandomBytesHandler_And_Hex cover that the requested byte count
// is honored (including the zero-count boundary) and that the hex
// variant is exactly twice as long as the requested byte count.
func TestApiRandomBytesHandler_And_Hex(t *testing.T) {
	tests := []struct {
		name  string
		count string
	}{
		{"default count", ""},
		{"explicit count 8", "8"},
		{"zero count", "0"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			params := map[string]string{}
			if tt.count != "" {
				params["count"] = tt.count
			}

			req := reqWithParams(http.MethodGet, "/api/v1/crypto/random/bytes/x", params, nil)
			rec := httptest.NewRecorder()
			apiRandomBytesHandler(rec, req)
			require.Equal(t, http.StatusOK, rec.Code)
			got := decodeJSON(t, rec)

			hexReq := reqWithParams(http.MethodGet, "/api/v1/crypto/random/hex/x", params, nil)
			hexRec := httptest.NewRecorder()
			apiRandomHexHandler(hexRec, hexReq)
			require.Equal(t, http.StatusOK, hexRec.Code)

			wantLen := got["length"].(float64)
			assert.Len(t, hexRec.Body.String(), int(wantLen)*2)
		})
	}
}

// TestApiPasswordStrengthHandler_And_Post cover both the GET URL-param
// and POST JSON-body variants of password-strength, including the
// malformed JSON body error path for the POST variant.
func TestApiPasswordStrengthHandler_And_Post(t *testing.T) {
	weakReq := reqWithParams(http.MethodGet, "/api/v1/crypto/password/strength/x", map[string]string{"password": "1234"}, nil)
	weakRec := httptest.NewRecorder()
	apiPasswordStrengthHandler(weakRec, weakReq)
	assert.Equal(t, http.StatusOK, weakRec.Code)

	body := bytes.NewBufferString(`{"password":"Tr0ub4dor&3xtraLong!"}`)
	strongReq := reqWithParams(http.MethodPost, "/api/v1/crypto/password/strength", nil, body)
	strongRec := httptest.NewRecorder()
	apiPasswordStrengthPostHandler(strongRec, strongReq)
	assert.Equal(t, http.StatusOK, strongRec.Code)

	t.Run("invalid body", func(t *testing.T) {
		badBody := bytes.NewBufferString(`not json`)
		req := reqWithParams(http.MethodPost, "/api/v1/crypto/password/strength", nil, badBody)
		rec := httptest.NewRecorder()
		apiPasswordStrengthPostHandler(rec, req)
		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})
}

// TestApiDateTimeNowHandler covers no-timezone, URL-param timezone, and
// an invalid timezone error path.
func TestApiDateTimeNowHandler(t *testing.T) {
	t.Run("no timezone", func(t *testing.T) {
		req := reqWithParams(http.MethodGet, "/api/v1/datetime/now", nil, nil)
		rec := httptest.NewRecorder()
		apiDateTimeNowHandler(rec, req)
		assert.Equal(t, http.StatusOK, rec.Code)
	})

	t.Run("valid timezone param", func(t *testing.T) {
		req := reqWithParams(http.MethodGet, "/api/v1/datetime/now/x", map[string]string{"timezone": "UTC"}, nil)
		rec := httptest.NewRecorder()
		apiDateTimeNowHandler(rec, req)
		assert.Equal(t, http.StatusOK, rec.Code)
	})

	t.Run("invalid timezone", func(t *testing.T) {
		req := reqWithParams(http.MethodGet, "/api/v1/datetime/now/x", map[string]string{"timezone": "Not/A_Real_Zone"}, nil)
		rec := httptest.NewRecorder()
		apiDateTimeNowHandler(rec, req)
		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})
}

// TestApiTimestampHandler covers all three unix representations are
// present and internally consistent (ms derived from the same instant as
// the seconds value, within a generous tolerance).
func TestApiTimestampHandler(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/datetime/timestamp", nil)
	rec := httptest.NewRecorder()
	apiTimestampHandler(rec, req)

	got := decodeJSON(t, rec)
	unix, _ := got["unix"].(float64)
	unixMS, _ := got["unix_ms"].(float64)
	assert.Greater(t, unix, float64(0))
	assert.InDelta(t, unix*1000, unixMS, 2000)
}

// TestApiConvertTimestampHandler covers a valid timestamp and an invalid
// (non-numeric) timestamp.
func TestApiConvertTimestampHandler(t *testing.T) {
	t.Run("valid timestamp", func(t *testing.T) {
		req := reqWithParams(http.MethodGet, "/api/v1/datetime/convert/x", map[string]string{"timestamp": "1700000000"}, nil)
		rec := httptest.NewRecorder()
		apiConvertTimestampHandler(rec, req)
		assert.Equal(t, http.StatusOK, rec.Code)
	})

	t.Run("invalid timestamp", func(t *testing.T) {
		req := reqWithParams(http.MethodGet, "/api/v1/datetime/convert/x", map[string]string{"timestamp": "not-a-number"}, nil)
		rec := httptest.NewRecorder()
		apiConvertTimestampHandler(rec, req)
		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})
}

// TestApiToUnixHandler covers a well-formed datetime string and the
// invalid-input error path.
func TestApiToUnixHandler(t *testing.T) {
	t.Run("valid RFC3339", func(t *testing.T) {
		req := reqWithParams(http.MethodGet, "/api/v1/datetime/to-unix/x", map[string]string{"datetime": "2024-01-01T00:00:00Z"}, nil)
		rec := httptest.NewRecorder()
		apiToUnixHandler(rec, req)
		assert.Equal(t, http.StatusOK, rec.Code)
	})

	t.Run("invalid datetime", func(t *testing.T) {
		req := reqWithParams(http.MethodGet, "/api/v1/datetime/to-unix/x", map[string]string{"datetime": "not-a-date"}, nil)
		rec := httptest.NewRecorder()
		apiToUnixHandler(rec, req)
		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})
}

// TestApiAddDurationHandler covers a valid timestamp+duration pair and
// the invalid-timestamp error path.
func TestApiAddDurationHandler(t *testing.T) {
	t.Run("valid", func(t *testing.T) {
		req := reqWithParams(http.MethodGet, "/api/v1/datetime/add/x/y", map[string]string{
			"timestamp": "1700000000",
			"duration":  "1h",
		}, nil)
		rec := httptest.NewRecorder()
		apiAddDurationHandler(rec, req)
		assert.Equal(t, http.StatusOK, rec.Code)
	})

	t.Run("invalid timestamp", func(t *testing.T) {
		req := reqWithParams(http.MethodGet, "/api/v1/datetime/add/x/y", map[string]string{
			"timestamp": "nope",
			"duration":  "1h",
		}, nil)
		rec := httptest.NewRecorder()
		apiAddDurationHandler(rec, req)
		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})
}

// TestApiDiffHandler covers both timestamps valid, and each one being
// individually invalid (two distinct error branches in the handler).
func TestApiDiffHandler(t *testing.T) {
	t.Run("both valid", func(t *testing.T) {
		req := reqWithParams(http.MethodGet, "/api/v1/datetime/diff/x/y", map[string]string{
			"timestamp1": "1700000000",
			"timestamp2": "1700003600",
		}, nil)
		rec := httptest.NewRecorder()
		apiDiffHandler(rec, req)
		assert.Equal(t, http.StatusOK, rec.Code)
	})

	t.Run("invalid timestamp1", func(t *testing.T) {
		req := reqWithParams(http.MethodGet, "/api/v1/datetime/diff/x/y", map[string]string{
			"timestamp1": "nope",
			"timestamp2": "1700003600",
		}, nil)
		rec := httptest.NewRecorder()
		apiDiffHandler(rec, req)
		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})

	t.Run("invalid timestamp2", func(t *testing.T) {
		req := reqWithParams(http.MethodGet, "/api/v1/datetime/diff/x/y", map[string]string{
			"timestamp1": "1700000000",
			"timestamp2": "nope",
		}, nil)
		rec := httptest.NewRecorder()
		apiDiffHandler(rec, req)
		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})
}

// TestApiTimezonesHandler covers the list is non-empty.
func TestApiTimezonesHandler(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/datetime/timezones", nil)
	rec := httptest.NewRecorder()
	apiTimezonesHandler(rec, req)

	got := decodeJSON(t, rec)
	zones, ok := got["timezones"].([]interface{})
	require.True(t, ok)
	assert.NotEmpty(t, zones)
}

// TestApiTimezoneInfoHandler covers a known timezone and an invalid one.
func TestApiTimezoneInfoHandler(t *testing.T) {
	t.Run("valid", func(t *testing.T) {
		req := reqWithParams(http.MethodGet, "/api/v1/datetime/timezone/x", map[string]string{"timezone": "UTC"}, nil)
		rec := httptest.NewRecorder()
		apiTimezoneInfoHandler(rec, req)
		assert.Equal(t, http.StatusOK, rec.Code)
	})

	t.Run("invalid", func(t *testing.T) {
		req := reqWithParams(http.MethodGet, "/api/v1/datetime/timezone/x", map[string]string{"timezone": "Not/Real"}, nil)
		rec := httptest.NewRecorder()
		apiTimezoneInfoHandler(rec, req)
		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})
}

// TestApiConvertTimezoneHandler covers a valid conversion and the
// invalid-timestamp error path.
func TestApiConvertTimezoneHandler(t *testing.T) {
	t.Run("valid", func(t *testing.T) {
		req := reqWithParams(http.MethodGet, "/api/v1/datetime/timezone/convert/x/y/z", map[string]string{
			"timestamp": "1700000000",
			"from":      "UTC",
			"to":        "America/New_York",
		}, nil)
		rec := httptest.NewRecorder()
		apiConvertTimezoneHandler(rec, req)
		assert.Equal(t, http.StatusOK, rec.Code)
	})

	t.Run("invalid timestamp", func(t *testing.T) {
		req := reqWithParams(http.MethodGet, "/api/v1/datetime/timezone/convert/x/y/z", map[string]string{
			"timestamp": "nope",
			"from":      "UTC",
			"to":        "America/New_York",
		}, nil)
		rec := httptest.NewRecorder()
		apiConvertTimezoneHandler(rec, req)
		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})
}

// TestApiPlaceholderHandler covers the not-yet-implemented stub used for
// every unbuilt endpoint category — must always be 501 with a
// success:false JSON body.
func TestApiPlaceholderHandler(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/docker/version", nil)
	rec := httptest.NewRecorder()
	apiPlaceholderHandler(rec, req)

	assert.Equal(t, http.StatusNotImplemented, rec.Code)
	assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))
	assert.Contains(t, rec.Body.String(), `"success":false`)
}

// TestRobotsHandler covers that configured Allow/Deny paths are emitted
// and the sitemap line is present.
func TestRobotsHandler(t *testing.T) {
	cfg := &config.Config{}
	cfg.Server.FQDN = "example.com"
	cfg.Server.Port = "8080"
	cfg.Web.Robots.Allow = []string{"/"}
	cfg.Web.Robots.Deny = []string{"/private"}

	req := httptest.NewRequest(http.MethodGet, "/robots.txt", nil)
	rec := httptest.NewRecorder()
	robotsHandler(cfg)(rec, req)

	body := rec.Body.String()
	assert.Contains(t, body, "Allow: /")
	assert.Contains(t, body, "Disallow: /private")
	assert.Contains(t, body, "Sitemap: http://example.com:8080/sitemap.xml")
}

// TestOpenapiYAMLHandler_RedirectsToJSON covers the documented PART 20
// behavior: no YAML spec, always redirect to /openapi.json.
func TestOpenapiYAMLHandler_RedirectsToJSON(t *testing.T) {
	cfg := &config.Config{}
	req := httptest.NewRequest(http.MethodGet, "/openapi.yaml", nil)
	rec := httptest.NewRecorder()
	openapiYAMLHandler(cfg)(rec, req)

	assert.Equal(t, http.StatusFound, rec.Code)
	assert.Equal(t, "/openapi.json", rec.Header().Get("Location"))
}
