package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// decodeEnvelope unmarshals a PART 14 response envelope body into a
// generic map for field-level assertions.
func decodeEnvelope(t *testing.T, body []byte) map[string]interface{} {
	t.Helper()
	var out map[string]interface{}
	require.NoError(t, json.Unmarshal(body, &out))
	return out
}

// apiNetworkCallerHandler must always return 200 with an ok:true envelope
// carrying the caller's resolved IP.
func TestAPINetworkCallerHandler(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/network/caller", nil)
	req.RemoteAddr = "203.0.113.9:1234"
	w := httptest.NewRecorder()

	apiNetworkCallerHandler(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	env := decodeEnvelope(t, w.Body.Bytes())
	assert.Equal(t, true, env["ok"])
	data, ok := env["data"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "203.0.113.9", data["ip"])
}

// apiNetworkUserAgentHandler must prefer the explicit ?ua= override over
// the request's own User-Agent header.
func TestAPINetworkUserAgentHandler(t *testing.T) {
	t.Run("uses header when no override", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/network/useragent", nil)
		req.Header.Set("User-Agent", "curl/8.0")
		w := httptest.NewRecorder()

		apiNetworkUserAgentHandler(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		assert.Equal(t, true, env["ok"])
	})

	t.Run("prefers explicit ua override", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/network/useragent?ua=Mozilla%2F5.0", nil)
		req.Header.Set("User-Agent", "curl/8.0")
		w := httptest.NewRecorder()

		apiNetworkUserAgentHandler(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
	})
}

// apiNetworkMACVendorHandler must return 400 INVALID_MAC for a malformed
// MAC and 200 with a vendor field for a well-formed one.
func TestAPINetworkMACVendorHandler(t *testing.T) {
	t.Run("invalid mac", func(t *testing.T) {
		r := chi.NewRouter()
		r.Get("/mac/{mac}", apiNetworkMACVendorHandler)

		req := httptest.NewRequest(http.MethodGet, "/mac/not-a-mac", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		assert.Equal(t, false, env["ok"])
		assert.Equal(t, "INVALID_MAC", env["error"])
	})

	t.Run("valid mac", func(t *testing.T) {
		r := chi.NewRouter()
		r.Get("/mac/{mac}", apiNetworkMACVendorHandler)

		req := httptest.NewRequest(http.MethodGet, "/mac/00:00:00:11:22:33", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		assert.Equal(t, true, env["ok"])
		data, ok := env["data"].(map[string]interface{})
		require.True(t, ok)
		assert.Equal(t, "00:00:00:11:22:33", data["mac"])
	})
}

// apiNetworkSubnetHandler must 400 with MISSING_CIDR when ?cidr= is
// absent, 400 with INVALID_CIDR for a malformed value, and 200 for a
// valid CIDR.
func TestAPINetworkSubnetHandler(t *testing.T) {
	t.Run("missing cidr", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/network/subnet", nil)
		w := httptest.NewRecorder()

		apiNetworkSubnetHandler(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		assert.Equal(t, "MISSING_CIDR", env["error"])
	})

	t.Run("invalid cidr", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/network/subnet?cidr=not-a-cidr", nil)
		w := httptest.NewRecorder()

		apiNetworkSubnetHandler(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		assert.Equal(t, "INVALID_CIDR", env["error"])
	})

	t.Run("valid cidr", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/network/subnet?cidr=192.168.1.0/24", nil)
		w := httptest.NewRecorder()

		apiNetworkSubnetHandler(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		assert.Equal(t, true, env["ok"])
	})
}

// apiNetworkULAHandler must return 200 with a "ula" field on success.
func TestAPINetworkULAHandler(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/network/ula", nil)
	w := httptest.NewRecorder()

	apiNetworkULAHandler(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	env := decodeEnvelope(t, w.Body.Bytes())
	assert.Equal(t, true, env["ok"])
	data, ok := env["data"].(map[string]interface{})
	require.True(t, ok)
	assert.NotEmpty(t, data["ula"])
}

// apiNetworkPortHandler must return 200 with a numeric "port" field.
func TestAPINetworkPortHandler(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/network/port", nil)
	w := httptest.NewRecorder()

	apiNetworkPortHandler(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	env := decodeEnvelope(t, w.Body.Bytes())
	assert.Equal(t, true, env["ok"])
	data, ok := env["data"].(map[string]interface{})
	require.True(t, ok)
	assert.NotNil(t, data["port"])
}

// apiNetworkPingHandler must 400 with MISSING_HOST when ?host= is absent
// and 400 with INVALID_COUNT for a non-positive ?count=. The live-network
// success path is not exercised here to avoid CI flakiness (see
// TestAPIOsintDomainHandler for the same pattern).
func TestAPINetworkPingHandler(t *testing.T) {
	t.Run("missing host", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/network/ping", nil)
		w := httptest.NewRecorder()

		apiNetworkPingHandler(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		assert.Equal(t, "MISSING_HOST", env["error"])
	})

	t.Run("invalid count", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/network/ping?host=example.com&count=0", nil)
		w := httptest.NewRecorder()

		apiNetworkPingHandler(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		assert.Equal(t, "INVALID_COUNT", env["error"])
	})
}

// apiNetworkSSLHandler must 400 with MISSING_HOST when ?host= is absent.
// The live-network success path is not exercised here to avoid CI flakiness.
func TestAPINetworkSSLHandler(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/network/ssl", nil)
	w := httptest.NewRecorder()

	apiNetworkSSLHandler(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	env := decodeEnvelope(t, w.Body.Bytes())
	assert.Equal(t, "MISSING_HOST", env["error"])
}

// apiNetworkURLHandler must 400 with MISSING_URL when ?url= is absent, 400
// with INVALID_URL for a URL missing a scheme/host, and 200 with the parsed
// components for a well-formed URL. url.Parse has no network dependency, so
// the success path is safe to test deterministically.
func TestAPINetworkURLHandler(t *testing.T) {
	t.Run("missing url", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/network/url", nil)
		w := httptest.NewRecorder()

		apiNetworkURLHandler(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		assert.Equal(t, "MISSING_URL", env["error"])
	})

	t.Run("invalid url", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/network/url?url=%2Fjust%2Fa%2Fpath", nil)
		w := httptest.NewRecorder()

		apiNetworkURLHandler(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		assert.Equal(t, "INVALID_URL", env["error"])
	})

	t.Run("valid url", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/network/url?url=https%3A%2F%2Fuser%40example.com%3A8443%2Fpath%3Fa%3D1%23frag", nil)
		w := httptest.NewRecorder()

		apiNetworkURLHandler(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		env := decodeEnvelope(t, w.Body.Bytes())
		assert.Equal(t, true, env["ok"])
		data, ok := env["data"].(map[string]interface{})
		require.True(t, ok)
		assert.Equal(t, "https", data["scheme"])
		assert.Equal(t, "example.com", data["hostname"])
		assert.Equal(t, "8443", data["port"])
		assert.Equal(t, "/path", data["path"])
		assert.Equal(t, "frag", data["fragment"])
	})
}

// apiNetworkWhoisHandler must 400 with MISSING_DOMAIN when ?domain= is
// absent. The live-network success path is not exercised here to avoid CI
// flakiness.
func TestAPINetworkWhoisHandler(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/network/whois", nil)
	w := httptest.NewRecorder()

	apiNetworkWhoisHandler(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	env := decodeEnvelope(t, w.Body.Bytes())
	assert.Equal(t, "MISSING_DOMAIN", env["error"])
}

// apiNetworkTracerouteHandler honestly reports 501 NOT_SUPPORTED, matching
// the same pattern as apiGenerateQRHandler/apiLanguageDetectHandler.
func TestAPINetworkTracerouteHandler(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/network/traceroute", nil)
	w := httptest.NewRecorder()

	apiNetworkTracerouteHandler(w, req)

	assert.Equal(t, http.StatusNotImplemented, w.Code)
	env := decodeEnvelope(t, w.Body.Bytes())
	assert.Equal(t, false, env["ok"])
	assert.Equal(t, "NOT_SUPPORTED", env["error"])
}

// writeEnvelopeOK/writeEnvelopeError must emit the PART 14 envelope shape
// exactly, and writeEnvelopeError must only include "details" when non-nil.
func TestWriteEnvelopeHelpers(t *testing.T) {
	t.Run("ok envelope", func(t *testing.T) {
		w := httptest.NewRecorder()
		writeEnvelopeOK(w, http.StatusCreated, map[string]string{"a": "b"})

		assert.Equal(t, http.StatusCreated, w.Code)
		assert.Equal(t, "application/json", w.Header().Get("Content-Type"))
		env := decodeEnvelope(t, w.Body.Bytes())
		assert.Equal(t, true, env["ok"])
		assert.Equal(t, map[string]interface{}{"a": "b"}, env["data"])
	})

	t.Run("error envelope without details", func(t *testing.T) {
		w := httptest.NewRecorder()
		writeEnvelopeError(w, http.StatusBadRequest, "BAD", "bad request", nil)

		env := decodeEnvelope(t, w.Body.Bytes())
		assert.Equal(t, false, env["ok"])
		assert.Equal(t, "BAD", env["error"])
		assert.Equal(t, "bad request", env["message"])
		_, hasDetails := env["details"]
		assert.False(t, hasDetails)
	})

	t.Run("error envelope with details", func(t *testing.T) {
		w := httptest.NewRecorder()
		writeEnvelopeError(w, http.StatusBadRequest, "BAD", "bad request", map[string]interface{}{"field": "x"})

		env := decodeEnvelope(t, w.Body.Bytes())
		details, ok := env["details"].(map[string]interface{})
		require.True(t, ok)
		assert.Equal(t, "x", details["field"])
	})

	t.Run("body ends with trailing newline", func(t *testing.T) {
		w := httptest.NewRecorder()
		writeEnvelopeOK(w, http.StatusOK, map[string]string{})
		assert.Equal(t, byte('\n'), w.Body.Bytes()[len(w.Body.Bytes())-1])
	})
}

// writeJSONEnvelope must fall back to a hardcoded INTERNAL error body when
// the value passed cannot be marshaled to JSON (e.g. a channel).
func TestWriteJSONEnvelopeMarshalFailure(t *testing.T) {
	w := httptest.NewRecorder()
	writeJSONEnvelope(w, http.StatusOK, map[string]interface{}{"bad": make(chan int)})

	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.Contains(t, w.Body.String(), `"error":"INTERNAL"`)
}
